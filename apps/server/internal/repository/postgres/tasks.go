package postgres

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

const taskLeaseReturning = `
	t.id::text, t.user_id::text, t.operation_id::text, t.type, t.subject_type, t.subject_id::text,
	t.subject_generation, t.payload_version, t.payload, t.dedupe_key, t.status, t.priority,
	t.attempt, t.max_attempts, t.available_at, t.lease_token::text, t.lease_owner,
	t.lease_expires_at, t.heartbeat_at, t.cancel_requested_at, t.progress_bps, t.stage_code,
	t.error_class, t.error_code, t.created_at, t.updated_at, t.finished_at`

const leaseGuard = `id=$1::uuid AND lease_token=$2::uuid AND lease_owner=$3 AND status='leased' AND lease_expires_at>now()`

func (s *Store) Claim(ctx context.Context, owner string, lease time.Duration, types []domain.TaskType) (domain.TaskLease, bool, error) {
	claimed, err := scanTaskLease(s.pool.QueryRow(ctx, `
		WITH candidate AS (
			SELECT id
			FROM tasks
			WHERE type = ANY($1::text[])
			  AND cancel_requested_at IS NULL
			  AND attempt < max_attempts
			  AND (
			    (status IN ('queued','retry_wait') AND available_at <= now())
			    OR (status='leased' AND lease_expires_at <= now())
			  )
			ORDER BY priority DESC, available_at, created_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		UPDATE tasks t
		SET status='leased', attempt=t.attempt+1, lease_token=gen_random_uuid(),
		    lease_owner=$2, lease_expires_at=now()+$3::interval,
		    heartbeat_at=now(), updated_at=now()
		FROM candidate
		WHERE t.id=candidate.id
		RETURNING`+taskLeaseReturning, typeNames(types), owner, pgInterval(lease)))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.TaskLease{}, false, nil
	}
	if err != nil {
		return domain.TaskLease{}, false, err
	}
	return claimed, true, nil
}

func (s *Store) Heartbeat(ctx context.Context, lease domain.TaskLease, extend time.Duration) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE tasks
		SET lease_expires_at=now()+$4::interval, heartbeat_at=now(), updated_at=now()
		WHERE `+leaseGuard, lease.ID, lease.LeaseToken, lease.LeaseOwner, pgInterval(extend))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (s *Store) CommitLeasedTask(ctx context.Context, lease domain.TaskLease, subjectGeneration int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var currentGeneration int64
	err = tx.QueryRow(ctx, `
		SELECT subject_generation
		FROM tasks
		WHERE id=$1::uuid AND user_id=$2::uuid AND lease_token=$3::uuid
		  AND lease_owner=$4 AND status='leased' AND lease_expires_at>now()
		FOR UPDATE`, lease.ID, lease.UserID, lease.LeaseToken, lease.LeaseOwner).Scan(&currentGeneration)
	if errors.Is(err, pgx.ErrNoRows) {
		return repository.ErrLeaseLost
	}
	if err != nil {
		return err
	}

	taskStatus := domain.TaskSucceeded
	opStatus := domain.OperationSucceeded
	if currentGeneration != subjectGeneration {
		taskStatus = domain.TaskSuperseded
		opStatus = domain.OperationSuperseded
	}

	tag, err := tx.Exec(ctx, `
		UPDATE tasks
		SET status=$5,
		    lease_token=NULL,
		    lease_owner=NULL,
		    lease_expires_at=NULL,
		    progress_bps=10000,
		    finished_at=now(),
		    updated_at=now()
		WHERE id=$1::uuid AND user_id=$2::uuid AND lease_token=$3::uuid
		  AND lease_owner=$4 AND status='leased' AND lease_expires_at>now()`,
		lease.ID, lease.UserID, lease.LeaseToken, lease.LeaseOwner, taskStatus)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return repository.ErrLeaseLost
	}

	tag, err = tx.Exec(ctx, `
		UPDATE operations
		SET status=$3,
		    progress_bps=CASE WHEN $3='succeeded' THEN 10000 ELSE progress_bps END,
		    finished_at=now(),
		    updated_at=now(),
		    version=version+1
		WHERE id=$1::uuid AND user_id=$2::uuid`,
		lease.OperationID, lease.UserID, opStatus)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return repository.ErrNotFound
	}
	return tx.Commit(ctx)
}

func (s *Store) Fail(ctx context.Context, lease domain.TaskLease, failure domain.TaskFailure, availableAt time.Time) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	var status string
	err = tx.QueryRow(ctx, `
		UPDATE tasks
		SET
			status = CASE
				WHEN $4 = 'superseded' THEN 'superseded'
				WHEN $4 IN ('transient','throttled') AND attempt < max_attempts THEN 'retry_wait'
				ELSE 'failed'
			END,
			available_at = CASE
				WHEN $4 IN ('transient','throttled') AND attempt < max_attempts THEN $6
				ELSE available_at
			END,
			error_class = $4,
			error_code = $5,
			lease_token = NULL,
			lease_owner = NULL,
			lease_expires_at = NULL,
			finished_at = CASE
				WHEN $4 IN ('transient','throttled') AND attempt < max_attempts THEN NULL
				ELSE now()
			END,
			updated_at = now()
		WHERE `+leaseGuard+`
		RETURNING status`, lease.ID, lease.LeaseToken, lease.LeaseOwner, string(failure.Class), failure.Code, availableAt).
		Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	// 退避重试期间同步公开 Operation 状态,客户端轮询能看到"重试中"而不是
	// 进度冻结。终态(failed/superseded)的 Operation 写总仍归各域 commit
	// 路径(如 failAssessmentTx),这里只覆盖 retry_wait,避免双写打架。
	if status == string(domain.TaskRetryWait) {
		if _, err = tx.Exec(ctx, `
			UPDATE operations
			SET status='retrying', retryable=true, updated_at=now(), version=version+1
			WHERE id=$1::uuid AND user_id=$2::uuid
			  AND status NOT IN ('succeeded','failed','cancelled','superseded')`,
			lease.OperationID, lease.UserID); err != nil {
			return false, err
		}
	}
	return true, tx.Commit(ctx)
}

func scanTaskLease(row rowScanner) (domain.TaskLease, error) {
	var lease domain.TaskLease
	var leaseToken, leaseOwner, errorClass, errorCode *string
	var heartbeatAt, cancelRequestedAt, finishedAt *time.Time
	err := row.Scan(
		&lease.ID, &lease.UserID, &lease.OperationID, &lease.Type, &lease.SubjectType, &lease.SubjectID,
		&lease.SubjectGeneration, &lease.PayloadVersion, &lease.Payload, &lease.DedupeKey, &lease.Status, &lease.Priority,
		&lease.Attempt, &lease.MaxAttempts, &lease.AvailableAt, &leaseToken, &leaseOwner,
		&lease.LeaseExpiresAt, &heartbeatAt, &cancelRequestedAt, &lease.ProgressBPS, &lease.StageCode,
		&errorClass, &errorCode, &lease.CreatedAt, &lease.UpdatedAt, &finishedAt,
	)
	if err != nil {
		return domain.TaskLease{}, err
	}
	if leaseToken != nil {
		lease.LeaseToken = *leaseToken
	}
	if leaseOwner != nil {
		lease.LeaseOwner = *leaseOwner
	}
	lease.HeartbeatAt = heartbeatAt
	lease.CancelRequestedAt = cancelRequestedAt
	if errorClass != nil {
		lease.ErrorClass = domain.ErrorClass(*errorClass)
	}
	if errorCode != nil {
		lease.ErrorCode = *errorCode
	}
	lease.FinishedAt = finishedAt
	return lease, nil
}

func typeNames(types []domain.TaskType) []string {
	names := make([]string, len(types))
	for i, taskType := range types {
		names[i] = string(taskType)
	}
	return names
}

func pgInterval(d time.Duration) string {
	return strconv.FormatInt(d.Milliseconds(), 10) + " milliseconds"
}
