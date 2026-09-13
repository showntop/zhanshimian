package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
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
