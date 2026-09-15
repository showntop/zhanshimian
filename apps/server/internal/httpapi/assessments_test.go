package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/assessment"
	"github.com/zhanshimian/server/internal/service/taskrunner"
)

func TestPostAssessmentRequiresIdempotencyAndNamedSlots(t *testing.T) {
	api := newAssessmentAPI(t)
	body := `{"photos":{"face_asset_id":"face","side_asset_id":"side","body_asset_id":"body"}}`
	res := api.Do(http.MethodPost, "/v1/assessments", body, nil)
	assertError(t, res, 400, "idempotency_key_required", false)

	res = api.Do(http.MethodPost, "/v1/assessments", body, map[string]string{"Idempotency-Key": "assessment-1"})
	assertStatus(t, res, 202)
	assertJSONPath(t, res, "operation.kind", "assessment")
	assertJSONAbsent(t, res, "task")
}

type assessmentHTTP struct {
	t       *testing.T
	handler http.Handler
	userID  string
}

func (c *assessmentHTTP) Do(method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	c.t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	req = req.WithContext(context.WithValue(req.Context(), userKey, domain.User{ID: c.userID}))
	rec := httptest.NewRecorder()
	c.handler.ServeHTTP(rec, req)
	return rec
}

func newAssessmentAPI(t *testing.T) *assessmentHTTP {
	t.Helper()
	return newAssessmentHTTP(t, "user-1")
}

func newReportAPI(t *testing.T) *assessmentHTTP {
	t.Helper()
	return newAssessmentHTTP(t, "user-1")
}

func newReportAPIAs(t *testing.T, userID string) *assessmentHTTP {
	t.Helper()
	return newAssessmentHTTP(t, userID)
}

func newAssessmentHTTP(t *testing.T, userID string) *assessmentHTTP {
	t.Helper()
	repo := newHTTPAssessmentRepo()
	svc := assessment.NewService(repo, validHTTPAssetReader(), stubHTTPProfiles(), stubHTTPMedia(), httpAssessmentTaskDefinition())
	api := &API{
		assessment:  svc,
		idempotency: startedIdempotencyStore{},
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	mux := http.NewServeMux()
	mux.Handle("POST /v1/assessments", api.requireIdempotency(http.HandlerFunc(api.createAssessment)))
	mux.HandleFunc("GET /v1/reports/current", api.getCurrentPublishedReport)
	mux.HandleFunc("GET /v1/reports/{id}", api.getPublishedReport)
	return &assessmentHTTP{t: t, handler: mux, userID: userID}
}

func assertStatus(t *testing.T, res *httptest.ResponseRecorder, want int) {
	t.Helper()
	if res.Code != want {
		t.Fatalf("status = %d, want %d body=%s", res.Code, want, res.Body.String())
	}
}

func assertError(t *testing.T, res *httptest.ResponseRecorder, status int, code string, retryable bool) {
	t.Helper()
	assertStatus(t, res, status)
	assertJSONPath(t, res, "error.code", code)
	assertJSONPath(t, res, "error.retryable", retryable)
}

func assertJSONPath(t *testing.T, res *httptest.ResponseRecorder, path string, want any) {
	t.Helper()
	var root any
	if err := json.Unmarshal(res.Body.Bytes(), &root); err != nil {
		t.Fatalf("json: %v body=%s", err, res.Body.String())
	}
	got, ok := lookupJSON(root, strings.Split(path, "."))
	if !ok {
		t.Fatalf("missing %s in %s", path, res.Body.String())
	}
	if !jsonValueEqual(got, want) {
		t.Fatalf("%s = %#v, want %#v body=%s", path, got, want, res.Body.String())
	}
}

func assertJSONAbsent(t *testing.T, res *httptest.ResponseRecorder, key string) {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &root); err != nil {
		t.Fatalf("json: %v body=%s", err, res.Body.String())
	}
	if _, ok := root[key]; ok {
		t.Fatalf("did not expect top-level %q in %s", key, res.Body.String())
	}
}

func assertJSONDoesNotContainKey(t *testing.T, res *httptest.ResponseRecorder, key string) {
	t.Helper()
	var root any
	if err := json.Unmarshal(res.Body.Bytes(), &root); err != nil {
		t.Fatalf("json: %v body=%s", err, res.Body.String())
	}
	if jsonContainsKey(root, key) {
		t.Fatalf("response leaked key %q: %s", key, res.Body.String())
	}
}

func lookupJSON(value any, path []string) (any, bool) {
	if len(path) == 0 {
		return value, true
	}
	switch typed := value.(type) {
	case map[string]any:
		next, ok := typed[path[0]]
		if !ok {
			return nil, false
		}
		return lookupJSON(next, path[1:])
	case []any:
		index, err := strconv.Atoi(path[0])
		if err != nil || index < 0 || index >= len(typed) {
			return nil, false
		}
		return lookupJSON(typed[index], path[1:])
	default:
		return nil, false
	}
}

func jsonContainsKey(value any, key string) bool {
	switch typed := value.(type) {
	case map[string]any:
		if _, ok := typed[key]; ok {
			return true
		}
		for _, item := range typed {
			if jsonContainsKey(item, key) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if jsonContainsKey(item, key) {
				return true
			}
		}
	}
	return false
}

func jsonValueEqual(got, want any) bool {
	switch expected := want.(type) {
	case float64:
		actual, ok := got.(float64)
		return ok && actual == expected
	case int:
		actual, ok := got.(float64)
		return ok && actual == float64(expected)
	case bool, string:
		return got == expected
	default:
		wantRaw, _ := json.Marshal(want)
		gotRaw, _ := json.Marshal(got)
		return bytes.Equal(wantRaw, gotRaw)
	}
}

type startedIdempotencyStore struct{}

func (startedIdempotencyStore) BeginIdempotency(context.Context, domain.BeginIdempotency) (domain.IdempotencyRecord, domain.IdempotencyBeginOutcome, error) {
	return domain.IdempotencyRecord{}, domain.IdempotencyBeginStarted, nil
}

func (startedIdempotencyStore) CompleteIdempotency(context.Context, string, string, int, json.RawMessage) error {
	return nil
}

func (startedIdempotencyStore) AbortIdempotency(context.Context, string, string) error {
	return nil
}

func (startedIdempotencyStore) InvalidateIdempotency(context.Context, string, string) error {
	return nil
}

type httpAssessmentRepo struct {
	report domain.AssessmentReport
}

func newHTTPAssessmentRepo() *httpAssessmentRepo {
	return &httpAssessmentRepo{report: publishedHTTPReport()}
}

func (r *httpAssessmentRepo) CreateOrReuseAssessment(_ context.Context, params domain.CreateAssessmentParams) (domain.CreatedAssessment, error) {
	return domain.CreatedAssessment{
		PhotoSet: domain.PhotoSet{ID: "photoset-1", UserID: params.UserID},
		Run: domain.AnalysisRun{
			ID: "run-1", UserID: params.UserID, PhotoSetID: "photoset-1",
			CreatedAt: time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC),
		},
		Operation: domain.Operation{
			ID: "op-1", UserID: params.UserID, Kind: domain.OperationAssessment,
			Status: domain.OperationAccepted,
		},
		Task: domain.Task{ID: "task-1", UserID: params.UserID, Type: domain.TaskType("assessment")},
	}, nil
}

func (r *httpAssessmentRepo) GetReport(_ context.Context, userID, reportID string) (domain.AssessmentReport, error) {
	if userID != r.report.UserID || reportID != r.report.ID {
		return domain.AssessmentReport{}, repository.ErrNotFound
	}
	return r.report, nil
}

func (r *httpAssessmentRepo) GetCurrentReport(_ context.Context, userID string) (domain.AssessmentReport, error) {
	if userID != r.report.UserID {
		return domain.AssessmentReport{}, repository.ErrNotFound
	}
	return r.report, nil
}

type httpAssetReader struct {
	assets map[string]domain.MediaAsset
}

func (r httpAssetReader) GetReadyAssets(_ context.Context, _ string, ids []string) ([]domain.MediaAsset, error) {
	out := make([]domain.MediaAsset, 0, len(ids))
	for _, id := range ids {
		if asset, ok := r.assets[id]; ok {
			out = append(out, asset)
		}
	}
	return out, nil
}

func validHTTPAssetReader() httpAssetReader {
	return httpAssetReader{assets: map[string]domain.MediaAsset{
		"face": readyHTTPAsset("face", domain.MediaPurposeFace),
		"side": readyHTTPAsset("side", domain.MediaPurposeSide),
		"body": readyHTTPAsset("body", domain.MediaPurposeBody),
	}}
}

func readyHTTPAsset(id string, purpose domain.MediaPurpose) domain.MediaAsset {
	return domain.MediaAsset{
		ID: id, UserID: "user-1", Origin: domain.MediaOriginUserUpload, Purpose: purpose,
		ObjectKey: "objects/" + id, SHA256: id + "-sha", MIMEType: "image/jpeg",
		State: domain.MediaStateReady,
	}
}

type httpProfileReader struct{}

func (httpProfileReader) Snapshot(context.Context, string) (json.RawMessage, error) {
	return json.RawMessage(`{"role":"designer"}`), nil
}

func stubHTTPProfiles() assessment.ProfileReader { return httpProfileReader{} }

type httpMediaPresenter struct{}

func (httpMediaPresenter) Present(_ context.Context, asset domain.MediaAsset) (assessment.PresentedMedia, error) {
	return assessment.PresentedMedia{
		URL:          "https://signed/" + asset.ID + ".jpg",
		URLExpiresAt: time.Unix(1_700_000_000, 0).UTC(),
		MIMEType:     asset.MIMEType,
		SourceKind:   "user_original",
		DisplayLabel: "原本",
	}, nil
}

func stubHTTPMedia() assessment.MediaPresenter { return httpMediaPresenter{} }

func httpAssessmentTaskDefinition() taskrunner.Definition {
	return taskrunner.Definition{
		Type:           domain.TaskType("assessment"),
		MaxAttempts:    3,
		Timeout:        90 * time.Second,
		LeaseDuration:  30 * time.Second,
		HeartbeatEvery: 10 * time.Second,
		Concurrency:    2,
		RetryBackoff:   taskrunner.ExponentialBackoff(time.Second, time.Minute),
	}
}

func publishedHTTPReport() domain.AssessmentReport {
	items := []domain.PhotoSetItem{
		{ID: "item-face", Role: domain.PhotoRoleFace, Asset: readyHTTPAsset("face", domain.MediaPurposeFace)},
		{ID: "item-side", Role: domain.PhotoRoleSide, Asset: readyHTTPAsset("side", domain.MediaPurposeSide)},
		{ID: "item-body", Role: domain.PhotoRoleBody, Asset: readyHTTPAsset("body", domain.MediaPurposeBody)},
	}
	return domain.AssessmentReport{
		Report: domain.Report{
			ID: "report-1", UserID: "user-1", PhotoSetID: "photoset-1", HeroAssetID: "face",
			SchemaVersion: "report.v1", PriorityTitle: "先整理额前碎发", PriorityCopy: "额前碎发会挡住眉形。",
			ImpressionTags:       []string{"利落"},
			ProviderInvocationID: "inv-secret",
			QualityEvaluationID:  "quality-secret",
			Findings: []domain.ReportFinding{{
				ID: "finding-1", UserID: "user-1", ReportID: "report-1",
				Label: "额前碎发", VisibleObservation: "额前碎发落到眉毛上方", Recommendation: "向后梳理并固定",
				SourcePhotoItemID: "item-face", Category: "hair", Priority: 1, Position: 1,
				Anchor: domain.EvidenceAnchor{X: 0.1, Y: 0.1, W: 0.2, H: 0.3}, Confidence: 0.99,
			}},
		},
		PhotoSet: domain.PhotoSet{ID: "photoset-1", UserID: "user-1", Items: items},
	}
}

// 分析日限（2 次/日）超限 → 429 rate_limited，不创建、不扣费。
type httpUsageCounterFake struct{ count int }

func (u httpUsageCounterFake) CountOperationsCreatedSince(context.Context, string, []domain.OperationKind, []string, time.Time) (int, error) {
	return u.count, nil
}

func TestPostAssessmentRateLimitedIs429(t *testing.T) {
	repo := newHTTPAssessmentRepo()
	svc := assessment.NewService(repo, validHTTPAssetReader(), stubHTTPProfiles(), stubHTTPMedia(), httpAssessmentTaskDefinition()).
		WithUsageLimits(httpUsageCounterFake{count: 2})
	api := &API{
		assessment:  svc,
		idempotency: startedIdempotencyStore{},
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	mux := http.NewServeMux()
	mux.Handle("POST /v1/assessments", api.requireIdempotency(http.HandlerFunc(api.createAssessment)))
	client := &assessmentHTTP{t: t, handler: mux, userID: "user-1"}
	body := `{"photos":{"face_asset_id":"face","side_asset_id":"side","body_asset_id":"body"}}`
	res := client.Do(http.MethodPost, "/v1/assessments", body, map[string]string{"Idempotency-Key": "assessment-limited"})
	assertError(t, res, http.StatusTooManyRequests, "rate_limited", true)
	assertJSONPath(t, res, "error.message", "今日形象分析次数已用完，明天再来")
}
