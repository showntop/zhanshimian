package operation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

func TestGetHidesOtherUsersOperations(t *testing.T) {
	svc := New(newReaderFake(domain.Operation{
		ID: "op1", UserID: "u1", Kind: domain.OperationAssessment, Status: domain.OperationRunning,
	}))
	_, err := svc.Get(context.Background(), "u2", "op1")
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("Get error = %v, want ErrNotFound", err)
	}
}

func TestGetManyOperationsRejectsEmptyAndTooMany(t *testing.T) {
	svc := New(newReaderFake())
	if _, err := svc.GetMany(context.Background(), "u1", nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty error = %v, want ErrValidation", err)
	}
	ids := make([]string, 21)
	for i := range ids {
		ids[i] = uuid.NewString()
	}
	if _, err := svc.GetMany(context.Background(), "u1", ids); !errors.Is(err, ErrValidation) {
		t.Fatalf("too many error = %v, want ErrValidation", err)
	}
}

func TestGetManyOperationsRejectsDuplicatesAndInvalidIDs(t *testing.T) {
	svc := New(newReaderFake())
	id := uuid.NewString()
	if _, err := svc.GetMany(context.Background(), "u1", []string{id, id}); !errors.Is(err, ErrValidation) {
		t.Fatalf("duplicate error = %v, want ErrValidation", err)
	}
	if _, err := svc.GetMany(context.Background(), "u1", []string{"not-a-uuid"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("invalid error = %v, want ErrValidation", err)
	}
}

func TestGetManyOperationsMissingIDReturnsNotFound(t *testing.T) {
	owned := uuid.NewString()
	other := uuid.NewString()
	svc := New(newReaderFake(
		domain.Operation{ID: owned, UserID: "u1", Kind: domain.OperationAssessment, Status: domain.OperationRunning},
		domain.Operation{ID: other, UserID: "u2", Kind: domain.OperationAssessment, Status: domain.OperationRunning},
	))
	_, err := svc.GetMany(context.Background(), "u1", []string{owned, other})
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("GetMany error = %v, want ErrNotFound", err)
	}
}

func TestGetManyOperationsPreservesRequestOrder(t *testing.T) {
	first := uuid.NewString()
	second := uuid.NewString()
	svc := New(newReaderFake(
		domain.Operation{ID: second, UserID: "u1", Kind: domain.OperationRender, Status: domain.OperationSucceeded},
		domain.Operation{ID: first, UserID: "u1", Kind: domain.OperationAssessment, Status: domain.OperationAccepted},
	))
	got, err := svc.GetMany(context.Background(), "u1", []string{first, second})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != first || got[1].ID != second {
		t.Fatalf("order = %#v", idsOf(got))
	}
}

func TestGetReturnsOwnedOperation(t *testing.T) {
	svc := New(newReaderFake(domain.Operation{
		ID: "op1", UserID: "u1", Kind: domain.OperationAssessment,
		Status: domain.OperationRunning, ProgressBPS: 3500, StageCode: "photo.technical_check",
	}))
	got, err := svc.Get(context.Background(), "u1", "op1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "op1" || got.UserID != "u1" || got.ProgressBPS != 3500 {
		t.Fatalf("operation = %#v", got)
	}
}

func idsOf(ops []domain.Operation) string {
	ids := make([]string, 0, len(ops))
	for _, op := range ops {
		ids = append(ids, op.ID)
	}
	return strings.Join(ids, ",")
}

type readerFake struct {
	ops map[string]domain.Operation
}

func newReaderFake(ops ...domain.Operation) *readerFake {
	fake := &readerFake{ops: map[string]domain.Operation{}}
	for _, op := range ops {
		fake.ops[op.ID] = op
	}
	return fake
}

func (r *readerFake) GetOperation(_ context.Context, userID, id string) (domain.Operation, error) {
	op, ok := r.ops[id]
	if !ok || op.UserID != userID {
		return domain.Operation{}, repository.ErrNotFound
	}
	return op, nil
}

func (r *readerFake) GetOperations(ctx context.Context, userID string, ids []string) ([]domain.Operation, error) {
	out := make([]domain.Operation, 0, len(ids))
	for _, id := range ids {
		op, err := r.GetOperation(ctx, userID, id)
		if err != nil {
			return nil, err
		}
		out = append(out, op)
	}
	return out, nil
}

// ---- stale 孤儿兜底：worker 整体死亡时读路径落失败终态（旧线 GetAnalysis 同款） ----

type staleFailerFake struct {
	ops     map[string]domain.Operation
	calls   int
	fail    bool
	err     error
	idleFor time.Duration
	code    string
	message string
}

func (f *staleFailerFake) FailStaleOperation(_ context.Context, _, id string, idleFor time.Duration, code, publicMessage string) (bool, error) {
	f.calls++
	f.idleFor, f.code, f.message = idleFor, code, publicMessage
	if f.err != nil {
		return false, f.err
	}
	if !f.fail {
		return false, nil
	}
	op := f.ops[id]
	op.Status = domain.OperationFailed
	op.ErrorCode = code
	op.PublicMessage = publicMessage
	op.Retryable = true
	f.ops[id] = op
	return true, nil
}

func TestGetFailsStaleRunningOperation(t *testing.T) {
	op := domain.Operation{
		ID: "op-1", UserID: "u1", Kind: domain.OperationRender, Status: domain.OperationRunning,
		UpdatedAt: time.Now().Add(-12 * time.Minute),
	}
	reader := newReaderFake(op)
	failer := &staleFailerFake{ops: reader.ops, fail: true}
	svc := New(reader).WithStaleFailer(failer)

	got, err := svc.Get(context.Background(), "u1", "op-1")
	if err != nil {
		t.Fatal(err)
	}
	if failer.calls != 1 || failer.idleFor != 11*time.Minute {
		t.Fatalf("failer calls=%d idleFor=%v, want 1/11m", failer.calls, failer.idleFor)
	}
	if got.Status != domain.OperationFailed || !got.Retryable || got.PublicMessage == "" || got.ErrorCode == "" {
		t.Fatalf("stale operation = %#v", got)
	}
}

func TestGetFailsStaleQueuedOperationWithWiderThreshold(t *testing.T) {
	op := domain.Operation{
		ID: "op-1", UserID: "u1", Kind: domain.OperationAssessment, Status: domain.OperationAccepted,
		UpdatedAt: time.Now().Add(-21 * time.Minute),
	}
	reader := newReaderFake(op)
	failer := &staleFailerFake{ops: reader.ops, fail: true}
	svc := New(reader).WithStaleFailer(failer)

	got, err := svc.Get(context.Background(), "u1", "op-1")
	if err != nil {
		t.Fatal(err)
	}
	if failer.idleFor != 20*time.Minute {
		t.Fatalf("queued idleFor = %v, want 20m", failer.idleFor)
	}
	if got.Status != domain.OperationFailed {
		t.Fatalf("status = %q, want failed", got.Status)
	}
}

// 新鲜在途行不动：worker 活着时心跳/进度在刷新，读路径不能误杀。
func TestGetKeepsFreshInFlightOperation(t *testing.T) {
	for _, status := range []domain.OperationStatus{domain.OperationAccepted, domain.OperationRunning, domain.OperationRetrying} {
		op := domain.Operation{
			ID: "op-1", UserID: "u1", Kind: domain.OperationRender, Status: status,
			UpdatedAt: time.Now().Add(-2 * time.Minute),
		}
		failer := &staleFailerFake{}
		svc := New(newReaderFake(op)).WithStaleFailer(failer)
		got, err := svc.Get(context.Background(), "u1", "op-1")
		if err != nil {
			t.Fatal(err)
		}
		if failer.calls != 0 || got.Status != status {
			t.Fatalf("status %q: failer calls=%d got=%q", status, failer.calls, got.Status)
		}
	}
}

// 终态行永不兜底。
func TestGetKeepsTerminalOperation(t *testing.T) {
	op := domain.Operation{
		ID: "op-1", UserID: "u1", Kind: domain.OperationRender, Status: domain.OperationSucceeded,
		UpdatedAt: time.Now().Add(-48 * time.Hour),
	}
	failer := &staleFailerFake{}
	svc := New(newReaderFake(op)).WithStaleFailer(failer)
	if _, err := svc.Get(context.Background(), "u1", "op-1"); err != nil {
		t.Fatal(err)
	}
	if failer.calls != 0 {
		t.Fatalf("terminal operation must never be swept: calls=%d", failer.calls)
	}
}

// CAS 未生效（worker 并发刷新了行）时返回原样，不伪造失败。
func TestGetReturnsOriginalWhenStaleWriteLosesRace(t *testing.T) {
	op := domain.Operation{
		ID: "op-1", UserID: "u1", Kind: domain.OperationRender, Status: domain.OperationRunning,
		UpdatedAt: time.Now().Add(-30 * time.Minute),
	}
	failer := &staleFailerFake{fail: false}
	svc := New(newReaderFake(op)).WithStaleFailer(failer)
	got, err := svc.Get(context.Background(), "u1", "op-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.OperationRunning {
		t.Fatalf("status = %q, want running (lost race keeps original)", got.Status)
	}
}

// 批量读路径同样兜底。
func TestGetManySweepsStaleOperations(t *testing.T) {
	staleID, freshID := uuid.NewString(), uuid.NewString()
	stale := domain.Operation{
		ID: staleID, UserID: "u1", Kind: domain.OperationRender, Status: domain.OperationRunning,
		UpdatedAt: time.Now().Add(-15 * time.Minute),
	}
	fresh := domain.Operation{
		ID: freshID, UserID: "u1", Kind: domain.OperationAssessment, Status: domain.OperationRunning,
		UpdatedAt: time.Now(),
	}
	reader := newReaderFake(stale, fresh)
	failer := &staleFailerFake{ops: reader.ops, fail: true}
	svc := New(reader).WithStaleFailer(failer)

	got, err := svc.GetMany(context.Background(), "u1", []string{staleID, freshID})
	if err != nil {
		t.Fatal(err)
	}
	if failer.calls != 1 {
		t.Fatalf("failer calls = %d, want 1 (only the stale row)", failer.calls)
	}
	if got[0].Status != domain.OperationFailed || got[1].Status != domain.OperationRunning {
		t.Fatalf("statuses = %q,%q", got[0].Status, got[1].Status)
	}
}
