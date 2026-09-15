package planning

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/zhanshimian/server/internal/domain"
)

func TestCreatePlanSetStartsOperationAndTaskOnce(t *testing.T) {
	reportID := "20000000-0000-0000-0000-000000000001"
	starter := &fakeStarter{}
	svc := NewService(Dependencies{
		Reports:    fakeReports{report: validReport(reportID)},
		Operations: starter,
		Store:      &fakeStore{},
		IDs:        func() string { return "10000000-0000-0000-0000-000000000001" },
	})
	got, err := svc.CreatePlanSet(context.Background(), CreateCommand{
		UserID: "00000000-0000-0000-0000-000000000001", ReportID: reportID, Scene: domain.SceneDaily,
		Answers:        map[string]string{"activity": "office", "weather": "air_conditioned", "preparation": "closet", "impression": "natural"},
		IdempotencyKey: "plan-daily-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Operation.ID == "" || got.PlanSetID == "" || !got.Accepted {
		t.Fatalf("unexpected result: %#v", got)
	}
	if starter.calls != 1 || starter.command.Task.Type != PlanSetGenerationTaskType {
		t.Fatalf("unexpected start command: %#v", starter.command)
	}
	payload := starter.command.Task.Payload.(GenerateTaskPayload)
	if payload.ContentAttempt != 1 || payload.PriorReasonCodes != nil || starter.command.Task.PayloadVersion != 1 {
		t.Fatalf("unexpected initial payload: %#v", payload)
	}
	if payload.BriefHash == "" || payload.BriefHash != BriefHash(payload.Brief) {
		t.Fatalf("payload brief hash mismatch: %#v", payload)
	}
}

func TestCreatePlanSetReturnsPublishedResultWithoutStartingAI(t *testing.T) {
	store := &fakeStore{found: true, planSet: validPublishedPlanSet()}
	starter := &fakeStarter{}
	svc := NewService(Dependencies{Reports: fakeReports{report: validReport(store.planSet.ReportID)}, Operations: starter, Store: store})
	got, err := svc.CreatePlanSet(context.Background(), validCreateCommand(store.planSet.ReportID))
	if err != nil {
		t.Fatal(err)
	}
	if got.Accepted || got.PlanSetID != store.planSet.ID || starter.calls != 0 {
		t.Fatalf("published result was not reused: %#v", got)
	}
}

func TestCreatePlanSetFoldsPreferenceMemoriesIntoPlanningInputHash(t *testing.T) {
	reportID := "20000000-0000-0000-0000-000000000001"
	report := validReport(reportID)
	memories := &fakeMemories{memories: []domain.PreferenceMemory{
		{ID: "90000000-0000-0000-0000-000000000001", Key: "outfit.palette", Category: domain.CategoryOutfit, Value: "偏爱低饱和"},
	}}
	starter := &fakeStarter{}
	svc := NewService(Dependencies{
		Reports: fakeReports{report: report}, Operations: starter, Store: &fakeStore{}, Memories: memories,
		IDs: func() string { return "10000000-0000-0000-0000-000000000001" },
	})
	if _, err := svc.CreatePlanSet(context.Background(), validCreateCommand(reportID)); err != nil {
		t.Fatal(err)
	}
	if memories.calls != 1 || memories.limit != planningMemoryLimit {
		t.Fatalf("memories read = %d calls limit=%d, want 1/%d", memories.calls, memories.limit, planningMemoryLimit)
	}
	payload := starter.command.Task.Payload.(GenerateTaskPayload)
	want := PlanningInputHash(report.ID, report.ProfileSnapshot, payload.BriefHash, memories.memories)
	if payload.PlanningInputHash == "" || payload.PlanningInputHash != want {
		t.Fatalf("planning_input_hash = %q, want %q", payload.PlanningInputHash, want)
	}
}

// 新增一条明确偏好后,服务必须用新的 planning_input_hash 去查已发布结果,并派生出
// 新的方案身份——否则用户会拿回写入偏好之前的旧方案。
func TestCreatePlanSetYieldsNewIdentityAfterNewMemory(t *testing.T) {
	reportID := "20000000-0000-0000-0000-000000000001"
	report := validReport(reportID)
	store := &fakeStore{}
	starter := &fakeStarter{}
	memories := &fakeMemories{}
	svc := NewService(Dependencies{
		Reports: fakeReports{report: report}, Operations: starter, Store: store, Memories: memories,
		IDs: func() string { return "10000000-0000-0000-0000-000000000001" },
	})

	before, err := svc.CreatePlanSet(context.Background(), validCreateCommand(reportID))
	if err != nil {
		t.Fatal(err)
	}
	memories.memories = []domain.PreferenceMemory{{
		ID: "90000000-0000-0000-0000-000000000001", Key: "formality",
		Category: domain.CategoryOverall, Value: "less", SourceTag: domain.ExecutionTooFormal,
	}}
	after, err := svc.CreatePlanSet(context.Background(), validCreateCommand(reportID))
	if err != nil {
		t.Fatal(err)
	}

	if len(store.findKeys) != 2 {
		t.Fatalf("FindPublished calls = %d, want 2", len(store.findKeys))
	}
	if store.findKeys[0].PlanningInputHash == store.findKeys[1].PlanningInputHash {
		t.Fatal("planning input hash did not change after a new preference memory")
	}
	if before.PlanSetID == after.PlanSetID {
		t.Fatalf("plan set identity was reused after new memory: %s", before.PlanSetID)
	}
	if starter.command.Task.Payload.(GenerateTaskPayload).PlanningInputHash != store.findKeys[1].PlanningInputHash {
		t.Fatal("task payload hash diverged from the lookup key")
	}
}

func TestCreatePlanSetConvergesOnSameSemanticKey(t *testing.T) {
	reportID := "20000000-0000-0000-0000-000000000001"
	starter := &fakeStarter{existing: true}
	svc := NewService(Dependencies{
		Reports:    fakeReports{report: validReport(reportID)},
		Operations: starter,
		Store:      &fakeStore{},
	})
	cmd := validCreateCommand(reportID)
	first, err := svc.CreatePlanSet(context.Background(), cmd)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreatePlanSet(context.Background(), cmd)
	if err != nil {
		t.Fatal(err)
	}
	if first.PlanSetID != second.PlanSetID {
		t.Fatalf("semantic key must converge: %s vs %s", first.PlanSetID, second.PlanSetID)
	}
	if second.Accepted {
		t.Fatal("duplicate semantic start must not claim created")
	}
}

func TestCreatePlanSetReservesWhenCreated(t *testing.T) {
	reportID := "20000000-0000-0000-0000-000000000001"
	starter := &fakeStarter{}
	billing := &billingFake{}
	svc := NewService(Dependencies{
		Reports:    fakeReports{report: validReport(reportID)},
		Operations: starter,
		Store:      &fakeStore{},
		Billing:    billing,
	})
	if _, err := svc.CreatePlanSet(context.Background(), validCreateCommand(reportID)); err != nil {
		t.Fatal(err)
	}
	if len(billing.calls) != 1 {
		t.Fatalf("reserve calls = %d, want 1", len(billing.calls))
	}
	got := billing.calls[0]
	if got.operationID != "30000000-0000-0000-0000-000000000001" ||
		got.product != domain.ProductPlanSet || got.units != 1 {
		t.Fatalf("unexpected reserve call: %#v", got)
	}
}

func TestCreatePlanSetRejectsInvalidBriefAndForeignReport(t *testing.T) {
	reportID := "20000000-0000-0000-0000-000000000001"
	svc := NewService(Dependencies{
		Reports:    fakeReports{report: validReport(reportID)},
		Operations: &fakeStarter{},
		Store:      &fakeStore{},
	})
	bad := validCreateCommand(reportID)
	bad.Answers["extra"] = "nope"
	if _, err := svc.CreatePlanSet(context.Background(), bad); err == nil {
		t.Fatal("invalid brief passed")
	}
	foreign := NewService(Dependencies{
		Reports:    fakeReports{err: errNotFound},
		Operations: &fakeStarter{},
		Store:      &fakeStore{},
	})
	if _, err := foreign.CreatePlanSet(context.Background(), validCreateCommand(reportID)); !isNotFound(err) {
		t.Fatalf("foreign report: got %v", err)
	}
}

func TestGetPlanSetAndListDelegateToStore(t *testing.T) {
	store := &fakeStore{planSet: validPublishedPlanSet()}
	svc := NewService(Dependencies{Reports: fakeReports{}, Operations: &fakeStarter{}, Store: store})
	scene := domain.SceneDaily
	if _, err := svc.GetPlanSet(context.Background(), "user-1", store.planSet.ID); err != nil || store.getCalls != 1 {
		t.Fatalf("get: %v calls=%d", err, store.getCalls)
	}
	if _, err := svc.ListPlanSets(context.Background(), "user-1", store.planSet.ReportID, &scene); err != nil || store.listCalls != 1 || store.listScene == nil || *store.listScene != domain.SceneDaily {
		t.Fatalf("list: %v calls=%d scene=%v", err, store.listCalls, store.listScene)
	}
}

// ---- helpers shared by the planning tests ----

var errNotFound = errors.New("not found")

func isNotFound(err error) bool { return errors.Is(err, errNotFound) }

type fakeReports struct {
	report ReportSnapshot
	err    error
}

func (f fakeReports) GetPlanningReport(context.Context, string, string) (ReportSnapshot, error) {
	return f.report, f.err
}

type fakeMemories struct {
	memories []domain.PreferenceMemory
	calls    int
	limit    int
}

func (f *fakeMemories) ListPreferenceMemories(_ context.Context, _ string, limit int) ([]domain.PreferenceMemory, error) {
	f.calls++
	f.limit = limit
	return f.memories, nil
}

type fakeStarter struct {
	calls    int
	command  StartOperationCommand
	existing bool
}

func (f *fakeStarter) StartWithTask(_ context.Context, command StartOperationCommand) (domain.OperationRef, bool, error) {
	f.calls++
	f.command = command
	return domain.OperationRef{
		ID:     "30000000-0000-0000-0000-000000000001",
		Kind:   domain.OperationPlanSet,
		Status: domain.OperationAccepted,
	}, !f.existing, nil
}

type billingFake struct {
	calls []reserveCall
}

type reserveCall struct {
	userID, operationID string
	product             domain.Product
	units               int
}

func (b *billingFake) Reserve(_ context.Context, userID, operationID string, product domain.Product, units int) (domain.Reservation, error) {
	b.calls = append(b.calls, reserveCall{userID: userID, operationID: operationID, product: product, units: units})
	return domain.Reservation{ID: "reservation-1"}, nil
}

type fakeStore struct {
	found        bool
	planSet      domain.PlanSet
	list         []domain.PlanSet
	getCalls     int
	listCalls    int
	listScene    *domain.Scene
	prepareCalls int
	commitCalls  int
	command      PrepareCommand
	findKeys     []PlanSetKey
}

// FindPublished 记录每次查询用的语义键。查找分支本身由 postgres
// 的 TestFindPublishedKeysOnPlanningInputHash 覆盖,这里只断言服务把折入了
// 偏好记忆的 hash 交给了它。
func (f *fakeStore) FindPublished(_ context.Context, key PlanSetKey) (domain.PlanSet, bool, error) {
	f.findKeys = append(f.findKeys, key)
	return f.planSet, f.found, nil
}

func (f *fakeStore) Get(_ context.Context, userID, planSetID string) (domain.PlanSet, error) {
	f.getCalls++
	if !f.found && planSetID != f.planSet.ID || userID == "" {
		return domain.PlanSet{}, errNotFound
	}
	f.planSet.ID = planSetID
	return f.planSet, nil
}

func (f *fakeStore) List(_ context.Context, _ string, _ string, scene *domain.Scene) ([]domain.PlanSet, error) {
	f.listCalls++
	f.listScene = scene
	if f.list != nil {
		return f.list, nil
	}
	return []domain.PlanSet{f.planSet}, nil
}

func (f *fakeStore) Prepare(_ context.Context, _ domain.TaskLease, command PrepareCommand) (domain.PlanSet, error) {
	f.prepareCalls++
	f.command = command
	f.planSet = command.PlanSet
	f.found = true
	return command.PlanSet, nil
}

func (f *fakeStore) CommitPrepared(_ context.Context, _ domain.TaskLease, _ domain.TaskResult) (domain.CommitOutcome, error) {
	f.commitCalls++
	return domain.CommitApplied, nil
}

func validReport(reportID string) ReportSnapshot {
	return ReportSnapshot{
		ID:                reportID,
		UserID:            "00000000-0000-0000-0000-000000000001",
		PhotoSetID:        "50000000-0000-0000-0000-000000000001",
		FaceAssetID:       "40000000-0000-0000-0000-000000000002",
		BodyAssetID:       "40000000-0000-0000-0000-000000000001",
		ProfileSnapshot:   json.RawMessage(`{"role":"designer"}`),
		ImpressionTags:    []string{"利落"},
		PriorityTitle:     "先整理额前碎发",
		PriorityCopy:      "额前碎发落到眉毛上方，先固定发根。",
		PriorityFindingID: "21000000-0000-0000-0000-000000000001",
		Findings: []FindingSnapshot{
			{ID: "21000000-0000-0000-0000-000000000001", Category: "hair", Priority: 1, Label: "额前碎发", VisibleObservation: "额前碎发落到眉毛上方", Recommendation: "向后梳理并固定"},
			{ID: "21000000-0000-0000-0000-000000000002", Category: "outfit", Priority: 2, Label: "肩线偏塌", VisibleObservation: "上衣肩线低于自然肩点", Recommendation: "换成合肩线的上装"},
			{ID: "21000000-0000-0000-0000-000000000003", Category: "color", Priority: 3, Label: "整体色偏灰", VisibleObservation: "上装与背景同为灰色系", Recommendation: "用亮色内搭拉开层次"},
		},
	}
}

func validCreateCommand(reportID string) CreateCommand {
	return CreateCommand{
		UserID:   "00000000-0000-0000-0000-000000000001",
		ReportID: reportID,
		Scene:    domain.SceneDaily,
		Answers:  map[string]string{"activity": "office", "weather": "air_conditioned", "preparation": "closet", "impression": "natural"},
	}
}

func validPublishedPlanSet() domain.PlanSet {
	brief, _ := NormalizeBrief(domain.SceneDaily, map[string]string{
		"activity": "office", "weather": "air_conditioned", "preparation": "closet", "impression": "natural",
	})
	return domain.PlanSet{
		ID:                   "10000000-0000-0000-0000-000000000001",
		UserID:               "00000000-0000-0000-0000-000000000001",
		ReportID:             "20000000-0000-0000-0000-000000000001",
		ProfileSnapshot:      json.RawMessage(`{"role":"designer"}`),
		Scene:                domain.SceneDaily,
		SceneBrief:           brief,
		BriefHash:            BriefHash(brief),
		PlannerSchemaVersion: PlannerSchemaVersion,
		StyleRuleVersion:     StyleRuleVersion,
	}
}

type fakeRenderReader struct {
	views     map[string]domain.RenderRunView
	calls     int
	requested []string
}

func (f *fakeRenderReader) ListCurrentByVariantIDs(_ context.Context, _ string, variantIDs []string) (map[string]domain.RenderRunView, error) {
	f.calls++
	f.requested = variantIDs
	return f.views, nil
}

func TestGetPlanSetProjectsReadyPartialAcrossVariants(t *testing.T) {
	store := &fakeStore{planSet: validPublishedPlanSet()}
	// 造三套 variant 的渲染状态:ready / generating / failed。
	store.planSet.Variants = []domain.PlanVariant{
		{ID: "v-1", Key: domain.VariantSharp, Slot: 1, Recommended: true},
		{ID: "v-2", Key: domain.VariantWarm, Slot: 2},
		{ID: "v-3", Key: domain.VariantNatural, Slot: 3},
	}
	runID := "run-1"
	publicationID := "pub-1"
	views := map[string]domain.RenderRunView{
		"v-1": {Render: domain.RenderStatusView{State: domain.RenderStateReady, OperationID: "op-1",
			RenderRunID: &runID, PublicationID: &publicationID,
			Media: &domain.RenderMediaView{AssetID: "asset-1", URL: "https://signed.example/preview.jpg",
				SourceKind: domain.SourceKindGeneratedPreview, DisplayLabel: domain.DisplayLabelStyleReference}}},
		"v-2": {Render: domain.RenderStatusView{State: domain.RenderStateGenerating, OperationID: "op-2"}},
		"v-3": {Render: domain.RenderStatusView{State: domain.RenderStateFailed, OperationID: "op-3", Retryable: true}},
	}
	renders := &fakeRenderReader{views: views}
	svc := NewService(Dependencies{
		Reports: fakeReports{}, Operations: &fakeStarter{}, Store: store,
		Renders: renders,
	})
	got, err := svc.GetPlanSet(context.Background(), "user-1", store.planSet.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.RenderState != "ready_partial" {
		t.Fatalf("plan set state = %q, want ready_partial", got.RenderState)
	}
	ready := got.Variants[0].Render
	if ready == nil || ready.State != domain.RenderStateReady || ready.OperationID != "op-1" {
		t.Fatalf("ready variant projection = %#v", ready)
	}
	if ready.PublicationID == nil || *ready.PublicationID != "pub-1" || ready.Media == nil || ready.Media.URL == "" {
		t.Fatalf("ready variant must carry publication and signed media: %#v", ready)
	}
	if got.Variants[2].Render == nil || got.Variants[2].Render.Media != nil {
		t.Fatal("failed variant must not carry media")
	}
	if !got.Variants[2].Render.Retryable {
		t.Fatal("failed variant must pass through the render failure policy's retryable flag")
	}
	if got.Variants[1].Render == nil || got.Variants[1].Render.State != domain.RenderStateGenerating {
		t.Fatalf("generating variant state = %#v", got.Variants[1].Render)
	}
}

// 列表与详情走同一条合并:跨方案集一次批量读取,各套分别聚合整体状态。
func TestListPlanSetsMergesRenderStateInOneBatch(t *testing.T) {
	first := validPublishedPlanSet()
	first.Variants = []domain.PlanVariant{{ID: "v-1", Slot: 1}, {ID: "v-2", Slot: 2}, {ID: "v-3", Slot: 3}}
	second := validPublishedPlanSet()
	second.ID = "10000000-0000-0000-0000-000000000002"
	second.Variants = []domain.PlanVariant{{ID: "v-4", Slot: 1}, {ID: "v-5", Slot: 2}, {ID: "v-6", Slot: 3}}
	store := &fakeStore{planSet: first, list: []domain.PlanSet{first, second}}
	views := map[string]domain.RenderRunView{
		"v-1": {Render: domain.RenderStatusView{State: domain.RenderStateReady, OperationID: "op-1"}},
		"v-2": {Render: domain.RenderStatusView{State: domain.RenderStateGenerating, OperationID: "op-2"}},
	}
	renders := &fakeRenderReader{views: views}
	svc := NewService(Dependencies{
		Reports: fakeReports{}, Operations: &fakeStarter{}, Store: store, Renders: renders,
	})
	got, err := svc.ListPlanSets(context.Background(), "user-1", first.ReportID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if renders.calls != 1 {
		t.Fatalf("render reads = %d, want 1 batched call", renders.calls)
	}
	if len(renders.requested) != 6 {
		t.Fatalf("requested variant IDs = %v, want all 6 across both sets", renders.requested)
	}
	if got[0].RenderState != "ready_partial" {
		t.Fatalf("first set state = %q, want ready_partial", got[0].RenderState)
	}
	// 第二套没有任何渲染头:各 variant 无渲染视图,整体仍在 rendering。
	if got[1].RenderState != "rendering" {
		t.Fatalf("second set state = %q, want rendering", got[1].RenderState)
	}
	if got[1].Variants[0].Render != nil {
		t.Fatalf("variant without render head must stay nil, got %#v", got[1].Variants[0].Render)
	}
}
