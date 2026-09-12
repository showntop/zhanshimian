package httpapi

import (
	"context"
	"net/http"
	"testing"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/planning"
)

func TestPostPlanSetsReturns202OperationEnvelope(t *testing.T) {
	api := newPlanSetAPI(t, fakePlanSetService{create: planning.CreateResult{
		PlanSetID: "10000000-0000-0000-0000-000000000001",
		Accepted:  true,
		Operation: domain.OperationRef{ID: "30000000-0000-0000-0000-000000000001", Kind: domain.OperationPlanSet, Status: domain.OperationAccepted},
	}})
	body := `{"report_id":"20000000-0000-0000-0000-000000000001","scene":"daily","brief":{"activity":"office","weather":"air_conditioned","preparation":"closet","impression":"natural"}}`
	res := api.Do(http.MethodPost, "/v1/plan-sets", body, map[string]string{"Idempotency-Key": "plan-daily-1"})
	assertStatus(t, res, http.StatusAccepted)
	assertJSONPath(t, res, "data.id", "10000000-0000-0000-0000-000000000001")
	assertJSONPath(t, res, "data.state", "planning")
	assertJSONPath(t, res, "operation.id", "30000000-0000-0000-0000-000000000001")
	assertJSONPath(t, res, "operation.kind", "plan_set")
	assertJSONPath(t, res, "operation.status", "accepted")
	assertJSONAbsent(t, res, "task")
}

func TestPostPlanSetsRejectsMissingIdempotencyKey(t *testing.T) {
	api := newPlanSetAPI(t, fakePlanSetService{})
	res := api.Do(http.MethodPost, "/v1/plan-sets", `{"report_id":"20000000-0000-0000-0000-000000000001","scene":"daily","brief":{}}`, nil)
	assertError(t, res, http.StatusBadRequest, "idempotency_key_required", false)
}

func TestPostPlanSetsReturnsPublishedPlanSetOnReuse(t *testing.T) {
	published := planningPublishedPlanSet()
	api := newPlanSetAPI(t, fakePlanSetService{create: planning.CreateResult{
		PlanSetID: published.ID, Accepted: false, PlanSet: &published,
	}})
	body := `{"report_id":"20000000-0000-0000-0000-000000000001","scene":"daily","brief":{"activity":"office","weather":"air_conditioned","preparation":"closet","impression":"natural"}}`
	res := api.Do(http.MethodPost, "/v1/plan-sets", body, map[string]string{"Idempotency-Key": "plan-daily-2"})
	assertStatus(t, res, http.StatusOK)
	assertJSONPath(t, res, "data.id", published.ID)
	assertJSONPath(t, res, "data.scene", "daily")
	assertJSONPath(t, res, "data.variants.0.key", "sharp")
	assertJSONPath(t, res, "data.variants.0.render.state", "unavailable")
}

func TestGetPlanSetExposesGroundedStepsWithoutInternalFields(t *testing.T) {
	published := planningPublishedPlanSet()
	api := newPlanSetAPI(t, fakePlanSetService{get: published})
	res := api.Do(http.MethodGet, "/v1/plan-sets/"+published.ID, "", nil)
	assertStatus(t, res, http.StatusOK)
	assertJSONPath(t, res, "data.variants.0.steps.0.category", "hair")
	assertJSONPath(t, res, "data.variants.0.steps.0.details.intensity", "low")
	assertJSONPath(t, res, "data.variants.0.steps.2.details.palette.0", "象牙白")
	assertJSONPath(t, res, "data.variants.0.steps.0.groundings.0.source_type", "report_finding")
	for _, field := range []string{"confidence", "provider_invocation_id", "quality_evaluation_id", "provider_version"} {
		assertJSONDoesNotContainKey(t, res, field)
	}
}

func TestGetPlanSetCrossTenantIs404(t *testing.T) {
	api := newPlanSetAPI(t, fakePlanSetService{getErr: repository.ErrNotFound})
	res := api.Do(http.MethodGet, "/v1/plan-sets/10000000-0000-0000-0000-000000000001", "", nil)
	assertError(t, res, http.StatusNotFound, "not_found", false)
}

func TestListPlanSetsRequiresReportID(t *testing.T) {
	api := newPlanSetAPI(t, fakePlanSetService{list: []domain.PlanSet{planningPublishedPlanSet()}})
	res := api.Do(http.MethodGet, "/v1/plan-sets", "", nil)
	assertError(t, res, http.StatusBadRequest, "validation_error", false)

	res = api.Do(http.MethodGet, "/v1/plan-sets?report_id=20000000-0000-0000-0000-000000000001&scene=daily", "", nil)
	assertStatus(t, res, http.StatusOK)
	assertJSONPath(t, res, "data.0.id", "10000000-0000-0000-0000-000000000001")
}

// ---- fixtures ----

type fakePlanSetService struct {
	create planning.CreateResult
	get    domain.PlanSet
	getErr error
	list   []domain.PlanSet
}

func (f fakePlanSetService) CreatePlanSet(context.Context, planning.CreateCommand) (planning.CreateResult, error) {
	return f.create, nil
}

func (f fakePlanSetService) GetPlanSet(context.Context, string, string) (domain.PlanSet, error) {
	if f.getErr != nil {
		return domain.PlanSet{}, f.getErr
	}
	return f.get, nil
}

func (f fakePlanSetService) ListPlanSets(context.Context, string, string, *domain.Scene) ([]domain.PlanSet, error) {
	return f.list, nil
}

func newPlanSetAPI(t *testing.T, service fakePlanSetService) *assessmentHTTP {
	t.Helper()
	api := &API{
		planning:    service,
		idempotency: startedIdempotencyStore{},
	}
	mux := http.NewServeMux()
	mux.Handle("POST /v1/plan-sets", api.requireIdempotency(http.HandlerFunc(api.createPlanSet)))
	mux.HandleFunc("GET /v1/plan-sets/{id}", api.getPlanSet)
	mux.HandleFunc("GET /v1/plan-sets", api.listPlanSets)
	return &assessmentHTTP{t: t, handler: mux, userID: "00000000-0000-0000-0000-000000000001"}
}

func planningPublishedPlanSet() domain.PlanSet {
	brief, _ := planning.NormalizeBrief(domain.SceneDaily, map[string]string{
		"activity": "office", "weather": "air_conditioned", "preparation": "closet", "impression": "natural",
	})
	set := domain.PlanSet{
		ID:                   "10000000-0000-0000-0000-000000000001",
		UserID:               "00000000-0000-0000-0000-000000000001",
		ReportID:             "20000000-0000-0000-0000-000000000001",
		Scene:                domain.SceneDaily,
		SceneBrief:           brief,
		PlannerSchemaVersion: planning.PlannerSchemaVersion,
	}
	keys := []domain.PlanVariantKey{domain.VariantSharp, domain.VariantWarm, domain.VariantNatural}
	for slot, key := range keys {
		variant := domain.PlanVariant{
			ID: string(key) + "-id", UserID: set.UserID, PlanSetID: set.ID,
			Slot: slot + 1, Key: key, Name: "方案" + string(key),
			Descriptor: "有精神且自然", Rationale: "落实报告优先建议",
			Recommended: slot == 0, OutcomeTags: []string{"易执行"}, DifferenceTags: []string{"发型线条"},
		}
		for _, category := range []domain.StepCategory{domain.CategoryHair, domain.CategoryMakeup, domain.CategoryOutfit} {
			step := domain.PlanStep{
				ID: variant.ID + "-" + string(category), Category: category,
				Action: domain.ActionAdjust, Title: "调整" + string(category),
				Summary: "按报告依据微调。", Position: len(variant.Steps) + 1,
				Details: domain.PlanStepDetails{
					Target: "目标" + string(category), Intensity: "low",
					Silhouette: "合肩直线版型", Palette: []string{"象牙白"},
					Layers: []string{"浅色内搭"}, Avoid: []string{"夸张图案"},
				},
				Groundings: []domain.PlanStepGrounding{{
					SourceType: domain.SourceReportFinding, SourceID: "finding-1", Reason: "落实最高优先 finding",
				}},
			}
			variant.Steps = append(variant.Steps, step)
		}
		set.Variants = append(set.Variants, variant)
	}
	return set
}
