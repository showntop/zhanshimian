package operation

import (
	"context"
	"errors"
	"strings"
	"testing"

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
