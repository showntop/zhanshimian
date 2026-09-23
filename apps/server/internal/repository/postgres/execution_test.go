package postgres

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/execution"
)

const (
	hashA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hashB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

type executionFixture struct {
	store            *Store
	UserA            string
	UserB            string
	PlanSetA         string
	VariantA         string
	UserBPublication string

	SelectionA    string
	HairStep      string
	HairStepTitle string
	Pool          *pgxpool.Pool

	ExecutionA          string
	HairExecutionStep   string
	MakeupExecutionStep string
}

// newExecutionFixture 复用 planning 夹具提交一套已发布的 PlanSet,取其第一个
// variant。UserBPublication 是一个不指向 UserA/VariantA 当前发布的 publication,
// 与真正的跨用户发布走同一条归属校验路径。
func newExecutionFixture(t *testing.T) (*Store, *executionFixture) {
	t.Helper()
	store, users := newPlanningStore(t)
	lease := validDatabaseLease(t, store, users)
	got, err := store.Prepare(context.Background(), lease, validPrepareCommand(users))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CommitPrepared(context.Background(), lease, domain.TaskResult{
		Disposition: domain.TaskPublish, ResultType: "plan_set", ResultID: got.ID,
	}); err != nil {
		t.Fatal(err)
	}
	return store, &executionFixture{
		store:            store,
		UserA:            users.A,
		UserB:            users.B,
		PlanSetA:         got.ID,
		VariantA:         got.Variants[0].ID,
		UserBPublication: uuid.NewString(),
	}
}

func TestCreateSelectionEnforcesVariantAndPublicationOwnership(t *testing.T) {
	store, fx := newExecutionFixture(t)
	_, _, err := store.CreateSelection(ctx, domain.CreateSelectionCommand{
		UserID:              fx.UserA,
		PlanSetID:           fx.PlanSetA,
		PlanVariantID:       fx.VariantA,
		RenderPublicationID: &fx.UserBPublication,
		IdempotencyKey:      "selection-cross-user",
		RequestHash:         hashA,
	})
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("cross-user publication must look absent: %v", err)
	}
}

func TestCreateSelectionRejectsVariantNotInPlanSet(t *testing.T) {
	store, fx := newExecutionFixture(t)
	_, _, err := store.CreateSelection(ctx, domain.CreateSelectionCommand{
		UserID: fx.UserA, PlanSetID: fx.PlanSetA,
		PlanVariantID:  uuid.NewString(),
		IdempotencyKey: "selection-foreign-variant", RequestHash: hashA,
	})
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("foreign variant must look absent: %v", err)
	}
}

func TestCreateSelectionReplaysSameKeyAndHash(t *testing.T) {
	store, fx := newExecutionFixture(t)
	command := domain.CreateSelectionCommand{
		UserID: fx.UserA, PlanSetID: fx.PlanSetA, PlanVariantID: fx.VariantA,
		IdempotencyKey: "selection-replay", RequestHash: hashA,
	}
	first, created, err := store.CreateSelection(ctx, command)
	if err != nil || !created {
		t.Fatalf("first: created=%v err=%v", created, err)
	}
	second, created, err := store.CreateSelection(ctx, command)
	if err != nil || created || second.ID != first.ID {
		t.Fatalf("replay: %#v created=%v err=%v", second, created, err)
	}
}

func TestCreateSelectionConflictsSameKeyDifferentHash(t *testing.T) {
	store, fx := newExecutionFixture(t)
	command := domain.CreateSelectionCommand{
		UserID: fx.UserA, PlanSetID: fx.PlanSetA, PlanVariantID: fx.VariantA,
		IdempotencyKey: "selection-conflict", RequestHash: hashA,
	}
	if _, _, err := store.CreateSelection(ctx, command); err != nil {
		t.Fatal(err)
	}
	command.RequestHash = hashB
	if _, _, err := store.CreateSelection(ctx, command); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("same key different hash err=%v", err)
	}
}

func TestCreateSelectionDedupesNaturalWithNewKey(t *testing.T) {
	store, fx := newExecutionFixture(t)
	first, created, err := store.CreateSelection(ctx, domain.CreateSelectionCommand{
		UserID: fx.UserA, PlanSetID: fx.PlanSetA, PlanVariantID: fx.VariantA,
		IdempotencyKey: "selection-a", RequestHash: hashA,
	})
	if err != nil || !created {
		t.Fatalf("first: created=%v err=%v", created, err)
	}
	second, created, err := store.CreateSelection(ctx, domain.CreateSelectionCommand{
		UserID: fx.UserA, PlanSetID: fx.PlanSetA, PlanVariantID: fx.VariantA,
		IdempotencyKey: "selection-b", RequestHash: hashA,
	})
	if err != nil || created || second.ID != first.ID {
		t.Fatalf("natural dedupe: %#v created=%v err=%v", second, created, err)
	}
	got, err := store.GetSelection(ctx, fx.UserA, first.ID)
	if err != nil || got.ID != first.ID {
		t.Fatalf("get selection: %#v err=%v", got, err)
	}
}

func TestCreateSelectionConcurrentReplayProducesOne(t *testing.T) {
	store, fx := newExecutionFixture(t)
	command := domain.CreateSelectionCommand{
		UserID: fx.UserA, PlanSetID: fx.PlanSetA, PlanVariantID: fx.VariantA,
		IdempotencyKey: "selection-concurrent", RequestHash: hashA,
	}
	type result struct {
		selection domain.PlanSelection
		created   bool
		err       error
	}
	const n = 8
	results := make(chan result, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			selection, created, err := store.CreateSelection(context.Background(), command)
			results <- result{selection, created, err}
		}()
	}
	wg.Wait()
	close(results)

	createdCount := 0
	selectionID := ""
	for r := range results {
		if r.err != nil {
			t.Fatalf("unexpected error under concurrency: %v", r.err)
		}
		if r.created {
			createdCount++
		}
		if selectionID == "" {
			selectionID = r.selection.ID
		} else if r.selection.ID != selectionID {
			t.Fatalf("replay diverged: %s vs %s", r.selection.ID, selectionID)
		}
	}
	if createdCount != 1 {
		t.Fatalf("created count = %d, want 1", createdCount)
	}
}

// newExecutionSnapshotFixture 在已发布 PlanSet 上额外创建一次 Selection,并捕获
// VariantA 的 hair 源步骤 id 与标题,用于执行快照测试。
func newExecutionSnapshotFixture(t *testing.T) (*Store, *executionFixture) {
	t.Helper()
	store, fx := newExecutionFixture(t)
	selection, _, err := store.CreateSelection(ctx, domain.CreateSelectionCommand{
		UserID: fx.UserA, PlanSetID: fx.PlanSetA, PlanVariantID: fx.VariantA,
		IdempotencyKey: "execution-selection", RequestHash: hashA,
	})
	if err != nil {
		t.Fatal(err)
	}
	fx.SelectionA = selection.ID

	if err := store.pool.QueryRow(ctx, `
		SELECT id::text, title FROM plan_steps
		WHERE user_id=$1::uuid AND plan_variant_id=$2::uuid AND category='hair'`,
		fx.UserA, fx.VariantA).Scan(&fx.HairStep, &fx.HairStepTitle); err != nil {
		t.Fatal(err)
	}
	fx.Pool = store.pool
	return store, fx
}

func TestExecutionSnapshotSurvivesSourceDeletionAttempt(t *testing.T) {
	store, fx := newExecutionSnapshotFixture(t)
	exec, _, err := store.CreateExecutionFromSelection(ctx, domain.CreateExecutionCommand{
		UserID: fx.UserA, SelectionID: fx.SelectionA,
		IdempotencyKey: "execution-snapshot", RequestHash: hashA,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fx.Pool.Exec(ctx, `UPDATE plan_steps SET title='mutated' WHERE id=$1::uuid`, fx.HairStep); err == nil {
		t.Fatal("plan_steps are immutable; update must be denied by database trigger")
	}
	got, err := store.GetExecution(ctx, fx.UserA, exec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Steps) != 3 {
		t.Fatalf("snapshot steps = %d, want 3", len(got.Steps))
	}
	if got.Steps[0].Title != fx.HairStepTitle {
		t.Fatalf("snapshot changed with source: got %q want %q", got.Steps[0].Title, fx.HairStepTitle)
	}
}

func TestCreateExecutionFromSelectionDedupesBySelection(t *testing.T) {
	store, fx := newExecutionSnapshotFixture(t)
	first, created, err := store.CreateExecutionFromSelection(ctx, domain.CreateExecutionCommand{
		UserID: fx.UserA, SelectionID: fx.SelectionA,
		IdempotencyKey: "execution-a", RequestHash: hashA,
	})
	if err != nil || !created {
		t.Fatalf("first: created=%v err=%v", created, err)
	}
	second, created, err := store.CreateExecutionFromSelection(ctx, domain.CreateExecutionCommand{
		UserID: fx.UserA, SelectionID: fx.SelectionA,
		IdempotencyKey: "execution-b", RequestHash: hashA,
	})
	if err != nil || created || second.ID != first.ID {
		t.Fatalf("selection dedupe: %#v created=%v err=%v", second, created, err)
	}
}

// newExecutionEventsFixture 在已创建 Selection 与 Execution 的基础上,捕获 hair 与
// makeup 两个 execution_step id,用于事件 CAS 测试。
func newExecutionEventsFixture(t *testing.T) (*Store, *executionFixture) {
	t.Helper()
	store, fx := newExecutionSnapshotFixture(t)
	exec, _, err := store.CreateExecutionFromSelection(ctx, domain.CreateExecutionCommand{
		UserID: fx.UserA, SelectionID: fx.SelectionA,
		IdempotencyKey: "execution-events", RequestHash: hashA,
	})
	if err != nil {
		t.Fatal(err)
	}
	fx.ExecutionA = exec.ID

	if err := store.pool.QueryRow(ctx, `
		SELECT id::text FROM execution_steps
		WHERE user_id=$1::uuid AND execution_id=$2::uuid AND category='hair'`,
		fx.UserA, fx.ExecutionA).Scan(&fx.HairExecutionStep); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `
		SELECT id::text FROM execution_steps
		WHERE user_id=$1::uuid AND execution_id=$2::uuid AND category='makeup'`,
		fx.UserA, fx.ExecutionA).Scan(&fx.MakeupExecutionStep); err != nil {
		t.Fatal(err)
	}
	return store, fx
}

func TestAppendExecutionEventConcurrentCASAllowsOneWriter(t *testing.T) {
	store, fx := newExecutionEventsFixture(t)
	var ok, conflict atomic.Int32
	var wg sync.WaitGroup
	for _, stepID := range []string{fx.HairExecutionStep, fx.MakeupExecutionStep} {
		wg.Add(1)
		go func(stepID string) {
			defer wg.Done()
			_, err := store.AppendExecutionEvent(ctx, domain.AppendEventCommand{
				UserID: fx.UserA, ExecutionID: fx.ExecutionA,
				ClientEventID: uuid.NewString(), Type: domain.EventStepCompleted,
				StepID: &stepID, OccurredAt: time.Now(), ExpectedVersion: 1,
			})
			if err == nil {
				ok.Add(1)
			} else if errors.Is(err, execution.ErrVersionConflict) {
				conflict.Add(1)
			}
		}(stepID)
	}
	wg.Wait()
	if ok.Load() != 1 || conflict.Load() != 1 {
		t.Fatalf("ok=%d conflict=%d", ok.Load(), conflict.Load())
	}
}
