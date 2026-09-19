package planning

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
)

// ---- fixtures ----

type fakeDecisionStore struct {
	items     []domain.VariantDecisionItem
	byVariant map[string]domain.PlanVariantDecision
	upserts   []domain.UpsertVariantDecisionCommand
	deletes   []string
	limit     int
}

func (f *fakeDecisionStore) ListRecentDecisions(_ context.Context, _ string, limit int) ([]domain.VariantDecisionItem, error) {
	f.limit = limit
	return f.items, nil
}

func (f *fakeDecisionStore) UpsertVariantDecision(_ context.Context, _ string, command domain.UpsertVariantDecisionCommand) (domain.PlanVariantDecision, error) {
	f.upserts = append(f.upserts, command)
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	return domain.PlanVariantDecision{
		PlanVariantID: command.PlanVariantID,
		PlanSetID:     "10000000-0000-0000-0000-000000000001",
		Decision:      command.Decision,
		CreatedAt:     now,
		UpdatedAt:     now,
	}, nil
}

func (f *fakeDecisionStore) DeleteVariantDecision(_ context.Context, _ string, planVariantID string) error {
	f.deletes = append(f.deletes, planVariantID)
	return nil
}

func (f *fakeDecisionStore) ListDecisionsByVariantIDs(_ context.Context, _ string, _ []string) (map[string]domain.PlanVariantDecision, error) {
	return f.byVariant, nil
}

func decisionTestService(store *fakeStore, decisions *fakeDecisionStore) *Service {
	return NewService(Dependencies{
		Reports:   fakeReports{report: validReport("20000000-0000-0000-0000-000000000001")},
		Store:     store,
		Decisions: decisions,
	})
}

// ---- 输入校验 ----

func TestPutVariantDecisionRejectsBadValue(t *testing.T) {
	svc := decisionTestService(&fakeStore{}, &fakeDecisionStore{})
	for _, bad := range []string{"", "LIKE", "love", "dislike"} {
		if _, err := svc.PutVariantDecision(context.Background(), "00000000-0000-0000-0000-000000000001", "80000000-0000-0000-0000-000000000001", bad); !equalDecisionErr(err) {
			t.Fatalf("decision %q: err = %v, want ErrInvalidDecision", bad, err)
		}
	}
}

func TestPutVariantDecisionRejectsMalformedVariantID(t *testing.T) {
	svc := decisionTestService(&fakeStore{}, &fakeDecisionStore{})
	if _, err := svc.PutVariantDecision(context.Background(), "00000000-0000-0000-0000-000000000001", "not-a-uuid", "like"); !equalDecisionErr(err) {
		t.Fatalf("err = %v, want ErrInvalidDecision", err)
	}
}

func TestPutVariantDecisionDelegatesToStore(t *testing.T) {
	store := &fakeDecisionStore{}
	svc := decisionTestService(&fakeStore{}, store)
	got, err := svc.PutVariantDecision(context.Background(), "00000000-0000-0000-0000-000000000001", "80000000-0000-0000-0000-000000000001", "like")
	if err != nil {
		t.Fatal(err)
	}
	if len(store.upserts) != 1 || store.upserts[0].Decision != domain.DecisionLike {
		t.Fatalf("upserts = %#v", store.upserts)
	}
	if got.PlanVariantID != "80000000-0000-0000-0000-000000000001" || got.Decision != domain.DecisionLike {
		t.Fatalf("result = %#v", got)
	}
}

func TestPutVariantDecisionWithoutStoreUnavailable(t *testing.T) {
	// 未装配决策存储:写入必须显式失败(500 通道),绝不能静默吞掉用户的态度。
	svc := NewService(Dependencies{Reports: fakeReports{report: validReport("20000000-0000-0000-0000-000000000001")}, Store: &fakeStore{}})
	if _, err := svc.PutVariantDecision(context.Background(), "00000000-0000-0000-0000-000000000001", "80000000-0000-0000-0000-000000000001", "like"); err == nil || strings.Contains(err.Error(), "invalid") {
		t.Fatalf("err = %v, want unavailable error", err)
	}
}

func TestDeleteVariantDecisionValidatesAndDelegates(t *testing.T) {
	store := &fakeDecisionStore{}
	svc := decisionTestService(&fakeStore{}, store)
	if err := svc.DeleteVariantDecision(context.Background(), "00000000-0000-0000-0000-000000000001", "oops"); !equalDecisionErr(err) {
		t.Fatalf("err = %v, want ErrInvalidDecision", err)
	}
	if err := svc.DeleteVariantDecision(context.Background(), "00000000-0000-0000-0000-000000000001", "80000000-0000-0000-0000-000000000001"); err != nil {
		t.Fatal(err)
	}
	if len(store.deletes) != 1 {
		t.Fatalf("deletes = %#v", store.deletes)
	}
}

func equalDecisionErr(err error) bool {
	return err != nil && err.Error() == ErrInvalidDecision.Error()
}

// ---- 读投影合并 ----

func TestGetPlanSetMergesDecisionViews(t *testing.T) {
	planSet := validPublishedPlanSet()
	variantID := "81000000-0000-0000-0000-000000000001"
	planSet.Variants = []domain.PlanVariant{{ID: variantID, Slot: 1}}
	decisions := &fakeDecisionStore{byVariant: map[string]domain.PlanVariantDecision{
		variantID: {PlanVariantID: variantID, Decision: domain.DecisionSkip},
	}}
	store := &fakeStore{planSet: planSet}
	svc := decisionTestService(store, decisions)
	got, err := svc.GetPlanSet(context.Background(), "00000000-0000-0000-0000-000000000001", planSet.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Variants[0].Decision == nil || got.Variants[0].Decision.Decision != domain.DecisionSkip {
		t.Fatalf("decision view = %#v", got.Variants[0].Decision)
	}
}

func TestGetPlanSetWithoutDecisionsKeepsVariantsUndecided(t *testing.T) {
	planSet := validPublishedPlanSet()
	planSet.Variants = []domain.PlanVariant{{ID: "81000000-0000-0000-0000-000000000001", Slot: 1}}
	svc := NewService(Dependencies{
		Reports: fakeReports{report: validReport(planSet.ReportID)},
		Store:   &fakeStore{planSet: planSet},
	})
	got, err := svc.GetPlanSet(context.Background(), "00000000-0000-0000-0000-000000000001", planSet.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Variants[0].Decision != nil {
		t.Fatalf("undecided variant must keep nil decision, got %#v", got.Variants[0].Decision)
	}
}

// ---- 指纹闭环 ----

func TestPlanningInputHashFoldsDecisions(t *testing.T) {
	base := PlanningInputHash("20000000-0000-0000-0000-000000000001", json.RawMessage(`{}`), "bh", nil, nil)
	with := PlanningInputHash("20000000-0000-0000-0000-000000000001", json.RawMessage(`{}`), "bh", nil, []domain.VariantDecisionItem{
		{VariantID: "80000000-0000-0000-0000-000000000001", Key: "sharp", Name: "干练", Decision: "skip"},
	})
	if base == "" || with == "" || base == with {
		t.Fatalf("a new decision must change the planning input hash: base=%s with=%s", base, with)
	}
}

func TestPlanningInputHashDecisionOrderIndependent(t *testing.T) {
	items := []domain.VariantDecisionItem{
		{VariantID: "80000000-0000-0000-0000-000000000001", Key: "sharp", Name: "干练", Decision: "skip"},
		{VariantID: "80000000-0000-0000-0000-000000000002", Key: "warm", Name: "暖意", Decision: "like"},
	}
	reversed := []domain.VariantDecisionItem{items[1], items[0]}
	a := PlanningInputHash("20000000-0000-0000-0000-000000000001", json.RawMessage(`{}`), "bh", nil, items)
	b := PlanningInputHash("20000000-0000-0000-0000-000000000001", json.RawMessage(`{}`), "bh", nil, reversed)
	if a != b {
		t.Fatalf("decision order must not change the hash: %s vs %s", a, b)
	}
}

func TestCreatePlanSetFoldsDecisionsIntoPlanningInputHash(t *testing.T) {
	reportID := "20000000-0000-0000-0000-000000000001"
	report := validReport(reportID)
	decisions := &fakeDecisionStore{items: []domain.VariantDecisionItem{
		{VariantID: "80000000-0000-0000-0000-000000000001", Key: "sharp", Name: "干练", Decision: "skip"},
	}}
	starter := &fakeStarter{}
	svc := NewService(Dependencies{
		Reports: fakeReports{report: report}, Operations: starter, Store: &fakeStore{}, Decisions: decisions,
		IDs: func() string { return "10000000-0000-0000-0000-000000000001" },
	})
	if _, err := svc.CreatePlanSet(context.Background(), validCreateCommand(reportID)); err != nil {
		t.Fatal(err)
	}
	if decisions.limit != planningDecisionLimit {
		t.Fatalf("decisions read limit = %d, want %d", decisions.limit, planningDecisionLimit)
	}
	payload := starter.command.Task.Payload.(GenerateTaskPayload)
	want := PlanningInputHash(report.ID, report.ProfileSnapshot, payload.BriefHash, nil, decisions.items)
	if payload.PlanningInputHash == "" || payload.PlanningInputHash != want {
		t.Fatalf("planning_input_hash = %q, want %q", payload.PlanningInputHash, want)
	}
}

// ---- worker 注入 ----

func TestExecuteEmbedsDecisionMemoryIntoProfileSnapshot(t *testing.T) {
	deps := validHandlerDependencies()
	decisionStore := &fakeDecisionStore{items: []domain.VariantDecisionItem{
		{VariantID: "80000000-0000-0000-0000-000000000001", Key: "sharp", Name: "干练细节优化", Decision: "skip"},
	}}
	deps.decisions = decisionStore
	handler := NewHandlerForTest(deps)
	if _, err := handler.Execute(context.Background(), validGenerateLease(1)); err != nil {
		t.Fatal(err)
	}
	if len(deps.generator.inputs) != 1 {
		t.Fatalf("generate calls = %d", len(deps.generator.inputs))
	}
	snapshot := string(deps.generator.inputs[0].Report.ProfileSnapshot)
	if !strings.Contains(snapshot, `"decision_memory"`) || !strings.Contains(snapshot, "干练细节优化") {
		t.Fatalf("profile snapshot missing decision_memory: %s", snapshot)
	}
}

func TestExecuteWithoutDecisionsKeepsSnapshotClean(t *testing.T) {
	deps := validHandlerDependencies()
	handler := NewHandlerForTest(deps)
	if _, err := handler.Execute(context.Background(), validGenerateLease(1)); err != nil {
		t.Fatal(err)
	}
	snapshot := string(deps.generator.inputs[0].Report.ProfileSnapshot)
	if strings.Contains(snapshot, `"decision_memory"`) {
		t.Fatalf("no decisions wired, snapshot must not carry decision_memory: %s", snapshot)
	}
}
