package httpapi

import (
	"context"
	"net/http"
	"testing"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/planning"
)

type fakeDecisionService struct {
	put       domain.PlanVariantDecision
	putErr    error
	deleteErr error
	upserts   [][2]string // [variantID, decision]
}

func (f *fakeDecisionService) PutVariantDecision(_ context.Context, _ string, planVariantID string, decision string) (domain.PlanVariantDecision, error) {
	// 真实服务在 service 层做值域校验;fake 模拟同一行为,让 400 映射可测。
	if decision != "like" && decision != "skip" {
		return domain.PlanVariantDecision{}, planning.ErrInvalidDecision
	}
	f.upserts = append(f.upserts, [2]string{planVariantID, decision})
	return f.put, f.putErr
}

func (f *fakeDecisionService) DeleteVariantDecision(context.Context, string, string) error {
	return f.deleteErr
}

func newDecisionAPI(t *testing.T, service *fakeDecisionService) *assessmentHTTP {
	t.Helper()
	api := &API{
		decisions:   service,
		idempotency: startedIdempotencyStore{},
	}
	mux := http.NewServeMux()
	mux.Handle("PUT /v1/plan-variants/{id}/decision", api.requireIdempotency(http.HandlerFunc(api.putPlanVariantDecision)))
	mux.Handle("DELETE /v1/plan-variants/{id}/decision", http.HandlerFunc(api.deletePlanVariantDecision))
	return &assessmentHTTP{t: t, handler: mux, userID: "00000000-0000-0000-0000-000000000001"}
}

func TestPutPlanVariantDecisionReturnsEnvelope(t *testing.T) {
	service := &fakeDecisionService{put: domain.PlanVariantDecision{
		PlanVariantID: "80000000-0000-0000-0000-000000000001",
		Decision:      domain.DecisionLike,
		CreatedAt:     timeFixture(),
		UpdatedAt:     timeFixture(),
	}}
	api := newDecisionAPI(t, service)
	res := api.Do(http.MethodPut, "/v1/plan-variants/80000000-0000-0000-0000-000000000001/decision", `{"decision":"like"}`, map[string]string{"Idempotency-Key": "decision-1"})
	assertStatus(t, res, http.StatusOK)
	assertJSONPath(t, res, "data.plan_variant_id", "80000000-0000-0000-0000-000000000001")
	assertJSONPath(t, res, "data.decision", "like")
	assertJSONPath(t, res, "data.created_at", timeFixture())
	if len(service.upserts) != 1 || service.upserts[0][1] != "like" {
		t.Fatalf("upserts = %#v", service.upserts)
	}
}

func TestPutPlanVariantDecisionRejectsBadDecision(t *testing.T) {
	api := newDecisionAPI(t, &fakeDecisionService{})
	res := api.Do(http.MethodPut, "/v1/plan-variants/80000000-0000-0000-0000-000000000001/decision", `{"decision":"love"}`, map[string]string{"Idempotency-Key": "decision-bad"})
	assertError(t, res, http.StatusBadRequest, "validation_error", false)
}

func TestPutPlanVariantDecisionRejectsUnknownFields(t *testing.T) {
	api := newDecisionAPI(t, &fakeDecisionService{})
	res := api.Do(http.MethodPut, "/v1/plan-variants/80000000-0000-0000-0000-000000000001/decision", `{"decision":"like","reason":"太多"}`, map[string]string{"Idempotency-Key": "decision-unknown"})
	assertError(t, res, http.StatusBadRequest, "validation_error", false)
}

func TestPutPlanVariantDecisionRejectsMissingIdempotencyKey(t *testing.T) {
	api := newDecisionAPI(t, &fakeDecisionService{})
	res := api.Do(http.MethodPut, "/v1/plan-variants/80000000-0000-0000-0000-000000000001/decision", `{"decision":"like"}`, nil)
	assertError(t, res, http.StatusBadRequest, "idempotency_key_required", false)
}

func TestPutPlanVariantDecisionMapsUnknownVariantTo404(t *testing.T) {
	api := newDecisionAPI(t, &fakeDecisionService{putErr: repository.ErrNotFound})
	res := api.Do(http.MethodPut, "/v1/plan-variants/80000000-0000-0000-0000-000000000001/decision", `{"decision":"skip"}`, map[string]string{"Idempotency-Key": "decision-404"})
	assertError(t, res, http.StatusNotFound, "not_found", false)
}

func TestPutPlanVariantDecisionMapsInvalidTo400(t *testing.T) {
	api := newDecisionAPI(t, &fakeDecisionService{putErr: planning.ErrInvalidDecision})
	res := api.Do(http.MethodPut, "/v1/plan-variants/not-a-uuid/decision", `{"decision":"like"}`, map[string]string{"Idempotency-Key": "decision-invalid"})
	assertError(t, res, http.StatusBadRequest, "validation_error", false)
}

func TestDeletePlanVariantDecisionReturnsEmpty204(t *testing.T) {
	api := newDecisionAPI(t, &fakeDecisionService{})
	res := api.Do(http.MethodDelete, "/v1/plan-variants/80000000-0000-0000-0000-000000000001/decision", "", nil)
	if res.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", res.Code)
	}
	if body := res.Body.String(); body != "" {
		t.Fatalf("204 must carry no body, got %q", body)
	}
}

func TestDeletePlanVariantDecisionMapsError(t *testing.T) {
	api := newDecisionAPI(t, &fakeDecisionService{deleteErr: repository.ErrNotFound})
	res := api.Do(http.MethodDelete, "/v1/plan-variants/80000000-0000-0000-0000-000000000001/decision", "", nil)
	assertError(t, res, http.StatusNotFound, "not_found", false)
}

// 决策读投影:GET 方案集时 variant.decision 出现在响应里;未决 variant 该字段
// 整体缺席(omitempty),旧客户端不受影响。
func TestGetPlanSetProjectsVariantDecisions(t *testing.T) {
	published := planningPublishedPlanSet()
	published.Variants[0].Decision = &domain.PlanVariantDecision{
		PlanVariantID: published.Variants[0].ID,
		PlanSetID:     published.ID,
		Decision:      domain.DecisionLike,
		CreatedAt:     timeFixture(),
		UpdatedAt:     timeFixture(),
	}
	api := newPlanSetAPI(t, fakePlanSetService{get: published})
	res := api.Do(http.MethodGet, "/v1/plan-sets/"+published.ID, "", nil)
	assertStatus(t, res, http.StatusOK)
	assertJSONPath(t, res, "data.variants.0.decision.decision", "like")
	assertJSONPath(t, res, "data.variants.0.decision.plan_variant_id", published.Variants[0].ID)
	assertJSONPathAbsent(t, res, "data.variants.1.decision")
}
