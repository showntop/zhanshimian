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

// executionQuerier 是 loadExecution 复用事务与连接池的最小查询端口。
type executionQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// executionRow 是 executions 的完整行,包含 domain.Execution 投影刻意省略的
// 内部字段(user_id、idempotency_key、request_hash、abandoned_at)。
type executionRow struct {
	ID             string
	UserID         string
	SelectionID    string
	State          string
	Version        int
	IdempotencyKey string
	RequestHash    string
	StartedAt      *time.Time
	CompletedAt    *time.Time
	AbandonedAt    *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (r executionRow) public() domain.Execution {
	return domain.Execution{
		ID:          r.ID,
		SelectionID: r.SelectionID,
		State:       domain.ExecutionState(r.State),
		Version:     r.Version,
		StartedAt:   r.StartedAt,
		CompletedAt: r.CompletedAt,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

// executionStepRow 是 execution_steps 的行,completed/completed_at 由事件投影
// 计算,不写入快照表。
type executionStepRow struct {
	ID               string
	UserID           string
	ExecutionID      string
	SourcePlanStepID string
	Category         string
	Action           string
	Title            string
	Summary          string
	Details          json.RawMessage
	Position         int
	Completed        bool
	CompletedAt      *time.Time
}

func (r executionStepRow) public() domain.ExecutionStep {
	return domain.ExecutionStep{
		ID:               r.ID,
		SourcePlanStepID: r.SourcePlanStepID,
		Category:         r.Category,
		Action:           r.Action,
		Title:            r.Title,
		Summary:          r.Summary,
		Details:          r.Details,
		Position:         r.Position,
		Completed:        r.Completed,
		CompletedAt:      r.CompletedAt,
	}
}

const executionSelect = `
	SELECT id::text, user_id::text, selection_id::text, state, version, idempotency_key, request_hash,
	       started_at, completed_at, abandoned_at, created_at, updated_at
	FROM executions`

const executionReturning = `
	RETURNING id::text, user_id::text, selection_id::text, state, version, idempotency_key, request_hash,
	          started_at, completed_at, abandoned_at, created_at, updated_at`

// executionStepSelect 用窗口子查询取每个 step 最后一条 step_completed/step_reopened
// 事件,投影出 API 的 completed/completed_at;没有事件时一律 false/nil。
const executionStepSelect = `
	SELECT es.id::text, es.user_id::text, es.execution_id::text, es.source_plan_step_id::text,
	       es.category, es.action, es.title, es.summary, es.details, es.position,
	       COALESCE(ev.event_type = 'step_completed', false) AS completed,
	       CASE WHEN ev.event_type = 'step_completed' THEN ev.occurred_at END AS completed_at
	FROM execution_steps es
	LEFT JOIN LATERAL (
		SELECT e.event_type, e.occurred_at
		FROM execution_events e
		WHERE e.user_id = es.user_id
		  AND e.execution_id = es.execution_id
		  AND e.execution_step_id = es.id
		  AND e.event_type IN ('step_completed','step_reopened')
		ORDER BY e.created_at DESC, e.id DESC
		LIMIT 1
	) ev ON true
	WHERE es.user_id = $1::uuid AND es.execution_id = $2::uuid
	ORDER BY es.position`

func scanExecutionRow(row rowScanner) (executionRow, error) {
	var r executionRow
	err := row.Scan(
		&r.ID, &r.UserID, &r.SelectionID, &r.State, &r.Version, &r.IdempotencyKey, &r.RequestHash,
		&r.StartedAt, &r.CompletedAt, &r.AbandonedAt, &r.CreatedAt, &r.UpdatedAt,
	)
	return r, err
}

func scanExecutionStepRow(row rowScanner) (executionStepRow, error) {
	var r executionStepRow
	var details []byte
	err := row.Scan(
		&r.ID, &r.UserID, &r.ExecutionID, &r.SourcePlanStepID,
		&r.Category, &r.Action, &r.Title, &r.Summary, &details, &r.Position,
		&r.Completed, &r.CompletedAt,
	)
	if err != nil {
		return r, err
	}
	r.Details = json.RawMessage(details)
	return r, nil
}

// CreateExecutionFromSelection 在单个事务里锁定 Selection、按 key/selection 去重,
// 再插入 execution 并复制该 PlanVariant 的全部 plan_steps;快照必须是恰好
// hair/makeup/outfit 且 position 唯一,否则回滚并返回 ErrInvalidSnapshot。
func (s *Store) CreateExecutionFromSelection(ctx context.Context, command domain.CreateExecutionCommand) (domain.Execution, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Execution{}, false, err
	}
	defer tx.Rollback(ctx)

	variantID, err := lockSelection(ctx, tx, command.UserID, command.SelectionID)
	if err != nil {
		return domain.Execution{}, false, err
	}

	if existing, found, err := findExecutionByKey(ctx, tx, command.UserID, command.IdempotencyKey); err != nil {
		return domain.Execution{}, false, err
	} else if found {
		if existing.RequestHash != command.RequestHash {
			return domain.Execution{}, false, domain.ErrIdempotencyConflict
		}
		execution, err := loadExecution(ctx, tx, command.UserID, existing.ID)
		if err != nil {
			return domain.Execution{}, false, err
		}
		return execution, false, nil
	}

	if existing, found, err := findExecutionBySelection(ctx, tx, command.UserID, command.SelectionID); err != nil {
		return domain.Execution{}, false, err
	} else if found {
		if err := recordExecutionKey(ctx, tx, command.UserID, command.IdempotencyKey, command.RequestHash, existing.ID); err != nil {
			return domain.Execution{}, false, err
		}
		execution, err := loadExecution(ctx, tx, command.UserID, existing.ID)
		if err != nil {
			return domain.Execution{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.Execution{}, false, err
		}
		return execution, false, nil
	}

	row, err := insertExecution(ctx, tx, command)
	if err != nil {
		if isUniqueViolation(err) {
			if existing, found, lookupErr := findExecutionByKey(ctx, tx, command.UserID, command.IdempotencyKey); lookupErr != nil {
				return domain.Execution{}, false, lookupErr
			} else if found {
				if existing.RequestHash != command.RequestHash {
					return domain.Execution{}, false, domain.ErrIdempotencyConflict
				}
				execution, err := loadExecution(ctx, tx, command.UserID, existing.ID)
				if err != nil {
					return domain.Execution{}, false, err
				}
				return execution, false, nil
			}
			if existing, found, lookupErr := findExecutionBySelection(ctx, tx, command.UserID, command.SelectionID); lookupErr != nil {
				return domain.Execution{}, false, lookupErr
			} else if found {
				execution, err := loadExecution(ctx, tx, command.UserID, existing.ID)
				if err != nil {
					return domain.Execution{}, false, err
				}
				return execution, false, nil
			}
		}
		return domain.Execution{}, false, err
	}

	if err := insertExecutionSteps(ctx, tx, command.UserID, row.ID, variantID); err != nil {
		return domain.Execution{}, false, err
	}
	if err := validateSnapshot(ctx, tx, command.UserID, row.ID); err != nil {
		return domain.Execution{}, false, err
	}

	execution, err := loadExecution(ctx, tx, command.UserID, row.ID)
	if err != nil {
		return domain.Execution{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Execution{}, false, err
	}
	return execution, true, nil
}

// GetExecution 按租户读取一次执行,跨用户一律读作不存在。
func (s *Store) GetExecution(ctx context.Context, userID, executionID string) (domain.Execution, error) {
	return loadExecution(ctx, s.pool, userID, executionID)
}

func loadExecution(ctx context.Context, q executionQuerier, userID, executionID string) (domain.Execution, error) {
	row, err := scanExecutionRow(q.QueryRow(ctx, executionSelect+` WHERE user_id=$1::uuid AND id=$2::uuid`, userID, executionID))
	if err != nil {
		return domain.Execution{}, mapNotFound(err)
	}
	steps, err := loadExecutionSteps(ctx, q, userID, executionID)
	if err != nil {
		return domain.Execution{}, err
	}
	e := row.public()
	e.Steps = steps
	return e, nil
}

func loadExecutionSteps(ctx context.Context, q executionQuerier, userID, executionID string) ([]domain.ExecutionStep, error) {
	rows, err := q.Query(ctx, executionStepSelect, userID, executionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var steps []domain.ExecutionStep
	for rows.Next() {
		row, err := scanExecutionStepRow(rows)
		if err != nil {
			return nil, err
		}
		steps = append(steps, row.public())
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return steps, nil
}

// lockSelection 串行化同一 Selection 下的执行创建,并返回其 PlanVariant。
func lockSelection(ctx context.Context, tx pgx.Tx, userID, selectionID string) (string, error) {
	var variantID string
	err := tx.QueryRow(ctx, `SELECT plan_variant_id::text FROM plan_selections WHERE user_id=$1::uuid AND id=$2::uuid FOR UPDATE`, userID, selectionID).Scan(&variantID)
	if err != nil {
		return "", mapNotFound(err)
	}
	return variantID, nil
}

func findExecutionByKey(ctx context.Context, tx pgx.Tx, userID, key string) (executionRow, bool, error) {
	row, err := scanExecutionRow(tx.QueryRow(ctx, executionSelect+` WHERE user_id=$1::uuid AND idempotency_key=$2`, userID, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return executionRow{}, false, nil
	}
	if err != nil {
		return executionRow{}, false, err
	}
	return row, true, nil
}

func findExecutionBySelection(ctx context.Context, tx pgx.Tx, userID, selectionID string) (executionRow, bool, error) {
	row, err := scanExecutionRow(tx.QueryRow(ctx, executionSelect+` WHERE user_id=$1::uuid AND selection_id=$2::uuid`, userID, selectionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return executionRow{}, false, nil
	}
	if err != nil {
		return executionRow{}, false, err
	}
	return row, true, nil
}

func insertExecution(ctx context.Context, tx pgx.Tx, command domain.CreateExecutionCommand) (executionRow, error) {
	return scanExecutionRow(tx.QueryRow(ctx, `
		INSERT INTO executions(user_id, selection_id, state, version, idempotency_key, request_hash)
		VALUES ($1::uuid, $2::uuid, 'planned', 1, $3, $4)
		`+executionReturning,
		command.UserID, command.SelectionID, command.IdempotencyKey, command.RequestHash))
}

func insertExecutionSteps(ctx context.Context, tx pgx.Tx, userID, executionID, planVariantID string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO execution_steps(user_id, execution_id, source_plan_step_id, category, action, title, summary, details, position)
		SELECT $1::uuid, $2::uuid, ps.id, ps.category, ps.action, ps.title, ps.summary, ps.details, ps.position
		FROM plan_steps ps
		WHERE ps.user_id = $1::uuid AND ps.plan_variant_id = $3::uuid`,
		userID, executionID, planVariantID)
	return err
}

// validateSnapshot 强制快照恰好包含 hair/makeup/outfit 三个分类且 position 唯一。
func validateSnapshot(ctx context.Context, tx pgx.Tx, userID, executionID string) error {
	var total, distinctPos, hair, makeup, outfit int
	err := tx.QueryRow(ctx, `
		SELECT count(*), count(DISTINCT position),
		       count(*) FILTER (WHERE category='hair'),
		       count(*) FILTER (WHERE category='makeup'),
		       count(*) FILTER (WHERE category='outfit')
		FROM execution_steps WHERE user_id=$1::uuid AND execution_id=$2::uuid`,
		userID, executionID).Scan(&total, &distinctPos, &hair, &makeup, &outfit)
	if err != nil {
		return err
	}
	if total != 3 || distinctPos != 3 || hair != 1 || makeup != 1 || outfit != 1 {
		return domain.ErrInvalidSnapshot
	}
	return nil
}

// recordExecutionKey 为同一 Selection 下新 key 记录指向既有 Execution 的幂等指针。
func recordExecutionKey(ctx context.Context, tx pgx.Tx, userID, key, requestHash, executionID string) error {
	body, err := json.Marshal(map[string]string{"execution_id": executionID})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO idempotency_keys(user_id, key, request_fingerprint, status, scope, resource_id, expires_at, response_status, response_body)
		VALUES ($1::uuid, $2, $3, 'completed', 'execution', $4::uuid, now() + interval '30 days', 200, $5::jsonb)
		ON CONFLICT (user_id, key) DO NOTHING`,
		userID, key, requestHash, executionID, body)
	return err
}
