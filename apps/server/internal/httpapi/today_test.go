package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/service/today"
	"github.com/zhanshimian/server/internal/storage"
)

type fakeTodayService struct {
	plan      today.Plan
	err       error
	generateN *atomic.Int32
}

func (f fakeTodayService) Context(_ context.Context, city string, schedule string) domain.TodayContext {
	return domain.TodayContext{City: city, Schedule: schedule}
}

func (f fakeTodayService) Generate(context.Context, string, today.CreateInput) (today.Plan, error) {
	if f.generateN != nil {
		f.generateN.Add(1)
	}
	return f.plan, f.err
}

func (f fakeTodayService) Current(context.Context, string) (today.Plan, error) {
	return f.plan, f.err
}

func (f fakeTodayService) Activate(context.Context, string, string) (today.Plan, error) {
	return f.plan, f.err
}

func (f fakeTodayService) Feedback(context.Context, string, string, string) (today.Plan, error) {
	return f.plan, f.err
}

// 契约 TodayPlanAccepted：创建响应必须是 {data, operation}——客户端凭
// operation.id 轮询搭配图渲染进度，缺 operation 直接 TypeError。
func TestCreateTodayPlanResponseCarriesOperation(t *testing.T) {
	api := &API{
		today: fakeTodayService{plan: today.Plan{
			ID: "today-1", Title: "今日利落通勤", State: "planning",
			Steps:     []domain.TodayPlanStep{},
			Operation: domain.OperationRef{ID: "op-1", Kind: domain.OperationRender, Status: domain.OperationAccepted},
		}},
		logger: discardLogger(),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/today/plans", api.createTodayPlan)

	req := httptest.NewRequest(http.MethodPost, "/v1/today/plans", strings.NewReader(`{"city":"杭州"}`))
	req = req.WithContext(context.WithValue(req.Context(), userKey, domain.User{ID: "user-1"}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusCreated)
	assertJSONPath(t, rec, "data.id", "today-1")
	assertJSONPath(t, rec, "data.operation.id", "op-1")
	assertJSONPath(t, rec, "operation.id", "op-1")
	assertJSONPath(t, rec, "operation.kind", "render")
	assertJSONPath(t, rec, "operation.status", "accepted")
}

// 契约要求 POST /v1/today/plans 必带 Idempotency-Key：缺键 400；
// 同键同体重放返回首次响应且不重复生成。
func TestCreateTodayPlanRouteRequiresIdempotencyKey(t *testing.T) {
	repo := newSessionMediaRepo()
	objects, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var generateN atomic.Int32
	deps := newTestDependencies(repo, objects)
	deps.Today = fakeTodayService{
		generateN: &generateN,
		plan: today.Plan{
			ID: "today-1", Title: "今日利落通勤", State: "planning",
			Steps:     []domain.TodayPlanStep{},
			Operation: domain.OperationRef{ID: "op-1", Kind: domain.OperationRender, Status: domain.OperationAccepted},
		},
	}
	handler := New(deps, discardLogger(), true, RuntimeInfo{})

	login := httptest.NewRecorder()
	loginReq := httptest.NewRequest(http.MethodPost, "/v1/auth/dev", strings.NewReader(`{"nickname":"today"}`))
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

	post := func(key string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/today/plans", strings.NewReader(`{"city":"杭州"}`))
		req.Header.Set("Authorization", "Bearer "+session.Data.Token)
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		handler.ServeHTTP(rec, req)
		return rec
	}

	missing := post("")
	assertError(t, missing, http.StatusBadRequest, "idempotency_key_required", false)
	if generateN.Load() != 0 {
		t.Fatalf("missing key must not reach the service: %d calls", generateN.Load())
	}

	first := post("today-key-1")
	if first.Code != http.StatusCreated {
		t.Fatalf("first status = %d body=%s", first.Code, first.Body.String())
	}
	if !strings.Contains(first.Body.String(), `"operation"`) {
		t.Fatalf("created response missing operation: %s", first.Body.String())
	}
	replay := post("today-key-1")
	if replay.Body.String() != first.Body.String() {
		t.Fatalf("replay body mismatch\nfirst  %s\nsecond %s", first.Body.String(), replay.Body.String())
	}
	if generateN.Load() != 1 {
		t.Fatalf("service calls = %d, want 1（重放不得重复生成）", generateN.Load())
	}
}
