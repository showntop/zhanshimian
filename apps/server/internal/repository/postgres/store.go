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

type Store struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

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
