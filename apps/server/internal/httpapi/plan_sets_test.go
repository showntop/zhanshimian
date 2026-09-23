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
	// 从未触发渲染的 variant:占位 unavailable,其余渲染字段一律为 null。
	assertJSONPath(t, res, "data.variants.0.render.state", "unavailable")
	assertJSONPath(t, res, "data.variants.0.render.retryable", false)
	assertJSONPath(t, res, "data.variants.0.render.operation_id", nil)
	assertJSONPath(t, res, "data.variants.0.render.media", nil)
	assertJSONPath(t, res, "data.variants.0.render.render_run_id", nil)
	assertJSONPath(t, res, "data.variants.0.render.publication_id", nil)
}

// 渲染读模型合并后,variant 渲染字段必须透传真实状态:ready 嵌签名媒体与
// publication_id,failed 透传渲染失败策略判定后的 retryable;整体 state 取聚合值。
func TestGetPlanSetProjectsRenderReadModel(t *testing.T) {
	published := planningPublishedPlanSet()
	published.RenderState = "ready_partial"
	published.Variants[0].Render = &domain.RenderStatusView{
		State:         domain.RenderStateReady,
		Retryable:     false,
		OperationID:   "30000000-0000-0000-0000-000000000010",
		RenderRunID:   stringPtr("40000000-0000-0000-0000-000000000010"),
		PublicationID: stringPtr("50000000-0000-0000-0000-000000000010"),
		Media: &domain.RenderMediaView{
			AssetID:      "60000000-0000-0000-0000-000000000010",
			URL:          "https://signed.example/preview.jpg",
			URLExpiresAt: timeFixture(),
			SourceKind:   domain.SourceKindGeneratedPreview,
			DisplayLabel: domain.DisplayLabelStyleReference,
		},
	}
	published.Variants[1].Render = &domain.RenderStatusView{
		State:       domain.RenderStateFailed,
		Retryable:   true,
		OperationID: "30000000-0000-0000-0000-000000000011",
		RenderRunID: stringPtr("40000000-0000-0000-0000-000000000011"),
	}
	api := newPlanSetAPI(t, fakePlanSetService{get: published})
	res := api.Do(http.MethodGet, "/v1/plan-sets/"+published.ID, "", nil)
	assertStatus(t, res, http.StatusOK)
	assertJSONPath(t, res, "data.state", "ready_partial")
	assertJSONPath(t, res, "data.variants.0.render.state", "ready")
	assertJSONPath(t, res, "data.variants.0.render.operation_id", "30000000-0000-0000-0000-000000000010")
	assertJSONPath(t, res, "data.variants.0.render.render_run_id", "40000000-0000-0000-0000-000000000010")
	assertJSONPath(t, res, "data.variants.0.render.publication_id", "50000000-0000-0000-0000-000000000010")
	assertJSONPath(t, res, "data.variants.0.render.media.url", "https://signed.example/preview.jpg")
	assertJSONPath(t, res, "data.variants.0.render.media.source_kind", "generated_preview")
	assertJSONPath(t, res, "data.variants.0.render.media.display_label", "风格参考")
	assertJSONPath(t, res, "data.variants.1.render.state", "failed")
	assertJSONPath(t, res, "data.variants.1.render.retryable", true)
	assertJSONPath(t, res, "data.variants.1.render.media", nil)
	assertJSONPath(t, res, "data.variants.1.render.publication_id", nil)
	assertJSONPath(t, res, "data.variants.2.render.state", "unavailable")
}

// 语义键尚未发布、但同键操作已在途(dedupe 命中)时,必须重放在途 operation
// 引用让客户端继续轮询;Accepted=false 不等于"已发布",解引用空 PlanSet 会
// panic(线上实测:卡在重试循环的旧操作使新 POST 拿到 Accepted=false+nil)。
func TestPostPlanSetsReturnsInFlightOperationOnDedupe(t *testing.T) {
	api := newPlanSetAPI(t, fakePlanSetService{create: planning.CreateResult{
		PlanSetID: "10000000-0000-0000-0000-000000000001",
		Accepted:  false,
		Operation: domain.OperationRef{ID: "30000000-0000-0000-0000-000000000002", Kind: domain.OperationPlanSet, Status: domain.OperationRunning},
	}})
	body := `{"report_id":"20000000-0000-0000-0000-000000000001","scene":"daily","brief":{"activity":"office","weather":"air_conditioned","preparation":"closet","impression":"natural"}}`
	res := api.Do(http.MethodPost, "/v1/plan-sets", body, map[string]string{"Idempotency-Key": "plan-daily-dedupe"})
	assertStatus(t, res, http.StatusAccepted)
	assertJSONPath(t, res, "data.id", "10000000-0000-0000-0000-000000000001")
	assertJSONPath(t, res, "data.state", "planning")
	assertJSONPath(t, res, "operation.id", "30000000-0000-0000-0000-000000000002")
	assertJSONPath(t, res, "operation.status", "running")
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
		for _, category := range []domain.StepCategory{domain.StepCategoryHair, domain.StepCategoryMakeup, domain.StepCategoryOutfit} {
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
