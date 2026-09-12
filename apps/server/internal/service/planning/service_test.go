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

type fakeStore struct {
	found      bool
	planSet    domain.PlanSet
	getCalls   int
	listCalls  int
	listScene  *domain.Scene
	prepareCmd *PrepareCommand
}

func (f *fakeStore) FindPublished(_ context.Context, _ PlanSetKey) (domain.PlanSet, bool, error) {
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
	return []domain.PlanSet{f.planSet}, nil
}

func (f *fakeStore) Prepare(_ context.Context, _ domain.TaskLease, command PrepareCommand) (domain.PlanSet, error) {
	f.prepareCmd = &command
	f.planSet = command.PlanSet
	f.found = true
	return command.PlanSet, nil
}

func (f *fakeStore) CommitPrepared(_ context.Context, _ domain.TaskLease, _ domain.TaskResult) (domain.CommitOutcome, error) {
	return domain.CommitApplied, nil
}

func validReport(reportID string) ReportSnapshot {
	return ReportSnapshot{
		ID:          reportID,
		UserID:      "00000000-0000-0000-0000-000000000001",
		PhotoSetID:  "50000000-0000-0000-0000-000000000001",
		FaceAssetID: "40000000-0000-0000-0000-000000000002",
		BodyAssetID: "40000000-0000-0000-0000-000000000001",
		ProfileSnapshot: json.RawMessage(`{"role":"designer"}`),
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
