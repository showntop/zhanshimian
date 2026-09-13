package execution

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

var (
	ctx       = context.Background()
	userID    = "11111111-1111-1111-1111-111111111111"
	planSetID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	variantA  = "22222222-2222-2222-2222-222222222222"
	variantB  = "33333333-3333-3333-3333-333333333333"
)

func TestPutSelectionReturnsReplayForSameKeyAndBody(t *testing.T) {
	repo := newFakeRepository()
	svc := New(repo)
	in := PutSelectionInput{
		PlanVariantID:  variantA,
		IdempotencyKey: "select-1",
	}
	first, created, err := svc.PutSelection(ctx, userID, planSetID, in)
	if err != nil || !created {
		t.Fatalf("first: %#v %v", first, err)
	}
	second, created, err := svc.PutSelection(ctx, userID, planSetID, in)
	if err != nil || created || second.ID != first.ID {
		t.Fatalf("replay: %#v created=%v err=%v", second, created, err)
	}
}

func TestPutSelectionRejectsSameKeyWithDifferentVariant(t *testing.T) {
	repo := newFakeRepository()
	svc := New(repo)
	_, _, _ = svc.PutSelection(ctx, userID, planSetID, PutSelectionInput{
		PlanVariantID: variantA, IdempotencyKey: "select-1",
	})
	_, _, err := svc.PutSelection(ctx, userID, planSetID, PutSelectionInput{
		PlanVariantID: variantB, IdempotencyKey: "select-1",
	})
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("err=%v", err)
	}
}

// fakeRepository 用内存 map 模拟幂等重放与冲突语义。
type fakeRepository struct {
	byKey     map[string]domain.PlanSelection
	hashByKey map[string]string
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		byKey:     map[string]domain.PlanSelection{},
		hashByKey: map[string]string{},
	}
}

func (f *fakeRepository) CreateSelection(_ context.Context, command CreateSelectionCommand) (domain.PlanSelection, bool, error) {
	if existing, ok := f.byKey[command.IdempotencyKey]; ok {
		if f.hashByKey[command.IdempotencyKey] != command.RequestHash {
			return domain.PlanSelection{}, false, ErrIdempotencyConflict
		}
		return existing, false, nil
	}
	selection := domain.PlanSelection{
		ID:                  uuid.NewString(),
		PlanSetID:           command.PlanSetID,
		PlanVariantID:       command.PlanVariantID,
		RenderPublicationID: command.RenderPublicationID,
		CreatedAt:           time.Now(),
	}
	f.byKey[command.IdempotencyKey] = selection
	f.hashByKey[command.IdempotencyKey] = command.RequestHash
	return selection, true, nil
}

func (f *fakeRepository) GetSelection(_ context.Context, _, _ string) (domain.PlanSelection, error) {
	return domain.PlanSelection{}, repository.ErrNotFound
}
