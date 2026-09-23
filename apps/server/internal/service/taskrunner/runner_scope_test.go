package taskrunner_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/zhanshimian/server/internal/domain"
)

// AI 台账按 (user, operation, task, attempt) 归集：runner 是唯一能同时看到
// lease 与 handler 调用的地方，必须在 Execute 前把任务身份注入 ctx。
func TestRunnerInjectsInvocationScopeIntoExecute(t *testing.T) {
	store := &taskStoreFake{leases: []domain.TaskLease{{
		Task: domain.Task{
			ID: "task-9", UserID: "user-9", OperationID: "op-9",
			Type: "assessment", Attempt: 2, MaxAttempts: 3,
		},
		LeaseToken: "token-9", LeaseOwner: "worker-1",
	}}}
	var seen atomic.Value
	handler := &recordingHandler{taskType: "assessment"}
	handler.executeFn = func(ctx context.Context, _ domain.TaskLease) (domain.TaskResult, error) {
		scope, ok := domain.InvocationScopeFrom(ctx)
		if !ok {
			seen.Store("missing")
		} else {
			seen.Store(scope)
		}
		return domain.TaskResult{Disposition: domain.TaskPublish}, nil
	}
	runUntil(t, store, handler, func() bool { return seen.Load() != nil })

	scope, ok := seen.Load().(domain.InvocationScope)
	if !ok {
		t.Fatalf("execute ctx must carry the invocation scope, got %v", seen.Load())
	}
	if scope.UserID != "user-9" || scope.OperationID != "op-9" || scope.TaskID != "task-9" || scope.AttemptNo != 2 {
		t.Fatalf("scope must mirror the lease identity: %#v", scope)
	}
}
