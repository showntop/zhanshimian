package taskrunner_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/service/taskrunner"
)

func TestRunnerDoesNotCommitAfterLeaseLoss(t *testing.T) {
	store := &taskStoreFake{heartbeatResults: []bool{true, false}}
	handler := &blockingHandler{taskType: "assessment"}
	runner := taskrunner.NewRunner(store, oneTypeRegistry(handler), "worker-1", 10*time.Millisecond, slog.Default())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go runner.Run(ctx)
	waitUntil(t, time.Second, func() bool { return handler.executeCancelled.Load() })
	if handler.commitCalls.Load() != 0 {
		t.Fatalf("commitCalls = %d, want 0", handler.commitCalls.Load())
	}
	if store.failCount() != 0 {
		t.Fatalf("failCalls = %d, want 0", store.failCount())
	}
}

func TestRunnerCommitsSuccessfulExecute(t *testing.T) {
	store := &taskStoreFake{leases: []domain.TaskLease{fakeLease("ok-1", 1)}}
	want := domain.TaskResult{Disposition: domain.TaskPublish, ResultType: "report", ResultID: "r1"}
	handler := &recordingHandler{
		taskType:  "assessment",
		executeFn: func(context.Context, domain.TaskLease) (domain.TaskResult, error) { return want, nil },
	}
	runUntil(t, store, handler, func() bool { return handler.commitCalls.Load() == 1 })
	if store.failCount() != 0 {
		t.Fatalf("Fail called %d times", store.failCount())
	}
	got := handler.lastResult()
	if got.Disposition != want.Disposition || got.ResultID != want.ResultID {
		t.Fatalf("committed %+v, want %+v", got, want)
	}
}

func TestRunnerTreatsCommitSupersededAsSuccess(t *testing.T) {
	store := &taskStoreFake{leases: []domain.TaskLease{fakeLease("stale-1", 1)}}
	handler := &recordingHandler{
		taskType: "assessment",
		executeFn: func(context.Context, domain.TaskLease) (domain.TaskResult, error) {
			return domain.TaskResult{Disposition: domain.TaskPublish}, nil
		},
		commitFn: func(context.Context, domain.TaskLease, domain.TaskResult) (domain.CommitOutcome, error) {
			return domain.CommitSuperseded, nil
		},
	}
	runUntil(t, store, handler, func() bool { return handler.commitCalls.Load() == 1 })
	if handler.executeCalls.Load() != 1 {
		t.Fatalf("executeCalls = %d, want 1", handler.executeCalls.Load())
	}
	if store.failCount() != 0 {
		t.Fatal("CommitSuperseded must not Fail")
	}
}

func TestRunnerRetriesTransientWithRegistryBackoff(t *testing.T) {
	backoff := 42 * time.Second
	store := &taskStoreFake{leases: []domain.TaskLease{fakeLease("retry-1", 1)}}
	handler := &recordingHandler{
		taskType: "assessment",
		executeFn: func(context.Context, domain.TaskLease) (domain.TaskResult, error) {
			return domain.TaskResult{}, &taskrunner.TaskError{Class: domain.ErrorTransient, Code: "provider_timeout"}
		},
	}
	def := shortDefinition("assessment", 1)
	def.MaxAttempts = 3
	def.RetryBackoff = func(int) time.Duration { return backoff }
	runUntilRegistry(t, store, mustRegistry(t, def, handler), handler, func() bool { return store.failCount() == 1 })
	if handler.commitCalls.Load() != 0 {
		t.Fatal("retryable error must Fail, not Commit")
	}
	call := store.lastFail()
	if call.failure.Class != domain.ErrorTransient || call.failure.Code != "provider_timeout" {
		t.Fatalf("failure = %+v", call.failure)
	}
	delay := time.Until(call.availableAt)
	if delay < 40*time.Second || delay > 44*time.Second {
		t.Fatalf("available_at delay = %s, want ~%s from Registry backoff", delay, backoff)
	}
}

func TestRunnerRetriesHandlerTimeoutWithRegistryBackoff(t *testing.T) {
	backoff := 42 * time.Second
	store := &taskStoreFake{leases: []domain.TaskLease{fakeLease("timeout-1", 1)}}
	handler := &recordingHandler{
		taskType: "assessment",
		executeFn: func(ctx context.Context, _ domain.TaskLease) (domain.TaskResult, error) {
			<-ctx.Done()
			return domain.TaskResult{}, ctx.Err()
		},
	}
	def := shortDefinition("assessment", 1)
	def.Timeout = 25 * time.Millisecond
	def.HeartbeatEvery = 10 * time.Millisecond
	def.LeaseDuration = 40 * time.Millisecond
	def.MaxAttempts = 3
	def.RetryBackoff = func(int) time.Duration { return backoff }
	runUntilRegistry(t, store, mustRegistry(t, def, handler), handler, func() bool { return store.failCount() == 1 })
	if handler.commitCalls.Load() != 0 {
		t.Fatal("handler timeout must Fail, not Commit")
	}
	call := store.lastFail()
	if call.failure.Class != domain.ErrorTransient || call.failure.Code != "handler_timeout" {
		t.Fatalf("failure = %+v, want transient/handler_timeout", call.failure)
	}
	delay := time.Until(call.availableAt)
	if delay < 40*time.Second || delay > 44*time.Second {
		t.Fatalf("available_at delay = %s, want ~%s from Registry backoff", delay, backoff)
	}
}

func TestRunnerCommitsDomainFailWhenNotRetryable(t *testing.T) {
	cases := []struct {
		name    string
		attempt int
		err     error
		class   domain.ErrorClass
		code    string
	}{
		{name: "permanent", attempt: 1, err: &taskrunner.TaskError{Class: domain.ErrorPermanent, Code: "bad_input"}, class: domain.ErrorPermanent, code: "bad_input"},
		{name: "quality", attempt: 1, err: &taskrunner.TaskError{Class: domain.ErrorQualityRejected, Code: "photo"}, class: domain.ErrorQualityRejected, code: "photo"},
		{name: "exhausted", attempt: 3, err: &taskrunner.TaskError{Class: domain.ErrorThrottled, Code: "rate"}, class: domain.ErrorThrottled, code: "rate"},
		{name: "unclassified", attempt: 1, err: errors.New("mystery"), class: domain.ErrorPermanent, code: "unclassified"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &taskStoreFake{leases: []domain.TaskLease{fakeLease(tc.name, tc.attempt)}}
			handler := &recordingHandler{
				taskType:  "assessment",
				executeFn: func(context.Context, domain.TaskLease) (domain.TaskResult, error) { return domain.TaskResult{}, tc.err },
			}
			runUntil(t, store, handler, func() bool { return handler.commitCalls.Load() == 1 })
			if store.failCount() != 0 {
				t.Fatalf("Fail called for %s", tc.name)
			}
			got := handler.lastResult()
			if got.Disposition != domain.TaskDomainFail || got.Failure == nil {
				t.Fatalf("result = %+v", got)
			}
			if got.Failure.Class != tc.class || got.Failure.Code != tc.code {
				t.Fatalf("failure = %+v, want %s/%s", got.Failure, tc.class, tc.code)
			}
		})
	}
}

func TestRunnerFailsSupersededWithoutCommit(t *testing.T) {
	store := &taskStoreFake{leases: []domain.TaskLease{fakeLease("sup-1", 1)}}
	handler := &recordingHandler{
		taskType: "assessment",
		executeFn: func(context.Context, domain.TaskLease) (domain.TaskResult, error) {
			return domain.TaskResult{}, &taskrunner.TaskError{Class: domain.ErrorSuperseded, Code: "replaced"}
		},
	}
	runUntil(t, store, handler, func() bool { return store.failCount() == 1 })
	if handler.commitCalls.Load() != 0 {
		t.Fatal("superseded must not Commit")
	}
	if store.lastFail().failure.Class != domain.ErrorSuperseded {
		t.Fatalf("failure = %+v", store.lastFail().failure)
	}
}

func TestRunnerStopsWhenFailLosesLease(t *testing.T) {
	store := &taskStoreFake{
		leases:      []domain.TaskLease{fakeLease("lost-fail", 1)},
		failUpdated: boolPtr(false),
	}
	handler := &recordingHandler{
		taskType: "assessment",
		executeFn: func(context.Context, domain.TaskLease) (domain.TaskResult, error) {
			return domain.TaskResult{}, &taskrunner.TaskError{Class: domain.ErrorTransient, Code: "again"}
		},
	}
	runUntil(t, store, handler, func() bool { return store.failCount() == 1 })
	time.Sleep(30 * time.Millisecond)
	if handler.executeCalls.Load() != 1 {
		t.Fatalf("executeCalls = %d, want 1 after lease loss", handler.executeCalls.Load())
	}
	if handler.commitCalls.Load() != 0 {
		t.Fatal("lost Fail must not Commit")
	}
}

func TestRunnerRespectsPerTypeConcurrency(t *testing.T) {
	var inFlight, peak atomic.Int32
	release := make(chan struct{})
	handler := &recordingHandler{
		taskType: "assessment",
		executeFn: func(ctx context.Context, _ domain.TaskLease) (domain.TaskResult, error) {
			cur := inFlight.Add(1)
			for {
				old := peak.Load()
				if cur <= old || peak.CompareAndSwap(old, cur) {
					break
				}
			}
			defer inFlight.Add(-1)
			select {
			case <-release:
			case <-ctx.Done():
				return domain.TaskResult{}, ctx.Err()
			}
			return domain.TaskResult{Disposition: domain.TaskPublish}, nil
		},
	}
	store := &taskStoreFake{leases: []domain.TaskLease{
		fakeLease("c1", 1), fakeLease("c2", 1), fakeLease("c3", 1),
	}}
	def := shortDefinition("assessment", 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go taskrunner.NewRunner(store, mustRegistry(t, def, handler), "worker-1", 5*time.Millisecond, slog.Default()).Run(ctx)
	waitUntil(t, time.Second, func() bool { return inFlight.Load() == 2 })
	time.Sleep(40 * time.Millisecond)
	if peak.Load() > 2 {
		t.Fatalf("peak in-flight = %d, want <= 2", peak.Load())
	}
	if store.claimCount() != 2 {
		t.Fatalf("claims = %d, want 2 while at capacity", store.claimCount())
	}
	close(release)
	waitUntil(t, time.Second, func() bool { return handler.commitCalls.Load() == 3 })
}

func TestRunnerIsolatesHandlerPanic(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	payload, _ := json.Marshal(map[string]string{"secret": "do-not-log-payload"})
	panicLease := fakeLease("panic-task", 1)
	panicLease.Payload = payload
	okLease := fakeLease("ok-task", 1)
	store := &taskStoreFake{leases: []domain.TaskLease{panicLease, okLease}}
	handler := &recordingHandler{
		taskType: "assessment",
		executeFn: func(_ context.Context, lease domain.TaskLease) (domain.TaskResult, error) {
			if lease.ID == "panic-task" {
				panic("explode")
			}
			return domain.TaskResult{Disposition: domain.TaskPublish, ResultID: "ok"}, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go taskrunner.NewRunner(store, oneTypeRegistry(handler), "worker-1", 5*time.Millisecond, logger).Run(ctx)
	waitUntil(t, time.Second, func() bool { return store.failCount() == 1 && handler.commitCalls.Load() == 1 })
	fail := store.lastFail()
	if fail.failure.Class != domain.ErrorTransient || fail.failure.Code != "handler_panic" {
		t.Fatalf("panic failure = %+v", fail.failure)
	}
	out := logs.String()
	if !strings.Contains(out, "panic-task") {
		t.Fatalf("panic log missing task id: %s", out)
	}
	if !strings.Contains(out, "trace_id") {
		t.Fatalf("panic log missing trace id field: %s", out)
	}
	if strings.Contains(out, "do-not-log-payload") || strings.Contains(out, string(payload)) {
		t.Fatalf("panic log leaked payload: %s", out)
	}
}

func oneTypeRegistry(handler taskrunner.Handler) *taskrunner.Registry {
	def := shortDefinition(handler.Type(), 1)
	reg, err := taskrunner.NewRegistry([]taskrunner.Definition{def}, []taskrunner.Handler{handler})
	if err != nil {
		panic(err)
	}
	return reg
}

func shortDefinition(taskType domain.TaskType, concurrency int) taskrunner.Definition {
	return taskrunner.Definition{
		Type:           taskType,
		MaxAttempts:    3,
		Timeout:        90 * time.Second,
		LeaseDuration:  40 * time.Millisecond,
		HeartbeatEvery: 15 * time.Millisecond,
		Concurrency:    concurrency,
		RetryBackoff:   taskrunner.ExponentialBackoff(time.Second, time.Minute),
	}
}

func mustRegistry(t *testing.T, def taskrunner.Definition, handler taskrunner.Handler) *taskrunner.Registry {
	t.Helper()
	reg, err := taskrunner.NewRegistry([]taskrunner.Definition{def}, []taskrunner.Handler{handler})
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func runUntil(t *testing.T, store *taskStoreFake, handler *recordingHandler, cond func() bool) {
	t.Helper()
	runUntilRegistry(t, store, oneTypeRegistry(handler), handler, cond)
}

func runUntilRegistry(t *testing.T, store *taskStoreFake, registry *taskrunner.Registry, _ *recordingHandler, cond func() bool) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go taskrunner.NewRunner(store, registry, "worker-1", 5*time.Millisecond, slog.Default()).Run(ctx)
	waitUntil(t, time.Second, cond)
}

func fakeLease(id string, attempt int) domain.TaskLease {
	return domain.TaskLease{
		Task: domain.Task{
			ID:          id,
			Type:        "assessment",
			Attempt:     attempt,
			MaxAttempts: 3,
		},
		LeaseToken: "token-" + id,
		LeaseOwner: "worker-1",
	}
}

func waitUntil(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met")
}

func boolPtr(v bool) *bool { return &v }

type fakeHandler struct{ taskType domain.TaskType }

func (h fakeHandler) Type() domain.TaskType { return h.taskType }
func (h fakeHandler) Execute(context.Context, domain.TaskLease) (domain.TaskResult, error) {
	return domain.TaskResult{Disposition: domain.TaskPublish}, nil
}
func (h fakeHandler) Commit(context.Context, domain.TaskLease, domain.TaskResult) (domain.CommitOutcome, error) {
	return domain.CommitApplied, nil
}

type blockingHandler struct {
	taskType         domain.TaskType
	executeCancelled atomic.Bool
	commitCalls      atomic.Int32
}

func (h *blockingHandler) Type() domain.TaskType { return h.taskType }
func (h *blockingHandler) Execute(ctx context.Context, _ domain.TaskLease) (domain.TaskResult, error) {
	<-ctx.Done()
	h.executeCancelled.Store(true)
	return domain.TaskResult{}, ctx.Err()
}
func (h *blockingHandler) Commit(context.Context, domain.TaskLease, domain.TaskResult) (domain.CommitOutcome, error) {
	h.commitCalls.Add(1)
	return domain.CommitApplied, nil
}

type recordingHandler struct {
	taskType     domain.TaskType
	executeFn    func(context.Context, domain.TaskLease) (domain.TaskResult, error)
	commitFn     func(context.Context, domain.TaskLease, domain.TaskResult) (domain.CommitOutcome, error)
	executeCalls atomic.Int32
	commitCalls  atomic.Int32
	mu           sync.Mutex
	result       domain.TaskResult
}

func (h *recordingHandler) Type() domain.TaskType { return h.taskType }
func (h *recordingHandler) Execute(ctx context.Context, lease domain.TaskLease) (domain.TaskResult, error) {
	h.executeCalls.Add(1)
	if h.executeFn != nil {
		return h.executeFn(ctx, lease)
	}
	return domain.TaskResult{Disposition: domain.TaskPublish}, nil
}
func (h *recordingHandler) Commit(ctx context.Context, lease domain.TaskLease, result domain.TaskResult) (domain.CommitOutcome, error) {
	h.commitCalls.Add(1)
	h.mu.Lock()
	h.result = result
	h.mu.Unlock()
	if h.commitFn != nil {
		return h.commitFn(ctx, lease, result)
	}
	return domain.CommitApplied, nil
}
func (h *recordingHandler) lastResult() domain.TaskResult {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.result
}

type failCall struct {
	lease       domain.TaskLease
	failure     domain.TaskFailure
	availableAt time.Time
}

type taskStoreFake struct {
	mu               sync.Mutex
	leases           []domain.TaskLease
	claimN           int
	heartbeatResults []bool
	heartbeatN       int
	failUpdated      *bool
	fails            []failCall
}

func (s *taskStoreFake) Claim(_ context.Context, owner string, _ time.Duration, types []domain.TaskType) (domain.TaskLease, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.claimN < len(s.leases) {
		lease := s.leases[s.claimN]
		s.claimN++
		if lease.LeaseOwner == "" {
			lease.LeaseOwner = owner
		}
		if len(types) > 0 && lease.Type == "" {
			lease.Type = types[0]
		}
		return lease, true, nil
	}
	if len(s.leases) == 0 && s.claimN == 0 {
		s.claimN++
		typ := domain.TaskType("assessment")
		if len(types) > 0 {
			typ = types[0]
		}
		return domain.TaskLease{
			Task:       domain.Task{ID: "task-1", Type: typ, Attempt: 1, MaxAttempts: 3},
			LeaseToken: "token-1",
			LeaseOwner: owner,
		}, true, nil
	}
	return domain.TaskLease{}, false, nil
}

func (s *taskStoreFake) Heartbeat(context.Context, domain.TaskLease, time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.heartbeatN < len(s.heartbeatResults) {
		ok := s.heartbeatResults[s.heartbeatN]
		s.heartbeatN++
		return ok, nil
	}
	return true, nil
}

func (s *taskStoreFake) Fail(_ context.Context, lease domain.TaskLease, failure domain.TaskFailure, availableAt time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fails = append(s.fails, failCall{lease: lease, failure: failure, availableAt: availableAt})
	if s.failUpdated != nil {
		return *s.failUpdated, nil
	}
	return true, nil
}

func (s *taskStoreFake) failCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.fails)
}

func (s *taskStoreFake) lastFail() failCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fails[len(s.fails)-1]
}

func (s *taskStoreFake) claimCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.claimN
}
