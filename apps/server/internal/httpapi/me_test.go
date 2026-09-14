package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zhanshimian/server/internal/domain"
)

// fakeAccountService 只实现 /v1/me/profile 链路与编译期接口满足，其余方法 panic。
type fakeAccountService struct {
	AccountService
	profile domain.UserProfile
	saved   domain.UserProfile
	err     error
}

func (f *fakeAccountService) GetProfile(_ context.Context, _ string) (domain.UserProfile, error) {
	return f.profile, f.err
}

func (f *fakeAccountService) UpdateProfile(_ context.Context, _ string, p domain.UserProfile) (domain.UserProfile, error) {
	f.saved = p
	return p, nil
}

func newProfileAPI(account AccountService) (*API, *http.ServeMux) {
	api := &API{account: account, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/me/profile", api.getMyProfile)
	mux.HandleFunc("PUT /v1/me/profile", api.updateMyProfile)
	return api, mux
}

func profileRequest(method, body string) *http.Request {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, "/v1/me/profile", reader)
	return req.WithContext(context.WithValue(req.Context(), userKey, domain.User{ID: "user-1"}))
}

// 冻结 OpenAPI：/v1/me/profile 说 UserProfile 线形（height_cm/role/budget 必填，
// weight_kg/bust_cm/waist_cm/hip_cm 选填）。第 15 轮全量 E2E 实测：PUT 送
// {"height_cm":165,...} 被 400（handler 解进未打标签的 domain.UserProfile，
// DisallowUnknownFields 把 height_cm 当未知字段），小程序保存资料必挂。
func TestUpdateMyProfileAcceptsOpenAPIWireShape(t *testing.T) {
	fake := &fakeAccountService{}
	_, mux := newProfileAPI(fake)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, profileRequest(http.MethodPut,
		`{"height_cm":165,"role":"产品经理","budget":"500-1500","weight_kg":55,"bust_cm":88}`))

	assertStatus(t, rec, 200)
	assertJSONPath(t, rec, "data.height_cm", 165)
	assertJSONPath(t, rec, "data.role", "产品经理")
	assertJSONPath(t, rec, "data.budget", "500-1500")
	assertJSONPath(t, rec, "data.weight_kg", 55)
	assertJSONPath(t, rec, "data.bust_cm", 88)
	assertJSONPathAbsent(t, rec, "data.waist_cm")
	assertJSONPathAbsent(t, rec, "data.hip_cm")

	// 测量项必须进入 Preferences 补丁，由仓储合并进 preferences JSONB
	// （全量保存：未提供的测量键删除，feedback_memory 等其他键保留）。
	if fake.saved.HeightCM != 165 || fake.saved.Role != "产品经理" || fake.saved.Budget != "500-1500" {
		t.Fatalf("saved profile = %+v", fake.saved)
	}
	var patch map[string]any
	if err := json.Unmarshal(fake.saved.Preferences, &patch); err != nil {
		t.Fatalf("preferences patch: %v raw=%s", err, fake.saved.Preferences)
	}
	if patch["weight_kg"] != 55.0 || patch["bust_cm"] != 88.0 {
		t.Fatalf("patch = %v", patch)
	}
	if _, ok := patch["waist_cm"]; ok {
		t.Fatalf("patch must not carry unprovided keys: %v", patch)
	}
}

func TestGetMyProfileReturnsOpenAPIWireShape(t *testing.T) {
	fake := &fakeAccountService{profile: domain.UserProfile{
		ID: "p-1", UserID: "user-1", Role: "产品经理", HeightCM: 165, Budget: "500-1500",
		Preferences: json.RawMessage(`{"weight_kg":55,"feedback_memory":[{"key":"x"}]}`),
	}}
	_, mux := newProfileAPI(fake)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, profileRequest(http.MethodGet, ""))

	assertStatus(t, rec, 200)
	assertJSONPath(t, rec, "data.height_cm", 165)
	assertJSONPath(t, rec, "data.role", "产品经理")
	assertJSONPath(t, rec, "data.budget", "500-1500")
	assertJSONPath(t, rec, "data.weight_kg", 55)
	// 内部反馈记忆不得漏到外线。
	assertJSONPathAbsent(t, rec, "data.feedback_memory")
	assertJSONPathAbsent(t, rec, "data.preferences")
	assertJSONPathAbsent(t, rec, "data.bust_cm")
}

func TestGetMyProfileTreatsBackfilledBlankRowAsUnfilled(t *testing.T) {
	// 评估发布会回填仅含 current_report_id 的空行（ID/UserID 已存在），
	// 契约「从未填写时 data 为 null」同样适用。
	fake := &fakeAccountService{profile: domain.UserProfile{ID: "p-1", UserID: "user-1"}}
	_, mux := newProfileAPI(fake)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, profileRequest(http.MethodGet, ""))

	assertStatus(t, rec, 200)
	assertJSONPath(t, rec, "data", nil)
}

func TestUpdateMyProfileRejectsUnknownField(t *testing.T) {
	fake := &fakeAccountService{}
	_, mux := newProfileAPI(fake)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, profileRequest(http.MethodPut,
		`{"height_cm":165,"role":"产品经理","budget":"500-1500","HeightCM":166}`))

	assertStatus(t, rec, 400)
}
