package postgres

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

type renderingFixture struct {
	store     *Store
	userA     string
	userB     string
	variantID string
	specID    string
}

// newRenderingStore publishes a real plan set through Store.Prepare/Commit
// and hands back one variant plus its render spec — the minimal chain
// rendering builds upon.
func newRenderingStore(t *testing.T) *renderingFixture {
	t.Helper()
	store, users := newPlanningStore(t)
	lease := validDatabaseLease(t, store, users)
	command := validPrepareCommand(users)
	got, err := store.Prepare(context.Background(), lease, command)
	if err != nil {
		t.Fatalf("seed plan set: %v", err)
	}
	if _, err = store.CommitPrepared(context.Background(), lease, domain.TaskResult{
		Disposition: domain.TaskPublish, ResultType: "plan_set", ResultID: got.ID,
	}); err != nil {
		t.Fatalf("commit seed plan set: %v", err)
	}
	variantID := got.Variants[0].ID
	var specID string
	if err = store.pool.QueryRow(context.Background(),
		`SELECT id::text FROM render_specs WHERE user_id=$1::uuid AND plan_variant_id=$2::uuid`,
		users.A, variantID).Scan(&specID); err != nil {
		t.Fatal(err)
	}
	return &renderingFixture{store: store, userA: users.A, userB: users.B, variantID: variantID, specID: specID}
}

func renderCreateCommand(f *renderingFixture, key string) domain.RenderCreateRunCommand {
	return domain.RenderCreateRunCommand{
		UserID:               f.userA,
		PlanVariantID:        f.variantID,
		RenderSpecID:         f.specID,
		IdempotencyKey:       key,
		RoutingPolicyVersion: "render-route-v1",
		QualityPolicyVersion: "render-quality-v1",
	}
}

func TestCreateRunSameKeyReturnsSameRunAndOneTask(t *testing.T) {
	f := newRenderingStore(t)
	ctx := context.Background()
	first, err := f.store.CreateRun(ctx, renderCreateCommand(f, "same-key"))
	if err != nil || !first.Created {
		t.Fatalf("first: created=%v err=%v", first.Created, err)
	}
	second, err := f.store.CreateRun(ctx, renderCreateCommand(f, "same-key"))
	if err != nil {
		t.Fatal(err)
	}
	if second.Created {
		t.Fatal("duplicate key claimed created")
	}
	if first.Run.ID != second.Run.ID || first.Operation.ID != second.Operation.ID {
		t.Fatalf("run/operation mismatch: %s/%s vs %s/%s",
			first.Run.ID, first.Operation.ID, second.Run.ID, second.Operation.ID)
	}
	if n := f.countVariant(t, "render_runs"); n != 1 {
		t.Fatalf("render_runs = %d, want 1", n)
	}
	if n := f.countRunTasks(t); n != 1 {
		t.Fatalf("tasks = %d, want 1", n)
	}
}

// countRunTasks 统计本 variant 各 run 名下的任务数,避免 seed 数据干扰。
func (f *renderingFixture) countRunTasks(t *testing.T) int {
	t.Helper()
	var n int
	err := f.store.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM tasks t
		JOIN render_runs r ON r.user_id=t.user_id AND r.operation_id=t.operation_id
		WHERE r.user_id=$1::uuid AND r.plan_variant_id=$2::uuid`,
		f.userA, f.variantID).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// countVariant 统计本 variant 名下的行数,避免 seed 数据干扰。
func (f *renderingFixture) countVariant(t *testing.T, table string) int {
	t.Helper()
	var n int
	err := f.store.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM `+table+` WHERE user_id=$1::uuid AND plan_variant_id=$2::uuid`,
		f.userA, f.variantID).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCreateRunConcurrentSameKeyCreatesOnce(t *testing.T) {
	f := newRenderingStore(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	results := make(chan domain.RenderCreateRunResult, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := f.store.CreateRun(ctx, renderCreateCommand(f, "concurrent-key"))
			if err != nil {
				t.Errorf("concurrent create: %v", err)
				return
			}
			results <- got
		}()
	}
	wg.Wait()
	close(results)
	created := 0
	var runIDs []string
	for got := range results {
		if got.Created {
			created++
		}
		runIDs = append(runIDs, got.Run.ID)
	}
	if created != 1 {
		t.Fatalf("created = %d, want 1", created)
	}
	if runIDs[0] != runIDs[1] {
		t.Fatalf("run ids diverged: %s vs %s", runIDs[0], runIDs[1])
	}
	if n := countRows(t, f.store.pool, "render_runs"); n != 1 {
		t.Fatalf("render_runs = %d, want 1", n)
	}
}

func TestCreateRunDifferentKeyIncrementsGeneration(t *testing.T) {
	f := newRenderingStore(t)
	ctx := context.Background()
	first, err := f.store.CreateRun(ctx, renderCreateCommand(f, "key-1"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.store.CreateRun(ctx, renderCreateCommand(f, "key-2"))
	if err != nil {
		t.Fatal(err)
	}
	if first.Run.Generation != 1 || second.Run.Generation != 2 {
		t.Fatalf("generations = %d/%d, want 1/2", first.Run.Generation, second.Run.Generation)
	}
	var headGeneration int
	if err := f.store.pool.QueryRow(ctx, `
		SELECT generation FROM render_heads WHERE user_id=$1::uuid AND plan_variant_id=$2::uuid`,
		f.userA, f.variantID).Scan(&headGeneration); err != nil {
		t.Fatal(err)
	}
	if headGeneration != 2 {
		t.Fatalf("head generation = %d, want 2", headGeneration)
	}
}

func TestGetRunRequiresMatchingUserID(t *testing.T) {
	f := newRenderingStore(t)
	ctx := context.Background()
	got, err := f.store.CreateRun(ctx, renderCreateCommand(f, "tenant-key"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = f.store.GetRun(ctx, f.userB, got.Run.ID); err != repository.ErrNotFound {
		t.Fatalf("cross-tenant read = %v", err)
	}
}

func TestCreateRunRejectsSpecFromAnotherUser(t *testing.T) {
	f := newRenderingStore(t)
	command := renderCreateCommand(f, "foreign-spec")
	command.RenderSpecID = uuid.NewString() // user B 侧不存在的 spec id
	if _, err := f.store.CreateRun(context.Background(), command); err == nil {
		t.Fatal("foreign render spec was accepted")
	}
	var n int
	if err := f.store.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM operations WHERE user_id=$1::uuid AND subject_type='render_run'`,
		f.userA).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("render operations = %d, want 0", n)
	}
}

func TestCreateRunSupersedesOlderUnfinishedRun(t *testing.T) {
	f := newRenderingStore(t)
	ctx := context.Background()
	first, err := f.store.CreateRun(ctx, renderCreateCommand(f, "gen-1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.CreateRun(ctx, renderCreateCommand(f, "gen-2")); err != nil {
		t.Fatal(err)
	}
	var outcome string
	var opStatus string
	if err = f.store.pool.QueryRow(ctx,
		`SELECT outcome FROM render_runs WHERE id=$1::uuid`, first.Run.ID).Scan(&outcome); err != nil {
		t.Fatal(err)
	}
	if outcome != domain.RenderOutcomeSuperseded {
		t.Fatalf("old run outcome = %q, want superseded", outcome)
	}
	if err = f.store.pool.QueryRow(ctx,
		`SELECT status FROM operations WHERE id=$1::uuid`, first.Operation.ID).Scan(&opStatus); err != nil {
		t.Fatal(err)
	}
	if opStatus != string(domain.OperationSuperseded) {
		t.Fatalf("old operation status = %q, want superseded", opStatus)
	}
}
