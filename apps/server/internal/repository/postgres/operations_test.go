package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/testutil"
)

func TestGetOperationCrossUserNotFound(t *testing.T) {
	store, userID := newOperationStore(t)
	otherID := insertUser(t, store)
	op := seedOperation(t, store, userID, domain.Operation{
		Kind: domain.OperationAssessment, Status: domain.OperationRunning,
		StageCode: "photo.technical_check", PublicMessage: "正在检查照片", ProgressBPS: 3500,
	})
	_, err := store.GetOperation(context.Background(), otherID, op.ID)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("cross-user GetOperation error = %v, want ErrNotFound", err)
	}
}

func TestGetOperationsMissingIDReturnsNotFound(t *testing.T) {
	store, userID := newOperationStore(t)
	otherID := insertUser(t, store)
	owned := seedOperation(t, store, userID, domain.Operation{
		Kind: domain.OperationAssessment, Status: domain.OperationAccepted,
	})
	foreign := seedOperation(t, store, otherID, domain.Operation{
		Kind: domain.OperationRender, Status: domain.OperationRunning,
	})
	_, err := store.GetOperations(context.Background(), userID, []string{owned.ID, foreign.ID})
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("GetOperations error = %v, want ErrNotFound", err)
	}
}

func TestGetOperationsPreservesRequestOrder(t *testing.T) {
	store, userID := newOperationStore(t)
	first := seedOperation(t, store, userID, domain.Operation{
		Kind: domain.OperationAssessment, Status: domain.OperationRunning,
	})
	second := seedOperation(t, store, userID, domain.Operation{
		Kind: domain.OperationPlanSet, Status: domain.OperationSucceeded,
	})
	got, err := store.GetOperations(context.Background(), userID, []string{second.ID, first.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != second.ID || got[1].ID != first.ID {
		t.Fatalf("order = %s,%s want %s,%s", got[0].ID, got[1].ID, second.ID, first.ID)
	}
}

func TestGetFailedOperationIncludesTraceID(t *testing.T) {
	store, userID := newOperationStore(t)
	op := seedOperation(t, store, userID, domain.Operation{
		Kind: domain.OperationExecutionFeedback, Status: domain.OperationFailed,
		TraceID: "trace-db-1", PublicMessage: "执行反馈失败", ErrorCode: "provider_failed",
	})
	got, err := store.GetOperation(context.Background(), userID, op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.TraceID != "trace-db-1" {
		t.Fatalf("trace_id = %q", got.TraceID)
	}
	if got.ErrorCode != "provider_failed" || got.Status != domain.OperationFailed {
		t.Fatalf("failed operation = %#v", got)
	}
}

func newOperationStore(t *testing.T) (*Store, string) {
	t.Helper()
	store := New(testutil.NewPostgres(t))
	return store, insertUser(t, store)
}

func seedOperation(t *testing.T, store *Store, userID string, op domain.Operation) domain.Operation {
	t.Helper()
	if op.Kind == "" {
		op.Kind = domain.OperationAssessment
	}
	if op.Status == "" {
		op.Status = domain.OperationAccepted
	}
	if op.SubjectType == "" {
		op.SubjectType = "assessment"
	}
	subjectID := op.SubjectID
	if subjectID == "" {
		subjectID = uuid.NewString()
	}
	var errorCode, traceID, resultType, resultID *string
	if op.ErrorCode != "" {
		errorCode = &op.ErrorCode
	}
	if op.TraceID != "" {
		traceID = &op.TraceID
	}
	if op.ResultType != "" {
		resultType = &op.ResultType
	}
	if op.ResultID != "" {
		resultID = &op.ResultID
	}
	var id string
	err := store.pool.QueryRow(context.Background(), `
		INSERT INTO operations(
			user_id, kind, subject_type, subject_id, status, progress_bps,
			stage_code, public_message, error_code, trace_id, retryable, result_type, result_id
		) VALUES (
			$1::uuid, $2, $3, $4::uuid, $5, $6,
			$7, $8, $9, $10, $11, $12, $13
		) RETURNING id::text`,
		userID, op.Kind, op.SubjectType, subjectID, op.Status, op.ProgressBPS,
		op.StageCode, op.PublicMessage, errorCode, traceID, op.Retryable, resultType, resultID,
	).Scan(&id)
	if err != nil {
		t.Fatalf("seed operation: %v", err)
	}
	got, err := store.GetOperation(context.Background(), userID, id)
	if err != nil {
		t.Fatalf("reload seeded operation: %v", err)
	}
	return got
}

// ---- stale 孤儿兜底（读路径 CAS 落失败终态） ----

func ageOperation(t *testing.T, store *Store, id string, age time.Duration) {
	t.Helper()
	if _, err := store.pool.Exec(context.Background(),
		`UPDATE operations SET updated_at=now()-$2::interval WHERE id=$1::uuid`, id, age.String()); err != nil {
		t.Fatal(err)
	}
}

func TestFailStaleOperationFailsIdleInFlight(t *testing.T) {
	store, userID := newOperationStore(t)
	op := seedOperation(t, store, userID, domain.Operation{
		Kind: domain.OperationRender, Status: domain.OperationRunning,
	})
	ageOperation(t, store, op.ID, 15*time.Minute)

	failed, err := store.FailStaleOperation(context.Background(), userID, op.ID, 11*time.Minute, "stale_orphan", "处理时间过长，请重新发起")
	if err != nil {
		t.Fatal(err)
	}
	if !failed {
		t.Fatal("idle in-flight operation must be failed")
	}
	got, err := store.GetOperation(context.Background(), userID, op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.OperationFailed || !got.Retryable ||
		got.PublicMessage != "处理时间过长，请重新发起" || got.ErrorCode != "stale_orphan" ||
		got.TraceID == "" || got.FinishedAt == nil {
		t.Fatalf("stale operation = %#v", got)
	}
	// 幂等：终态后再次调用不再命中。
	again, err := store.FailStaleOperation(context.Background(), userID, op.ID, 11*time.Minute, "stale_orphan", "处理时间过长，请重新发起")
	if err != nil {
		t.Fatal(err)
	}
	if again {
		t.Fatal("terminal operation must not be failed twice")
	}
}

// 与 worker 正常租约的竞态：operation 行旧但其 task 仍在心跳（worker 活着）→ 不误杀。
func TestFailStaleOperationSkipsWhenTaskActive(t *testing.T) {
	store, userID := newOperationStore(t)
	op := seedOperation(t, store, userID, domain.Operation{
		Kind: domain.OperationRender, SubjectType: "render_run", Status: domain.OperationRunning,
	})
	ageOperation(t, store, op.ID, 30*time.Minute)
	if _, err := store.pool.Exec(context.Background(), `
		INSERT INTO tasks(id, user_id, operation_id, type, subject_type, subject_id, subject_generation,
		                  payload_version, payload, dedupe_key, status, stage_code, max_attempts)
		VALUES (gen_random_uuid(), $1::uuid, $2::uuid, 'render_candidate_generate', 'render_run', $3::uuid, 1, 1, '{}', $4, 'queued', '', 3)`,
		userID, op.ID, op.SubjectID, "dedupe-"+op.ID); err != nil {
		t.Fatal(err)
	}

	failed, err := store.FailStaleOperation(context.Background(), userID, op.ID, 11*time.Minute, "stale_orphan", "处理时间过长，请重新发起")
	if err != nil {
		t.Fatal(err)
	}
	if failed {
		t.Fatal("operation with a live task must not be swept")
	}
	got, err := store.GetOperation(context.Background(), userID, op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.OperationRunning {
		t.Fatalf("status = %q, want running", got.Status)
	}
}

func TestFailStaleOperationSkipsFreshAndTerminal(t *testing.T) {
	store, userID := newOperationStore(t)
	fresh := seedOperation(t, store, userID, domain.Operation{Kind: domain.OperationRender, Status: domain.OperationAccepted})
	failed, err := store.FailStaleOperation(context.Background(), userID, fresh.ID, 20*time.Minute, "stale_orphan", "处理时间过长，请重新发起")
	if err != nil || failed {
		t.Fatalf("fresh operation: failed=%v err=%v", failed, err)
	}

	terminal := seedOperation(t, store, userID, domain.Operation{Kind: domain.OperationRender, Status: domain.OperationSucceeded})
	ageOperation(t, store, terminal.ID, 48*time.Hour)
	failed, err = store.FailStaleOperation(context.Background(), userID, terminal.ID, 11*time.Minute, "stale_orphan", "处理时间过长，请重新发起")
	if err != nil || failed {
		t.Fatalf("terminal operation: failed=%v err=%v", failed, err)
	}
}

// ---- 用量计数（日限/并发闸的仓储自计数口径） ----

func TestCountOperationsCreatedSinceFiltersKindSubjectAndTime(t *testing.T) {
	store, userID := newOperationStore(t)
	otherID := insertUser(t, store)
	seedOperation(t, store, userID, domain.Operation{Kind: domain.OperationRender, SubjectType: "render_run"})
	seedOperation(t, store, userID, domain.Operation{Kind: domain.OperationRender, SubjectType: "hair_preview"})
	seedOperation(t, store, userID, domain.Operation{Kind: domain.OperationAssessment, SubjectType: "photo_set"})
	seedOperation(t, store, otherID, domain.Operation{Kind: domain.OperationRender, SubjectType: "render_run"})

	now := time.Now()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	renderRuns, err := store.CountOperationsCreatedSince(context.Background(), userID,
		[]domain.OperationKind{domain.OperationRender}, []string{"render_run"}, dayStart)
	if err != nil {
		t.Fatal(err)
	}
	if renderRuns != 1 {
		t.Fatalf("render runs today = %d, want 1", renderRuns)
	}
	allRenders, err := store.CountOperationsCreatedSince(context.Background(), userID,
		[]domain.OperationKind{domain.OperationRender}, nil, dayStart)
	if err != nil {
		t.Fatal(err)
	}
	if allRenders != 2 {
		t.Fatalf("render ops today = %d, want 2", allRenders)
	}
	future, err := store.CountOperationsCreatedSince(context.Background(), userID,
		[]domain.OperationKind{domain.OperationRender}, nil, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if future != 0 {
		t.Fatalf("future count = %d, want 0", future)
	}
}

func TestCountActiveOperationsCountsOnlyInFlightSubjects(t *testing.T) {
	store, userID := newOperationStore(t)
	subjects := []string{"render_run", "hair_preview", "body_presentation"}
	seedOperation(t, store, userID, domain.Operation{Kind: domain.OperationRender, SubjectType: "render_run", Status: domain.OperationAccepted})
	seedOperation(t, store, userID, domain.Operation{Kind: domain.OperationRender, SubjectType: "hair_preview", Status: domain.OperationRetrying})
	seedOperation(t, store, userID, domain.Operation{Kind: domain.OperationRender, SubjectType: "render_run", Status: domain.OperationSucceeded})
	seedOperation(t, store, userID, domain.Operation{Kind: domain.OperationRender, SubjectType: "today_plan", Status: domain.OperationAccepted})
	seedOperation(t, store, userID, domain.Operation{Kind: domain.OperationAssessment, SubjectType: "photo_set", Status: domain.OperationRunning})

	active, err := store.CountActiveOperations(context.Background(), userID, subjects)
	if err != nil {
		t.Fatal(err)
	}
	if active != 2 {
		t.Fatalf("active = %d, want 2", active)
	}
}
