package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

// ---- DecisionStore ----

// UpsertVariantDecision 写入（或覆盖）一条卡堆决策。归属不走运行时校验：
// (user_id, plan_variant_id) 复合外键保证决策行只能指向本人名下的 variant，
// variant 不存在/不属于该用户时外键前置查询读作不存在；并发下 variant 被注销
// 清除级联删除时 23503 同样折算为 ErrNotFound。
func (s *Store) UpsertVariantDecision(ctx context.Context, userID string, command domain.UpsertVariantDecisionCommand) (domain.PlanVariantDecision, error) {
	var out domain.PlanVariantDecision
	err := s.pool.QueryRow(ctx, `
		WITH variant AS (
			SELECT id, plan_set_id FROM plan_variants
			WHERE user_id=$1::uuid AND id=$2::uuid
		)
		INSERT INTO plan_variant_decisions(user_id, plan_set_id, plan_variant_id, decision)
		SELECT $1::uuid, variant.plan_set_id, variant.id, $3
		FROM variant
		ON CONFLICT (user_id, plan_variant_id) DO UPDATE
		SET decision = EXCLUDED.decision, updated_at = now()
		RETURNING plan_variant_id::text, plan_set_id::text, decision, created_at, updated_at`,
		userID, command.PlanVariantID, string(command.Decision)).Scan(
		&out.PlanVariantID, &out.PlanSetID, &out.Decision, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PlanVariantDecision{}, repository.ErrNotFound
	}
	if isForeignKeyViolation(err, "plan_variant_decisions_user_id_plan_variant_id_fkey") ||
		isForeignKeyViolation(err, "plan_variant_decisions_user_id_plan_set_id_fkey") {
		return domain.PlanVariantDecision{}, repository.ErrNotFound
	}
	return out, err
}

// DeleteVariantDecision 清除一条决策（撤销）。行不存在同样是成功——
// 撤销的语义是「回到未决」，不依赖之前真的决策过。
func (s *Store) DeleteVariantDecision(ctx context.Context, userID, planVariantID string) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM plan_variant_decisions WHERE user_id=$1::uuid AND plan_variant_id=$2::uuid`,
		userID, planVariantID)
	return err
}

// ListDecisionsByVariantIDs 批量读取一批 variant 的当前决策（方案集读模型
// 回填用）：跨方案集一次查询，不逐套 N+1。
func (s *Store) ListDecisionsByVariantIDs(ctx context.Context, userID string, planVariantIDs []string) (map[string]domain.PlanVariantDecision, error) {
	out := make(map[string]domain.PlanVariantDecision, len(planVariantIDs))
	if len(planVariantIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT plan_variant_id::text, plan_set_id::text, decision, created_at, updated_at
		FROM plan_variant_decisions
		WHERE user_id=$1::uuid AND plan_variant_id = ANY($2::uuid[])`,
		userID, planVariantIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var decision domain.PlanVariantDecision
		if err := rows.Scan(&decision.PlanVariantID, &decision.PlanSetID, &decision.Decision,
			&decision.CreatedAt, &decision.UpdatedAt); err != nil {
			return nil, err
		}
		out[decision.PlanVariantID] = decision
	}
	return out, rows.Err()
}

// ListRecentDecisions 按新旧读最近的决策（规划指纹与 decision_memory 快照
// 的唯一来源）。JOIN plan_variants 带出 key/name，模型才能理解跳过的是哪类方向。
func (s *Store) ListRecentDecisions(ctx context.Context, userID string, limit int) ([]domain.VariantDecisionItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT d.plan_variant_id::text, v.key::text, v.name, d.decision, d.created_at
		FROM plan_variant_decisions d
		JOIN plan_variants v ON v.user_id = d.user_id AND v.id = d.plan_variant_id
		WHERE d.user_id=$1::uuid
		ORDER BY d.created_at DESC, d.id DESC
		LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.VariantDecisionItem, 0, limit)
	for rows.Next() {
		var item domain.VariantDecisionItem
		if err := rows.Scan(&item.VariantID, &item.Key, &item.Name, &item.Decision, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
