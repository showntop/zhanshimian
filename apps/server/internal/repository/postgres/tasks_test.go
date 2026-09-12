package postgres

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/testutil"
)

func TestClaimUsesSkipLockedAndClaimsEachTaskOnce(t *testing.T) {
	store, pool := newTaskStore(t)
	enqueueTasks(t, pool, 20, "assessment")
	var claimed sync.Map
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		owner := fmt.Sprintf("worker-%d", worker)
		go func() {
			defer wg.Done()
			for {
				lease, ok, err := store.Claim(context.Background(), owner, 30*time.Second, []domain.TaskType{"assessment"})
				if err != nil {
					errs <- err
					return
				}
				if !ok {
					return
				}
				if _, loaded := claimed.LoadOrStore(lease.ID, true); loaded {
					errs <- fmt.Errorf("task %s claimed twice", lease.ID)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if got := mapSize(&claimed); got != 20 {
		t.Fatalf("claimed %d tasks, want 20", got)
	}
}

func TestExpiredLeaseIsReclaimedAndOldTokenCannotWrite(t *testing.T) {
	store, pool := newTaskStore(t)
	taskID := enqueueTask(t, pool, "assessment")
	oldLease, ok, err := store.Claim(context.Background(), "old", time.Second, []domain.TaskType{"assessment"})
	if err != nil || !ok {
		t.Fatalf("old claim: ok=%v err=%v", ok, err)
	}
	if oldLease.ID != taskID {
		t.Fatalf("claimed %s, want %s", oldLease.ID, taskID)
	}
	advanceLeaseExpiry(t, pool, taskID)
	newLease, ok, err := store.Claim(context.Background(), "new", 30*time.Second, []domain.TaskType{"assessment"})
	if err != nil || !ok {
		t.Fatalf("reclaim: ok=%v err=%v", ok, err)
	}
	if newLease.LeaseToken == oldLease.LeaseToken {
		t.Fatal("reclaim reused the old lease token")
	}
	if newLease.LeaseOwner != "new" {
		t.Fatalf("reclaim owner = %q", newLease.LeaseOwner)
	}
	updated, err := store.Heartbeat(context.Background(), oldLease, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if updated {
		t.Fatal("old-token heartbeat updated a row")
	}
	updated, err = store.Fail(context.Background(), oldLease, domain.TaskFailure{Class: domain.ErrorPermanent, Code: "gone"}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if updated {
		t.Fatal("old-token fail updated a row")
	}
	row := loadTaskRow(t, pool, taskID)
	if row.status != string(domain.TaskLeased) || row.leaseOwner != "new" || row.leaseToken != newLease.LeaseToken {
		t.Fatalf("newer lease overwritten: %+v", row)
	}
}

func TestHeartbeatExtendsActiveLease(t *testing.T) {
	store, pool := newTaskStore(t)
	taskID := enqueueTask(t, pool, "assessment")
	lease, ok, err := store.Claim(context.Background(), "worker-a", 30*time.Second, []domain.TaskType{"assessment"})
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	before := loadTaskRow(t, pool, taskID)
	updated, err := store.Heartbeat(context.Background(), lease, time.Minute)
	if err != nil || !updated {
		t.Fatalf("heartbeat: updated=%v err=%v", updated, err)
	}
	after := loadTaskRow(t, pool, taskID)
	if !after.leaseExpiresAt.After(before.leaseExpiresAt) {
		t.Fatalf("lease expiry %s did not move past %s", after.leaseExpiresAt, before.leaseExpiresAt)
	}
	if after.heartbeatAt.IsZero() || after.status != string(domain.TaskLeased) {
		t.Fatalf("heartbeat row = %+v", after)
	}
}

func TestFailRetryableTaskMovesToRetryWait(t *testing.T) {
	store, pool := newTaskStore(t)
	taskID := enqueueTask(t, pool, "assessment")
	lease, ok, err := store.Claim(context.Background(), "worker-a", 30*time.Second, []domain.TaskType{"assessment"})
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	availableAt := time.Now().Add(15 * time.Second).UTC().Truncate(time.Millisecond)
	updated, err := store.Fail(context.Background(), lease, domain.TaskFailure{Class: domain.ErrorTransient, Code: "provider_timeout"}, availableAt)
	if err != nil || !updated {
		t.Fatalf("fail: updated=%v err=%v", updated, err)
	}
	row := loadTaskRow(t, pool, taskID)
	if row.status != string(domain.TaskRetryWait) {
		t.Fatalf("status = %s, want retry_wait", row.status)
	}
	if row.errorClass != string(domain.ErrorTransient) || row.errorCode != "provider_timeout" {
		t.Fatalf("error = %s/%s", row.errorClass, row.errorCode)
	}
	if row.leaseToken != "" || row.leaseOwner != "" || row.leaseExpiresAt != (time.Time{}) {
		t.Fatalf("lease fields not cleared: %+v", row)
	}
	if row.finishedAt != (time.Time{}) {
		t.Fatalf("retry_wait finished_at = %s", row.finishedAt)
	}
	if row.availableAt.Sub(availableAt).Abs() > 2*time.Second {
		t.Fatalf("available_at = %s, want %s", row.availableAt, availableAt)
	}
}

func TestFailPermanentTaskAndQualityRejected(t *testing.T) {
	store, pool := newTaskStore(t)
	cases := []domain.ErrorClass{domain.ErrorPermanent, domain.ErrorQualityRejected}
	for _, class := range cases {
		taskID := enqueueTask(t, pool, "assessment")
		lease, ok, err := store.Claim(context.Background(), "worker-a", 30*time.Second, []domain.TaskType{"assessment"})
		if err != nil || !ok {
			t.Fatalf("%s claim: ok=%v err=%v", class, ok, err)
		}
		updated, err := store.Fail(context.Background(), lease, domain.TaskFailure{Class: class, Code: string(class)}, time.Time{})
		if err != nil || !updated {
			t.Fatalf("%s fail: updated=%v err=%v", class, updated, err)
		}
		row := loadTaskRow(t, pool, taskID)
		if row.status != string(domain.TaskFailed) || row.errorClass != string(class) {
			t.Fatalf("%s row = %+v", class, row)
		}
		if row.leaseToken != "" || row.finishedAt.IsZero() {
			t.Fatalf("%s did not clear lease / set finished: %+v", class, row)
		}
	}
}

func TestFailSupersededTask(t *testing.T) {
	store, pool := newTaskStore(t)
	taskID := enqueueTask(t, pool, "assessment")
	lease, ok, err := store.Claim(context.Background(), "worker-a", 30*time.Second, []domain.TaskType{"assessment"})
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	updated, err := store.Fail(context.Background(), lease, domain.TaskFailure{Class: domain.ErrorSuperseded, Code: "replaced"}, time.Time{})
	if err != nil || !updated {
		t.Fatalf("fail: updated=%v err=%v", updated, err)
	}
	row := loadTaskRow(t, pool, taskID)
	if row.status != string(domain.TaskSuperseded) {
		t.Fatalf("status = %s", row.status)
	}
	if row.leaseToken != "" || row.finishedAt.IsZero() {
		t.Fatalf("superseded row = %+v", row)
	}
}

func TestFailExhaustedRetryableTaskFailed(t *testing.T) {
	store, pool := newTaskStore(t)
	taskID := enqueueTask(t, pool, "assessment")
	if _, err := pool.Exec(context.Background(), `UPDATE tasks SET attempt=2, max_attempts=3 WHERE id=$1::uuid`, taskID); err != nil {
		t.Fatal(err)
	}
	lease, ok, err := store.Claim(context.Background(), "worker-a", 30*time.Second, []domain.TaskType{"assessment"})
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if lease.Attempt != 3 {
		t.Fatalf("attempt = %d, want 3", lease.Attempt)
	}
	updated, err := store.Fail(context.Background(), lease, domain.TaskFailure{Class: domain.ErrorThrottled, Code: "rate"}, time.Now().Add(time.Minute))
	if err != nil || !updated {
		t.Fatalf("fail: updated=%v err=%v", updated, err)
	}
	row := loadTaskRow(t, pool, taskID)
	if row.status != string(domain.TaskFailed) {
		t.Fatalf("exhausted status = %s, want failed", row.status)
	}
}

func TestClaimSkipsCancelRequestedAndExhausted(t *testing.T) {
	store, pool := newTaskStore(t)
	cancelID := enqueueTask(t, pool, "assessment")
	exhaustedID := enqueueTask(t, pool, "assessment")
	if _, err := pool.Exec(context.Background(), `UPDATE tasks SET cancel_requested_at=now() WHERE id=$1::uuid`, cancelID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE tasks SET attempt=3, max_attempts=3 WHERE id=$1::uuid`, exhaustedID); err != nil {
		t.Fatal(err)
	}
	_, ok, err := store.Claim(context.Background(), "worker-a", 30*time.Second, []domain.TaskType{"assessment"})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("claimed a cancelled or exhausted task")
	}
}

func TestClaimOrdersByPriorityThenAvailableAt(t *testing.T) {
	store, pool := newTaskStore(t)
	userID, opID := seedTaskOwner(t, pool)
	low := insertQueuedTask(t, pool, userID, opID, "assessment", 1, time.Now().Add(-time.Minute))
	high := insertQueuedTask(t, pool, userID, opID, "assessment", 10, time.Now())
	mid := insertQueuedTask(t, pool, userID, opID, "assessment", 5, time.Now().Add(-time.Hour))
	first, ok, err := store.Claim(context.Background(), "worker-a", 30*time.Second, []domain.TaskType{"assessment"})
	if err != nil || !ok || first.ID != high {
		t.Fatalf("first claim = %s ok=%v err=%v, want %s", first.ID, ok, err, high)
	}
	second, ok, err := store.Claim(context.Background(), "worker-a", 30*time.Second, []domain.TaskType{"assessment"})
	if err != nil || !ok || second.ID != mid {
		t.Fatalf("second claim = %s ok=%v err=%v, want %s", second.ID, ok, err, mid)
	}
	third, ok, err := store.Claim(context.Background(), "worker-a", 30*time.Second, []domain.TaskType{"assessment"})
	if err != nil || !ok || third.ID != low {
		t.Fatalf("third claim = %s ok=%v err=%v, want %s", third.ID, ok, err, low)
	}
}

func TestClaimIgnoresUnlistedTypes(t *testing.T) {
	store, pool := newTaskStore(t)
	enqueueTask(t, pool, "assessment")
	_, ok, err := store.Claim(context.Background(), "worker-a", 30*time.Second, []domain.TaskType{"render"})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("claimed a type that was not in the filter")
	}
}

func newTaskStore(t *testing.T) (*Store, *pgxpool.Pool) {
	t.Helper()
	pool := testutil.NewPostgres(t)
	return New(pool), pool
}

func enqueueTasks(t *testing.T, pool *pgxpool.Pool, n int, taskType string) {
	t.Helper()
	userID, opID := seedTaskOwner(t, pool)
	for i := 0; i < n; i++ {
		insertQueuedTask(t, pool, userID, opID, taskType, 0, time.Time{})
	}
}

func enqueueTask(t *testing.T, pool *pgxpool.Pool, taskType string) string {
	t.Helper()
	userID, opID := seedTaskOwner(t, pool)
	return insertQueuedTask(t, pool, userID, opID, taskType, 0, time.Time{})
}

func seedTaskOwner(t *testing.T, pool *pgxpool.Pool) (string, string) {
	t.Helper()
	store := New(pool)
	userID := insertUser(t, store)
	op := seedOperation(t, store, userID, domain.Operation{
		Kind:        domain.OperationAssessment,
		Status:      domain.OperationAccepted,
		SubjectType: "assessment",
	})
	return userID, op.ID
}

func insertQueuedTask(t *testing.T, pool *pgxpool.Pool, userID, operationID, taskType string, priority int, availableAt time.Time) string {
	t.Helper()
	var available any
	if !availableAt.IsZero() {
		available = availableAt
	}
	var id string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO tasks (
			user_id, operation_id, type, subject_type, subject_id, payload_version,
			payload, dedupe_key, status, priority, max_attempts, available_at, stage_code
		) VALUES (
			$1::uuid, $2::uuid, $3, 'assessment', $4::uuid, 1,
			'{}'::jsonb, $5, 'queued', $6, 3, COALESCE($7, now()), 'queued'
		) RETURNING id::text`,
		userID, operationID, taskType, uuid.NewString(), uuid.NewString(), priority, available,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert task: %v", err)
	}
	return id
}

func advanceLeaseExpiry(t *testing.T, pool *pgxpool.Pool, taskID string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `UPDATE tasks SET lease_expires_at=now() - interval '1 second' WHERE id=$1::uuid`, taskID); err != nil {
		t.Fatal(err)
	}
}

func mapSize(m *sync.Map) int {
	n := 0
	m.Range(func(any, any) bool {
		n++
		return true
	})
	return n
}

type taskRow struct {
	status         string
	leaseToken     string
	leaseOwner     string
	leaseExpiresAt time.Time
	heartbeatAt    time.Time
	availableAt    time.Time
	finishedAt     time.Time
	errorClass     string
	errorCode      string
}

func loadTaskRow(t *testing.T, pool *pgxpool.Pool, id string) taskRow {
	t.Helper()
	var row taskRow
	var token, owner, class, code *string
	var expires, heartbeat, finished *time.Time
	err := pool.QueryRow(context.Background(), `
		SELECT status, lease_token::text, lease_owner, lease_expires_at, heartbeat_at,
		       available_at, finished_at, error_class, error_code
		FROM tasks WHERE id=$1::uuid`, id).Scan(
		&row.status, &token, &owner, &expires, &heartbeat,
		&row.availableAt, &finished, &class, &code,
	)
	if err != nil {
		t.Fatalf("load task %s: %v", id, err)
	}
	if token != nil {
		row.leaseToken = *token
	}
	if owner != nil {
		row.leaseOwner = *owner
	}
	if expires != nil {
		row.leaseExpiresAt = *expires
	}
	if heartbeat != nil {
		row.heartbeatAt = *heartbeat
	}
	if finished != nil {
		row.finishedAt = *finished
	}
	if class != nil {
		row.errorClass = *class
	}
	if code != nil {
		row.errorCode = *code
	}
	return row
}
