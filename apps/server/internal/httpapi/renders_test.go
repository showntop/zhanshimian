package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/billing"
	"github.com/zhanshimian/server/internal/service/rendering"
)

type fakeRenderService struct {
	start       rendering.StartRunResult
	startErr    error
	get         domain.RenderRunView
	getErr      error
	startCount  int
	lastCommand rendering.StartRunCommand
}

func (f *fakeRenderService) StartRun(_ context.Context, cmd rendering.StartRunCommand) (rendering.StartRunResult, error) {
	f.startCount++
	f.lastCommand = cmd
	if f.startErr != nil {
		return rendering.StartRunResult{}, f.startErr
	}
	return f.start, nil
}

func (f *fakeRenderService) GetRun(context.Context, string, string) (domain.RenderRunView, error) {
	if f.getErr != nil {
		return domain.RenderRunView{}, f.getErr
	}
	return f.get, nil
}

func newRenderAPI(t *testing.T, service *fakeRenderService) *assessmentHTTP {
	t.Helper()
	api := &API{renders: service, idempotency: startedIdempotencyStore{}}
	mux := http.NewServeMux()
	mux.Handle("POST /v1/plan-variants/{id}/render-runs", api.requireIdempotency(http.HandlerFunc(api.createRenderRun)))
	mux.HandleFunc("GET /v1/render-runs/{id}", api.getRenderRun)
	return &assessmentHTTP{t: t, handler: mux, userID: "user-1"}
}

func startResult() rendering.StartRunResult {
	return rendering.StartRunResult{
		Run: domain.RenderRun{ID: "run-1", PlanVariantID: "variant-1", Generation: 1},
		Operation: domain.OperationRef{
			ID: "operation-1", Kind: domain.OperationRender, Status: domain.OperationAccepted,
		},
	}
}

func TestCreateRenderRunReturns202DataAndOperation(t *testing.T) {
	api := newRenderAPI(t, &fakeRenderService{start: startResult()})
	res := api.Do(http.MethodPost, "/v1/plan-variants/variant-1/render-runs", "{}",
		map[string]string{"Idempotency-Key": "render-variant-1-v1"})
	assertStatus(t, res, http.StatusAccepted)
	assertJSONPath(t, res, "data.id", "run-1")
	assertJSONPath(t, res, "data.plan_variant_id", "variant-1")
	assertJSONPath(t, res, "data.generation", 1)
	assertJSONPath(t, res, "data.render.state", "queued")
	assertJSONPath(t, res, "operation.id", "operation-1")
	assertJSONPath(t, res, "operation.kind", "render")
}

func TestCreateRenderRunRequiresIdempotencyKey(t *testing.T) {
	api := newRenderAPI(t, &fakeRenderService{start: startResult()})
	res := api.Do(http.MethodPost, "/v1/plan-variants/variant-1/render-runs", "{}", nil)
	assertError(t, res, http.StatusBadRequest, "idempotency_key_required", false)
}

func TestCreateRenderRunCrossTenantIs404(t *testing.T) {
	api := newRenderAPI(t, &fakeRenderService{startErr: repository.ErrNotFound})
	res := api.Do(http.MethodPost, "/v1/plan-variants/variant-1/render-runs", "{}",
		map[string]string{"Idempotency-Key": "render-x"})
	assertError(t, res, http.StatusNotFound, "not_found", false)
}

func TestGetRenderRunReadyCarriesStyleReferenceMedia(t *testing.T) {
	api := newRenderAPI(t, &fakeRenderService{get: domain.RenderRunView{
		ID: "run-1", PlanVariantID: "variant-1", Generation: 2,
		Render: domain.RenderStatusView{
			State:         domain.RenderStateReady,
			OperationID:   "operation-1",
			RenderRunID:   stringPtr("run-1"),
			PublicationID: stringPtr("publication-1"),
			Media: &domain.RenderMediaView{
				AssetID:      "asset-1",
				URL:          "https://signed.example/render.jpg",
				URLExpiresAt: timeFixture(),
				SourceKind:   domain.SourceKindGeneratedPreview,
				DisplayLabel: domain.DisplayLabelStyleReference,
			},
		},
	}})
	res := api.Do(http.MethodGet, "/v1/render-runs/run-1", "", nil)
	assertStatus(t, res, http.StatusOK)
	assertJSONPath(t, res, "data.render.state", "ready")
	assertJSONPath(t, res, "data.render.media.source_kind", "generated_preview")
	assertJSONPath(t, res, "data.render.media.display_label", "风格参考")
	// provider/model/internal_scores/reason_codes 不得出现。
	for _, key := range []string{"provider", "model", "internal_scores", "reason_codes"} {
		assertJSONDoesNotContainKey(t, res, key)
	}
}

func TestGetRenderRunCrossTenantIs404(t *testing.T) {
	api := newRenderAPI(t, &fakeRenderService{getErr: repository.ErrNotFound})
	res := api.Do(http.MethodGet, "/v1/render-runs/run-1", "", nil)
	assertError(t, res, http.StatusNotFound, "not_found", false)
}

func stringPtr(v string) *string { return &v }

func timeFixture() time.Time {
	return time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
}

// look 日限/并发超限 → 429 rate_limited（与下单限流同一公开形状）。
func TestCreateRenderRunRateLimitedIs429(t *testing.T) {
	api := newRenderAPI(t, &fakeRenderService{startErr: fmt.Errorf("%w: 今日形象方案制作次数已用完，明天再来", billing.ErrRateLimited)})
	res := api.Do(http.MethodPost, "/v1/plan-variants/variant-1/render-runs", "{}",
		map[string]string{"Idempotency-Key": "render-limited"})
	assertError(t, res, http.StatusTooManyRequests, "rate_limited", true)
}
