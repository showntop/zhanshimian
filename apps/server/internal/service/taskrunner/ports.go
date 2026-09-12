package taskrunner

import (
	"context"
	"time"

	"github.com/zhanshimian/server/internal/domain"
)

type TaskStore interface {
	Claim(context.Context, string, time.Duration, []domain.TaskType) (domain.TaskLease, bool, error)
	Heartbeat(context.Context, domain.TaskLease, time.Duration) (bool, error)
	Fail(context.Context, domain.TaskLease, domain.TaskFailure, time.Time) (bool, error)
}

type Handler interface {
	Type() domain.TaskType
	Execute(context.Context, domain.TaskLease) (domain.TaskResult, error)
	Commit(context.Context, domain.TaskLease, domain.TaskResult) (domain.CommitOutcome, error)
}
