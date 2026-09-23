package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/execution"
)

const (
	execPlanSetID   = "10000000-0000-0000-0000-000000000001"
	execVariantID   = "20000000-0000-0000-0000-000000000001"
	execSelectionID = "30000000-0000-0000-0000-000000000001"
	executionID     = "40000000-0000-0000-0000-000000000001"
	execStepID      = "50000000-0000-0000-0000-000000000001"
	execPublication = "60000000-0000-0000-0000-000000000001"
)

func TestPutSelectionReturnsCreatedThenReplay(t *testing.T) {
	service := &fakeExecutionService{selection: execSelectionFixture(), selectionCreated: true}
	api := newExecutionAPI(t, service)
	body := `{"plan_variant_id":"` + execVariantID + `","render_publication_id":"` + execPublication + `"}`

	res := api.Do(http.MethodPut, "/v1/plan-sets/"+execPlanSetID+"/selection", body,
		map[string]string{"Idempotency-Key": "selection-device-a-1"})
	assertStatus(t, res, http.StatusCreated)
	assertJSONPath(t, res, "data.id", execSelectionID)
	assertJSONPath(t, res, "data.plan_set_id", execPlanSetID)
	assertJSONPath(t, res, "data.plan_variant_id", execVariantID)
	assertJSONPath(t, res, "data.render_publication_id", execPublication)
	assertJSONDoesNotContainKey(t, res, "user_id")
	if service.lastPlanSetID != execPlanSetID || service.lastPutInput.PlanVariantID != execVariantID {
		t.Fatalf("plan set = %q input = %#v", service.lastPlanSetID, service.lastPutInput)
	}
	if service.lastPutInput.IdempotencyKey != "selection-device-a-1" {
		t.Fatalf("idempotency key = %q", service.lastPutInput.IdempotencyKey)
	}

	service.selectionCreated = false
	res = api.Do(http.MethodPut, "/v1/plan-sets/"+execPlanSetID+"/selection", body,
		map[string]string{"Idempotency-Key": "selection-device-a-1"})
	assertStatus(t, res, http.StatusOK)
	assertJSONPath(t, res, "data.id", execSelectionID)
}

func TestPutSelectionRequiresIdempotencyKey(t *testing.T) {
	api := newExecutionAPI(t, &fakeExecutionService{})
	res := api.Do(http.MethodPut, "/v1/plan-sets/"+execPlanSetID+"/selection",
		`{"plan_variant_id":"`+execVariantID+`"}`, nil)
	assertError(t, res, http.StatusBadRequest, "idempotency_key_required", false)
}

func TestPutSelectionValidationErrorIsBadRequest(t *testing.T) {
	api := newExecutionAPI(t, &fakeExecutionService{
		selectionErr: fmt.Errorf("%w: plan_variant_id must be a valid UUID", execution.ErrValidation),
	})
	res := api.Do(http.MethodPut, "/v1/plan-sets/"+execPlanSetID+"/selection",
		`{"plan_variant_id":"nope"}`, map[string]string{"Idempotency-Key": "selection-device-a-1"})
	assertError(t, res, http.StatusBadRequest, "validation_error", false)
}

func TestCreateExecutionReturnsCreatedWithETagThenReplay(t *testing.T) {
	service := &fakeExecutionService{snapshot: execExecutionFixture(1), snapshotCreated: true}
	api := newExecutionAPI(t, service)

	res := api.Do(http.MethodPost, "/v1/selections/"+execSelectionID+"/executions", `{}`,
		map[string]string{"Idempotency-Key": "execution-device-a-1"})
	assertStatus(t, res, http.StatusCreated)
	assertETag(t, res, 1)
	assertJSONPath(t, res, "data.id", executionID)
	assertJSONPath(t, res, "data.selection_id", execSelectionID)
	assertJSONPath(t, res, "data.state", "planned")
	assertJSONPath(t, res, "data.version", 1)
	if service.lastSelectionID != execSelectionID {
		t.Fatalf("selection id = %q", service.lastSelectionID)
	}

	service.snapshotCreated = false
	res = api.Do(http.MethodPost, "/v1/selections/"+execSelectionID+"/executions", `{}`,
		map[string]string{"Idempotency-Key": "execution-device-a-1"})
	assertStatus(t, res, http.StatusOK)
	assertETag(t, res, 1)
	assertJSONPath(t, res, "data.id", executionID)
}

func TestCreateExecutionRequiresIdempotencyKey(t *testing.T) {
	api := newExecutionAPI(t, &fakeExecutionService{})
	res := api.Do(http.MethodPost, "/v1/selections/"+execSelectionID+"/executions", `{}`, nil)
	assertError(t, res, http.StatusBadRequest, "idempotency_key_required", false)
}

func TestPostExecutionEventRequiresIdempotencyAndIfMatch(t *testing.T) {
	service := &fakeExecutionService{appendResult: execution.AppendEventResult{
		Event:     execEventFixture(domain.EventStarted, nil),
		Execution: execExecutionFixture(2),
	}}
	api := newExecutionAPI(t, service)
	body := `{"client_event_id":"device-a-0001","type":"started","occurred_at":"2026-09-12T07:00:00Z"}`

	// 缺少 If-Match 时先返回 428,而不是被幂等中间件拦成 400。
	res := api.Do(http.MethodPost, "/v1/executions/"+executionID+"/events", body, nil)
	assertError(t, res, http.StatusPreconditionRequired, "precondition_required", false)

	res = api.Do(http.MethodPost, "/v1/executions/"+executionID+"/events", body,
		map[string]string{"Idempotency-Key": "device-a-0001", "If-Match": `"1"`})
	assertStatus(t, res, http.StatusCreated)
	assertETag(t, res, 2)
	assertJSONPath(t, res, "data.event.type", "started")
	assertJSONPath(t, res, "data.event.client_event_id", "device-a-0001")
	assertJSONPath(t, res, "data.execution.version", 2)
	if service.lastAppendInput.ExpectedVersion != 1 {
		t.Fatalf("expected version = %d, want 1", service.lastAppendInput.ExpectedVersion)
	}
	if service.lastAppendInput.ClientEventID != "device-a-0001" {
		t.Fatalf("client event id = %q", service.lastAppendInput.ClientEventID)
	}
}

func TestPostExecutionEventForwardsStepIDAndReplayIsOK(t *testing.T) {
	step := execStepID
	service := &fakeExecutionService{appendResult: execution.AppendEventResult{
		Event:     execEventFixture(domain.EventStepCompleted, &step),
		Execution: execExecutionFixture(2),
		Replayed:  true,
	}}
	api := newExecutionAPI(t, service)
	body := `{"client_event_id":"device-a-0002","type":"step_completed","step_id":"` + execStepID +
		`","occurred_at":"2026-09-12T07:00:00Z"}`
	res := api.Do(http.MethodPost, "/v1/executions/"+executionID+"/events", body,
		map[string]string{"Idempotency-Key": "device-a-0002", "If-Match": `"1"`})
	assertStatus(t, res, http.StatusOK)
	assertETag(t, res, 2)
	assertJSONPath(t, res, "data.event.step_id", execStepID)
	if service.lastAppendInput.StepID == nil || *service.lastAppendInput.StepID != execStepID {
		t.Fatalf("step id = %v", service.lastAppendInput.StepID)
	}
}

func TestPostExecutionEventRejectsMalformedIfMatch(t *testing.T) {
	api := newExecutionAPI(t, &fakeExecutionService{})
	body := `{"client_event_id":"device-a-0001","type":"started","occurred_at":"2026-09-12T07:00:00Z"}`
	res := api.Do(http.MethodPost, "/v1/executions/"+executionID+"/events", body,
		map[string]string{"Idempotency-Key": "device-a-0001", "If-Match": "abc"})
	assertError(t, res, http.StatusBadRequest, "validation_error", false)
}

func TestPostExecutionEventVersionConflictIsRetryable(t *testing.T) {
	api := newExecutionAPI(t, &fakeExecutionService{appendErr: execution.ErrVersionConflict})
	res := api.Do(http.MethodPost, "/v1/executions/"+executionID+"/events", startedEventBody,
		map[string]string{"Idempotency-Key": "device-a-0001", "If-Match": `"1"`})
	assertError(t, res, http.StatusPreconditionFailed, "version_conflict", true)
}

func TestPostExecutionEventInvalidTransitionIsNotRetryable(t *testing.T) {
	api := newExecutionAPI(t, &fakeExecutionService{appendErr: domain.ErrInvalidTransition})
	res := api.Do(http.MethodPost, "/v1/executions/"+executionID+"/events", startedEventBody,
		map[string]string{"Idempotency-Key": "device-a-0001", "If-Match": `"1"`})
	assertError(t, res, http.StatusConflict, "invalid_transition", false)
}

func TestPostExecutionEventNotCompletedIsNotRetryable(t *testing.T) {
	api := newExecutionAPI(t, &fakeExecutionService{appendErr: domain.ErrExecutionNotCompleted})
	res := api.Do(http.MethodPost, "/v1/executions/"+executionID+"/events", startedEventBody,
		map[string]string{"Idempotency-Key": "device-a-0001", "If-Match": `"1"`})
	assertError(t, res, http.StatusConflict, "execution_not_completed", false)
}

func TestPostExecutionEventIdempotencyConflictIsNotRetryable(t *testing.T) {
	api := newExecutionAPI(t, &fakeExecutionService{appendErr: execution.ErrIdempotencyConflict})
	res := api.Do(http.MethodPost, "/v1/executions/"+executionID+"/events", startedEventBody,
		map[string]string{"Idempotency-Key": "device-a-0001", "If-Match": `"1"`})
	assertError(t, res, http.StatusConflict, "idempotency_conflict", false)
}

func TestGetExecutionReturnsVersionETagAndSteps(t *testing.T) {
	api := newExecutionAPI(t, &fakeExecutionService{get: execExecutionFixture(3)})
	res := api.Do(http.MethodGet, "/v1/executions/"+executionID, "", nil)
	assertStatus(t, res, http.StatusOK)
	assertETag(t, res, 3)
	assertJSONPath(t, res, "data.id", executionID)
	assertJSONPath(t, res, "data.steps.0.category", "hair")
	assertJSONPath(t, res, "data.steps.0.completed", false)
	assertJSONDoesNotContainKey(t, res, "user_id")
}

func TestGetExecutionCrossTenantIs404(t *testing.T) {
	api := newExecutionAPI(t, &fakeExecutionService{getErr: repository.ErrNotFound})
	res := api.Do(http.MethodGet, "/v1/executions/"+executionID, "", nil)
	assertError(t, res, http.StatusNotFound, "not_found", false)
}

// ---- fixtures ----

const startedEventBody = `{"client_event_id":"device-a-0001","type":"started","occurred_at":"2026-09-12T07:00:00Z"}`

func assertETag(t *testing.T, res *httptest.ResponseRecorder, version int) {
	t.Helper()
	want := fmt.Sprintf("%q", strconv.Itoa(version))
	if got := res.Header().Get("ETag"); got != want {
		t.Fatalf("etag = %q, want %q body=%s", got, want, res.Body.String())
	}
}

func newExecutionAPI(t *testing.T, service executionService) *assessmentHTTP {
	t.Helper()
	api := &API{execution: service, idempotency: startedIdempotencyStore{}}
	mux := http.NewServeMux()
	mux.Handle("PUT /v1/plan-sets/{id}/selection", api.requireIdempotency(http.HandlerFunc(api.putSelection)))
	mux.Handle("POST /v1/selections/{id}/executions", api.requireIdempotency(http.HandlerFunc(api.createExecution)))
	mux.HandleFunc("GET /v1/executions/{id}", api.getExecution)
	mux.Handle("POST /v1/executions/{id}/events", api.requireIfMatch(api.requireIdempotency(http.HandlerFunc(api.appendExecutionEvent))))
	return &assessmentHTTP{t: t, handler: mux, userID: "user-1"}
}

type fakeExecutionService struct {
	selection        domain.PlanSelection
	selectionCreated bool
	selectionErr     error
	lastPlanSetID    string
	lastPutInput     execution.PutSelectionInput

	snapshot        domain.Execution
	snapshotCreated bool
	snapshotErr     error
	lastSelectionID string
	lastExecInput   execution.CreateExecutionInput

	get    domain.Execution
	getErr error

	appendResult    execution.AppendEventResult
	appendErr       error
	lastAppendInput execution.AppendEventInput
}

func (f *fakeExecutionService) PutSelection(
	_ context.Context, _, planSetID string, input execution.PutSelectionInput,
) (domain.PlanSelection, bool, error) {
	f.lastPlanSetID = planSetID
	f.lastPutInput = input
	if f.selectionErr != nil {
		return domain.PlanSelection{}, false, f.selectionErr
	}
	return f.selection, f.selectionCreated, nil
}

func (f *fakeExecutionService) CreateExecution(
	_ context.Context, _, selectionID string, input execution.CreateExecutionInput,
) (domain.Execution, bool, error) {
	f.lastSelectionID = selectionID
	f.lastExecInput = input
	if f.snapshotErr != nil {
		return domain.Execution{}, false, f.snapshotErr
	}
	return f.snapshot, f.snapshotCreated, nil
}

func (f *fakeExecutionService) GetExecution(context.Context, string, string) (domain.Execution, error) {
	if f.getErr != nil {
		return domain.Execution{}, f.getErr
	}
	return f.get, nil
}

func (f *fakeExecutionService) AppendEvent(
	_ context.Context, _, _ string, input execution.AppendEventInput,
) (execution.AppendEventResult, error) {
	f.lastAppendInput = input
	if f.appendErr != nil {
		return execution.AppendEventResult{}, f.appendErr
	}
	return f.appendResult, nil
}

func execSelectionFixture() domain.PlanSelection {
	publication := execPublication
	return domain.PlanSelection{
		ID:                  execSelectionID,
		PlanSetID:           execPlanSetID,
		PlanVariantID:       execVariantID,
		RenderPublicationID: &publication,
		CreatedAt:           execTime(),
	}
}

func execExecutionFixture(version int) domain.Execution {
	return domain.Execution{
		ID:          executionID,
		SelectionID: execSelectionID,
		State:       domain.ExecutionPlanned,
		Version:     version,
		Steps: []domain.ExecutionStep{{
			ID: execStepID, SourcePlanStepID: execStepID, Category: "hair", Action: "adjust",
			Title: "向后梳理并固定", Summary: "先固定发根。", Position: 1,
		}},
		CreatedAt: execTime(),
		UpdatedAt: execTime(),
	}
}

func execEventFixture(kind domain.ExecutionEventType, stepID *string) domain.ExecutionEvent {
	return domain.ExecutionEvent{
		ID:            "70000000-0000-0000-0000-000000000001",
		ExecutionID:   executionID,
		ClientEventID: "device-a-0001",
		Type:          kind,
		StepID:        stepID,
		OccurredAt:    execTime(),
		CreatedAt:     execTime(),
	}
}

func execTime() time.Time { return time.Date(2026, 9, 12, 7, 0, 0, 0, time.UTC) }
