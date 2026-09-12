package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/testutil"
)

var ctx = context.Background()

func TestCreateOrReuseAssessmentReturnsSameRunAndOperation(t *testing.T) {
	store, fixture := newAssessmentStore(t)
	first, err := store.CreateOrReuseAssessment(ctx, fixture.createParams())
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	second, err := store.CreateOrReuseAssessment(ctx, fixture.createParams())
	if err != nil {
		t.Fatalf("second create: %v", err)
	}
	if first.Run.ID != second.Run.ID {
		t.Fatalf("run id = %s, want %s", second.Run.ID, first.Run.ID)
	}
	if first.Operation.ID != second.Operation.ID {
		t.Fatalf("operation id = %s, want %s", second.Operation.ID, first.Operation.ID)
	}
	if first.Task.ID != second.Task.ID {
		t.Fatalf("task id = %s, want %s", second.Task.ID, first.Task.ID)
	}
	if got := countRows(t, store.pool, "analysis_runs"); got != 1 {
		t.Fatalf("analysis_runs = %d, want 1", got)
	}
}

func TestPrepareAndCommitReportAreTenantSafeAndLeaseGuarded(t *testing.T) {
	store, fixture := newAssessmentStore(t)
	created, err := store.CreateOrReuseAssessment(ctx, fixture.createParams())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	input := validPublishParams(store, created, fixture)
	input.Report.UserID = fixture.otherUserID
	if _, err := store.PrepareReport(ctx, input); err == nil {
		t.Fatal("expected foreign UserID PrepareReport to fail")
	}
	if got := countRows(t, store.pool, "reports"); got != 0 {
		t.Fatalf("reports after foreign prepare = %d, want 0", got)
	}
	if id := currentReportID(t, store.pool, fixture.userID); id != nil {
		t.Fatalf("current_report_id = %s, want nil", *id)
	}

	input = validPublishParams(store, created, fixture)
	reportID, err := store.PrepareReport(ctx, input)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if _, err := store.GetReport(ctx, fixture.userID, reportID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("unpublished GetReport error = %v, want ErrNotFound", err)
	}
	lease := claimAssessmentTask(t, store, "worker-1")
	outcome, err := store.CommitAssessment(ctx, lease, domain.TaskResult{ResultType: "report", ResultID: reportID})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if outcome != domain.CommitApplied {
		t.Fatalf("outcome = %s, want %s", outcome, domain.CommitApplied)
	}
	got := currentReportID(t, store.pool, fixture.userID)
	if got == nil || *got != reportID {
		t.Fatalf("current_report_id = %v, want %s", got, reportID)
	}
}

func TestGetReportUsesUserIDAndReturnsEvidenceAssets(t *testing.T) {
	store, fixture := publishedAssessmentStore(t)
	if _, err := store.GetReport(ctx, fixture.otherUserID, fixture.reportID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("cross-user GetReport error = %v, want ErrNotFound", err)
	}
	got, err := store.GetReport(ctx, fixture.userID, fixture.reportID)
	if err != nil {
		t.Fatalf("GetReport: %v", err)
	}
	if len(got.Findings) != 3 {
		t.Fatalf("findings = %d, want 3", len(got.Findings))
	}
	if len(got.PhotoSet.Items) != 3 {
		t.Fatalf("photo set items = %d, want 3", len(got.PhotoSet.Items))
	}
}

func TestCreateOrReuseAssessmentHidesOtherUsersRow(t *testing.T) {
	store, fixture := newAssessmentStore(t)
	first, err := store.CreateOrReuseAssessment(ctx, fixture.createParams())
	if err != nil {
		t.Fatalf("owner create: %v", err)
	}
	other := *fixture
	other.userID = fixture.otherUserID
	other.assets = seedAssessmentAssets(t, store, fixture.otherUserID)
	other.slots = slotsFromAssets(other.assets)
	second, err := store.CreateOrReuseAssessment(ctx, other.createParams())
	if err != nil {
		t.Fatalf("other-user create: %v", err)
	}
	if second.Run.ID == first.Run.ID || second.PhotoSet.ID == first.PhotoSet.ID {
		t.Fatal("cross-user create reused the other tenant's row")
	}
	if got := countRows(t, store.pool, "analysis_runs"); got != 2 {
		t.Fatalf("analysis_runs = %d, want 2", got)
	}
}

func TestGetCurrentReportRequiresPublishedPointer(t *testing.T) {
	store, fixture := newAssessmentStore(t)
	if _, err := store.GetCurrentReport(ctx, fixture.userID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("empty current report error = %v, want ErrNotFound", err)
	}
	store, fixture = publishedAssessmentStore(t)
	if _, err := store.GetCurrentReport(ctx, fixture.otherUserID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("cross-user GetCurrentReport error = %v, want ErrNotFound", err)
	}
	got, err := store.GetCurrentReport(ctx, fixture.userID)
	if err != nil {
		t.Fatalf("GetCurrentReport: %v", err)
	}
	if got.ID != fixture.reportID {
		t.Fatalf("current report id = %s, want %s", got.ID, fixture.reportID)
	}
	if len(got.PhotoSet.Items) != 3 {
		t.Fatalf("current photo set items = %d, want 3", len(got.PhotoSet.Items))
	}
}

func TestCommitAssessmentStaleLeaseLeavesPointerUnchanged(t *testing.T) {
	store, fixture := newAssessmentStore(t)
	created, err := store.CreateOrReuseAssessment(ctx, fixture.createParams())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	reportID, err := store.PrepareReport(ctx, validPublishParams(store, created, fixture))
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	lease := claimAssessmentTask(t, store, "worker-stale")
	if _, err := store.pool.Exec(ctx, `UPDATE tasks SET lease_expires_at=now() - interval '1 second' WHERE id=$1::uuid`, lease.ID); err != nil {
		t.Fatalf("expire lease: %v", err)
	}
	outcome, err := store.CommitAssessment(ctx, lease, domain.TaskResult{
		Disposition: domain.TaskPublish, ResultType: "report", ResultID: reportID,
	})
	if err != nil && !errors.Is(err, repository.ErrLeaseLost) {
		t.Fatalf("stale commit error = %v", err)
	}
	if err == nil && outcome != domain.CommitSuperseded {
		t.Fatalf("stale outcome = %s, want superseded or ErrLeaseLost", outcome)
	}
	if id := currentReportID(t, store.pool, fixture.userID); id != nil {
		t.Fatalf("stale commit moved current_report_id to %s", *id)
	}
	var outcomeText *string
	if err := store.pool.QueryRow(ctx, `SELECT outcome FROM analysis_runs WHERE id=$1::uuid`, created.Run.ID).Scan(&outcomeText); err != nil {
		t.Fatalf("load run: %v", err)
	}
	if outcomeText != nil {
		t.Fatalf("stale commit published run outcome = %s", *outcomeText)
	}
}

func TestCommitAssessmentPublishesNewerReportAtSameGeneration(t *testing.T) {
	store, fixture := newAssessmentStore(t)
	first, err := store.CreateOrReuseAssessment(ctx, fixture.createParams())
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	secondParams := fixture.createParams()
	secondParams.AnalysisInputHash = strings.Repeat("11", 32)
	secondParams.PhotoSetContentHash = strings.Repeat("22", 32)
	second, err := store.CreateOrReuseAssessment(ctx, secondParams)
	if err != nil {
		t.Fatalf("second create: %v", err)
	}
	if first.Task.SubjectGeneration != second.Task.SubjectGeneration {
		t.Fatalf("subject_generation first=%d second=%d, want same profile version",
			first.Task.SubjectGeneration, second.Task.SubjectGeneration)
	}

	firstPrepare := validPublishParams(store, first, fixture)
	firstPrepare.Report.ContentHash = strings.Repeat("a1", 32)
	firstReportID, err := store.PrepareReport(ctx, firstPrepare)
	if err != nil {
		t.Fatalf("prepare first: %v", err)
	}
	secondPrepare := validPublishParams(store, second, fixture)
	secondPrepare.Report.ContentHash = strings.Repeat("b2", 32)
	secondReportID, err := store.PrepareReport(ctx, secondPrepare)
	if err != nil {
		t.Fatalf("prepare second: %v", err)
	}

	firstLease := claimAssessmentTask(t, store, "worker-older")
	if firstLease.ID != first.Task.ID {
		t.Fatalf("claimed %s, want first task %s", firstLease.ID, first.Task.ID)
	}
	firstOutcome, err := store.CommitAssessment(ctx, firstLease, domain.TaskResult{
		Disposition: domain.TaskPublish, ResultType: "report", ResultID: firstReportID,
	})
	if err != nil {
		t.Fatalf("commit first: %v", err)
	}
	if firstOutcome != domain.CommitApplied {
		t.Fatalf("first outcome = %s, want %s", firstOutcome, domain.CommitApplied)
	}

	secondLease := claimAssessmentTask(t, store, "worker-newer")
	if secondLease.ID != second.Task.ID {
		t.Fatalf("claimed %s, want second task %s", secondLease.ID, second.Task.ID)
	}
	secondOutcome, err := store.CommitAssessment(ctx, secondLease, domain.TaskResult{
		Disposition: domain.TaskPublish, ResultType: "report", ResultID: secondReportID,
	})
	if err != nil {
		t.Fatalf("commit second: %v", err)
	}
	if secondOutcome != domain.CommitApplied {
		t.Fatalf("second outcome = %s, want %s", secondOutcome, domain.CommitApplied)
	}

	assertRunPublished(t, store.pool, first.Run.ID, firstReportID)
	assertRunPublished(t, store.pool, second.Run.ID, secondReportID)
	wantPointer := newerReportID(t, store.pool, firstReportID, secondReportID)
	gotPointer := currentReportID(t, store.pool, fixture.userID)
	if gotPointer == nil || *gotPointer != wantPointer {
		t.Fatalf("current_report_id = %v, want newer report %s", gotPointer, wantPointer)
	}
	var version int64
	if err := store.pool.QueryRow(ctx, `SELECT version FROM user_profiles WHERE user_id=$1::uuid`, fixture.userID).Scan(&version); err != nil {
		t.Fatalf("profile version: %v", err)
	}
	if version != first.Task.SubjectGeneration {
		t.Fatalf("profile version = %d, want unchanged subject_generation %d", version, first.Task.SubjectGeneration)
	}
}

func TestCommitAssessmentQualityRejectedWritesRunRejected(t *testing.T) {
	store, fixture := newAssessmentStore(t)
	created, err := store.CreateOrReuseAssessment(ctx, fixture.createParams())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	lease := claimAssessmentTask(t, store, "worker-1")
	outcome, err := store.CommitAssessment(ctx, lease, domain.TaskResult{
		Disposition: domain.TaskDomainFail,
		Failure:     &domain.TaskFailure{Class: domain.ErrorQualityRejected, Code: "photo_identity_uncertain"},
	})
	if err != nil {
		t.Fatalf("CommitAssessment: %v", err)
	}
	if outcome != domain.CommitApplied {
		t.Fatalf("outcome = %s, want %s", outcome, domain.CommitApplied)
	}
	assertAssessmentDomainFail(t, store, created, domain.AnalysisOutcomeRejected, domain.ErrorQualityRejected, "photo_identity_uncertain")
}

func TestCommitAssessmentPermanentDomainFailWritesRunFailed(t *testing.T) {
	store, fixture := newAssessmentStore(t)
	created, err := store.CreateOrReuseAssessment(ctx, fixture.createParams())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	lease := claimAssessmentTask(t, store, "worker-1")
	outcome, err := store.CommitAssessment(ctx, lease, domain.TaskResult{
		Disposition: domain.TaskDomainFail,
		Failure:     &domain.TaskFailure{Class: domain.ErrorPermanent, Code: "provider_failed"},
	})
	if err != nil {
		t.Fatalf("CommitAssessment: %v", err)
	}
	if outcome != domain.CommitApplied {
		t.Fatalf("outcome = %s, want %s", outcome, domain.CommitApplied)
	}
	assertAssessmentDomainFail(t, store, created, domain.AnalysisOutcomeFailed, domain.ErrorPermanent, "provider_failed")
}

func TestFinishRunFailureDoesNotPublish(t *testing.T) {
	store, fixture := newAssessmentStore(t)
	created, err := store.CreateOrReuseAssessment(ctx, fixture.createParams())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := store.FinishRunFailure(ctx, created.Run.UserID, created.Run.ID, domain.AnalysisOutcomeFailed, domain.TaskFailure{
		Class: domain.ErrorPermanent, Code: "provider_failed",
	}, "trace-assessment-fail"); err != nil {
		t.Fatalf("FinishRunFailure: %v", err)
	}
	if id := currentReportID(t, store.pool, fixture.userID); id != nil {
		t.Fatalf("failure published current_report_id = %s", *id)
	}
	if _, err := store.GetReport(ctx, fixture.userID, uuid.NewString()); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("GetReport after failure = %v, want ErrNotFound", err)
	}
	var outcome string
	if err := store.pool.QueryRow(ctx, `SELECT outcome FROM analysis_runs WHERE id=$1::uuid`, created.Run.ID).Scan(&outcome); err != nil {
		t.Fatalf("load run outcome: %v", err)
	}
	if outcome != string(domain.AnalysisOutcomeFailed) {
		t.Fatalf("run outcome = %s, want failed", outcome)
	}
}

func TestGetRunInputUsesUserID(t *testing.T) {
	store, fixture := newAssessmentStore(t)
	created, err := store.CreateOrReuseAssessment(ctx, fixture.createParams())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := store.GetRunInput(ctx, fixture.otherUserID, created.Run.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("cross-user GetRunInput error = %v, want ErrNotFound", err)
	}
	got, err := store.GetRunInput(ctx, fixture.userID, created.Run.ID)
	if err != nil {
		t.Fatalf("GetRunInput: %v", err)
	}
	if got.Run.ID != created.Run.ID {
		t.Fatalf("run id = %s, want %s", got.Run.ID, created.Run.ID)
	}
	if len(got.PhotoSet.Items) != 3 {
		t.Fatalf("run photo items = %d, want 3", len(got.PhotoSet.Items))
	}
}

type assessmentFixture struct {
	userID, otherUserID string
	slots               domain.PhotoSlots
	assets              map[domain.PhotoRole]domain.MediaAsset
	contentHash         string
	inputHash           string
	profile             json.RawMessage
	reportID            string
}

func newAssessmentStore(t *testing.T) (*Store, *assessmentFixture) {
	t.Helper()
	store := New(testutil.NewPostgres(t))
	userID := insertUser(t, store)
	otherUserID := insertUser(t, store)
	assets := seedAssessmentAssets(t, store, userID)
	profile := json.RawMessage(`{"role":"daily"}`)
	contentHash := strings.Repeat("ab", 32)
	inputHash := strings.Repeat("cd", 32)
	return store, &assessmentFixture{
		userID:      userID,
		otherUserID: otherUserID,
		slots:       slotsFromAssets(assets),
		assets:      assets,
		contentHash: contentHash,
		inputHash:   inputHash,
		profile:     profile,
	}
}

func publishedAssessmentStore(t *testing.T) (*Store, *assessmentFixture) {
	t.Helper()
	store, fixture := newAssessmentStore(t)
	created, err := store.CreateOrReuseAssessment(ctx, fixture.createParams())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	reportID, err := store.PrepareReport(ctx, validPublishParams(store, created, fixture))
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	lease := claimAssessmentTask(t, store, "worker-1")
	outcome, err := store.CommitAssessment(ctx, lease, domain.TaskResult{
		Disposition: domain.TaskPublish, ResultType: "report", ResultID: reportID,
	})
	if err != nil || outcome != domain.CommitApplied {
		t.Fatalf("commit: outcome=%s err=%v", outcome, err)
	}
	fixture.reportID = reportID
	return store, fixture
}

func (f *assessmentFixture) createParams() CreateAssessmentParams {
	return CreateAssessmentParams{
		UserID:                f.userID,
		PhotoSetSchemaVersion: "v1",
		PhotoSetContentHash:   f.contentHash,
		AnalysisInputHash:     f.inputHash,
		ProfileSnapshot:       f.profile,
		Slots:                 f.slots,
		Assets:                f.assets,
		AnalyzerSchemaVersion: "analyzer-v1",
		QualityPolicyVersion:  "quality-v1",
		MaxTaskAttempts:       3,
	}
}

func validPublishParams(store *Store, created CreatedAssessment, fixture *assessmentFixture) PrepareReportParams {
	items := created.PhotoSet.Items
	findings := []domain.ReportFinding{
		publishedFinding(fixture.userID, items[0].ID, "hair", 1),
		publishedFinding(fixture.userID, items[1].ID, "makeup", 2),
		publishedFinding(fixture.userID, items[2].ID, "outfit", 3),
	}
	invocationID := insertAssessmentInvocation(store.pool, created)
	reportID := uuid.NewString()
	return PrepareReportParams{
		RunID:  created.Run.ID,
		UserID: fixture.userID,
		Report: domain.Report{
			ID:                   reportID,
			UserID:               fixture.userID,
			PhotoSetID:           created.PhotoSet.ID,
			HeroAssetID:          fixture.slots.BodyAssetID,
			SchemaVersion:        "report-v1",
			ContentHash:          strings.Repeat("ef", 32),
			PriorityTitle:        "先整理发型轮廓",
			PriorityCopy:         "从可见的发缝和衣领开始调整。",
			ImpressionTags:       []string{"干净", "利落"},
			ProfileSnapshot:      fixture.profile,
			ProviderInvocationID: invocationID,
			QualityEvaluationID:  uuid.NewString(),
			Findings:             findings,
		},
		Quality: domain.QualityEvaluation{
			ID:          uuid.NewString(),
			UserID:      fixture.userID,
			SubjectType: domain.QualitySubjectReport,
			SubjectID:   reportID,
			Policy:      domain.QualityPolicyRef{Version: "quality-v1"},
			Decision:    domain.QualityDecisionPass,
			ReasonCodes: []string{},
		},
		Findings: findings,
	}
}

func publishedFinding(userID, itemID, category string, position int) domain.ReportFinding {
	return domain.ReportFinding{
		UserID:             userID,
		Category:           category,
		Priority:           1,
		Label:              category + " visible",
		VisibleObservation: "A visible " + category + " detail.",
		Recommendation:     "Adjust the " + category + " next.",
		SourcePhotoItemID:  itemID,
		Anchor:             domain.EvidenceAnchor{X: 0.1, Y: 0.1, W: 0.2, H: 0.2},
		Confidence:         0.96,
		Position:           position,
	}
}

func assertAssessmentDomainFail(t *testing.T, store *Store, created CreatedAssessment, wantOutcome domain.AnalysisOutcome, wantClass domain.ErrorClass, wantCode string) {
	t.Helper()
	if id := currentReportID(t, store.pool, created.Run.UserID); id != nil {
		t.Fatalf("domain fail published current_report_id = %s", *id)
	}
	var outcome, opStatus, taskStatus, errorClass, errorCode string
	if err := store.pool.QueryRow(ctx, `SELECT outcome FROM analysis_runs WHERE id=$1::uuid`, created.Run.ID).Scan(&outcome); err != nil {
		t.Fatalf("load run outcome: %v", err)
	}
	if outcome != string(wantOutcome) {
		t.Fatalf("run outcome = %s, want %s", outcome, wantOutcome)
	}
	if err := store.pool.QueryRow(ctx, `SELECT status FROM operations WHERE id=$1::uuid`, created.Operation.ID).Scan(&opStatus); err != nil {
		t.Fatalf("load operation: %v", err)
	}
	if opStatus != string(domain.OperationFailed) {
		t.Fatalf("operation status = %s, want %s", opStatus, domain.OperationFailed)
	}
	if err := store.pool.QueryRow(ctx, `
		SELECT status, COALESCE(error_class, ''), COALESCE(error_code, '')
		FROM tasks WHERE id=$1::uuid`, created.Task.ID).Scan(&taskStatus, &errorClass, &errorCode); err != nil {
		t.Fatalf("load task: %v", err)
	}
	if taskStatus != string(domain.TaskFailed) {
		t.Fatalf("task status = %s, want %s", taskStatus, domain.TaskFailed)
	}
	if errorClass != string(wantClass) || errorCode != wantCode {
		t.Fatalf("task error = %s/%s, want %s/%s", errorClass, errorCode, wantClass, wantCode)
	}
}

func claimAssessmentTask(t *testing.T, store *Store, owner string) domain.TaskLease {
	t.Helper()
	lease, ok, err := store.Claim(ctx, owner, 30*time.Second, []domain.TaskType{"assessment"})
	if err != nil || !ok {
		t.Fatalf("claim %s: ok=%v err=%v", owner, ok, err)
	}
	return lease
}

func countRows(t *testing.T, pool *pgxpool.Pool, table string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{table}.Sanitize()).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func assertRunPublished(t *testing.T, pool *pgxpool.Pool, runID, reportID string) {
	t.Helper()
	var outcome, publishedReport string
	if err := pool.QueryRow(ctx, `
		SELECT outcome, report_id::text FROM analysis_runs WHERE id=$1::uuid`, runID).
		Scan(&outcome, &publishedReport); err != nil {
		t.Fatalf("load run %s: %v", runID, err)
	}
	if outcome != string(domain.AnalysisOutcomePublished) {
		t.Fatalf("run %s outcome = %s, want published", runID, outcome)
	}
	if publishedReport != reportID {
		t.Fatalf("run %s report_id = %s, want %s", runID, publishedReport, reportID)
	}
}

func newerReportID(t *testing.T, pool *pgxpool.Pool, left, right string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, `
		SELECT id::text FROM reports
		WHERE id IN ($1::uuid, $2::uuid)
		ORDER BY created_at DESC, id DESC
		LIMIT 1`, left, right).Scan(&id); err != nil {
		t.Fatalf("newer report: %v", err)
	}
	return id
}

func currentReportID(t *testing.T, pool *pgxpool.Pool, userID string) *string {
	t.Helper()
	var id *string
	err := pool.QueryRow(ctx, `SELECT current_report_id::text FROM user_profiles WHERE user_id=$1::uuid`, userID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		t.Fatalf("current report: %v", err)
	}
	return id
}

func seedAssessmentAssets(t *testing.T, store *Store, userID string) map[domain.PhotoRole]domain.MediaAsset {
	t.Helper()
	roles := []domain.PhotoRole{domain.PhotoRoleFace, domain.PhotoRoleSide, domain.PhotoRoleBody}
	assets := make(map[domain.PhotoRole]domain.MediaAsset, len(roles))
	for _, role := range roles {
		assets[role] = insertAssessmentMedia(t, store, userID, string(role))
	}
	return assets
}

func slotsFromAssets(assets map[domain.PhotoRole]domain.MediaAsset) domain.PhotoSlots {
	return domain.PhotoSlots{
		FaceAssetID: assets[domain.PhotoRoleFace].ID,
		SideAssetID: assets[domain.PhotoRoleSide].ID,
		BodyAssetID: assets[domain.PhotoRoleBody].ID,
	}
}

func insertAssessmentMedia(t *testing.T, store *Store, userID, purpose string) domain.MediaAsset {
	t.Helper()
	var asset domain.MediaAsset
	err := store.pool.QueryRow(ctx, `
		INSERT INTO media_assets(
			user_id, origin, purpose, object_key, sha256, mime_type, byte_size, state, display_kind
		) VALUES ($1::uuid,'user_upload',$2,$3,$4,'image/jpeg',128,'ready','original')
		RETURNING id::text, user_id::text, origin, purpose, object_key, sha256, mime_type, byte_size,
		          state, display_kind, created_at`,
		userID, purpose, "users/"+userID+"/uploads/"+uuid.NewString(), schemaHex64(),
	).Scan(
		&asset.ID, &asset.UserID, &asset.Origin, &asset.Purpose, &asset.ObjectKey, &asset.SHA256,
		&asset.MIMEType, &asset.ByteSize, &asset.State, &asset.DisplayKind, &asset.CreatedAt,
	)
	if err != nil {
		t.Fatalf("insert media %s: %v", purpose, err)
	}
	return asset
}

func insertAssessmentInvocation(pool *pgxpool.Pool, created CreatedAssessment) string {
	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO provider_invocations(
			user_id,operation_id,task_id,attempt_no,capability,
			routing_config_version,provider_key,model_key,protocol,request_hash,status
		) VALUES ($1::uuid,$2::uuid,$3::uuid,0,'vision','v1','p','m','http',$4,'succeeded')
		RETURNING id::text`,
		created.Run.UserID, created.Operation.ID, created.Task.ID, schemaHex64(),
	).Scan(&id)
	if err != nil {
		panic("insert invocation: " + err.Error())
	}
	return id
}
