package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

// planningQuerier is satisfied by both *pgxpool.Pool and pgx.Tx, so graph
// loaders run inside or outside a transaction with the same code.
type planningQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

const planningReadyProgress = 10000
const planningReadyStage = "plan.ready"
const planningReadyMessage = "三套方案已准备好"

// ---- ReportReader ----

// GetPlanningReport projects the published immutable report onto the planning
// snapshot: face/body asset ids, profile snapshot and the highest-priority
// finding id.
func (s *Store) GetPlanningReport(ctx context.Context, userID, reportID string) (domain.PlanningReportSnapshot, error) {
	report, err := s.GetReport(ctx, userID, reportID)
	if err != nil {
		return domain.PlanningReportSnapshot{}, err
	}
	out := domain.PlanningReportSnapshot{
		ID:              report.Report.ID,
		UserID:          report.Report.UserID,
		PhotoSetID:      report.Report.PhotoSetID,
		ProfileSnapshot: report.Report.ProfileSnapshot,
		ImpressionTags:  report.Report.ImpressionTags,
		PriorityTitle:   report.Report.PriorityTitle,
		PriorityCopy:    report.Report.PriorityCopy,
	}
	priority := -1
	for _, finding := range report.Report.Findings {
		out.Findings = append(out.Findings, domain.PlanningFindingSnapshot{
			ID:                 finding.ID,
			Category:           finding.Category,
			Priority:           finding.Priority,
			Label:              finding.Label,
			VisibleObservation: finding.VisibleObservation,
			Recommendation:     finding.Recommendation,
		})
		if priority == -1 || finding.Priority < priority {
			priority = finding.Priority
			out.PriorityFindingID = finding.ID
		}
	}
	for _, item := range report.PhotoSet.Items {
		switch item.Role {
		case domain.PhotoRoleFace:
			out.FaceAssetID = item.Asset.ID
		case domain.PhotoRoleBody:
			out.BodyAssetID = item.Asset.ID
		}
	}
	if out.FaceAssetID == "" || out.BodyAssetID == "" || out.PriorityFindingID == "" {
		return domain.PlanningReportSnapshot{}, fmt.Errorf("planning report %s is incomplete", reportID)
	}
	return out, nil
}

// ---- PlanSetStore ----

const planningPublishedGuard = `EXISTS (
	SELECT 1 FROM operations op
	WHERE op.user_id = ps.user_id AND op.subject_type = 'plan_set'
	  AND op.subject_id = ps.id AND op.status = 'succeeded')`

func (s *Store) FindPublished(ctx context.Context, key domain.PlanningPlanSetKey) (domain.PlanSet, bool, error) {
	var id string
	err := s.pool.QueryRow(ctx, `
		SELECT ps.id::text FROM plan_sets ps
		WHERE ps.user_id=$1::uuid AND ps.planning_input_hash=$2 AND `+planningPublishedGuard+`
		LIMIT 1`, key.UserID, key.PlanningInputHash).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PlanSet{}, false, nil
	}
	if err != nil {
		return domain.PlanSet{}, false, err
	}
	planSet, err := s.loadPlanningGraph(ctx, s.pool, key.UserID, id)
	if err != nil {
		return domain.PlanSet{}, false, err
	}
	return planSet, true, nil
}

func (s *Store) Get(ctx context.Context, userID, planSetID string) (domain.PlanSet, error) {
	return s.getPublishedPlanSet(ctx, s.pool, userID, planSetID)
}

func (s *Store) List(ctx context.Context, userID, reportID string, scene *domain.Scene) ([]domain.PlanSet, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT ps.id::text FROM plan_sets ps
		WHERE ps.user_id=$1::uuid AND ps.report_id=$2::uuid
		  AND ($3::text IS NULL OR ps.scene=$3) AND `+planningPublishedGuard+`
		ORDER BY ps.created_at DESC, ps.id DESC`, userID, reportID, sceneText(scene))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sets := make([]domain.PlanSet, 0, 4)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		planSet, err := s.loadPlanningGraph(ctx, s.pool, userID, id)
		if err != nil {
			return nil, err
		}
		sets = append(sets, planSet)
	}
	return sets, rows.Err()
}

func sceneText(scene *domain.Scene) any {
	if scene == nil {
		return nil
	}
	return string(*scene)
}

func (s *Store) getPublishedPlanSet(ctx context.Context, q planningQuerier, userID, planSetID string) (domain.PlanSet, error) {
	var id string
	err := q.QueryRow(ctx, `
		SELECT ps.id::text FROM plan_sets ps
		WHERE ps.user_id=$1::uuid AND ps.id=$2::uuid AND `+planningPublishedGuard,
		userID, planSetID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PlanSet{}, repository.ErrNotFound
	}
	if err != nil {
		return domain.PlanSet{}, err
	}
	return s.loadPlanningGraph(ctx, q, userID, id)
}

func (s *Store) loadPlanningGraph(ctx context.Context, q planningQuerier, userID, planSetID string) (domain.PlanSet, error) {
	var planSet domain.PlanSet
	var brief domain.SceneBrief
	var briefRaw, profileRaw []byte
	err := q.QueryRow(ctx, `
		SELECT id::text, user_id::text, report_id::text, profile_snapshot, scene,
		       scene_brief, brief_hash, planning_input_hash, planner_schema_version, style_rule_version,
		       provider_invocation_id::text, quality_evaluation_id::text, created_at
		FROM plan_sets WHERE user_id=$1::uuid AND id=$2::uuid`,
		userID, planSetID).Scan(
		&planSet.ID, &planSet.UserID, &planSet.ReportID, &profileRaw, &planSet.Scene,
		&briefRaw, &planSet.BriefHash, &planSet.PlanningInputHash, &planSet.PlannerSchemaVersion, &planSet.StyleRuleVersion,
		&planSet.ProviderInvocationID, &planSet.QualityEvaluationID, &planSet.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PlanSet{}, repository.ErrNotFound
	}
	if err != nil {
		return domain.PlanSet{}, err
	}
	planSet.ProfileSnapshot = profileRaw
	if err := json.Unmarshal(briefRaw, &brief); err != nil {
		return domain.PlanSet{}, fmt.Errorf("decode scene brief: %w", err)
	}
	planSet.SceneBrief = brief

	variantRows, err := q.Query(ctx, `
		SELECT id::text, slot, key, name, descriptor, rationale, recommended, outcome_tags, difference_tags, created_at
		FROM plan_variants WHERE user_id=$1::uuid AND plan_set_id=$2::uuid ORDER BY slot`, userID, planSetID)
	if err != nil {
		return domain.PlanSet{}, err
	}
	defer variantRows.Close()
	variantIDs := make([]string, 0, 3)
	for variantRows.Next() {
		variant := domain.PlanVariant{UserID: planSet.UserID, PlanSetID: planSet.ID}
		if err := variantRows.Scan(&variant.ID, &variant.Slot, &variant.Key, &variant.Name,
			&variant.Descriptor, &variant.Rationale, &variant.Recommended,
			&variant.OutcomeTags, &variant.DifferenceTags, &variant.CreatedAt); err != nil {
			return domain.PlanSet{}, err
		}
		planSet.Variants = append(planSet.Variants, variant)
		variantIDs = append(variantIDs, variant.ID)
	}
	if err := variantRows.Err(); err != nil {
		return domain.PlanSet{}, err
	}
	for index := range planSet.Variants {
		if err := s.loadPlanSteps(ctx, q, userID, &planSet.Variants[index]); err != nil {
			return domain.PlanSet{}, err
		}
	}
	return planSet, nil
}

func (s *Store) loadPlanSteps(ctx context.Context, q planningQuerier, userID string, variant *domain.PlanVariant) error {
	rows, err := q.Query(ctx, `
		SELECT id::text, category, action, title, summary, details, position, created_at
		FROM plan_steps WHERE user_id=$1::uuid AND plan_variant_id=$2::uuid ORDER BY position`,
		userID, variant.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var detailsRaw []byte
		step := domain.PlanStep{UserID: variant.UserID, PlanVariantID: variant.ID}
		if err := rows.Scan(&step.ID, &step.Category, &step.Action, &step.Title, &step.Summary,
			&detailsRaw, &step.Position, &step.CreatedAt); err != nil {
			return err
		}
		if err := json.Unmarshal(detailsRaw, &step.Details); err != nil {
			return fmt.Errorf("decode plan step details: %w", err)
		}
		variant.Steps = append(variant.Steps, step)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for index := range variant.Steps {
		if err := s.loadStepGroundings(ctx, q, userID, &variant.Steps[index]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) loadStepGroundings(ctx context.Context, q planningQuerier, userID string, step *domain.PlanStep) error {
	rows, err := q.Query(ctx, `
		SELECT source_type, source_id, reason
		FROM plan_step_groundings WHERE user_id=$1::uuid AND plan_step_id=$2::uuid
		ORDER BY source_type, source_id`, userID, step.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		grounding := domain.PlanStepGrounding{UserID: step.UserID, PlanStepID: step.ID}
		if err := rows.Scan(&grounding.SourceType, &grounding.SourceID, &grounding.Reason); err != nil {
			return err
		}
		step.Groundings = append(step.Groundings, grounding)
	}
	return rows.Err()
}

// Prepare writes the gate-passed graph in one transaction. Rows stay
// invisible until CommitPrepared flips the operation to succeeded; the
// deferred completeness trigger rejects partial graphs at COMMIT time.
func (s *Store) Prepare(ctx context.Context, lease domain.TaskLease, command domain.PlanningPrepareCommand) (domain.PlanSet, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.PlanSet{}, err
	}
	defer tx.Rollback(ctx)

	if err := validatePlanningLeaseTx(ctx, tx, lease); err != nil {
		return domain.PlanSet{}, err
	}
	planSet := command.PlanSet
	quality := command.Quality
	if _, err = tx.Exec(ctx, `
		INSERT INTO quality_evaluations(id, user_id, subject_type, subject_id, policy_version, decision, reason_codes, internal_scores, evaluator_invocation_id)
		VALUES ($1::uuid,$2::uuid,'plan_set',$3::uuid,$4,$5,$6,$7, NULLIF($8,'')::uuid)`,
		quality.ID, quality.UserID, planSet.ID, quality.PolicyVersion, quality.Decision,
		quality.ReasonCodes, defaultJSONB(quality.InternalScores), quality.EvaluatorInvocationID); err != nil {
		return domain.PlanSet{}, err
	}
	briefEncoded, err := json.Marshal(planSet.SceneBrief)
	if err != nil {
		return domain.PlanSet{}, err
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO plan_sets(id, user_id, report_id, profile_snapshot, scene, scene_brief, brief_hash, planning_input_hash, planner_schema_version, style_rule_version, provider_invocation_id, quality_evaluation_id)
		VALUES ($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7,$8,$9,$10,$11::uuid,$12::uuid)`,
		planSet.ID, planSet.UserID, planSet.ReportID, defaultJSONB(planSet.ProfileSnapshot),
		string(planSet.Scene), briefEncoded, planSet.BriefHash, planSet.PlanningInputHash, planSet.PlannerSchemaVersion,
		planSet.StyleRuleVersion, planSet.ProviderInvocationID, quality.ID); err != nil {
		return domain.PlanSet{}, err
	}
	for _, variant := range planSet.Variants {
		if _, err = tx.Exec(ctx, `
			INSERT INTO plan_variants(id, user_id, plan_set_id, slot, key, name, descriptor, rationale, recommended, outcome_tags, difference_tags)
			VALUES ($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7,$8,$9,$10,$11)`,
			variant.ID, variant.UserID, variant.PlanSetID, variant.Slot, string(variant.Key),
			variant.Name, variant.Descriptor, variant.Rationale, variant.Recommended,
			variant.OutcomeTags, variant.DifferenceTags); err != nil {
			return domain.PlanSet{}, err
		}
		for _, step := range variant.Steps {
			detailsEncoded, err := json.Marshal(step.Details)
			if err != nil {
				return domain.PlanSet{}, err
			}
			if _, err = tx.Exec(ctx, `
				INSERT INTO plan_steps(id, user_id, plan_variant_id, category, action, title, summary, details, position)
				VALUES ($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7,$8,$9)`,
				step.ID, step.UserID, step.PlanVariantID, string(step.Category),
				string(step.Action), step.Title, step.Summary, detailsEncoded, step.Position); err != nil {
				return domain.PlanSet{}, err
			}
			for _, grounding := range step.Groundings {
				if _, err = tx.Exec(ctx, `
					INSERT INTO plan_step_groundings(user_id, plan_step_id, source_type, source_id, reason)
					VALUES ($1::uuid,$2::uuid,$3,$4,$5)`,
					grounding.UserID, grounding.PlanStepID, string(grounding.SourceType),
					grounding.SourceID, grounding.Reason); err != nil {
					return domain.PlanSet{}, err
				}
			}
		}
	}
	for _, spec := range command.RenderSpecs {
		specEncoded, err := json.Marshal(spec.Spec)
		if err != nil {
			return domain.PlanSet{}, err
		}
		if _, err = tx.Exec(ctx, `
			INSERT INTO render_specs(id, user_id, plan_variant_id, source_photo_set_id, schema_version, spec, content_hash)
			VALUES ($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5,$6,$7)`,
			spec.ID, spec.UserID, spec.PlanVariantID, spec.SourcePhotoSetID,
			spec.SchemaVersion, specEncoded, spec.ContentHash); err != nil {
			return domain.PlanSet{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.PlanSet{}, err
	}
	return planSet, nil
}

// CommitPrepared applies the task result in one lease-CAS transaction:
// publish the prepared graph, or fail the task and the operation.
func (s *Store) CommitPrepared(ctx context.Context, lease domain.TaskLease, result domain.TaskResult) (domain.CommitOutcome, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	if err := validatePlanningLeaseTx(ctx, tx, lease); err != nil {
		if errors.Is(err, repository.ErrLeaseLost) {
			return domain.CommitSuperseded, nil
		}
		return "", err
	}
	switch result.Disposition {
	case domain.TaskPublish:
		if result.ResultType != "plan_set" || result.ResultID != lease.Task.SubjectID {
			return "", fmt.Errorf("plan_set publish result %s/%s does not match task subject %s",
				result.ResultType, result.ResultID, lease.Task.SubjectID)
		}
		if _, err = tx.Exec(ctx, `
			UPDATE tasks SET status='succeeded', finished_at=now(), updated_at=now()
			WHERE id=$1::uuid AND user_id=$2::uuid`, lease.ID, lease.UserID); err != nil {
			return "", err
		}
		if _, err = tx.Exec(ctx, `
			UPDATE operations
			SET status='succeeded', progress_bps=$3, stage_code=$4, public_message=$5,
			    result_type='plan_set', result_id=$6::uuid, version=version+1,
			    updated_at=now(), finished_at=now()
			WHERE id=$1::uuid AND user_id=$2::uuid`,
			lease.OperationID, lease.UserID, planningReadyProgress, planningReadyStage,
			planningReadyMessage, result.ResultID); err != nil {
			return "", err
		}
	case domain.TaskDomainFail:
		class, code := "", ""
		if result.Failure != nil {
			class, code = string(result.Failure.Class), result.Failure.Code
		}
		if _, err = tx.Exec(ctx, `
			UPDATE tasks SET status='failed', error_class=NULLIF($3,''), error_code=NULLIF($4,''),
			    finished_at=now(), updated_at=now()
			WHERE id=$1::uuid AND user_id=$2::uuid`, lease.ID, lease.UserID, class, code); err != nil {
			return "", err
		}
		if _, err = tx.Exec(ctx, `
			UPDATE operations SET status='failed', error_code=NULLIF($3,''), trace_id=$4, retryable=false,
			    version=version+1, updated_at=now(), finished_at=now()
			WHERE id=$1::uuid AND user_id=$2::uuid AND status <> 'failed'`,
			lease.OperationID, lease.UserID, code, uuid.NewString()); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("unsupported plan_set commit disposition %q", result.Disposition)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return domain.CommitApplied, nil
}

// ---- RenderSpecReader ----

func (s *Store) GetRenderSpecForVariant(ctx context.Context, userID, planVariantID string) (domain.RenderSpec, error) {
	var spec domain.RenderSpec
	var specRaw []byte
	err := s.pool.QueryRow(ctx, `
		SELECT r.id::text, r.user_id::text, r.plan_variant_id::text, r.source_photo_set_id::text,
		       r.schema_version, r.spec, r.content_hash, r.created_at
		FROM render_specs r
		JOIN plan_variants v ON v.user_id=r.user_id AND v.id=r.plan_variant_id
		WHERE r.user_id=$1::uuid AND r.plan_variant_id=$2::uuid AND EXISTS (
			SELECT 1 FROM operations op
			WHERE op.user_id = v.user_id AND op.subject_type = 'plan_set'
			  AND op.subject_id = v.plan_set_id AND op.status = 'succeeded')`,
		userID, planVariantID).Scan(
		&spec.ID, &spec.UserID, &spec.PlanVariantID, &spec.SourcePhotoSetID,
		&spec.SchemaVersion, &specRaw, &spec.ContentHash, &spec.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.RenderSpec{}, repository.ErrNotFound
	}
	if err != nil {
		return domain.RenderSpec{}, err
	}
	if err := json.Unmarshal(specRaw, &spec.Spec); err != nil {
		return domain.RenderSpec{}, fmt.Errorf("decode render spec: %w", err)
	}
	if err := spec.Validate(); err != nil {
		return domain.RenderSpec{}, err
	}
	return spec, nil
}

// ---- PlanningOperations: OperationStarter / OperationWriter / TaskEnqueuer ----

// PlanningOperations owns the planning operation/task transitions. It reads
// the retry budget from the registry definition at construction; commands
// never carry it.
type PlanningOperations struct {
	store           *Store
	maxTaskAttempts int
}

func NewPlanningOperations(store *Store, maxTaskAttempts int) *PlanningOperations {
	return &PlanningOperations{store: store, maxTaskAttempts: maxTaskAttempts}
}

func (p *PlanningOperations) StartWithTask(ctx context.Context, command domain.PlanningStartOperationCommand) (domain.OperationRef, bool, error) {
	tx, err := p.store.pool.Begin(ctx)
	if err != nil {
		return domain.OperationRef{}, false, err
	}
	defer tx.Rollback(ctx)

	ref, err := findPlanningOperationByDedupe(ctx, tx, command.UserID, command.DedupeKey)
	if err != nil {
		return domain.OperationRef{}, false, err
	}
	if ref.ID != "" {
		return ref, false, nil
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO operations(id, user_id, kind, subject_type, subject_id, status)
		VALUES ($1::uuid,$2::uuid,$3,$4,$5::uuid,'accepted')`,
		command.OperationID, command.UserID, string(command.Kind),
		command.SubjectType, command.SubjectID); err != nil {
		return domain.OperationRef{}, false, err
	}
	payload, err := json.Marshal(command.Task.Payload)
	if err != nil {
		return domain.OperationRef{}, false, err
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO tasks(id, user_id, operation_id, type, subject_type, subject_id, subject_generation,
		                  payload_version, payload, dedupe_key, status, stage_code, max_attempts)
		VALUES (gen_random_uuid(),$1::uuid,$2::uuid,$3,$4,$5::uuid,$6,$7,$8,$9,'queued','',$10)
		ON CONFLICT (user_id, dedupe_key) DO NOTHING`,
		command.UserID, command.OperationID, string(command.Task.Type), command.Task.SubjectType,
		command.Task.SubjectID, command.Task.SubjectGeneration, command.Task.PayloadVersion,
		payload, command.Task.DedupeKey, p.maxTaskAttempts)
	if err != nil {
		return domain.OperationRef{}, false, err
	}
	if tag.RowsAffected() == 0 {
		// A concurrent start won the dedupe key; roll the orphan operation
		// back and surface the existing one.
		tx.Rollback(ctx)
		existing, err := findPlanningOperationByDedupe(ctx, p.store.pool, command.UserID, command.DedupeKey)
		if err != nil {
			return domain.OperationRef{}, false, err
		}
		return existing, false, nil
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.OperationRef{}, false, err
	}
	return domain.OperationRef{ID: command.OperationID, Kind: command.Kind, Status: domain.OperationAccepted}, true, nil
}

func findPlanningOperationByDedupe(ctx context.Context, q planningQuerier, userID, dedupeKey string) (domain.OperationRef, error) {
	var ref domain.OperationRef
	err := q.QueryRow(ctx, `
		SELECT o.id::text, o.kind, o.status
		FROM operations o
		JOIN tasks t ON t.user_id=o.user_id AND t.operation_id=o.id
		WHERE t.user_id=$1::uuid AND t.dedupe_key=$2
		LIMIT 1`, userID, dedupeKey).Scan(&ref.ID, &ref.Kind, &ref.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.OperationRef{}, nil
	}
	return ref, err
}

func (p *PlanningOperations) MarkRunning(ctx context.Context, lease domain.TaskLease, progressBPS int, stageCode, publicMessage string) (bool, error) {
	tx, err := p.store.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if err := validatePlanningLeaseTx(ctx, tx, lease); err != nil {
		if errors.Is(err, repository.ErrLeaseLost) {
			return false, nil
		}
		return false, err
	}
	if _, err = tx.Exec(ctx, `
		UPDATE tasks SET heartbeat_at=now(), updated_at=now()
		WHERE id=$1::uuid AND user_id=$2::uuid`, lease.ID, lease.UserID); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `
		UPDATE operations SET status='running', progress_bps=$3, stage_code=$4,
		    public_message=$5, version=version+1, updated_at=now()
		WHERE id=$1::uuid AND user_id=$2::uuid`,
		lease.OperationID, lease.UserID, progressBPS, stageCode, publicMessage); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func (p *PlanningOperations) Fail(ctx context.Context, lease domain.TaskLease, quality domain.PlanningPlanQualityRecord, code, publicMessage string, retryable bool) (bool, error) {
	tx, err := p.store.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if err := validatePlanningLeaseTx(ctx, tx, lease); err != nil {
		if errors.Is(err, repository.ErrLeaseLost) {
			return false, nil
		}
		return false, err
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO quality_evaluations(id, user_id, subject_type, subject_id, policy_version, decision, reason_codes, internal_scores, evaluator_invocation_id)
		VALUES ($1::uuid,$2::uuid,'plan_set',$3::uuid,$4,$5,$6,$7, NULLIF($8,'')::uuid)`,
		quality.ID, quality.UserID, quality.SubjectID, quality.PolicyVersion, qualityDecisionValue(quality.Decision),
		quality.ReasonCodes, defaultJSONB(quality.InternalScores), quality.EvaluatorInvocationID); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `
		UPDATE tasks SET status='failed', error_class='quality_rejected', error_code=$3,
		    finished_at=now(), updated_at=now()
		WHERE id=$1::uuid AND user_id=$2::uuid`, lease.ID, lease.UserID, code); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `
		UPDATE operations SET status='failed', error_code=$3, public_message=$4, retryable=$5, trace_id=$6,
		    version=version+1, updated_at=now(), finished_at=now()
		WHERE id=$1::uuid AND user_id=$2::uuid`,
		lease.OperationID, lease.UserID, code, publicMessage, retryable, uuid.NewString()); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func (p *PlanningOperations) EnqueueRetry(ctx context.Context, lease domain.TaskLease, command domain.PlanningEnqueueRetryCommand) (domain.CommitOutcome, error) {
	tx, err := p.store.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if err := validatePlanningLeaseTx(ctx, tx, lease); err != nil {
		if errors.Is(err, repository.ErrLeaseLost) {
			return domain.CommitSuperseded, nil
		}
		return "", err
	}
	quality := command.Quality
	if _, err = tx.Exec(ctx, `
		INSERT INTO quality_evaluations(id, user_id, subject_type, subject_id, policy_version, decision, reason_codes, internal_scores, evaluator_invocation_id)
		VALUES ($1::uuid,$2::uuid,'plan_set',$3::uuid,$4,$5,$6,$7, NULLIF($8,'')::uuid)`,
		quality.ID, quality.UserID, quality.SubjectID, quality.PolicyVersion, qualityDecisionValue(quality.Decision),
		quality.ReasonCodes, defaultJSONB(quality.InternalScores), quality.EvaluatorInvocationID); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `
		UPDATE tasks SET status='succeeded', progress_bps=$3, stage_code=$4, finished_at=now(), updated_at=now()
		WHERE id=$1::uuid AND user_id=$2::uuid`, lease.ID, lease.UserID, command.ProgressBPS, command.StageCode); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `
		UPDATE operations SET status='retrying', progress_bps=$3, stage_code=$4, public_message=$5,
		    version=version+1, updated_at=now()
		WHERE id=$1::uuid AND user_id=$2::uuid`,
		command.OperationID, command.UserID, command.ProgressBPS, command.StageCode, command.PublicMessage); err != nil {
		return "", err
	}
	payload, err := json.Marshal(command.Task.Payload)
	if err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO tasks(id, user_id, operation_id, type, subject_type, subject_id, subject_generation,
		                  payload_version, payload, dedupe_key, status, stage_code, max_attempts)
		VALUES (gen_random_uuid(),$1::uuid,$2::uuid,$3,$4,$5::uuid,$6,$7,$8,$9,'queued','',$10)`,
		command.UserID, command.OperationID, string(command.Task.Type), command.Task.SubjectType,
		command.Task.SubjectID, command.Task.SubjectGeneration, command.Task.PayloadVersion,
		payload, command.Task.DedupeKey, p.maxTaskAttempts); err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return domain.CommitApplied, nil
}

func qualityDecisionValue(decision string) string {
	if decision == "" {
		return "error"
	}
	return decision
}

// validatePlanningLeaseTx locks the leased task row and rejects stale or
// superseded leases.
func validatePlanningLeaseTx(ctx context.Context, tx pgx.Tx, lease domain.TaskLease) error {
	var status string
	err := tx.QueryRow(ctx, `
		SELECT status FROM tasks
		WHERE id=$1::uuid AND user_id=$2::uuid AND lease_token=$3::uuid
		  AND lease_owner=$4 AND status='leased' AND lease_expires_at>now()
		FOR UPDATE`, lease.ID, lease.UserID, lease.LeaseToken, lease.LeaseOwner).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return repository.ErrLeaseLost
	}
	return err
}

func defaultJSONB(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return []byte("{}")
	}
	return raw
}
