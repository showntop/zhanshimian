package execution

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

var (
	ctx         = context.Background()
	userID      = "11111111-1111-1111-1111-111111111111"
	planSetID   = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	variantA    = "22222222-2222-2222-2222-222222222222"
	variantB    = "33333333-3333-3333-3333-333333333333"
	selectionID = "44444444-4444-4444-4444-444444444444"
	stepHair    = "55555555-5555-5555-5555-555555555555"
	stepMakeup  = "66666666-6666-6666-6666-666666666666"
	stepOutfit  = "77777777-7777-7777-7777-777777777777"
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

func TestCreateExecutionCopiesPlanStepsOnce(t *testing.T) {
	repo := newFakeRepository()
	repo.planSteps = []domain.PlanStep{
		{ID: stepHair, Category: "hair", Action: "keep", Title: "保留偏分", Summary: "整理分缝", Position: 1},
		{ID: stepMakeup, Category: "makeup", Action: "adjust", Title: "降低对比", Summary: "薄涂", Position: 2},
		{ID: stepOutfit, Category: "outfit", Action: "adjust", Title: "换浅色内搭", Summary: "保留外套", Position: 3},
	}
	execution, created, err := New(repo).CreateExecution(ctx, userID, selectionID, CreateExecutionInput{IdempotencyKey: "execution-1"})
	if err != nil || !created || len(execution.Steps) != 3 {
		t.Fatalf("%#v %v", execution, err)
	}
	repo.planSteps[0].Title = "被改动的源步骤"
	again, _, _ := New(repo).CreateExecution(ctx, userID, selectionID, CreateExecutionInput{IdempotencyKey: "execution-1"})
	if again.Steps[0].Title != "保留偏分" {
		t.Fatalf("snapshot changed with source: %#v", again.Steps[0])
	}
}

// fakeRepository 用内存 map 模拟幂等重放与冲突语义。
type fakeRepository struct {
	byKey     map[string]domain.PlanSelection
	hashByKey map[string]string

	planSteps        []domain.PlanStep
	executionByKey   map[string]domain.Execution
	executionHash    map[string]string
	executionBySel   map[string]domain.Execution
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		byKey:          map[string]domain.PlanSelection{},
		hashByKey:      map[string]string{},
		executionByKey: map[string]domain.Execution{},
		executionHash:  map[string]string{},
		executionBySel: map[string]domain.Execution{},
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

func (f *fakeRepository) CreateExecutionFromSelection(_ context.Context, command CreateExecutionCommand) (domain.Execution, bool, error) {
	if existing, ok := f.executionByKey[command.IdempotencyKey]; ok {
		if f.executionHash[command.IdempotencyKey] != command.RequestHash {
			return domain.Execution{}, false, ErrIdempotencyConflict
		}
		return existing, false, nil
	}
	if existing, ok := f.executionBySel[command.SelectionID]; ok {
		f.executionByKey[command.IdempotencyKey] = existing
		f.executionHash[command.IdempotencyKey] = command.RequestHash
		return existing, false, nil
	}
	steps := make([]domain.ExecutionStep, 0, len(f.planSteps))
	for _, p := range f.planSteps {
		details, _ := json.Marshal(p.Details)
		steps = append(steps, domain.ExecutionStep{
			ID:               uuid.NewString(),
			SourcePlanStepID: p.ID,
			Category:         string(p.Category),
			Action:           string(p.Action),
			Title:            p.Title,
			Summary:          p.Summary,
			Details:          details,
			Position:         p.Position,
		})
	}
	execution := domain.Execution{
		ID:          uuid.NewString(),
		SelectionID: command.SelectionID,
		State:       domain.ExecutionPlanned,
		Version:     1,
		Steps:       steps,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	f.executionByKey[command.IdempotencyKey] = execution
	f.executionHash[command.IdempotencyKey] = command.RequestHash
	f.executionBySel[command.SelectionID] = execution
	return execution, true, nil
}

func (f *fakeRepository) GetExecution(_ context.Context, _, executionID string) (domain.Execution, error) {
	for _, e := range f.executionByKey {
		if e.ID == executionID {
			return e, nil
		}
	}
	return domain.Execution{}, repository.ErrNotFound
}
