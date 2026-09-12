package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/zhanshimian/server/internal/domain"
)

type CreateAssessmentParams = domain.CreateAssessmentParams
type CreatedAssessment = domain.CreatedAssessment
type AssessmentReport = domain.AssessmentReport
type PrepareReportParams = domain.PrepareReportParams
type AssessmentRunInput = domain.AssessmentRunInput

type assessmentQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func (s *Store) CreateOrReuseAssessment(ctx context.Context, params CreateAssessmentParams) (CreatedAssessment, error) {
	if params.MaxTaskAttempts <= 0 {
		params.MaxTaskAttempts = 3
	}
	if len(params.ProfileSnapshot) == 0 {
		params.ProfileSnapshot = json.RawMessage(`{}`)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return CreatedAssessment{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "assessment:"+params.UserID+":"+params.AnalysisInputHash); err != nil {
		return CreatedAssessment{}, err
	}

	photoSet, createdSet, err := upsertPhotoSet(ctx, tx, params)
	if err != nil {
		return CreatedAssessment{}, err
	}
	if createdSet {
		if err = insertPhotoSetItems(ctx, tx, params, photoSet.ID); err != nil {
			return CreatedAssessment{}, err
		}
	}

	run, err := getAnalysisRunByHash(ctx, tx, params.UserID, params.AnalysisInputHash)
	if err == nil {
		op, err := scanOperation(tx.QueryRow(ctx, operationSelect+` WHERE user_id=$1::uuid AND id=$2::uuid`, params.UserID, run.OperationID))
		if err != nil {
			return CreatedAssessment{}, err
		}
		task, err := getAssessmentTask(ctx, tx, params.UserID, params.AnalysisInputHash)
		if err != nil {
			return CreatedAssessment{}, err
		}
		photoSet.Items, err = loadPhotoSetItems(ctx, tx, params.UserID, photoSet.ID)
		if err != nil {
			return CreatedAssessment{}, err
		}
		return CreatedAssessment{PhotoSet: photoSet, Run: run, Operation: op, Task: task, Reused: true}, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return CreatedAssessment{}, err
	}

	var opID string
	if err = tx.QueryRow(ctx, `
		INSERT INTO operations(user_id,kind,subject_type,subject_id,status,stage_code)
		VALUES ($1::uuid,'assessment','photo_set',$2::uuid,'accepted','')
		RETURNING id::text`, params.UserID, photoSet.ID).Scan(&opID); err != nil {
		return CreatedAssessment{}, err
	}
	op, err := scanOperation(tx.QueryRow(ctx, operationSelect+` WHERE user_id=$1::uuid AND id=$2::uuid`, params.UserID, opID))
	if err != nil {
		return CreatedAssessment{}, err
	}

	run, err = insertAssessmentRun(ctx, tx, params, photoSet.ID, op.ID)
	if err != nil {
		return CreatedAssessment{}, err
	}

	generation, err := currentProfileGeneration(ctx, tx, params.UserID)
	if err != nil {
		return CreatedAssessment{}, err
	}
	payload, err := json.Marshal(map[string]string{"run_id": run.ID, "photo_set_id": photoSet.ID})
	if err != nil {
		return CreatedAssessment{}, err
	}
	task, err := insertAssessmentTask(ctx, tx, params, op.ID, run.ID, generation, payload)
	if err != nil {
		return CreatedAssessment{}, err
	}

	photoSet.Items, err = loadPhotoSetItems(ctx, tx, params.UserID, photoSet.ID)
	if err != nil {
		return CreatedAssessment{}, err
	}
	return CreatedAssessment{PhotoSet: photoSet, Run: run, Operation: op, Task: task}, tx.Commit(ctx)
}

func (s *Store) GetRunInput(ctx context.Context, userID, runID string) (AssessmentRunInput, error) {
	run, err := scanAnalysisRun(s.pool.QueryRow(ctx, analysisRunSelect+` WHERE user_id=$1::uuid AND id=$2::uuid`, userID, runID))
	if err != nil {
		return AssessmentRunInput{}, mapNotFound(err)
	}
	photoSet, err := loadPhotoSet(ctx, s.pool, userID, run.PhotoSetID)
	if err != nil {
		return AssessmentRunInput{}, mapNotFound(err)
	}
	return AssessmentRunInput{Run: run, PhotoSet: photoSet}, nil
}

func (s *Store) PrepareReport(ctx context.Context, params PrepareReportParams) (string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	run, err := scanAnalysisRun(tx.QueryRow(ctx, analysisRunSelect+` WHERE user_id=$1::uuid AND id=$2::uuid`, params.UserID, params.RunID))
	if err != nil {
		return "", mapNotFound(err)
	}
	if params.Report.UserID != params.UserID || params.Report.UserID != run.UserID {
		return "", fmt.Errorf("report user_id does not match analysis run owner")
	}

	var existing string
	err = tx.QueryRow(ctx, `SELECT id::text FROM reports WHERE user_id=$1::uuid AND content_hash=$2`, params.UserID, params.Report.ContentHash).Scan(&existing)
	if err == nil {
		return existing, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}

	reportID := params.Report.ID
	if reportID == "" {
		reportID = uuid.NewString()
	}
	evalID := params.Quality.ID
	if evalID == "" {
		evalID = uuid.NewString()
	}
	scores := params.Quality.InternalScores
	if len(scores) == 0 {
		scores = json.RawMessage(`{}`)
	}
	reasonCodes := params.Quality.ReasonCodes
	if reasonCodes == nil {
		reasonCodes = []string{}
	}
	var evalInvocation any
	if params.Quality.EvaluatorInvocationID != "" {
		evalInvocation = params.Quality.EvaluatorInvocationID
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO quality_evaluations(
			id,user_id,subject_type,subject_id,policy_version,decision,reason_codes,internal_scores,evaluator_invocation_id
		) VALUES ($1::uuid,$2::uuid,'report',$3::uuid,$4,$5,$6,$7,$8)`,
		evalID, params.UserID, reportID, params.Quality.Policy.Version, string(params.Quality.Decision),
		reasonCodes, scores, evalInvocation); err != nil {
		return "", err
	}

	photoSetID := params.Report.PhotoSetID
	if photoSetID == "" {
		photoSetID = run.PhotoSetID
	}
	snapshot := params.Report.ProfileSnapshot
	if len(snapshot) == 0 {
		snapshot = run.ProfileSnapshot
	}
	tags := params.Report.ImpressionTags
	if tags == nil {
		tags = []string{}
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO reports(
			id,user_id,photo_set_id,profile_snapshot,impression_tags,priority_title,priority_copy,
			hero_asset_id,schema_version,content_hash,provider_invocation_id,quality_evaluation_id
		) VALUES (
			$1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7,$8::uuid,$9,$10,$11::uuid,$12::uuid
		)
		ON CONFLICT (user_id, content_hash) DO NOTHING
		RETURNING id::text`,
		reportID, params.UserID, photoSetID, snapshot, tags, params.Report.PriorityTitle, params.Report.PriorityCopy,
		params.Report.HeroAssetID, params.Report.SchemaVersion, params.Report.ContentHash,
		params.Report.ProviderInvocationID, evalID,
	).Scan(&reportID)
	if errors.Is(err, pgx.ErrNoRows) {
		if err = tx.QueryRow(ctx, `SELECT id::text FROM reports WHERE user_id=$1::uuid AND content_hash=$2`, params.UserID, params.Report.ContentHash).Scan(&reportID); err != nil {
			return "", err
		}
		return reportID, tx.Commit(ctx)
	}
	if err != nil {
		return "", err
	}

	findings := params.Findings
	if len(findings) == 0 {
		findings = params.Report.Findings
	}
	for _, finding := range findings {
		if finding.UserID != "" && finding.UserID != params.UserID {
			return "", fmt.Errorf("finding user_id does not match report owner")
		}
		if _, err = tx.Exec(ctx, `
			INSERT INTO report_findings(
				user_id,report_id,category,priority,label,visible_observation,recommendation,
				source_photo_item_id,anchor_x,anchor_y,anchor_w,anchor_h,confidence,position
			) VALUES ($1::uuid,$2::uuid,$3,$4,$5,$6,$7,$8::uuid,$9,$10,$11,$12,$13,$14)`,
			params.UserID, reportID, finding.Category, finding.Priority, finding.Label,
			finding.VisibleObservation, finding.Recommendation, finding.SourcePhotoItemID,
			finding.Anchor.X, finding.Anchor.Y, finding.Anchor.W, finding.Anchor.H,
			finding.Confidence, finding.Position); err != nil {
			return "", err
		}
	}
	return reportID, tx.Commit(ctx)
}

func (s *Store) CommitAssessment(ctx context.Context, lease domain.TaskLease, result domain.TaskResult) (domain.CommitOutcome, error) {
	if result.Disposition == "" && result.ResultType == "report" {
		result.Disposition = domain.TaskPublish
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var storedGen int64
	var subjectID, operationID string
	err = tx.QueryRow(ctx, `
		SELECT subject_generation, subject_id::text, operation_id::text
		FROM tasks
		WHERE id=$1::uuid AND user_id=$2::uuid AND lease_token=$3::uuid
		  AND lease_owner=$4 AND status='leased' AND lease_expires_at>now()
		FOR UPDATE`, lease.ID, lease.UserID, lease.LeaseToken, lease.LeaseOwner).
		Scan(&storedGen, &subjectID, &operationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CommitSuperseded, nil
	}
	if err != nil {
		return "", err
	}

	currentGen, err := currentProfileGeneration(ctx, tx, lease.UserID)
	if err != nil {
		return "", err
	}
	if storedGen != currentGen {
		if err = supersedeAssessmentTx(ctx, tx, lease.UserID, operationID, lease.ID); err != nil {
			return "", err
		}
		return domain.CommitSuperseded, tx.Commit(ctx)
	}

	if result.Disposition == domain.TaskDomainFail {
		if err = failAssessmentTx(ctx, tx, lease.UserID, subjectID, operationID, lease.ID, result.Failure); err != nil {
			return "", err
		}
		return domain.CommitApplied, tx.Commit(ctx)
	}

	if _, err = tx.Exec(ctx, `
		UPDATE analysis_runs
		SET outcome='published', report_id=$3::uuid, finished_at=now()
		WHERE id=$1::uuid AND user_id=$2::uuid`, subjectID, lease.UserID, result.ResultID); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO user_profiles(user_id, current_report_id)
		VALUES ($1::uuid, $2::uuid)
		ON CONFLICT (user_id) DO UPDATE SET
			current_report_id = EXCLUDED.current_report_id,
			updated_at = now()
		WHERE user_profiles.current_report_id IS NULL
		   OR EXISTS (
			SELECT 1
			FROM reports cur, reports incoming
			WHERE cur.id = user_profiles.current_report_id
			  AND incoming.id = EXCLUDED.current_report_id
			  AND (cur.created_at, cur.id) < (incoming.created_at, incoming.id)
		   )`, lease.UserID, result.ResultID); err != nil {
		return "", err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE tasks
		SET status='succeeded', lease_token=NULL, lease_owner=NULL, lease_expires_at=NULL,
		    progress_bps=10000, finished_at=now(), updated_at=now()
		WHERE id=$1::uuid AND user_id=$2::uuid AND lease_token=$3::uuid
		  AND lease_owner=$4 AND status='leased' AND lease_expires_at>now()`,
		lease.ID, lease.UserID, lease.LeaseToken, lease.LeaseOwner)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() != 1 {
		return domain.CommitSuperseded, nil
	}
	if _, err = tx.Exec(ctx, `
		UPDATE operations
		SET status='succeeded', result_type='report', result_id=$3::uuid,
		    progress_bps=10000, finished_at=now(), updated_at=now(), version=version+1
		WHERE id=$1::uuid AND user_id=$2::uuid`, operationID, lease.UserID, result.ResultID); err != nil {
		return "", err
	}
	return domain.CommitApplied, tx.Commit(ctx)
}

func (s *Store) FinishRunFailure(ctx context.Context, userID, runID string, outcome domain.AnalysisOutcome, failure domain.TaskFailure, traceID string) error {
	if outcome == "" {
		outcome = domain.AnalysisOutcomeFailed
	}
	if traceID == "" {
		traceID = uuid.NewString()
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var opID string
	err = tx.QueryRow(ctx, `
		UPDATE analysis_runs
		SET outcome=$3, finished_at=now()
		WHERE id=$1::uuid AND user_id=$2::uuid
		RETURNING operation_id::text`, runID, userID, string(outcome)).Scan(&opID)
	if err != nil {
		return mapNotFound(err)
	}
	if _, err = tx.Exec(ctx, `
		UPDATE operations
		SET status='failed', error_code=$3, trace_id=$4, retryable=false,
		    finished_at=now(), updated_at=now(), version=version+1
		WHERE id=$1::uuid AND user_id=$2::uuid`, opID, userID, nullIfEmpty(failure.Code), traceID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `
		UPDATE tasks
		SET status='failed', error_class=NULLIF($3,''), error_code=NULLIF($4,''),
		    lease_token=NULL, lease_owner=NULL, lease_expires_at=NULL,
		    finished_at=now(), updated_at=now()
		WHERE user_id=$1::uuid AND operation_id=$2::uuid
		  AND status NOT IN ('succeeded','failed','cancelled','superseded')`,
		userID, opID, string(failure.Class), failure.Code); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) GetReport(ctx context.Context, userID, reportID string) (AssessmentReport, error) {
	return s.loadPublishedReport(ctx, `
		SELECT `+publishedReportColumns+`
		FROM reports r
		JOIN analysis_runs ar ON ar.user_id=r.user_id AND ar.report_id=r.id AND ar.outcome='published'
		WHERE r.user_id=$1::uuid AND r.id=$2::uuid`, userID, reportID)
}

func (s *Store) GetCurrentReport(ctx context.Context, userID string) (AssessmentReport, error) {
	return s.loadPublishedReport(ctx, `
		SELECT `+publishedReportColumns+`
		FROM user_profiles p
		JOIN reports r ON r.id=p.current_report_id AND r.user_id=p.user_id
		JOIN analysis_runs ar ON ar.user_id=r.user_id AND ar.report_id=r.id AND ar.outcome='published'
		WHERE p.user_id=$1::uuid`, userID)
}

const publishedReportColumns = `
	r.id::text, r.user_id::text, r.photo_set_id::text, r.hero_asset_id::text, r.schema_version, r.content_hash,
	r.priority_title, r.priority_copy, r.impression_tags, r.profile_snapshot,
	r.provider_invocation_id::text, r.quality_evaluation_id::text, r.created_at`

const analysisRunSelect = `
	SELECT id::text, user_id::text, photo_set_id::text, operation_id::text, input_hash,
	       analyzer_schema_version, quality_policy_version, profile_snapshot,
	       provider_invocation_id::text, report_id::text, outcome, created_at, finished_at
	FROM analysis_runs`

const assessmentTaskSelect = `
	SELECT id::text, user_id::text, operation_id::text, type, subject_type, subject_id::text,
	       subject_generation, payload_version, payload, dedupe_key, status, priority,
	       attempt, max_attempts, available_at, progress_bps, stage_code,
	       error_class, error_code, created_at, updated_at, finished_at
	FROM tasks`

func (s *Store) loadPublishedReport(ctx context.Context, query string, args ...any) (AssessmentReport, error) {
	report, err := scanPublishedReport(s.pool.QueryRow(ctx, query, args...))
	if err != nil {
		return AssessmentReport{}, mapNotFound(err)
	}
	findings, err := loadReportFindings(ctx, s.pool, report.UserID, report.ID)
	if err != nil {
		return AssessmentReport{}, err
	}
	report.Findings = findings
	photoSet, err := loadPhotoSet(ctx, s.pool, report.UserID, report.PhotoSetID)
	if err != nil {
		return AssessmentReport{}, err
	}
	return AssessmentReport{Report: report, PhotoSet: photoSet}, nil
}

func upsertPhotoSet(ctx context.Context, tx pgx.Tx, params CreateAssessmentParams) (domain.PhotoSet, bool, error) {
	var set domain.PhotoSet
	err := tx.QueryRow(ctx, `
		INSERT INTO photo_sets(user_id, profile_snapshot, content_hash, schema_version)
		VALUES ($1::uuid, $2, $3, $4)
		ON CONFLICT (user_id, content_hash) DO NOTHING
		RETURNING id::text, user_id::text, content_hash, schema_version, profile_snapshot, created_at`,
		params.UserID, params.ProfileSnapshot, params.PhotoSetContentHash, params.PhotoSetSchemaVersion,
	).Scan(&set.ID, &set.UserID, &set.ContentHash, &set.SchemaVersion, &set.ProfileSnapshot, &set.CreatedAt)
	if err == nil {
		return set, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.PhotoSet{}, false, err
	}
	err = tx.QueryRow(ctx, `
		SELECT id::text, user_id::text, content_hash, schema_version, profile_snapshot, created_at
		FROM photo_sets WHERE user_id=$1::uuid AND content_hash=$2`,
		params.UserID, params.PhotoSetContentHash,
	).Scan(&set.ID, &set.UserID, &set.ContentHash, &set.SchemaVersion, &set.ProfileSnapshot, &set.CreatedAt)
	return set, false, err
}

func insertPhotoSetItems(ctx context.Context, tx pgx.Tx, params CreateAssessmentParams, photoSetID string) error {
	slots := []struct {
		role    domain.PhotoRole
		assetID string
	}{
		{domain.PhotoRoleFace, params.Slots.FaceAssetID},
		{domain.PhotoRoleSide, params.Slots.SideAssetID},
		{domain.PhotoRoleBody, params.Slots.BodyAssetID},
	}
	for _, slot := range slots {
		assetID := slot.assetID
		if assetID == "" && params.Assets != nil {
			assetID = params.Assets[slot.role].ID
		}
		itemID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(photoSetID+":"+string(slot.role))).String()
		if _, err := tx.Exec(ctx, `
			INSERT INTO photo_set_items(id,user_id,photo_set_id,role,media_asset_id)
			VALUES ($1::uuid,$2::uuid,$3::uuid,$4,$5::uuid)`,
			itemID, params.UserID, photoSetID, string(slot.role), assetID); err != nil {
			return err
		}
	}
	return nil
}

func insertAssessmentRun(ctx context.Context, tx pgx.Tx, params CreateAssessmentParams, photoSetID, operationID string) (domain.AnalysisRun, error) {
	return scanAnalysisRun(tx.QueryRow(ctx, `
		INSERT INTO analysis_runs(
			user_id,photo_set_id,operation_id,input_hash,profile_snapshot,
			analyzer_schema_version,quality_policy_version
		) VALUES ($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7)
		RETURNING id::text, user_id::text, photo_set_id::text, operation_id::text, input_hash,
		          analyzer_schema_version, quality_policy_version, profile_snapshot,
		          provider_invocation_id::text, report_id::text, outcome, created_at, finished_at`,
		params.UserID, photoSetID, operationID, params.AnalysisInputHash, params.ProfileSnapshot,
		params.AnalyzerSchemaVersion, params.QualityPolicyVersion))
}

func insertAssessmentTask(ctx context.Context, tx pgx.Tx, params CreateAssessmentParams, operationID, runID string, generation int64, payload []byte) (domain.Task, error) {
	return scanAssessmentTask(tx.QueryRow(ctx, `
		INSERT INTO tasks(
			user_id,operation_id,type,subject_type,subject_id,subject_generation,
			payload_version,payload,dedupe_key,status,max_attempts,stage_code
		) VALUES (
			$1::uuid,$2::uuid,'assessment','analysis_run',$3::uuid,$4,
			1,$5,$6,'queued',$7,''
		) RETURNING`+assessmentTaskReturning,
		params.UserID, operationID, runID, generation, payload, "assessment:"+params.AnalysisInputHash, params.MaxTaskAttempts))
}

const assessmentTaskReturning = `
	id::text, user_id::text, operation_id::text, type, subject_type, subject_id::text,
	subject_generation, payload_version, payload, dedupe_key, status, priority,
	attempt, max_attempts, available_at, progress_bps, stage_code,
	error_class, error_code, created_at, updated_at, finished_at`

func getAnalysisRunByHash(ctx context.Context, q assessmentQuerier, userID, inputHash string) (domain.AnalysisRun, error) {
	return scanAnalysisRun(q.QueryRow(ctx, analysisRunSelect+` WHERE user_id=$1::uuid AND input_hash=$2`, userID, inputHash))
}

func getAssessmentTask(ctx context.Context, q assessmentQuerier, userID, inputHash string) (domain.Task, error) {
	return scanAssessmentTask(q.QueryRow(ctx, assessmentTaskSelect+` WHERE user_id=$1::uuid AND dedupe_key=$2`, userID, "assessment:"+inputHash))
}

func currentProfileGeneration(ctx context.Context, q assessmentQuerier, userID string) (int64, error) {
	var generation int64
	err := q.QueryRow(ctx, `SELECT version FROM user_profiles WHERE user_id=$1::uuid`, userID).Scan(&generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return 1, nil
	}
	return generation, err
}

func loadPhotoSet(ctx context.Context, q assessmentQuerier, userID, photoSetID string) (domain.PhotoSet, error) {
	var set domain.PhotoSet
	err := q.QueryRow(ctx, `
		SELECT id::text, user_id::text, content_hash, schema_version, profile_snapshot, created_at
		FROM photo_sets WHERE user_id=$1::uuid AND id=$2::uuid`, userID, photoSetID).
		Scan(&set.ID, &set.UserID, &set.ContentHash, &set.SchemaVersion, &set.ProfileSnapshot, &set.CreatedAt)
	if err != nil {
		return domain.PhotoSet{}, err
	}
	set.Items, err = loadPhotoSetItems(ctx, q, userID, photoSetID)
	return set, err
}

func loadPhotoSetItems(ctx context.Context, q assessmentQuerier, userID, photoSetID string) ([]domain.PhotoSetItem, error) {
	rows, err := q.Query(ctx, `
		SELECT i.id::text, i.user_id::text, i.photo_set_id::text, i.media_asset_id::text, i.role,
		       m.id::text, m.user_id::text, m.origin, m.purpose, m.object_key, m.sha256, m.mime_type,
		       m.byte_size, m.width, m.height, m.state, m.display_kind, m.provider_invocation_id::text,
		       m.created_at, m.deleted_at
		FROM photo_set_items i
		JOIN media_assets m ON m.id=i.media_asset_id AND m.user_id=i.user_id AND m.deleted_at IS NULL
		WHERE i.user_id=$1::uuid AND i.photo_set_id=$2::uuid
		ORDER BY CASE i.role WHEN 'face' THEN 1 WHEN 'side' THEN 2 ELSE 3 END`, userID, photoSetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.PhotoSetItem, 0, 3)
	for rows.Next() {
		var item domain.PhotoSetItem
		var width, height *int
		var invocationID *string
		if err := rows.Scan(
			&item.ID, &item.UserID, &item.PhotoSetID, &item.MediaAssetID, &item.Role,
			&item.Asset.ID, &item.Asset.UserID, &item.Asset.Origin, &item.Asset.Purpose, &item.Asset.ObjectKey,
			&item.Asset.SHA256, &item.Asset.MIMEType, &item.Asset.ByteSize, &width, &height,
			&item.Asset.State, &item.Asset.DisplayKind, &invocationID, &item.Asset.CreatedAt, &item.Asset.DeletedAt,
		); err != nil {
			return nil, err
		}
		if width != nil {
			item.Asset.Width = *width
		}
		if height != nil {
			item.Asset.Height = *height
		}
		if invocationID != nil {
			item.Asset.ProviderInvocationID = *invocationID
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadReportFindings(ctx context.Context, q assessmentQuerier, userID, reportID string) ([]domain.ReportFinding, error) {
	rows, err := q.Query(ctx, `
		SELECT id::text, user_id::text, report_id::text, label, visible_observation, recommendation,
		       source_photo_item_id::text, category, priority, anchor_x, anchor_y, anchor_w, anchor_h,
		       confidence, position
		FROM report_findings
		WHERE user_id=$1::uuid AND report_id=$2::uuid
		ORDER BY position`, userID, reportID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	findings := make([]domain.ReportFinding, 0)
	for rows.Next() {
		var finding domain.ReportFinding
		if err := rows.Scan(
			&finding.ID, &finding.UserID, &finding.ReportID, &finding.Label, &finding.VisibleObservation,
			&finding.Recommendation, &finding.SourcePhotoItemID, &finding.Category, &finding.Priority,
			&finding.Anchor.X, &finding.Anchor.Y, &finding.Anchor.W, &finding.Anchor.H,
			&finding.Confidence, &finding.Position,
		); err != nil {
			return nil, err
		}
		findings = append(findings, finding)
	}
	return findings, rows.Err()
}

func scanPublishedReport(row pgx.Row) (domain.Report, error) {
	var report domain.Report
	err := row.Scan(
		&report.ID, &report.UserID, &report.PhotoSetID, &report.HeroAssetID, &report.SchemaVersion, &report.ContentHash,
		&report.PriorityTitle, &report.PriorityCopy, &report.ImpressionTags, &report.ProfileSnapshot,
		&report.ProviderInvocationID, &report.QualityEvaluationID, &report.CreatedAt,
	)
	return report, err
}

func scanAnalysisRun(row pgx.Row) (domain.AnalysisRun, error) {
	var run domain.AnalysisRun
	var invocationID, reportID, outcome *string
	err := row.Scan(
		&run.ID, &run.UserID, &run.PhotoSetID, &run.OperationID, &run.InputHash,
		&run.AnalyzerSchemaVersion, &run.QualityPolicyVersion, &run.ProfileSnapshot,
		&invocationID, &reportID, &outcome, &run.CreatedAt, &run.FinishedAt,
	)
	if invocationID != nil {
		run.ProviderInvocationID = *invocationID
	}
	if reportID != nil {
		run.ReportID = *reportID
	}
	if outcome != nil {
		value := domain.AnalysisOutcome(*outcome)
		run.Outcome = &value
	}
	return run, err
}

func scanAssessmentTask(row pgx.Row) (domain.Task, error) {
	var task domain.Task
	var errorClass, errorCode *string
	err := row.Scan(
		&task.ID, &task.UserID, &task.OperationID, &task.Type, &task.SubjectType, &task.SubjectID,
		&task.SubjectGeneration, &task.PayloadVersion, &task.Payload, &task.DedupeKey, &task.Status, &task.Priority,
		&task.Attempt, &task.MaxAttempts, &task.AvailableAt, &task.ProgressBPS, &task.StageCode,
		&errorClass, &errorCode, &task.CreatedAt, &task.UpdatedAt, &task.FinishedAt,
	)
	if errorClass != nil {
		task.ErrorClass = domain.ErrorClass(*errorClass)
	}
	if errorCode != nil {
		task.ErrorCode = *errorCode
	}
	return task, err
}

func supersedeAssessmentTx(ctx context.Context, tx pgx.Tx, userID, operationID, taskID string) error {
	if _, err := tx.Exec(ctx, `
		UPDATE tasks
		SET status='superseded', lease_token=NULL, lease_owner=NULL, lease_expires_at=NULL,
		    progress_bps=10000, finished_at=now(), updated_at=now()
		WHERE id=$1::uuid AND user_id=$2::uuid AND status='leased'`, taskID, userID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		UPDATE operations
		SET status='superseded', finished_at=now(), updated_at=now(), version=version+1
		WHERE id=$1::uuid AND user_id=$2::uuid`, operationID, userID)
	return err
}

func failAssessmentTx(ctx context.Context, tx pgx.Tx, userID, runID, operationID, taskID string, failure *domain.TaskFailure) error {
	code := ""
	class := ""
	runOutcome := domain.AnalysisOutcomeFailed
	if failure != nil {
		code = failure.Code
		class = string(failure.Class)
		if failure.Class == domain.ErrorQualityRejected {
			runOutcome = domain.AnalysisOutcomeRejected
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE analysis_runs SET outcome=$3, finished_at=now()
		WHERE id=$1::uuid AND user_id=$2::uuid`, runID, userID, string(runOutcome)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE operations
		SET status='failed', error_code=$3, trace_id=COALESCE(trace_id, $4), retryable=false,
		    finished_at=now(), updated_at=now(), version=version+1
		WHERE id=$1::uuid AND user_id=$2::uuid`, operationID, userID, nullIfEmpty(code), uuid.NewString()); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		UPDATE tasks
		SET status='failed', error_class=NULLIF($3,''), error_code=NULLIF($4,''),
		    lease_token=NULL, lease_owner=NULL, lease_expires_at=NULL,
		    finished_at=now(), updated_at=now()
		WHERE id=$1::uuid AND user_id=$2::uuid`, taskID, userID, class, code)
	return err
}
