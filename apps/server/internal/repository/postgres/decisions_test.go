package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

// 卡堆决策的完整生命周期:首次写入 → 改主意(同行覆盖) → 批量读 → 最近读
// (JOIN 带 key/name) → 撤销(幂等)。归属全部依赖复合外键:他人 variant
// 读作 not-found,而不是 500。
func TestUpsertVariantDecisionLifecycle(t *testing.T) {
	store, users := newPlanningStore(t)
	lease := validDatabaseLease(t, store, users)
	command := validPrepareCommand(users)
	got, err := store.Prepare(context.Background(), lease, command)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.CommitPrepared(context.Background(), lease, domain.TaskResult{
		Disposition: domain.TaskPublish, ResultType: "plan_set", ResultID: got.ID,
	}); err != nil {
		t.Fatal(err)
	}
	variantID := got.Variants[0].ID
	ctx := context.Background()

	// 首次写入:plan_set_id 由 variant 行解析,客户端不传。
	first, err := store.UpsertVariantDecision(ctx, users.A, domain.UpsertVariantDecisionCommand{
		PlanVariantID: variantID, Decision: domain.DecisionLike,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.PlanVariantID != variantID || first.PlanSetID != got.ID || first.Decision != domain.DecisionLike {
		t.Fatalf("first decision = %#v", first)
	}

	// 改主意:同一行覆盖——created_at 保留,updated_at 前移。
	second, err := store.UpsertVariantDecision(ctx, users.A, domain.UpsertVariantDecisionCommand{
		PlanVariantID: variantID, Decision: domain.DecisionSkip,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Decision != domain.DecisionSkip {
		t.Fatalf("upsert did not overwrite: %#v", second)
	}
	if !second.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("upsert must keep created_at: first=%v second=%v", first.CreatedAt, second.CreatedAt)
	}
	if second.UpdatedAt.Before(first.UpdatedAt) {
		t.Fatalf("upsert must advance updated_at: first=%v second=%v", first.UpdatedAt, second.UpdatedAt)
	}

	// 他人 variant:复合外键(CTE 落空)读作 not-found。
	if _, err = store.UpsertVariantDecision(ctx, users.B, domain.UpsertVariantDecisionCommand{
		PlanVariantID: variantID, Decision: domain.DecisionLike,
	}); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("foreign variant upsert = %v, want ErrNotFound", err)
	}

	// 批量读:命中 variant 有决策,未决 variant 缺席。
	decisions, err := store.ListDecisionsByVariantIDs(ctx, users.A, []string{variantID, got.Variants[1].ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 || decisions[variantID].Decision != domain.DecisionSkip {
		t.Fatalf("batch decisions = %#v", decisions)
	}

	// 最近读:JOIN plan_variants 带出 key/name,规划快照靠它理解跳过的方向。
	items, err := store.ListRecentDecisions(ctx, users.A, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].VariantID != variantID || items[0].Name != got.Variants[0].Name || items[0].Key != string(got.Variants[0].Key) {
		t.Fatalf("recent decisions = %#v", items)
	}

	// 撤销幂等:行不存在同样是成功,且不影响其他用户的数据。
	if err = store.DeleteVariantDecision(ctx, users.A, variantID); err != nil {
		t.Fatal(err)
	}
	if err = store.DeleteVariantDecision(ctx, users.A, variantID); err != nil {
		t.Fatalf("delete must be idempotent: %v", err)
	}
	if items, err = store.ListRecentDecisions(ctx, users.A, 10); err != nil || len(items) != 0 {
		t.Fatalf("after delete recent = %#v err=%v", items, err)
	}
}

// limit 生效:只取最近 N 条,不因历史决策膨胀规划快照。
func TestListRecentDecisionsRespectsLimit(t *testing.T) {
	store, users := newPlanningStore(t)
	lease := validDatabaseLease(t, store, users)
	command := validPrepareCommand(users)
	got, err := store.Prepare(context.Background(), lease, command)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.CommitPrepared(context.Background(), lease, domain.TaskResult{
		Disposition: domain.TaskPublish, ResultType: "plan_set", ResultID: got.ID,
	}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, variant := range got.Variants {
		if _, err = store.UpsertVariantDecision(ctx, users.A, domain.UpsertVariantDecisionCommand{
			PlanVariantID: variant.ID, Decision: domain.DecisionSkip,
		}); err != nil {
			t.Fatal(err)
		}
	}
	items, err := store.ListRecentDecisions(ctx, users.A, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("limit 2 got %d items", len(items))
	}
}
