package postgres

import (
	"context"
	"errors"
	"testing"

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
