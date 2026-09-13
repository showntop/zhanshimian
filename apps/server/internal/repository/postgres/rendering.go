package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

// renderCreateTaskType is the only rendering task type.
const renderCreateTaskType = domain.TaskType("render_candidate_generate")

// CreateRun idempotently starts one render generation in a single
// transaction: lock the render head, supersede unfinished predecessors,
// increment the generation, and insert the operation, run and first
// candidate task.
func (s *Store) CreateRun(ctx context.Context, command domain.RenderCreateRunCommand) (domain.RenderCreateRunResult, error) {
	dedupeKey := domain.RenderRunIdempotencyKey(command.UserID, command.PlanVariantID, command.IdempotencyKey)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.RenderCreateRunResult{}, err
	}
	defer tx.Rollback(ctx)

	existing, err := findRenderRunByDedupe(ctx, tx, command.UserID, dedupeKey)
	if err != nil {
		return domain.RenderCreateRunResult{}, err
	}
	if existing.Run.ID != "" {
		return existing, nil
	}

	if _, err = tx.Exec(ctx, `
		INSERT INTO render_heads(user_id, plan_variant_id)
		VALUES ($1::uuid, $2::uuid)
		ON CONFLICT (user_id, plan_variant_id) DO NOTHING`,
		command.UserID, command.PlanVariantID); err != nil {
		return domain.RenderCreateRunResult{}, err
	}
	var generation int
	if err = tx.QueryRow(ctx, `
		SELECT generation FROM render_heads
		WHERE user_id=$1::uuid AND plan_variant_id=$2::uuid
		FOR UPDATE`,
		command.UserID, command.PlanVariantID).Scan(&generation); err != nil {
		return domain.RenderCreateRunResult{}, err
	}
	newGeneration := generation + 1

	// Unfinished predecessors of older generations can never publish after
	// this one; mark them superseded immediately.
	if _, err = tx.Exec(ctx, `
		UPDATE render_runs r SET outcome='superseded', finished_at=now()
		WHERE r.user_id=$1::uuid AND r.plan_variant_id=$2::uuid AND r.outcome IS NULL`,
		command.UserID, command.PlanVariantID); err != nil {
		return domain.RenderCreateRunResult{}, err
	}
	if _, err = tx.Exec(ctx, `
		UPDATE operations o SET status='superseded', updated_at=now(), finished_at=now()
		FROM render_runs r
		WHERE r.user_id=o.user_id AND r.operation_id=o.id
		  AND r.user_id=$1::uuid AND r.plan_variant_id=$2::uuid
		  AND o.status IN ('accepted','running','retrying')`,
		command.UserID, command.PlanVariantID); err != nil {
		return domain.RenderCreateRunResult{}, err
	}

	operationID := uuid.NewString()
	runID := uuid.NewString()
	if _, err = tx.Exec(ctx, `
		INSERT INTO operations(id, user_id, kind, subject_type, subject_id, status)
		VALUES ($1::uuid,$2::uuid,'render','render_run',$3::uuid,'accepted')`,
		operationID, command.UserID, runID); err != nil {
		return domain.RenderCreateRunResult{}, err
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO render_runs(id, user_id, plan_variant_id, render_spec_id, generation, operation_id,
		                        candidate_limit, routing_policy_version, quality_policy_version)
		VALUES ($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5,$6::uuid,1,$7,$8)`,
		runID, command.UserID, command.PlanVariantID, command.RenderSpecID, newGeneration, operationID,
		command.RoutingPolicyVersion, command.QualityPolicyVersion); err != nil {
		return domain.RenderCreateRunResult{}, err
	}
	if _, err = tx.Exec(ctx, `
		UPDATE render_heads SET generation=$3 WHERE user_id=$1::uuid AND plan_variant_id=$2::uuid`,
		command.UserID, command.PlanVariantID, newGeneration); err != nil {
		return domain.RenderCreateRunResult{}, err
	}

	payload, err := json.Marshal(domain.RenderGenerateCandidatePayload{
		RenderRunID: runID, Ordinal: 1,
	})
	if err != nil {
		return domain.RenderCreateRunResult{}, err
	}
	var tag pgconn.CommandTag
	if tag, err = tx.Exec(ctx, `
		INSERT INTO tasks(id, user_id, operation_id, type, subject_type, subject_id, subject_generation,
		                  payload_version, payload, dedupe_key, status, stage_code, max_attempts)
		VALUES (gen_random_uuid(),$1::uuid,$2::uuid,$3,'render_run',$4::uuid,$5,1,$6,$7,'queued','',$8)
		ON CONFLICT (user_id, dedupe_key) DO NOTHING`,
		command.UserID, operationID, string(renderCreateTaskType), runID, newGeneration,
		payload, dedupeKey, renderTaskMaxAttempts); err != nil {
		return domain.RenderCreateRunResult{}, err
	} else if tag.RowsAffected() == 0 {
		// 并发同 key:回滚孤儿行,返回既有的 run/operation。
		tx.Rollback(ctx)
		existing, err := findRenderRunByDedupe(ctx, s.pool, command.UserID, dedupeKey)
		if err != nil {
			return domain.RenderCreateRunResult{}, err
		}
		return existing, nil
	}

	if err = tx.Commit(ctx); err != nil {
		return domain.RenderCreateRunResult{}, err
	}
	run, err := s.loadRenderRun(ctx, s.pool, command.UserID, runID)
	if err != nil {
		return domain.RenderCreateRunResult{}, err
	}
	return domain.RenderCreateRunResult{
		Run: run,
		Operation: domain.OperationRef{
			ID: operationID, Kind: domain.OperationRender, Status: domain.OperationAccepted,
		},
		Created: true,
	}, nil
}

// findRenderRunByDedupe returns the existing run/operation pair for a
// repeated idempotency key.
func findRenderRunByDedupe(ctx context.Context, q planningQuerier, userID, dedupeKey string) (domain.RenderCreateRunResult, error) {
	var runID, operationID string
	err := q.QueryRow(ctx, `
		SELECT t.subject_id::text, t.operation_id::text
		FROM tasks t WHERE t.user_id=$1::uuid AND t.dedupe_key=$2
		LIMIT 1`, userID, dedupeKey).Scan(&runID, &operationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.RenderCreateRunResult{}, nil
	}
	if err != nil {
		return domain.RenderCreateRunResult{}, err
	}
	run, err := loadRenderRunQuerier(ctx, q, userID, runID)
	if err != nil {
		return domain.RenderCreateRunResult{}, err
	}
	var kind, status string
	if err = q.QueryRow(ctx, `SELECT kind, status FROM operations WHERE id=$1::uuid AND user_id=$2::uuid`,
		operationID, userID).Scan(&kind, &status); err != nil {
		return domain.RenderCreateRunResult{}, err
	}
	return domain.RenderCreateRunResult{
		Run: run,
		Operation: domain.OperationRef{
			ID: operationID, Kind: domain.OperationKind(kind), Status: domain.OperationStatus(status),
		},
		Created: false,
	}, nil
}

// GetRun reads the run with its current publication and operation.
func (s *Store) GetRun(ctx context.Context, userID, runID string) (domain.RenderRun, *domain.RenderPublication, domain.Operation, error) {
	run, err := s.loadRenderRun(ctx, s.pool, userID, runID)
	if err != nil {
		return domain.RenderRun{}, nil, domain.Operation{}, err
	}
	operation, err := s.loadRenderOperation(ctx, s.pool, userID, run.OperationID)
	if err != nil {
		return domain.RenderRun{}, nil, domain.Operation{}, err
	}
	publication, err := s.loadCurrentPublication(ctx, s.pool, userID, run.PlanVariantID)
	if err != nil {
		return domain.RenderRun{}, nil, domain.Operation{}, err
	}
	// 只挂本 run 的 publication:旧 generation 的当前指针不属于这个 run。
	if publication != nil && publication.RenderRunID != run.ID {
		publication = nil
	}
	return run, publication, operation, nil
}

// ListCurrentByVariantIDs batch-reads the current run/publication/operation
// per variant, following the render head pointer.
func (s *Store) ListCurrentByVariantIDs(ctx context.Context, userID string, variantIDs []string) (map[string]domain.CurrentRender, error) {
	if len(variantIDs) == 0 {
		return map[string]domain.CurrentRender{}, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT v.id::text, r.id::text, r.render_spec_id::text, r.generation, r.operation_id::text,
		       r.candidate_limit, r.routing_policy_version, r.quality_policy_version,
		       COALESCE(r.outcome, ''), r.created_at, r.finished_at,
		       o.status, o.stage_code, o.retryable, o.public_message
		FROM render_heads h
		JOIN render_runs r ON r.user_id=h.user_id AND r.plan_variant_id=h.plan_variant_id AND r.generation=h.generation
		JOIN operations o ON o.user_id=r.user_id AND o.id=r.operation_id
		JOIN plan_variants v ON v.user_id=r.user_id AND v.id=r.plan_variant_id
		WHERE h.user_id=$1::uuid AND h.plan_variant_id = ANY($2::uuid[])`,
		userID, variantIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]domain.CurrentRender, len(variantIDs))
	for rows.Next() {
		var variantID string
		item := domain.CurrentRender{}
		if err := rows.Scan(&variantID, &item.Run.ID, &item.Run.RenderSpecID, &item.Run.Generation,
			&item.Run.OperationID, &item.Run.CandidateLimit,
			&item.Run.RoutingPolicyVersion, &item.Run.QualityPolicyVersion,
			&item.Run.Outcome, &item.Run.CreatedAt, &item.Run.FinishedAt,
			&item.Operation.Status, &item.Operation.StageCode, &item.Operation.Retryable,
			&item.Operation.PublicMessage); err != nil {
			return nil, err
		}
		item.Run.UserID = userID
		item.Run.PlanVariantID = variantID
		item.Operation.ID = item.Run.OperationID
		item.Operation.UserID = userID
		item.Operation.Kind = domain.OperationRender
		publication, err := s.loadCurrentPublication(ctx, s.pool, userID, variantID)
		if err != nil {
			return nil, err
		}
		if publication != nil && publication.RenderRunID == item.Run.ID {
			item.Publication = publication
		}
		out[variantID] = item
	}
	return out, rows.Err()
}

func (s *Store) loadRenderRun(ctx context.Context, q planningQuerier, userID, runID string) (domain.RenderRun, error) {
	return loadRenderRunQuerier(ctx, q, userID, runID)
}

func loadRenderRunQuerier(ctx context.Context, q planningQuerier, userID, runID string) (domain.RenderRun, error) {
	var run domain.RenderRun
	err := q.QueryRow(ctx, `
		SELECT id::text, user_id::text, plan_variant_id::text, render_spec_id::text, generation,
		       operation_id::text, candidate_limit, routing_policy_version, quality_policy_version,
		       COALESCE(outcome, ''), created_at, finished_at
		FROM render_runs WHERE user_id=$1::uuid AND id=$2::uuid`, userID, runID).Scan(
		&run.ID, &run.UserID, &run.PlanVariantID, &run.RenderSpecID, &run.Generation,
		&run.OperationID, &run.CandidateLimit, &run.RoutingPolicyVersion, &run.QualityPolicyVersion,
		&run.Outcome, &run.CreatedAt, &run.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.RenderRun{}, repository.ErrNotFound
	}
	return run, err
}

func (s *Store) loadRenderOperation(ctx context.Context, q planningQuerier, userID, operationID string) (domain.Operation, error) {
	var op domain.Operation
	err := q.QueryRow(ctx, `
		SELECT id::text, user_id::text, kind, status, stage_code, retryable, public_message
		FROM operations WHERE id=$1::uuid AND user_id=$2::uuid`, operationID, userID).Scan(
		&op.ID, &op.UserID, &op.Kind, &op.Status, &op.StageCode, &op.Retryable, &op.PublicMessage)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Operation{}, repository.ErrNotFound
	}
	return op, err
}

func (s *Store) loadCurrentPublication(ctx context.Context, q planningQuerier, userID, variantID string) (*domain.RenderPublication, error) {
	var publication domain.RenderPublication
	var objectKey *string
	err := q.QueryRow(ctx, `
		SELECT p.id::text, p.user_id::text, p.plan_variant_id::text, p.render_run_id::text,
		       p.candidate_id::text, p.quality_evaluation_id::text, p.generation,
		       a.object_key, a.id::text, p.created_at
		FROM render_heads h
		JOIN render_publications p ON p.user_id=h.user_id AND p.plan_variant_id=h.plan_variant_id AND p.id=h.current_publication_id
		JOIN media_assets a ON a.user_id=p.user_id AND a.id=p.asset_id
		WHERE h.user_id=$1::uuid AND h.plan_variant_id=$2::uuid`, userID, variantID).Scan(
		&publication.ID, &publication.UserID, &publication.PlanVariantID, &publication.RenderRunID,
		&publication.CandidateID, &publication.QualityEvaluationID, &publication.Generation,
		&objectKey, &publication.AssetID, &publication.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if objectKey != nil {
		publication.ObjectKey = *objectKey
	}
	return &publication, nil
}

// renderTaskMaxAttempts is the registry definition value mirrored at insert
// time; bootstrap constructs the adapter with the same constant.
const renderTaskMaxAttempts = 3
