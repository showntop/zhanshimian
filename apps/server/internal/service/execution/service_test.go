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
	executionID = "88888888-8888-8888-8888-888888888888"
	fixedTime   = time.Now().Truncate(time.Second).UTC()
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

func TestAppendEventReplayDoesNotIncrementVersion(t *testing.T) {
	repo := newFakeRepositoryWithExecution(1)
	svc := New(repo)
	step := stepHair
	in := AppendEventInput{
		ClientEventID: "device-a-0001", Type: domain.EventStepCompleted,
		StepID: &step, OccurredAt: fixedTime, ExpectedVersion: 1,
	}
	first, err := svc.AppendEvent(ctx, userID, executionID, in)
	if err != nil || first.Execution.Version != 2 || first.Replayed {
		t.Fatalf("%#v %v", first, err)
	}
	second, err := svc.AppendEvent(ctx, userID, executionID, in)
	if err != nil || second.Execution.Version != 2 || !second.Replayed {
		t.Fatalf("replay must win before stale CAS: %#v %v", second, err)
	}
}

func TestAppendEventRejectsConcurrentDifferentEvent(t *testing.T) {
	repo := newFakeRepositoryWithExecution(2)
	step := stepMakeup
	_, err := New(repo).AppendEvent(ctx, userID, executionID, AppendEventInput{
		ClientEventID: "device-b-0001", Type: domain.EventStepCompleted,
		StepID: &step, OccurredAt: fixedTime, ExpectedVersion: 1,
	})
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("err=%v", err)
	}
}

// fakeRepository 用内存 map 模拟幂等重放、冲突与 CAS 语义。
type fakeRepository struct {
	byKey     map[string]domain.PlanSelection
	hashByKey map[string]string

	planSteps []domain.PlanStep

	executions     map[string]domain.Execution // by execution ID
	executionByKey map[string]string           // idempotency key -> execution ID
	executionHash  map[string]string           // idempotency key -> request hash
	executionBySel map[string]string           // selection ID -> execution ID

	events    map[string]map[string]domain.ExecutionEvent // executionID -> clientEventID -> event
	eventHash map[string]map[string]string                // executionID -> clientEventID -> hash
	completed map[string]map[string]bool                  // executionID -> stepID -> completed
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		byKey:          map[string]domain.PlanSelection{},
		hashByKey:      map[string]string{},
		executions:     map[string]domain.Execution{},
		executionByKey: map[string]string{},
		executionHash:  map[string]string{},
		executionBySel: map[string]string{},
		events:         map[string]map[string]domain.ExecutionEvent{},
		eventHash:      map[string]map[string]string{},
		completed:      map[string]map[string]bool{},
	}
}

// newFakeRepositoryWithExecution 预置一个指定版本的 Execution,供事件 CAS 测试。
func newFakeRepositoryWithExecution(version int) *fakeRepository {
	f := newFakeRepository()
	exec := domain.Execution{
		ID:          executionID,
		SelectionID: selectionID,
		State:       domain.ExecutionPlanned,
		Version:     version,
		Steps: []domain.ExecutionStep{
			{ID: stepHair, SourcePlanStepID: stepHair, Category: "hair", Action: "keep", Title: "保留偏分", Position: 1},
			{ID: stepMakeup, SourcePlanStepID: stepMakeup, Category: "makeup", Action: "adjust", Title: "降低对比", Position: 2},
			{ID: stepOutfit, SourcePlanStepID: stepOutfit, Category: "outfit", Action: "adjust", Title: "换浅色内搭", Position: 3},
		},
		CreatedAt: fixedTime,
		UpdatedAt: fixedTime,
	}
	f.executions[executionID] = exec
	f.executionBySel[selectionID] = executionID
	return f
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
	if id, ok := f.executionByKey[command.IdempotencyKey]; ok {
		if f.executionHash[command.IdempotencyKey] != command.RequestHash {
			return domain.Execution{}, false, ErrIdempotencyConflict
		}
		return f.executions[id], false, nil
	}
	if id, ok := f.executionBySel[command.SelectionID]; ok {
		f.executionByKey[command.IdempotencyKey] = id
		f.executionHash[command.IdempotencyKey] = command.RequestHash
		return f.executions[id], false, nil
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
	f.executions[execution.ID] = execution
	f.executionByKey[command.IdempotencyKey] = execution.ID
	f.executionHash[command.IdempotencyKey] = command.RequestHash
	f.executionBySel[command.SelectionID] = execution.ID
	return execution, true, nil
}

func (f *fakeRepository) GetExecution(_ context.Context, _, executionID string) (domain.Execution, error) {
	if e, ok := f.executions[executionID]; ok {
		return e, nil
	}
	return domain.Execution{}, repository.ErrNotFound
}

func (f *fakeRepository) AppendExecutionEvent(_ context.Context, command AppendEventCommand) (AppendEventResult, error) {
	if f.events[command.ExecutionID] == nil {
		f.events[command.ExecutionID] = map[string]domain.ExecutionEvent{}
		f.eventHash[command.ExecutionID] = map[string]string{}
		f.completed[command.ExecutionID] = map[string]bool{}
	}
	if existing, ok := f.events[command.ExecutionID][command.ClientEventID]; ok {
		if f.eventHash[command.ExecutionID][command.ClientEventID] != command.RequestHash {
			return AppendEventResult{}, ErrIdempotencyConflict
		}
		return AppendEventResult{Event: existing, Execution: f.executions[command.ExecutionID], Replayed: true}, nil
	}
	execution, ok := f.executions[command.ExecutionID]
	if !ok {
		return AppendEventResult{}, repository.ErrNotFound
	}
	if command.ExpectedVersion != execution.Version {
		return AppendEventResult{}, ErrVersionConflict
	}
	if command.Type == domain.EventStepCompleted && command.StepID != nil {
		f.completed[command.ExecutionID][*command.StepID] = true
	} else if command.Type == domain.EventStepReopened && command.StepID != nil {
		f.completed[command.ExecutionID][*command.StepID] = false
	}
	allCompleted := len(execution.Steps) > 0
	for _, s := range execution.Steps {
		if !f.completed[command.ExecutionID][s.ID] {
			allCompleted = false
			break
		}
	}
	next, err := domain.ValidateTransition(execution.State, command.Type, allCompleted)
	if err != nil {
		return AppendEventResult{}, err
	}
	execution.State = next
	execution.Version++
	if execution.State == domain.ExecutionActive && execution.StartedAt == nil {
		now := time.Now()
		execution.StartedAt = &now
	}
	execution.UpdatedAt = time.Now()
	event := domain.ExecutionEvent{
		ID:            uuid.NewString(),
		ExecutionID:   command.ExecutionID,
		ClientEventID: command.ClientEventID,
		Type:          command.Type,
		StepID:        command.StepID,
		OccurredAt:    command.OccurredAt,
		CreatedAt:     time.Now(),
	}
	f.executions[command.ExecutionID] = execution
	f.events[command.ExecutionID][command.ClientEventID] = event
	f.eventHash[command.ExecutionID][command.ClientEventID] = command.RequestHash
	return AppendEventResult{Event: event, Execution: execution, Replayed: false}, nil
}
