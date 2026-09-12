package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/provider/ai"
	"github.com/zhanshimian/server/internal/repository"
)

func TestStartInvocationPersistsStartedRow(t *testing.T) {
	store, userID, opID, taskID := newInvocationFixture(t)
	started, err := store.StartInvocation(context.Background(), invocationStart(userID, opID, taskID))
	if err != nil {
		t.Fatal(err)
	}
	if started.ID == "" || started.Status != domain.InvocationStarted {
		t.Fatalf("started = %#v", started)
	}
	if started.UserID != userID || started.OperationID != opID || started.TaskID != taskID || started.AttemptNo != 1 {
		t.Fatalf("fk fields = %#v", started)
	}
	row := loadInvocationRow(t, store, started.ID)
	if row.status != string(domain.InvocationStarted) || row.finishedAt != nil {
		t.Fatalf("row = %+v", row)
	}
	if row.requestHash != invocationHash() {
		t.Fatalf("request_hash = %q", row.requestHash)
	}
	if row.inputImages == nil || *row.inputImages != 3 {
		t.Fatalf("input_images = %v", row.inputImages)
	}
}

func TestFinishInvocationSuccessAndConflict(t *testing.T) {
	store, userID, opID, taskID := newInvocationFixture(t)
	started, err := store.StartInvocation(context.Background(), invocationStart(userID, opID, taskID))
	if err != nil {
		t.Fatal(err)
	}
	cost := 0.21
	latency := 88
	inTokens, outTokens, outImages := 11, 7, 1
	err = store.FinishInvocation(context.Background(), domain.FinishInvocation{
		UserID:            userID,
		InvocationID:      started.ID,
		TaskID:            taskID,
		AttemptNo:         1,
		Status:            domain.InvocationSucceeded,
		ProviderRequestID: "req-ok",
		InputTokens:       &inTokens,
		OutputTokens:      &outTokens,
		OutputImages:      &outImages,
		EstimatedCostCNY:  &cost,
		LatencyMS:         &latency,
	})
	if err != nil {
		t.Fatal(err)
	}
	row := loadInvocationRow(t, store, started.ID)
	if row.status != string(domain.InvocationSucceeded) || row.finishedAt == nil {
		t.Fatalf("finished row = %+v", row)
	}
	if row.providerRequestID != "req-ok" || row.latencyMS == nil || *row.latencyMS != 88 {
		t.Fatalf("metrics = %+v", row)
	}
	if row.userID != userID || row.operationID != opID || row.taskID != taskID || row.attemptNo != 1 {
		t.Fatalf("linkage = %+v", row)
	}

	err = store.FinishInvocation(context.Background(), domain.FinishInvocation{
		UserID: userID, InvocationID: started.ID, TaskID: taskID, AttemptNo: 1,
		Status: domain.InvocationFailed, ErrorClass: domain.ErrorPermanent, ErrorCode: "late",
	})
	if !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("second finish error = %v, want ErrConflict", err)
	}
	if got := loadInvocationRow(t, store, started.ID); got.status != string(domain.InvocationSucceeded) {
		t.Fatalf("conflict overwrite status = %s", got.status)
	}
}

func TestFinishInvocationConflictWhenNotStarted(t *testing.T) {
	store, userID, _, taskID := newInvocationFixture(t)
	err := store.FinishInvocation(context.Background(), domain.FinishInvocation{
		UserID: userID, InvocationID: "00000000-0000-4000-8000-000000000001",
		TaskID: taskID, AttemptNo: 1, Status: domain.InvocationSucceeded,
	})
	if !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("missing finish error = %v, want ErrConflict", err)
	}
}

func TestRecordInvocationPersistsSuccessLinkedToTask(t *testing.T) {
	store, userID, opID, taskID := newInvocationFixture(t)
	recorder := ai.NewInvocationRecorder(store, nil)
	cost := 0.09
	_, meta, err := recorder.Record(context.Background(), invocationStart(userID, opID, taskID), func(context.Context) (ai.CallResult, error) {
		return ai.CallResult{ProviderRequestID: "req-ok", EstimatedCostCNY: &cost}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	assertOneFinishedInvocation(t, store, userID, opID, taskID, meta.InvocationID, domain.InvocationSucceeded)
}

func TestRecordInvocationPersistsProviderFailure(t *testing.T) {
	store, userID, opID, taskID := newInvocationFixture(t)
	recorder := ai.NewInvocationRecorder(store, nil)
	_, meta, err := recorder.Record(context.Background(), invocationStart(userID, opID, taskID), func(context.Context) (ai.CallResult, error) {
		return ai.CallResult{ProviderRequestID: "req-fail"}, ai.NewCallError(domain.ErrorThrottled, "rate_limited")
	})
	if err == nil {
		t.Fatal("expected provider failure")
	}
	row := assertOneFinishedInvocation(t, store, userID, opID, taskID, meta.InvocationID, domain.InvocationFailed)
	if row.errorClass != string(domain.ErrorThrottled) || row.errorCode != "rate_limited" {
		t.Fatalf("typed error = %+v", row)
	}
}

func TestRecordInvocationPersistsAfterContextCancel(t *testing.T) {
	store, userID, opID, taskID := newInvocationFixture(t)
	recorder := ai.NewInvocationRecorder(store, nil)
	ctx, cancel := context.WithCancel(context.Background())
	_, meta, err := recorder.Record(ctx, invocationStart(userID, opID, taskID), func(context.Context) (ai.CallResult, error) {
		cancel()
		return ai.CallResult{ProviderRequestID: "req-c"}, context.Canceled
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	assertOneFinishedInvocation(t, store, userID, opID, taskID, meta.InvocationID, domain.InvocationFailed)
}

func newInvocationFixture(t *testing.T) (*Store, string, string, string) {
	t.Helper()
	store, pool := newTaskStore(t)
	userID, opID := seedTaskOwner(t, pool)
	taskID := insertQueuedTask(t, pool, userID, opID, "assessment", 0, time.Time{})
	return store, userID, opID, taskID
}

func invocationStart(userID, opID, taskID string) domain.StartInvocation {
	return domain.StartInvocation{
		UserID: userID, OperationID: opID, TaskID: taskID, AttemptNo: 1,
		Capability: "photo_quality_check", RoutingConfigVersion: "route-v1",
		ProviderKey: "primary", ModelKey: "vision-main", Protocol: "openai_images",
		RequestHash: invocationHash(),
		InputImages: 3,
	}
}

func invocationHash() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte("redacted canonical request")))
}

type invocationRow struct {
	userID            string
	operationID       string
	taskID            string
	attemptNo         int
	requestHash       string
	providerRequestID string
	status            string
	inputImages       *int
	latencyMS         *int
	errorClass        string
	errorCode         string
	finishedAt        *time.Time
}

func loadInvocationRow(t *testing.T, store *Store, id string) invocationRow {
	t.Helper()
	var row invocationRow
	var providerRequestID, errorClass, errorCode *string
	err := store.pool.QueryRow(context.Background(), `
		SELECT user_id::text, operation_id::text, task_id::text, attempt_no, request_hash,
		       provider_request_id, status, input_images, latency_ms, error_class, error_code, finished_at
		FROM provider_invocations WHERE id=$1::uuid`, id).Scan(
		&row.userID, &row.operationID, &row.taskID, &row.attemptNo, &row.requestHash,
		&providerRequestID, &row.status, &row.inputImages, &row.latencyMS, &errorClass, &errorCode, &row.finishedAt,
	)
	if err != nil {
		t.Fatalf("load invocation %s: %v", id, err)
	}
	if providerRequestID != nil {
		row.providerRequestID = *providerRequestID
	}
	if errorClass != nil {
		row.errorClass = *errorClass
	}
	if errorCode != nil {
		row.errorCode = *errorCode
	}
	return row
}

func assertOneFinishedInvocation(t *testing.T, store *Store, userID, opID, taskID, invocationID string, status domain.InvocationStatus) invocationRow {
	t.Helper()
	var count int
	if err := store.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM provider_invocations
		WHERE user_id=$1::uuid AND operation_id=$2::uuid AND task_id=$3::uuid AND attempt_no=1`,
		userID, opID, taskID,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("invocation rows = %d, want 1", count)
	}
	row := loadInvocationRow(t, store, invocationID)
	if row.status != string(status) || row.finishedAt == nil {
		t.Fatalf("finished row = %+v, want status %s", row, status)
	}
	if row.userID != userID || row.operationID != opID || row.taskID != taskID || row.attemptNo != 1 {
		t.Fatalf("linkage = %+v", row)
	}
	return row
}
