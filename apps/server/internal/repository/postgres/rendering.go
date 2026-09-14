package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

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

// ---- worker-side transactions ----

// ErrRenderSuperseded 表示 generation 已过期(与 service 端语义一致)。
var ErrRenderSuperseded = errors.New("render run superseded")

// GetCandidateJob 组装一次候选生成所需的一切;ordinal 预算检查在 service
// 端复用 domain.CanRequestCandidate。
func (s *Store) GetCandidateJob(ctx context.Context, userID, runID string, ordinal int) (domain.RenderCandidateJob, error) {
	run, err := s.loadRenderRun(ctx, s.pool, userID, runID)
	if err != nil {
		return domain.RenderCandidateJob{}, err
	}
	var spec domain.RenderSpec
	var specRaw []byte
	err = s.pool.QueryRow(ctx, `
		SELECT sp.id::text, sp.user_id::text, sp.plan_variant_id::text, sp.source_photo_set_id::text,
		       sp.schema_version, sp.spec, sp.content_hash, sp.created_at
		FROM render_specs sp WHERE sp.user_id=$1::uuid AND sp.id=$2::uuid`,
		userID, run.RenderSpecID).Scan(
		&spec.ID, &spec.UserID, &spec.PlanVariantID, &spec.SourcePhotoSetID,
		&spec.SchemaVersion, &specRaw, &spec.ContentHash, &spec.CreatedAt)
	if err != nil {
		return domain.RenderCandidateJob{}, err
	}
	if err := json.Unmarshal(specRaw, &spec.Spec); err != nil {
		return domain.RenderCandidateJob{}, fmt.Errorf("decode render spec: %w", err)
	}
	body, face, err := s.loadRenderReferences(ctx, userID, spec)
	if err != nil {
		return domain.RenderCandidateJob{}, err
	}
	previous, err := s.loadLatestCandidateEvaluation(ctx, userID, runID, ordinal)
	if err != nil {
		return domain.RenderCandidateJob{}, err
	}
	return domain.RenderCandidateJob{
		Run: run, Spec: spec, Body: body, Face: face, Previous: previous,
	}, nil
}

func (s *Store) loadRenderReferences(ctx context.Context, userID string, spec domain.RenderSpec) (body, face domain.MediaAsset, err error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, user_id::text, origin, purpose, object_key, sha256, mime_type, byte_size, state, display_kind
		FROM media_assets
		WHERE user_id=$1::uuid AND id=ANY($2::uuid[]) AND state IN ('ready','published')`,
		userID, []string{spec.Spec.Identity.BodyAssetID, spec.Spec.Identity.FaceAssetID})
	if err != nil {
		return body, face, err
	}
	defer rows.Close()
	assets := map[string]domain.MediaAsset{}
	for rows.Next() {
		var asset domain.MediaAsset
		if err = rows.Scan(&asset.ID, &asset.UserID, &asset.Origin, &asset.Purpose, &asset.ObjectKey,
			&asset.SHA256, &asset.MIMEType, &asset.ByteSize, &asset.State, &asset.DisplayKind); err != nil {
			return body, face, err
		}
		assets[asset.ID] = asset
	}
	if err = rows.Err(); err != nil {
		return body, face, err
	}
	body, ok := assets[spec.Spec.Identity.BodyAssetID]
	if !ok {
		return body, face, repository.ErrNotFound
	}
	face, ok = assets[spec.Spec.Identity.FaceAssetID]
	if !ok {
		return body, face, repository.ErrNotFound
	}
	return body, face, nil
}

// loadLatestCandidateEvaluation 读取同一 run 上 ordinal 更小的最近一次评估
// (Candidate 2 需要Candidate 1 的失败原因)。
func (s *Store) loadLatestCandidateEvaluation(ctx context.Context, userID, runID string, ordinal int) (*domain.QualityEvaluation, error) {
	candidateID, err := s.latestCandidateID(ctx, userID, runID, ordinal)
	if err != nil {
		return nil, err
	}
	if candidateID == "" {
		return nil, nil
	}
	return s.loadLatestEvaluationByCandidate(ctx, userID, candidateID)
}

func (s *Store) latestCandidateID(ctx context.Context, userID, runID string, beforeOrdinal int) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `
		SELECT id::text FROM render_candidates
		WHERE user_id=$1::uuid AND render_run_id=$2::uuid AND ordinal<$3
		ORDER BY ordinal DESC LIMIT 1`, userID, runID, beforeOrdinal).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func (s *Store) loadLatestEvaluationByCandidate(ctx context.Context, userID, candidateID string) (*domain.QualityEvaluation, error) {
	var evaluation domain.QualityEvaluation
	var policyVersion string
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, user_id::text, subject_type, subject_id::text, policy_version, decision,
		       reason_codes, internal_scores, evaluator_invocation_id::text, created_at
		FROM quality_evaluations
		WHERE user_id=$1::uuid AND subject_type='render_candidate' AND subject_id=$2::uuid
		ORDER BY created_at DESC LIMIT 1`, userID, candidateID).Scan(
		&evaluation.ID, &evaluation.UserID, &evaluation.SubjectType, &evaluation.SubjectID,
		&policyVersion, &evaluation.Decision, &evaluation.ReasonCodes,
		&evaluation.InternalScores, &evaluation.EvaluatorInvocationID, &evaluation.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	evaluation.Policy = domain.QualityPolicyRef{Version: policyVersion}
	return &evaluation, nil
}

// renderLeaseCAS 是所有 worker 事务的第一条语句:锁定租约并校验 generation。
func renderLeaseCAS(ctx context.Context, tx pgx.Tx, command domain.RenderRecordCandidateCommand) error {
	tag, err := tx.Exec(ctx, `
		UPDATE tasks SET heartbeat_at=now()
		WHERE id=$1::uuid AND user_id=$2::uuid AND status='leased'
		  AND lease_token=$3::uuid AND lease_expires_at>now()
		  AND subject_generation=$4`,
		command.TaskID, command.UserID, command.LeaseToken, command.SubjectGeneration)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return repository.ErrLeaseLost
	}
	return nil
}

// checkRunGeneration 再次读取 RenderHead;generation 不同说明新 generation
// 已创建,当前任务必须放弃。
func checkRunGeneration(ctx context.Context, tx pgx.Tx, userID, planVariantID string, generation int) error {
	var headGeneration int
	err := tx.QueryRow(ctx, `
		SELECT generation FROM render_heads
		WHERE user_id=$1::uuid AND plan_variant_id=$2::uuid`, userID, planVariantID).Scan(&headGeneration)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrRenderSuperseded
	}
	if err != nil {
		return err
	}
	if headGeneration != generation {
		return ErrRenderSuperseded
	}
	return nil
}

// RecordCandidate 在 lease CAS + generation 校验后写入隔离候选。
func (s *Store) RecordCandidate(ctx context.Context, command domain.RenderRecordCandidateCommand) (domain.RenderCandidate, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.RenderCandidate{}, err
	}
	defer tx.Rollback(ctx)

	if err = renderLeaseCAS(ctx, tx, command); err != nil {
		return domain.RenderCandidate{}, err
	}
	var runUserID, runVariantID, specID string
	var runGeneration int
	err = tx.QueryRow(ctx, `
		SELECT user_id::text, plan_variant_id::text, render_spec_id::text, generation
		FROM render_runs WHERE user_id=$1::uuid AND id=$2::uuid FOR UPDATE`,
		command.UserID, command.RenderRunID).Scan(&runUserID, &runVariantID, &specID, &runGeneration)
	if err != nil {
		return domain.RenderCandidate{}, err
	}
	if runGeneration != command.SubjectGeneration {
		return domain.RenderCandidate{}, ErrRenderSuperseded
	}

	// 候选资产先落 quarantined MediaAsset,再挂 RenderCandidate。
	assetID := uuid.NewString()
	if _, err = tx.Exec(ctx, `
		INSERT INTO media_assets(id, user_id, origin, purpose, object_key, sha256, mime_type, byte_size, width, height, state, display_kind, provider_invocation_id)
		VALUES ($1::uuid,$2::uuid,'provider_output','render_candidate',$3,$4,'image/jpeg',$5,$6,$7,'quarantined','generated_reference',$8::uuid)`,
		assetID, command.UserID, command.Asset.ObjectKey, command.Asset.SHA256,
		command.Asset.ByteSize, command.Asset.Width, command.Asset.Height,
		command.ProviderInvocationID); err != nil {
		return domain.RenderCandidate{}, err
	}
	var candidateID string
	var createdAtPlaceholder time.Time
	err = tx.QueryRow(ctx, `
		INSERT INTO render_candidates(user_id, render_run_id, ordinal, asset_id, provider_invocation_id)
		VALUES ($1::uuid,$2::uuid,$3,$4::uuid,$5::uuid)
		RETURNING id::text, created_at`,
		command.UserID, command.RenderRunID, command.Ordinal, assetID,
		command.ProviderInvocationID).Scan(&candidateID, &createdAtPlaceholder)
	if err != nil {
		return domain.RenderCandidate{}, mapRenderConstraint(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.RenderCandidate{}, err
	}
	return domain.RenderCandidate{
		ID: candidateID, UserID: command.UserID, RenderRunID: command.RenderRunID,
		Ordinal: command.Ordinal, AssetID: assetID, ProviderInvocationID: command.ProviderInvocationID,
	}, nil
}

func mapRenderConstraint(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return fmt.Errorf("%w: candidate already recorded", repository.ErrConflict)
		case "23514":
			return ErrRenderSuperseded
		}
	}
	return err
}

// ExpandCandidateBudget 把 candidate_limit 1→2 并在唯一 dedupe key 下入队
// Candidate 2;并发时唯一约束保证只有一个任务。
func (s *Store) ExpandCandidateBudget(ctx context.Context, command domain.RenderEnqueueNextCandidateCommand) (string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	if _, err = tx.Exec(ctx, `
		INSERT INTO quality_evaluations(id, user_id, subject_type, subject_id, policy_version, decision, reason_codes, internal_scores, evaluator_invocation_id)
		VALUES ($1::uuid,$2::uuid,'render_candidate',$3::uuid,$4,$5,$6,$7, NULLIF($8::text,'')::uuid)`,
		uuidOrNull(command.Quality.ID), command.Quality.UserID, command.Quality.SubjectID,
		command.Quality.Policy.Version, string(command.Quality.Decision),
		command.Quality.ReasonCodes, defaultJSONB(command.Quality.InternalScores),
		uuidOrNull(command.Quality.EvaluatorInvocationID)); err != nil {
		return "", fmt.Errorf("eval insert: %w", err)
	}

	var runUserID, runVariantID string
	var runGeneration int
	err = tx.QueryRow(ctx, `
		UPDATE render_runs SET candidate_limit=2
		WHERE id=$1::uuid AND user_id=$2::uuid AND generation=$3 AND candidate_limit=1
		RETURNING user_id::text, plan_variant_id::text, generation`,
		command.RenderRunID, command.UserID, command.SubjectGeneration).
		Scan(&runUserID, &runVariantID, &runGeneration)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("limit update: %w", err)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		// 已扩过预算(并发或重试):读回现有行继续。
		err = tx.QueryRow(ctx, `
			SELECT user_id::text, plan_variant_id::text, generation FROM render_runs
			WHERE id=$1::uuid AND user_id=$2::uuid`, command.RenderRunID, command.UserID).
			Scan(&runUserID, &runVariantID, &runGeneration)
		if err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	if runGeneration != command.SubjectGeneration {
		return "", ErrRenderSuperseded
	}

	var operationID string
	err = tx.QueryRow(ctx, `
		SELECT operation_id::text FROM render_runs WHERE id=$1::uuid AND user_id=$2::uuid`,
		command.RenderRunID, command.UserID).Scan(&operationID)
	if err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `
		UPDATE operations SET status='retrying', retryable=true, updated_at=now()
		WHERE id=$1::uuid AND user_id=$2::uuid`, operationID, command.UserID); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `
		UPDATE tasks SET status='succeeded', finished_at=now(), updated_at=now()
		WHERE id=$1::uuid AND user_id=$2::uuid`, command.TaskID, command.UserID); err != nil {
		return "", err
	}

	payload, err := json.Marshal(domain.RenderGenerateCandidatePayload{
		RenderRunID: command.RenderRunID, Ordinal: 2,
	})
	if err != nil {
		return "", err
	}
	dedupeKey := domain.RenderCandidateDedupeKey(command.RenderRunID, 2)
	var taskID string
	err = tx.QueryRow(ctx, `
		INSERT INTO tasks(id, user_id, operation_id, type, subject_type, subject_id, subject_generation,
		                  payload_version, payload, dedupe_key, status, stage_code, max_attempts)
		VALUES (gen_random_uuid(),$1::uuid,$2::uuid,$3,'render_run',$4::uuid,$5,1,$6,$7,'queued','',$8)
		ON CONFLICT (user_id, dedupe_key) DO UPDATE SET dedupe_key=EXCLUDED.dedupe_key
		RETURNING id::text`,
		command.UserID, operationID, string(renderCreateTaskType), command.RenderRunID,
		runGeneration, payload, dedupeKey, renderTaskMaxAttempts).Scan(&taskID)
	if err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return taskID, nil
}

// FailRun 终态失败 run 和 operation。
func (s *Store) FailRun(ctx context.Context, command domain.RenderFailRunCommand) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err = tx.Exec(ctx, `
		UPDATE tasks SET heartbeat_at=now()
		WHERE id=$1::uuid AND user_id=$2::uuid AND status='leased'
		  AND lease_token=$3::uuid AND lease_expires_at>now()
		  AND subject_generation=$4`,
		command.TaskID, command.UserID, command.LeaseToken, command.SubjectGeneration); err != nil {
		return err
	}
	var runUserID, runVariantID string
	var runGeneration int
	err = tx.QueryRow(ctx, `
		UPDATE render_runs SET outcome=$3, finished_at=now()
		WHERE id=$1::uuid AND user_id=$2::uuid AND outcome IS NULL
		RETURNING user_id::text, plan_variant_id::text, generation`,
		command.RenderRunID, command.UserID, command.Outcome).
		Scan(&runUserID, &runVariantID, &runGeneration)
	if errors.Is(err, pgx.ErrNoRows) {
		// run 已终态(可能被 generation supersede);保持幂等。
		if err = checkRunGeneration(ctx, tx, command.UserID, command.PlanVariantID, command.SubjectGeneration); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `
		UPDATE operations SET status='failed', error_code=$3, retryable=$4, trace_id=$5, updated_at=now(), finished_at=now()
		WHERE id=(SELECT operation_id FROM render_runs WHERE id=$1::uuid AND user_id=$2::uuid)
		  AND user_id=$2::uuid`,
		command.RenderRunID, command.UserID, command.ErrorCode, command.Retryable, uuid.NewString()); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `
		UPDATE tasks SET status='failed', error_class='quality_rejected', error_code=$3,
		    finished_at=now(), updated_at=now()
		WHERE id=$1::uuid AND user_id=$2::uuid`, command.TaskID, command.UserID, command.ErrorCode); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CommitEvaluation 是 pass 分支的原子发布:lease CAS → Head FOR UPDATE →
// 质量评估 → 资产 published → Publication → Head CAS → run/operation/task 终态。
func (s *Store) CommitEvaluation(ctx context.Context, command domain.RenderCommitEvaluationCommand) (domain.RenderCommitEvaluationResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.RenderCommitEvaluationResult{}, err
	}
	defer tx.Rollback(ctx)

	leaseCheck := domain.RenderRecordCandidateCommand{
		TaskID: command.TaskID, LeaseToken: command.LeaseToken,
		UserID: command.UserID, SubjectGeneration: command.SubjectGeneration,
	}
	if err = renderLeaseCAS(ctx, tx, leaseCheck); err != nil {
		return domain.RenderCommitEvaluationResult{}, err
	}

	var runUserID, runVariantID string
	var runGeneration int
	err = tx.QueryRow(ctx, `
		SELECT user_id::text, plan_variant_id::text, generation FROM render_runs
		WHERE id=$1::uuid AND user_id=$2::uuid FOR UPDATE`,
		command.RenderRunID, command.UserID).Scan(&runUserID, &runVariantID, &runGeneration)
	if err != nil {
		return domain.RenderCommitEvaluationResult{}, err
	}
	if runGeneration != command.SubjectGeneration {
		return domain.RenderCommitEvaluationResult{}, ErrRenderSuperseded
	}

	// 插入质量评估(pass 或终态留档)。
	evaluation := command.Evaluation
	if _, err = tx.Exec(ctx, `
		INSERT INTO quality_evaluations(id, user_id, subject_type, subject_id, policy_version, decision, reason_codes, internal_scores, evaluator_invocation_id)
		VALUES ($1::uuid,$2::uuid,'render_candidate',$3::uuid,$4,$5,$6,$7, NULLIF($8::text,'')::uuid)`,
		uuid.NewString(), command.UserID, command.CandidateID,
		evaluation.Policy.Version, string(evaluation.Decision),
		evaluation.ReasonCodes, defaultJSONB(evaluation.InternalScores),
		uuidOrNull(evaluation.EvaluatorInvocationID)); err != nil {
		return domain.RenderCommitEvaluationResult{}, err
	}

	if string(evaluation.Decision) != string(domain.QualityDecisionPass) || command.PublishedObject == nil {
		// 非发布留档:完成当前 task 即可,run/operation 由 FailRun 处理。
		if _, err = tx.Exec(ctx, `
			UPDATE tasks SET status='succeeded', finished_at=now(), updated_at=now()
			WHERE id=$1::uuid AND user_id=$2::uuid`, command.TaskID, command.UserID); err != nil {
			return domain.RenderCommitEvaluationResult{}, err
		}
		return domain.RenderCommitEvaluationResult{Outcome: string(command.Evaluation.Decision)}, tx.Commit(ctx)
	}

	// 资产 quarantined → published,object_key 换成 immutable published key。
	var publicationID string
	err = tx.QueryRow(ctx, `
		UPDATE media_assets a SET state='published', object_key=$2, mime_type='image/jpeg'
		WHERE a.id=(SELECT asset_id FROM render_candidates WHERE user_id=$1::uuid AND id=$3::uuid)
		  AND a.user_id=$1::uuid
		RETURNING a.id::text`,
		command.UserID, command.PublishedObject.Key, command.CandidateID).Scan(&publicationID)
	if err != nil {
		return domain.RenderCommitEvaluationResult{}, fmt.Errorf("promote asset: %w", err)
	}
	_ = publicationID

	// 先插入 publication,再走 Head CAS:复合外键要求 publication 已存在。
	publicationUUID := uuid.NewString()
	if command.PublicationID != "" {
		publicationUUID = command.PublicationID
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO render_publications(id, user_id, plan_variant_id, render_run_id, candidate_id, quality_evaluation_id, generation)
		VALUES ($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,
		        (SELECT id FROM quality_evaluations WHERE user_id=$2::uuid AND subject_type='render_candidate' AND subject_id=$5::uuid ORDER BY created_at DESC LIMIT 1),
		        $6)`,
		publicationUUID, command.UserID, runVariantID, command.RenderRunID,
		command.CandidateID, runGeneration); err != nil {
		return domain.RenderCommitEvaluationResult{}, err
	}

	// Head CAS:generation+version 双条件。
	var headVersion int64
	if err = tx.QueryRow(ctx, `
		SELECT version FROM render_heads
		WHERE user_id=$1::uuid AND plan_variant_id=$2::uuid FOR UPDATE`,
		command.UserID, runVariantID).Scan(&headVersion); err != nil {
		return domain.RenderCommitEvaluationResult{}, err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE render_heads SET current_publication_id=$3::uuid, version=version+1
		WHERE user_id=$1::uuid AND plan_variant_id=$2::uuid
		  AND generation=$4 AND version=$5`,
		command.UserID, runVariantID, publicationUUID, runGeneration, headVersion)
	if err != nil {
		return domain.RenderCommitEvaluationResult{}, err
	}
	if tag.RowsAffected() != 1 {
		return domain.RenderCommitEvaluationResult{}, ErrRenderSuperseded
	}

	if _, err = tx.Exec(ctx, `
		UPDATE render_runs SET outcome='published', finished_at=now()
		WHERE id=$1::uuid AND user_id=$2::uuid`, command.RenderRunID, command.UserID); err != nil {
		return domain.RenderCommitEvaluationResult{}, err
	}
	if _, err = tx.Exec(ctx, `
		UPDATE operations SET status='succeeded', result_type='render_publication', result_id=$3::uuid,
		    progress_bps=10000, stage_code='render.ready', public_message='效果图已生成',
		    updated_at=now(), finished_at=now()
		WHERE id=(SELECT operation_id FROM render_runs WHERE id=$1::uuid AND user_id=$2::uuid)
		  AND user_id=$2::uuid`,
		command.RenderRunID, command.UserID, publicationUUID); err != nil {
		return domain.RenderCommitEvaluationResult{}, err
	}
	if _, err = tx.Exec(ctx, `
		UPDATE tasks SET status='succeeded', finished_at=now(), updated_at=now()
		WHERE id=$1::uuid AND user_id=$2::uuid`, command.TaskID, command.UserID); err != nil {
		return domain.RenderCommitEvaluationResult{}, err
	}

	if err = tx.Commit(ctx); err != nil {
		return domain.RenderCommitEvaluationResult{}, err
	}
	return domain.RenderCommitEvaluationResult{Outcome: domain.RenderOutcomePublished}, nil
}

// uuidOrNull 把空字符串映射为 NULL uuid 参数。
func uuidOrNull(value string) any {
	if value == "" {
		return nil
	}
	return value
}
