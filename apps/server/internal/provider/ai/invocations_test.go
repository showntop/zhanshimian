package ai_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/provider/ai"
)

func TestRecordInvocationFinishesFailureWithoutSensitiveInput(t *testing.T) {
	store := &invocationStoreFake{}
	recorder := ai.NewInvocationRecorder(store, fixedClock())
	_, meta, err := recorder.Record(context.Background(), domain.StartInvocation{
		UserID: "u1", OperationID: "op1", TaskID: "task1", AttemptNo: 1,
		Capability: "photo_quality_check", RoutingConfigVersion: "route-v1",
		ProviderKey: "primary", ModelKey: "vision-main", Protocol: "openai_images",
		RequestHash: fmt.Sprintf("%x", sha256.Sum256([]byte("redacted canonical request"))),
		InputImages: 3,
	}, func(context.Context) (ai.CallResult, error) {
		return ai.CallResult{ProviderRequestID: "req-1"}, ai.NewCallError(domain.ErrorThrottled, "rate_limited")
	})
	if err == nil {
		t.Fatal("expected provider call error")
	}
	var callErr *ai.CallError
	if !errors.As(err, &callErr) || callErr.Class != domain.ErrorThrottled || callErr.Code != "rate_limited" {
		t.Fatalf("error = %v, want throttled/rate_limited CallError", err)
	}
	if store.finished.Status != domain.InvocationFailed {
		t.Fatalf("status = %s, want %s", store.finished.Status, domain.InvocationFailed)
	}
	if store.finished.ErrorClass != domain.ErrorThrottled {
		t.Fatalf("error class = %s, want %s", store.finished.ErrorClass, domain.ErrorThrottled)
	}
	if store.finished.ErrorCode != "rate_limited" {
		t.Fatalf("error code = %q", store.finished.ErrorCode)
	}
	if strings.Contains(string(store.serialized), "image/jpeg;base64") {
		t.Fatal("ledger persisted image bytes")
	}
	if strings.Contains(string(store.serialized), "https://private-cos") {
		t.Fatal("ledger persisted a signed URL")
	}
	if meta.InvocationID == "" || meta.InvocationID != store.finished.InvocationID {
		t.Fatalf("meta id %q != finished %q", meta.InvocationID, store.finished.InvocationID)
	}
}

func TestRecordInvocationHashesCanonicalRequestAndSucceeds(t *testing.T) {
	store := &invocationStoreFake{}
	recorder := ai.NewInvocationRecorder(store, fixedClock())
	canonical := `{"image":"data:image/jpeg;base64,AAAA","url":"https://private-cos/signed"}`
	cost := 0.18
	tokens := 42
	result, meta, err := recorder.Record(context.Background(), domain.StartInvocation{
		UserID: "u1", OperationID: "op1", TaskID: "task1", AttemptNo: 1,
		Capability: "photo_quality_check", RoutingConfigVersion: "route-v1",
		ProviderKey: "primary", ModelKey: "vision-main", Protocol: "openai_images",
		RequestHash: canonical,
		InputImages: 3,
	}, func(context.Context) (ai.CallResult, error) {
		return ai.CallResult{
			ProviderRequestID: "req-ok",
			InputTokens:       &tokens,
			EstimatedCostCNY:  &cost,
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	wantHash := fmt.Sprintf("%x", sha256.Sum256([]byte(canonical)))
	if store.started.RequestHash != wantHash {
		t.Fatalf("request hash = %q, want hex digest", store.started.RequestHash)
	}
	if store.finished.Status != domain.InvocationSucceeded {
		t.Fatalf("status = %s, want %s", store.finished.Status, domain.InvocationSucceeded)
	}
	if result.ProviderRequestID != "req-ok" || meta.ProviderRequestID != "req-ok" {
		t.Fatalf("provider request id result=%q meta=%q", result.ProviderRequestID, meta.ProviderRequestID)
	}
	if meta.InvocationID != store.finished.InvocationID {
		t.Fatalf("meta id %q != finished %q", meta.InvocationID, store.finished.InvocationID)
	}
	if meta.EstimatedCostCNY == nil || *meta.EstimatedCostCNY != cost {
		t.Fatalf("meta cost = %v, want %v", meta.EstimatedCostCNY, cost)
	}
	if strings.Contains(string(store.serialized), "image/jpeg;base64") {
		t.Fatal("ledger persisted image bytes")
	}
	if strings.Contains(string(store.serialized), "https://private-cos") {
		t.Fatal("ledger persisted a signed URL")
	}
}

func TestRecordInvocationFinishesAfterParentCancel(t *testing.T) {
	store := &invocationStoreFake{}
	recorder := ai.NewInvocationRecorder(store, fixedClock())
	ctx, cancel := context.WithCancel(context.Background())
	_, meta, err := recorder.Record(ctx, domain.StartInvocation{
		UserID: "u1", OperationID: "op1", TaskID: "task1", AttemptNo: 1,
		Capability: "photo_quality_check", RoutingConfigVersion: "route-v1",
		ProviderKey: "primary", ModelKey: "vision-main", Protocol: "openai_images",
		RequestHash: fmt.Sprintf("%x", sha256.Sum256([]byte("redacted canonical request"))),
		InputImages: 3,
	}, func(context.Context) (ai.CallResult, error) {
		cancel()
		return ai.CallResult{ProviderRequestID: "req-c"}, context.Canceled
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if store.finished.Status != domain.InvocationFailed {
		t.Fatalf("status = %s, want %s", store.finished.Status, domain.InvocationFailed)
	}
	if store.finishErr != nil {
		t.Fatalf("finish used cancelled context: %v", store.finishErr)
	}
	if !store.finishHasDeadline {
		t.Fatal("finish context missing write timeout")
	}
	if meta.InvocationID != store.finished.InvocationID {
		t.Fatalf("meta id %q != finished %q", meta.InvocationID, store.finished.InvocationID)
	}
}

type invocationStoreFake struct {
	started           domain.ProviderInvocation
	finished          domain.FinishInvocation
	serialized        []byte
	finishErr         error
	finishHasDeadline bool
}

func (s *invocationStoreFake) StartInvocation(_ context.Context, in domain.StartInvocation) (domain.ProviderInvocation, error) {
	started := domain.ProviderInvocation{
		ID:                   "inv-1",
		UserID:               in.UserID,
		OperationID:          in.OperationID,
		TaskID:               in.TaskID,
		AttemptNo:            in.AttemptNo,
		Capability:           in.Capability,
		RoutingConfigVersion: in.RoutingConfigVersion,
		ProviderKey:          in.ProviderKey,
		ModelKey:             in.ModelKey,
		Protocol:             in.Protocol,
		RequestHash:          in.RequestHash,
		Status:               domain.InvocationStarted,
	}
	s.started = started
	s.serialized = mustJSON(started)
	return started, nil
}

func (s *invocationStoreFake) FinishInvocation(ctx context.Context, in domain.FinishInvocation) error {
	s.finishErr = ctx.Err()
	_, s.finishHasDeadline = ctx.Deadline()
	s.finished = in
	s.serialized = append(s.serialized, mustJSON(in)...)
	return nil
}

type stubClock struct{ now time.Time }

func (c stubClock) Now() time.Time { return c.now }

func fixedClock() ai.Clock {
	return stubClock{now: time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)}
}

func mustJSON(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}
