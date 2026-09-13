package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/feedback"
)

const (
	fbPublicationID = "10000000-0000-0000-0000-0000000000f1"
	fbExecutionID   = "20000000-0000-0000-0000-0000000000f2"
	fbAssetID       = "30000000-0000-0000-0000-0000000000f3"
)

func TestGenerationFeedbackAcceptsNoMedia(t *testing.T) {
	service := &fakeFeedbackService{generation: generationFeedbackFixture(nil), generationCreated: true}
	api := newFeedbackAPI(t, service)
	res := api.Do(http.MethodPost, "/v1/generation-feedback",
		`{"publication_id":"`+fbPublicationID+`","tags":["unnatural"]}`,
		map[string]string{"Idempotency-Key": "generation-feedback-http-1"})

	assertStatus(t, res, http.StatusCreated)
	assertJSONPath(t, res, "data.acknowledgement_code", "feedback_recorded")
	// 上传失败时省略 media_asset_id,文字与标签照常提交。
	assertJSONPathAbsent(t, res, "data.media_asset_id")
	assertJSONDoesNotContainKey(t, res, "user_id")
	if service.lastGenerationInput.PublicationID != fbPublicationID ||
		service.lastGenerationInput.MediaAssetID != nil {
		t.Fatalf("generation input = %#v", service.lastGenerationInput)
	}
	if len(service.lastGenerationInput.Tags) != 1 || service.lastGenerationInput.Tags[0] != domain.GenerationUnnatural {
		t.Fatalf("tags = %#v", service.lastGenerationInput.Tags)
	}
}

func TestGenerationFeedbackForwardsUploadedMediaAndReplays(t *testing.T) {
	service := &fakeFeedbackService{generation: generationFeedbackFixture(&fbAsset), generationCreated: true}
	api := newFeedbackAPI(t, service)
	body := `{"publication_id":"` + fbPublicationID + `","tags":["identity_mismatch"],` +
		`"comment":"下巴不像","media_asset_id":"` + fbAssetID + `"}`

	res := api.Do(http.MethodPost, "/v1/generation-feedback", body,
		map[string]string{"Idempotency-Key": "generation-feedback-http-2"})
	assertStatus(t, res, http.StatusCreated)
	assertJSONPath(t, res, "data.media_asset_id", fbAssetID)
	if service.lastGenerationInput.MediaAssetID == nil || *service.lastGenerationInput.MediaAssetID != fbAssetID {
		t.Fatalf("media asset = %v", service.lastGenerationInput.MediaAssetID)
	}

	service.generationCreated = false
	res = api.Do(http.MethodPost, "/v1/generation-feedback", body,
		map[string]string{"Idempotency-Key": "generation-feedback-http-2"})
	assertStatus(t, res, http.StatusOK)
}

func TestGenerationFeedbackRequiresIdempotencyKey(t *testing.T) {
	api := newFeedbackAPI(t, &fakeFeedbackService{})
	res := api.Do(http.MethodPost, "/v1/generation-feedback",
		`{"publication_id":"`+fbPublicationID+`","tags":["unnatural"]}`, nil)
	assertError(t, res, http.StatusBadRequest, "idempotency_key_required", false)
}

func TestGenerationFeedbackRejectsForeignPublication(t *testing.T) {
	api := newFeedbackAPI(t, &fakeFeedbackService{generationErr: repository.ErrNotFound})
	res := api.Do(http.MethodPost, "/v1/generation-feedback",
		`{"publication_id":"`+fbPublicationID+`","tags":["unnatural"]}`,
		map[string]string{"Idempotency-Key": "generation-feedback-http-3"})
	assertError(t, res, http.StatusNotFound, "not_found", false)
}

func TestGenerationFeedbackRejectsPreferenceField(t *testing.T) {
	// Generation 反馈绝不形成偏好记忆,preference 字段在传输层就被拒绝。
	api := newFeedbackAPI(t, &fakeFeedbackService{})
	res := api.Do(http.MethodPost, "/v1/generation-feedback",
		`{"publication_id":"`+fbPublicationID+`","tags":["unnatural"],`+
			`"preference":{"kind":"less_formal","category":"overall"}}`,
		map[string]string{"Idempotency-Key": "generation-feedback-http-4"})
	assertError(t, res, http.StatusBadRequest, "validation_error", false)
}

func TestExecutionFeedbackReturnsPromiseOnlyForAppliedMemory(t *testing.T) {
	service := &fakeFeedbackService{
		execution: executionFeedbackFixture(domain.AckLessFormalSaved, []domain.PreferenceMemory{{
			ID: "90000000-0000-0000-0000-0000000000f9", Key: "formality",
			Category: domain.CategoryOverall, Value: "less", SourceTag: domain.ExecutionTooFormal,
		}}),
		executionCreated: true,
	}
	api := newFeedbackAPI(t, service)
	res := api.Do(http.MethodPost, "/v1/execution-feedback",
		`{"execution_id":"`+fbExecutionID+`","tags":["too_formal"],`+
			`"preference":{"kind":"less_formal","category":"overall"}}`,
		map[string]string{"Idempotency-Key": "execution-feedback-http-1"})

	assertStatus(t, res, http.StatusCreated)
	assertJSONPath(t, res, "data.acknowledgement_code", "less_formal_saved")
	assertJSONArrayLength(t, res, "data.applied_memories", 1)
	assertJSONPath(t, res, "data.applied_memories.0.key", "formality")
	if got := service.lastExecutionInput.Preference.Kind; got != domain.PreferenceLessFormal {
		t.Fatalf("preference kind = %q", got)
	}
}

func TestExecutionFeedbackWithoutMemoryOnlyRecords(t *testing.T) {
	service := &fakeFeedbackService{
		execution:        executionFeedbackFixture(domain.AckFeedbackRecorded, []domain.PreferenceMemory{}),
		executionCreated: true,
	}
	api := newFeedbackAPI(t, service)
	res := api.Do(http.MethodPost, "/v1/execution-feedback",
		`{"execution_id":"`+fbExecutionID+`","tags":["easy_to_execute"]}`,
		map[string]string{"Idempotency-Key": "execution-feedback-http-2"})

	assertStatus(t, res, http.StatusCreated)
	assertJSONPath(t, res, "data.acknowledgement_code", "feedback_recorded")
	assertJSONArrayLength(t, res, "data.applied_memories", 0)
	// 只返回受枚举约束的 code,不返回任意 message。
	assertJSONPathAbsent(t, res, "data.message")
}

func TestExecutionFeedbackAcceptsNoMediaAndReplays(t *testing.T) {
	service := &fakeFeedbackService{
		execution:        executionFeedbackFixture(domain.AckFeedbackRecorded, []domain.PreferenceMemory{}),
		executionCreated: true,
	}
	api := newFeedbackAPI(t, service)
	body := `{"execution_id":"` + fbExecutionID + `","tags":["want_to_keep"],"comment":"偏分很好"}`

	res := api.Do(http.MethodPost, "/v1/execution-feedback", body,
		map[string]string{"Idempotency-Key": "execution-feedback-http-3"})
	assertStatus(t, res, http.StatusCreated)
	assertJSONPathAbsent(t, res, "data.media_asset_id")

	service.executionCreated = false
	res = api.Do(http.MethodPost, "/v1/execution-feedback", body,
		map[string]string{"Idempotency-Key": "execution-feedback-http-3"})
	assertStatus(t, res, http.StatusOK)
	assertJSONDoesNotContainKey(t, res, "user_id")
}

func TestExecutionFeedbackActiveExecutionIsConflict(t *testing.T) {
	api := newFeedbackAPI(t, &fakeFeedbackService{executionErr: feedback.ErrExecutionNotCompleted})
	res := api.Do(http.MethodPost, "/v1/execution-feedback",
		`{"execution_id":"`+fbExecutionID+`","tags":["easy_to_execute"]}`,
		map[string]string{"Idempotency-Key": "execution-feedback-http-4"})
	assertError(t, res, http.StatusConflict, "execution_not_completed", false)
}

func TestExecutionFeedbackIdempotencyConflictIsConflict(t *testing.T) {
	api := newFeedbackAPI(t, &fakeFeedbackService{executionErr: feedback.ErrIdempotencyConflict})
	res := api.Do(http.MethodPost, "/v1/execution-feedback",
		`{"execution_id":"`+fbExecutionID+`","tags":["easy_to_execute"]}`,
		map[string]string{"Idempotency-Key": "execution-feedback-http-5"})
	assertError(t, res, http.StatusConflict, "idempotency_conflict", false)
}

func TestExecutionFeedbackValidationErrorIsBadRequest(t *testing.T) {
	api := newFeedbackAPI(t, &fakeFeedbackService{
		executionErr: fmt.Errorf("%w: execution_id must be a valid UUID", feedback.ErrValidation),
	})
	res := api.Do(http.MethodPost, "/v1/execution-feedback",
		`{"execution_id":"nope","tags":["easy_to_execute"]}`,
		map[string]string{"Idempotency-Key": "execution-feedback-http-6"})
	assertError(t, res, http.StatusBadRequest, "validation_error", false)
}

func TestExecutionFeedbackGenerationTagIsRejected(t *testing.T) {
	// Generation 标签不得进入执行反馈;服务层返回 ErrPreferenceNotAllowed。
	api := newFeedbackAPI(t, &fakeFeedbackService{executionErr: domain.ErrPreferenceNotAllowed})
	res := api.Do(http.MethodPost, "/v1/execution-feedback",
		`{"execution_id":"`+fbExecutionID+`","tags":["unnatural"]}`,
		map[string]string{"Idempotency-Key": "execution-feedback-http-7"})
	assertError(t, res, http.StatusBadRequest, "validation_error", false)
}

// ---- fixtures ----

var fbAsset = fbAssetID

func assertJSONPathAbsent(t *testing.T, res *httptest.ResponseRecorder, path string) {
	t.Helper()
	var root any
	if err := json.Unmarshal(res.Body.Bytes(), &root); err != nil {
		t.Fatalf("json: %v body=%s", err, res.Body.String())
	}
	if _, ok := lookupJSON(root, strings.Split(path, ".")); ok {
		t.Fatalf("did not expect %s in %s", path, res.Body.String())
	}
}

func assertJSONArrayLength(t *testing.T, res *httptest.ResponseRecorder, path string, want int) {
	t.Helper()
	var root any
	if err := json.Unmarshal(res.Body.Bytes(), &root); err != nil {
		t.Fatalf("json: %v body=%s", err, res.Body.String())
	}
	value, ok := lookupJSON(root, strings.Split(path, "."))
	if !ok {
		t.Fatalf("missing %s in %s", path, res.Body.String())
	}
	items, ok := value.([]any)
	if !ok {
		t.Fatalf("%s is not an array: %s", path, res.Body.String())
	}
	if len(items) != want {
		t.Fatalf("%s length = %d, want %d body=%s", path, len(items), want, res.Body.String())
	}
}

func newFeedbackAPI(t *testing.T, service feedbackService) *assessmentHTTP {
	t.Helper()
	api := &API{feedback: service, idempotency: startedIdempotencyStore{}}
	mux := http.NewServeMux()
	mux.Handle("POST /v1/generation-feedback", api.requireIdempotency(http.HandlerFunc(api.createGenerationFeedback)))
	mux.Handle("POST /v1/execution-feedback", api.requireIdempotency(http.HandlerFunc(api.createExecutionFeedback)))
	return &assessmentHTTP{t: t, handler: mux, userID: "user-1"}
}

type fakeFeedbackService struct {
	generation          feedback.GenerationFeedback
	generationCreated   bool
	generationErr       error
	lastGenerationInput feedback.CreateGenerationFeedbackInput

	execution          feedback.ExecutionFeedback
	executionCreated   bool
	executionErr       error
	lastExecutionInput feedback.CreateExecutionFeedbackInput
}

func (f *fakeFeedbackService) CreateGenerationFeedback(
	_ context.Context, _ string, input feedback.CreateGenerationFeedbackInput,
) (feedback.GenerationFeedback, bool, error) {
	f.lastGenerationInput = input
	if f.generationErr != nil {
		return feedback.GenerationFeedback{}, false, f.generationErr
	}
	return f.generation, f.generationCreated, nil
}

func (f *fakeFeedbackService) CreateExecutionFeedback(
	_ context.Context, _ string, input feedback.CreateExecutionFeedbackInput,
) (feedback.ExecutionFeedback, bool, error) {
	f.lastExecutionInput = input
	if f.executionErr != nil {
		return feedback.ExecutionFeedback{}, false, f.executionErr
	}
	return f.execution, f.executionCreated, nil
}

func generationFeedbackFixture(mediaAssetID *string) feedback.GenerationFeedback {
	return feedback.GenerationFeedback{
		ID:                  "10000000-0000-0000-0000-0000000000a1",
		PublicationID:       fbPublicationID,
		RenderRunID:         "10000000-0000-0000-0000-0000000000a2",
		CandidateID:         "10000000-0000-0000-0000-0000000000a3",
		AssetID:             fbAssetID,
		Generation:          1,
		Tags:                []domain.Tag{domain.GenerationUnnatural},
		MediaAssetID:        mediaAssetID,
		AcknowledgementCode: domain.AckFeedbackRecorded,
		CreatedAt:           execTime(),
	}
}

func executionFeedbackFixture(
	code domain.AcknowledgementCode, memories []domain.PreferenceMemory,
) feedback.ExecutionFeedback {
	return feedback.ExecutionFeedback{
		ID:                  "20000000-0000-0000-0000-0000000000b1",
		ExecutionID:         fbExecutionID,
		SelectionID:         "20000000-0000-0000-0000-0000000000b2",
		PlanSetID:           "20000000-0000-0000-0000-0000000000b3",
		Tags:                []domain.Tag{domain.ExecutionTooFormal},
		AppliedMemories:     memories,
		AcknowledgementCode: code,
		CreatedAt:           execTime(),
	}
}
