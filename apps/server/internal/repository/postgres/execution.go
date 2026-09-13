package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/zhanshimian/server/internal/domain"
)

// selectionRow 是 plan_selections 的完整行,包含 domain.PlanSelection 投影
// 刻意省略的内部字段(user_id、idempotency_key、request_hash)。
type selectionRow struct {
	ID                  string
	UserID              string
	PlanSetID           string
	PlanVariantID       string
	RenderPublicationID *string
	IdempotencyKey      string
	RequestHash         string
	CreatedAt           time.Time
}

func (r selectionRow) public() domain.PlanSelection {
	return domain.PlanSelection{
		ID:                  r.ID,
		PlanSetID:           r.PlanSetID,
		PlanVariantID:       r.PlanVariantID,
		RenderPublicationID: r.RenderPublicationID,
		CreatedAt:           r.CreatedAt,
	}
}

const selectionSelect = `
	SELECT id::text, user_id::text, plan_set_id::text, plan_variant_id::text,
	       render_publication_id::text, idempotency_key, request_hash, created_at
	FROM plan_selections`

const selectionReturning = `
	RETURNING id::text, user_id::text, plan_set_id::text, plan_variant_id::text,
	          render_publication_id::text, idempotency_key, request_hash, created_at`

func scanSelectionRow(row rowScanner) (selectionRow, error) {
	var r selectionRow
	err := row.Scan(
		&r.ID, &r.UserID, &r.PlanSetID, &r.PlanVariantID,
		&r.RenderPublicationID, &r.IdempotencyKey, &r.RequestHash, &r.CreatedAt,
	)
	return r, err
}

// CreateSelection 在单个事务里锁定 PlanSet、校验 variant/publication 归属,
// 再做幂等与自然去重,最后插入。捕获唯一冲突后重新读取,绝不把并发重放变成 500。
func (s *Store) CreateSelection(ctx context.Context, command domain.CreateSelectionCommand) (domain.PlanSelection, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.PlanSelection{}, false, err
	}
	defer tx.Rollback(ctx)

	if err := lockPlanSet(ctx, tx, command.UserID, command.PlanSetID); err != nil {
		return domain.PlanSelection{}, false, err
	}
	if err := verifySelectionVariant(ctx, tx, command.UserID, command.PlanSetID, command.PlanVariantID); err != nil {
		return domain.PlanSelection{}, false, err
	}
	if err := verifySelectionPublication(ctx, tx, command.UserID, command.PlanVariantID, command.RenderPublicationID); err != nil {
		return domain.PlanSelection{}, false, err
	}

	if existing, found, err := findSelectionByKey(ctx, tx, command.UserID, command.IdempotencyKey); err != nil {
		return domain.PlanSelection{}, false, err
	} else if found {
		if existing.RequestHash != command.RequestHash {
			return domain.PlanSelection{}, false, domain.ErrIdempotencyConflict
		}
		return existing.public(), false, nil
	}

	if existing, found, err := findSelectionByNatural(ctx, tx, command.UserID, command.PlanSetID, command.PlanVariantID, command.RenderPublicationID); err != nil {
		return domain.PlanSelection{}, false, err
	} else if found {
		if err := recordSelectionKey(ctx, tx, command.UserID, command.IdempotencyKey, command.RequestHash, existing.ID); err != nil {
			return domain.PlanSelection{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.PlanSelection{}, false, err
		}
		return existing.public(), false, nil
	}

	row, err := insertSelection(ctx, tx, command)
	if err != nil {
		if isUniqueViolation(err) {
			if existing, found, lookupErr := findSelectionByKey(ctx, tx, command.UserID, command.IdempotencyKey); lookupErr != nil {
				return domain.PlanSelection{}, false, lookupErr
			} else if found {
				if existing.RequestHash != command.RequestHash {
					return domain.PlanSelection{}, false, domain.ErrIdempotencyConflict
				}
				return existing.public(), false, nil
			}
		}
		return domain.PlanSelection{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.PlanSelection{}, false, err
	}
	return row.public(), true, nil
}

// GetSelection 按租户读取一次选择;跨用户一律读作不存在。
func (s *Store) GetSelection(ctx context.Context, userID, selectionID string) (domain.PlanSelection, error) {
	row, err := scanSelectionRow(s.pool.QueryRow(ctx, selectionSelect+` WHERE user_id=$1::uuid AND id=$2::uuid`, userID, selectionID))
	if err != nil {
		return domain.PlanSelection{}, mapNotFound(err)
	}
	return row.public(), nil
}

// lockPlanSet 串行化同一 PlanSet 下的选择创建,使幂等与自然去重检查无竞态。
func lockPlanSet(ctx context.Context, tx pgx.Tx, userID, planSetID string) error {
	var id string
	err := tx.QueryRow(ctx, `SELECT id::text FROM plan_sets WHERE user_id=$1::uuid AND id=$2::uuid FOR UPDATE`, userID, planSetID).Scan(&id)
	return mapNotFound(err)
}

// verifySelectionVariant 确认 variant 属于该用户的该 PlanSet。
func verifySelectionVariant(ctx context.Context, tx pgx.Tx, userID, planSetID, planVariantID string) error {
	var one int
	err := tx.QueryRow(ctx, `SELECT 1 FROM plan_variants WHERE user_id=$1::uuid AND id=$2::uuid AND plan_set_id=$3::uuid`,
		userID, planVariantID, planSetID).Scan(&one)
	return mapNotFound(err)
}

// verifySelectionPublication 确认非空 publication 是该 variant 的当前发布;
// 不属于该 variant、未发布或跨用户,三者都因不命中 render_heads 而读作不存在。
func verifySelectionPublication(ctx context.Context, tx pgx.Tx, userID, planVariantID string, publicationID *string) error {
	if publicationID == nil {
		return nil
	}
	var one int
	err := tx.QueryRow(ctx, `SELECT 1 FROM render_heads WHERE user_id=$1::uuid AND plan_variant_id=$2::uuid AND current_publication_id=$3::uuid`,
		userID, planVariantID, *publicationID).Scan(&one)
	return mapNotFound(err)
}

func findSelectionByKey(ctx context.Context, tx pgx.Tx, userID, key string) (selectionRow, bool, error) {
	row, err := scanSelectionRow(tx.QueryRow(ctx, selectionSelect+` WHERE user_id=$1::uuid AND idempotency_key=$2`, userID, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return selectionRow{}, false, nil
	}
	if err != nil {
		return selectionRow{}, false, err
	}
	return row, true, nil
}

func findSelectionByNatural(ctx context.Context, tx pgx.Tx, userID, planSetID, planVariantID string, publicationID *string) (selectionRow, bool, error) {
	publication := any(nil)
	if publicationID != nil {
		publication = *publicationID
	}
	row, err := scanSelectionRow(tx.QueryRow(ctx, selectionSelect+`
		WHERE user_id=$1::uuid AND plan_set_id=$2::uuid AND plan_variant_id=$3::uuid
		  AND coalesce(render_publication_id, '00000000-0000-0000-0000-000000000000'::uuid)
		    = coalesce($4::uuid, '00000000-0000-0000-0000-000000000000'::uuid)`,
		userID, planSetID, planVariantID, publication))
	if errors.Is(err, pgx.ErrNoRows) {
		return selectionRow{}, false, nil
	}
	if err != nil {
		return selectionRow{}, false, err
	}
	return row, true, nil
}

func insertSelection(ctx context.Context, tx pgx.Tx, command domain.CreateSelectionCommand) (selectionRow, error) {
	publication := any(nil)
	if command.RenderPublicationID != nil {
		publication = *command.RenderPublicationID
	}
	return scanSelectionRow(tx.QueryRow(ctx, `
		INSERT INTO plan_selections(user_id, plan_set_id, plan_variant_id, render_publication_id, idempotency_key, request_hash)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5, $6)
		`+selectionReturning,
		command.UserID, command.PlanSetID, command.PlanVariantID, publication,
		command.IdempotencyKey, command.RequestHash))
}

// recordSelectionKey 为同自然选择下的新 key 记录一个指向既有 Selection 的
// Foundation 幂等指针,不覆盖旧事实。
func recordSelectionKey(ctx context.Context, tx pgx.Tx, userID, key, requestHash, selectionID string) error {
	body, err := json.Marshal(map[string]string{"selection_id": selectionID})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO idempotency_keys(user_id, key, request_fingerprint, status, scope, resource_id, expires_at, response_status, response_body)
		VALUES ($1::uuid, $2, $3, 'completed', 'selection', $4::uuid, now() + interval '30 days', 200, $5::jsonb)
		ON CONFLICT (user_id, key) DO NOTHING`,
		userID, key, requestHash, selectionID, body)
	return err
}

// isUniqueViolation 报告 Postgres 23505 唯一约束冲突。
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
