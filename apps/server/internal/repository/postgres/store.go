package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

// leasedTaskStore matches service/taskrunner.TaskStore in the interface ledger.
type leasedTaskStore interface {
	Claim(context.Context, string, time.Duration, []domain.TaskType) (domain.TaskLease, bool, error)
	Heartbeat(context.Context, domain.TaskLease, time.Duration) (bool, error)
	Fail(context.Context, domain.TaskLease, domain.TaskFailure, time.Time) (bool, error)
}

var _ leasedTaskStore = (*Store)(nil)

type Store struct {
	pool *pgxpool.Pool
	// skipCreditCharge 对应未开通支付的部署：次数不足不拦生成（reserve 记 0
	// 扣减台账），每日配额与欢迎礼不受影响。退款按 reserve 实际扣减退回。
	skipCreditCharge bool
}

// StoreOption 调整 Store 的可选行为；默认即生产安全姿态（扣次门禁开启）。
type StoreOption func(*Store)

// WithSkipCreditCharge 关闭次数不足拦截（支付未开通时使用）。
func WithSkipCreditCharge(skip bool) StoreOption {
	return func(s *Store) { s.skipCreditCharge = skip }
}

func New(pool *pgxpool.Pool, opts ...StoreOption) *Store {
	s := &Store{pool: pool}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func mapNotFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return repository.ErrNotFound
	}
	return err
}

// isForeignKeyViolation reports a Postgres 23503 error raised because the row
// referenced by the named constraint disappeared underneath an in-flight
// worker job (for example a user data wipe cascading to analyses).
func isForeignKeyViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503" && pgErr.ConstraintName == constraint
}
