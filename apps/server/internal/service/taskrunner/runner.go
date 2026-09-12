package taskrunner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zhanshimian/server/internal/domain"
)

const (
	codeUnclassified = "unclassified"
	codeHandlerPanic = "handler_panic"
)

type TaskError struct {
	Class domain.ErrorClass
	Code  string
	err   error
}

func (e *TaskError) Error() string {
	if e == nil {
		return "task error"
	}
	if e.err != nil {
		return fmt.Sprintf("%s/%s: %v", e.Class, e.Code, e.err)
	}
	return fmt.Sprintf("%s/%s", e.Class, e.Code)
}

func (e *TaskError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

type Runner struct {
	store        TaskStore
	registry     *Registry
	workerID     string
	pollInterval time.Duration
	logger       *slog.Logger
	slots        map[domain.TaskType]chan struct{}
	wg           sync.WaitGroup
}

func NewRunner(store TaskStore, registry *Registry, workerID string, pollInterval time.Duration, logger *slog.Logger) *Runner {
	if logger == nil {
		logger = slog.Default()
	}
	slots := make(map[domain.TaskType]chan struct{}, len(registry.Types()))
	for _, taskType := range registry.Types() {
		def, _ := registry.Definition(taskType)
		slots[taskType] = make(chan struct{}, def.Concurrency)
	}
	return &Runner{
		store:        store,
		registry:     registry,
		workerID:     workerID,
		pollInterval: pollInterval,
		logger:       logger,
		slots:        slots,
	}
}

func (r *Runner) Run(ctx context.Context) {
	ticker := time.NewTicker(r.pollInterval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			r.wg.Wait()
			return
		}
		r.claimAvailable(ctx)
		select {
		case <-ctx.Done():
			r.wg.Wait()
			return
		case <-ticker.C:
		}
	}
}

func (r *Runner) claimAvailable(ctx context.Context) {
	for _, taskType := range r.registry.Types() {
		if ctx.Err() != nil {
			return
		}
		if !r.tryAcquire(taskType) {
			continue
		}
		def, _ := r.registry.Definition(taskType)
		lease, ok, err := r.store.Claim(ctx, r.workerID, def.LeaseDuration, []domain.TaskType{taskType})
		if err != nil || !ok {
			r.release(taskType)
			if err != nil && ctx.Err() == nil {
				r.logger.Error("claim failed", "task_type", taskType, "error", err)
			}
			continue
		}
		r.wg.Add(1)
		go func(lease domain.TaskLease, def Definition) {
			defer r.wg.Done()
			defer r.release(lease.Type)
			r.process(ctx, lease, def)
		}(lease, def)
	}
}

func (r *Runner) tryAcquire(taskType domain.TaskType) bool {
	select {
	case r.slots[taskType] <- struct{}{}:
		return true
	default:
		return false
	}
}

func (r *Runner) release(taskType domain.TaskType) {
	select {
	case <-r.slots[taskType]:
	default:
	}
}

func (r *Runner) process(ctx context.Context, lease domain.TaskLease, def Definition) {
	handler, ok := r.registry.Handler(lease.Type)
	if !ok {
		return
	}

	workCtx, cancelWork := context.WithCancel(ctx)
	defer cancelWork()

	var leaseLost atomic.Bool
	heartbeatDone := make(chan struct{})
	go r.heartbeat(workCtx, lease, def, &leaseLost, cancelWork, heartbeatDone)

	execCtx, cancelExec := context.WithTimeout(workCtx, def.Timeout)
	result, err := r.execute(execCtx, handler, lease)
	cancelExec()
	cancelWork()
	<-heartbeatDone

	if leaseLost.Load() || ctx.Err() != nil {
		return
	}

	if err == nil {
		r.commit(ctx, handler, lease, result)
		return
	}

	class, code := classifyExecuteError(err)
	failure := domain.TaskFailure{Class: class, Code: code}
	if class == domain.ErrorSuperseded {
		r.fail(ctx, lease, failure, time.Time{})
		return
	}
	if class.Retryable() && lease.Attempt < def.MaxAttempts {
		availableAt := time.Now().Add(def.RetryBackoff(lease.Attempt))
		r.fail(ctx, lease, failure, availableAt)
		return
	}
	r.commit(ctx, handler, lease, domain.TaskResult{
		Disposition: domain.TaskDomainFail,
		Failure:     &failure,
	})
}

func (r *Runner) execute(ctx context.Context, handler Handler, lease domain.TaskLease) (result domain.TaskResult, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			r.logger.Error("handler panic", "task_id", lease.ID, "trace_id", traceID(ctx, lease))
			err = &TaskError{Class: domain.ErrorTransient, Code: codeHandlerPanic}
			result = domain.TaskResult{}
		}
	}()
	return handler.Execute(ctx, lease)
}

func (r *Runner) heartbeat(ctx context.Context, lease domain.TaskLease, def Definition, lost *atomic.Bool, cancel context.CancelFunc, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(def.HeartbeatEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			updated, err := r.store.Heartbeat(ctx, lease, def.LeaseDuration)
			if ctx.Err() != nil {
				return
			}
			if err != nil || !updated {
				lost.Store(true)
				cancel()
				return
			}
		}
	}
}

func (r *Runner) commit(ctx context.Context, handler Handler, lease domain.TaskLease, result domain.TaskResult) {
	defer func() {
		if rec := recover(); rec != nil {
			r.logger.Error("handler panic", "task_id", lease.ID, "trace_id", traceID(ctx, lease))
		}
	}()
	_, err := handler.Commit(ctx, lease, result)
	if err != nil {
		r.logger.Error("commit failed", "task_id", lease.ID, "error", err)
	}
}

func (r *Runner) fail(ctx context.Context, lease domain.TaskLease, failure domain.TaskFailure, availableAt time.Time) {
	updated, err := r.store.Fail(ctx, lease, failure, availableAt)
	if err != nil {
		r.logger.Error("fail failed", "task_id", lease.ID, "error", err)
		return
	}
	if !updated {
		return
	}
}

func classifyExecuteError(err error) (domain.ErrorClass, string) {
	var te *TaskError
	if errors.As(err, &te) && te != nil && te.Class != "" {
		code := te.Code
		if code == "" {
			code = codeUnclassified
		}
		return te.Class, code
	}
	return domain.ErrorPermanent, codeUnclassified
}

func traceID(_ context.Context, lease domain.TaskLease) string {
	return lease.OperationID
}
