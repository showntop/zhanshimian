package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/operation"
	"github.com/zhanshimian/server/internal/storage"
)

func TestGetOperationHidesTaskInternals(t *testing.T) {
	op := domain.Operation{
		ID: "op1", UserID: "u1", Kind: domain.OperationAssessment,
		Status: domain.OperationRunning, ProgressBPS: 3500,
		StageCode: "photo.technical_check", PublicMessage: "正在检查照片",
	}
	api := newOperationAPI(t, op)
	response := authenticatedRequest(t, api, "u1", http.MethodGet, "/v1/operations/op1", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, leaked := range []string{"payload", "provider", "lease", "user_id"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("response leaked %q: %s", leaked, body)
		}
	}
	assertJSONEq(t, `{"data":{"id":"op1","kind":"assessment","status":"running","progress_bps":3500,"stage_code":"photo.technical_check","public_message":"正在检查照片","retryable":false}}`, body)
}

func TestGetOperationFromAnotherUserReturnsNotFound(t *testing.T) {
	api := newOperationAPI(t, domain.Operation{ID: "op1", UserID: "u1"})
	response := authenticatedRequest(t, api, "u2", http.MethodGet, "/v1/operations/op1", nil)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
}

func TestListOperationsPreservesRequestOrder(t *testing.T) {
	first := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	second := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	api := newOperationAPI(t,
		domain.Operation{ID: second.String(), UserID: "u1", Kind: domain.OperationRender, Status: domain.OperationSucceeded},
		domain.Operation{ID: first.String(), UserID: "u1", Kind: domain.OperationAssessment, Status: domain.OperationRunning},
	)
	response := authenticatedRequest(t, api, "u1", http.MethodGet, "/v1/operations?ids="+first.String()+","+second.String(), nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data) != 2 || payload.Data[0].ID != first.String() || payload.Data[1].ID != second.String() {
		t.Fatalf("order = %#v", payload.Data)
	}
}

func TestListOperationsMissingIDReturnsNotFound(t *testing.T) {
	owned := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	other := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	api := newOperationAPI(t,
		domain.Operation{ID: owned.String(), UserID: "u1", Kind: domain.OperationAssessment, Status: domain.OperationRunning},
		domain.Operation{ID: other.String(), UserID: "u2", Kind: domain.OperationAssessment, Status: domain.OperationRunning},
	)
	response := authenticatedRequest(t, api, "u1", http.MethodGet, "/v1/operations?ids="+owned.String()+","+other.String(), nil)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
}

func TestListOperationsRejectsDuplicates(t *testing.T) {
	id := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	api := newOperationAPI(t, domain.Operation{ID: id.String(), UserID: "u1"})
	response := authenticatedRequest(t, api, "u1", http.MethodGet, "/v1/operations?ids="+id.String()+","+id.String(), nil)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
}

func TestGetFailedOperationIncludesTraceID(t *testing.T) {
	op := domain.Operation{
		ID: "op-fail", UserID: "u1", Kind: domain.OperationPlanSet,
		Status: domain.OperationFailed, TraceID: "trace-fail-1", PublicMessage: "生成失败",
	}
	api := newOperationAPI(t, op)
	response := authenticatedRequest(t, api, "u1", http.MethodGet, "/v1/operations/op-fail", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"trace_id":"trace-fail-1"`) {
		t.Fatalf("failed operation missing trace_id: %s", response.Body.String())
	}
}

func TestNewWiresOperations(t *testing.T) {
	repo := newSessionOperationRepo()
	local, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc := newServiceForAPI(t, repo, local)
	handler := New(svc, discardLogger(), true, RuntimeInfo{})

	login := httptest.NewRecorder()
	loginReq := httptest.NewRequest(http.MethodPost, "/v1/auth/dev", strings.NewReader(`{"nickname":"ops"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(login, loginReq)
	if login.Code != http.StatusCreated {
		t.Fatalf("login status = %d body=%s", login.Code, login.Body.String())
	}
	var session struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if session.Data.Token == "" {
		t.Fatalf("login token is empty: %s", login.Body.String())
	}

	opID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	repo.put(domain.Operation{
		ID: opID, UserID: repo.lastUserID, Kind: domain.OperationAssessment,
		Status: domain.OperationAccepted, PublicMessage: "已受理",
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/operations/"+opID, nil)
	req.Header.Set("Authorization", "Bearer "+session.Data.Token)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get operation via New status = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "user_id") {
		t.Fatalf("wired response leaked user_id: %s", rec.Body.String())
	}
}

func newOperationAPI(t *testing.T, ops ...domain.Operation) *API {
	t.Helper()
	return &API{operations: operation.New(newOperationReaderFake(ops...)), logger: discardLogger()}
}

func authenticatedRequest(t *testing.T, api *API, userID, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), userKey, domain.User{ID: userID}))
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/operations/{id}", api.getOperation)
	mux.HandleFunc("GET /v1/operations", api.listOperations)
	mux.ServeHTTP(rec, req)
	return rec
}

func assertJSONEq(t *testing.T, want, got string) {
	t.Helper()
	var wantVal, gotVal any
	if err := json.Unmarshal([]byte(want), &wantVal); err != nil {
		t.Fatalf("want json: %v", err)
	}
	if err := json.Unmarshal([]byte(got), &gotVal); err != nil {
		t.Fatalf("got json: %v", err)
	}
	wantRaw, err := json.Marshal(wantVal)
	if err != nil {
		t.Fatal(err)
	}
	gotRaw, err := json.Marshal(gotVal)
	if err != nil {
		t.Fatal(err)
	}
	if string(wantRaw) != string(gotRaw) {
		t.Fatalf("json mismatch\nwant %s\ngot  %s", wantRaw, gotRaw)
	}
}

type operationReaderFake struct {
	ops map[string]domain.Operation
}

func newOperationReaderFake(ops ...domain.Operation) *operationReaderFake {
	fake := &operationReaderFake{ops: map[string]domain.Operation{}}
	for _, op := range ops {
		fake.ops[op.ID] = op
	}
	return fake
}

func (r *operationReaderFake) GetOperation(_ context.Context, userID, id string) (domain.Operation, error) {
	op, ok := r.ops[id]
	if !ok || op.UserID != userID {
		return domain.Operation{}, repository.ErrNotFound
	}
	return op, nil
}

func (r *operationReaderFake) GetOperations(ctx context.Context, userID string, ids []string) ([]domain.Operation, error) {
	out := make([]domain.Operation, 0, len(ids))
	for _, id := range ids {
		op, err := r.GetOperation(ctx, userID, id)
		if err != nil {
			return nil, err
		}
		out = append(out, op)
	}
	return out, nil
}

type sessionOperationRepo struct {
	*sessionMediaRepo
	*operationReaderFake
	lastUserID string
}

func newSessionOperationRepo() *sessionOperationRepo {
	return &sessionOperationRepo{
		sessionMediaRepo:    newSessionMediaRepo(),
		operationReaderFake: newOperationReaderFake(),
	}
}

func (r *sessionOperationRepo) CreateDevUser(ctx context.Context, nickname string) (domain.User, error) {
	user, err := r.sessionMediaRepo.CreateDevUser(ctx, nickname)
	if err != nil {
		return domain.User{}, err
	}
	r.lastUserID = user.ID
	return user, nil
}

func (r *sessionOperationRepo) put(op domain.Operation) {
	r.ops[op.ID] = op
}
