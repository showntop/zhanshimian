package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/service/advisor"

	"github.com/zhanshimian/server/internal/service/wardrobe"
)

type fakeAdvisorService struct {
	message domain.AdvisorMessage
	err     error
	sendN   int
}

func (f *fakeAdvisorService) Send(context.Context, string, advisor.SendInput) (domain.AdvisorMessage, error) {
	f.sendN++
	return f.message, f.err
}

func (f *fakeAdvisorService) List(context.Context, string) ([]domain.AdvisorMessage, error) {
	return []domain.AdvisorMessage{f.message}, f.err
}

func (f *fakeAdvisorService) ApplyAction(_ context.Context, _ string, id string) (domain.AdvisorAction, error) {
	for _, action := range f.message.Actions {
		if action.ID == id {
			action.Applied = true
			return action, nil
		}
	}
	return domain.AdvisorAction{}, errors.New("not found")
}

func newAdvisorMux(svc *fakeAdvisorService) *http.ServeMux {
	api := &API{advisor: svc, logger: discardLogger()}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/advisor/messages", api.sendAdvisorMessage)
	return mux
}

func postAdvisorMessage(t *testing.T, mux *http.ServeMux, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/advisor/messages", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), userKey, domain.User{ID: "user-1"}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// 输入闸（httpapi 层 400）：空内容与超过 500 字直接拒绝，不进入服务。
func TestSendAdvisorMessageRejectsOutOfRangeContent(t *testing.T) {
	svc := &fakeAdvisorService{}
	mux := newAdvisorMux(svc)
	for _, body := range []string{`{"content":""}`, `{"content":"   "}`, `{"content":"` + strings.Repeat("字", 501) + `"`} {
		rec := postAdvisorMessage(t, mux, body)
		assertStatus(t, rec, http.StatusBadRequest)
		assertJSONPath(t, rec, "error.code", "validation_error")
	}
	if svc.sendN != 0 {
		t.Fatalf("rejected input must not reach the service: sends=%d", svc.sendN)
	}
}

// 动作卡随 201 下发：客户端动作按钮 UI 依赖 actions 非空。
func TestSendAdvisorMessageReturnsActions(t *testing.T) {
	svc := &fakeAdvisorService{message: domain.AdvisorMessage{
		ID: "msg-1", ConversationID: "conv-1", Role: "assistant", Content: "可以，先保留下装的垂感。",
		Actions: []domain.AdvisorAction{{
			ID: "action-1", Kind: "adjust_plan_step", Label: "加入今日调整",
			Payload: []byte(`{"target":"today_plan"}`),
		}},
		CreatedAt: time.Now(),
	}}
	rec := postAdvisorMessage(t, newAdvisorMux(svc), `{"content":"明天面试怎么穿？"}`)
	assertStatus(t, rec, http.StatusCreated)
	assertJSONPath(t, rec, "data.actions.0.kind", "adjust_plan_step")
	assertJSONPath(t, rec, "data.actions.0.label", "加入今日调整")
	assertJSONPath(t, rec, "data.actions.0.applied", false)
}

// 顾问限额超限 → 429 rate_limited（与下单限流同一公开形状）。
func TestSendAdvisorMessageRateLimitedIs429(t *testing.T) {
	svc := &fakeAdvisorService{err: fmt.Errorf("%w: 今日咨询次数已用完，明天再来", advisor.ErrRateLimited)}
	rec := postAdvisorMessage(t, newAdvisorMux(svc), `{"content":"今天怎么穿？"}`)
	assertStatus(t, rec, http.StatusTooManyRequests)
	assertJSONPath(t, rec, "error.code", "rate_limited")
	assertJSONPath(t, rec, "error.message", "今日咨询次数已用完，明天再来")
}

// 服务层校验错误 → 400（防御性：httpapi 未拦住的非法输入也绝不 500）。
func TestSendAdvisorMessageValidationIs400(t *testing.T) {
	svc := &fakeAdvisorService{err: fmt.Errorf("%w: 请输入 1–500 字的问题", advisor.ErrValidation)}
	rec := postAdvisorMessage(t, newAdvisorMux(svc), `{"content":"今天怎么穿？"}`)
	assertStatus(t, rec, http.StatusBadRequest)
	assertJSONPath(t, rec, "error.code", "validation_error")
}

type fakeWardrobeService struct {
	err error
}

func (f fakeWardrobeService) ListItems(context.Context, string) ([]wardrobe.Item, error) {
	return nil, f.err
}

func (f fakeWardrobeService) CreateItem(context.Context, string, wardrobe.CreateItemInput) (wardrobe.Item, error) {
	return wardrobe.Item{}, f.err
}

func (f fakeWardrobeService) DeleteItem(context.Context, string, string) error { return f.err }

func (f fakeWardrobeService) CreateOutfit(context.Context, string, wardrobe.CreateOutfitInput) (wardrobe.Outfit, error) {
	return wardrobe.Outfit{}, f.err
}

func (f fakeWardrobeService) WearOutfit(context.Context, string, string) (wardrobe.Outfit, error) {
	return wardrobe.Outfit{}, f.err
}

// wardrobe 校验错误 → 400 validation_error（服务端校验恢复的公开形状）。
func TestCreateWardrobeItemValidationIs400(t *testing.T) {
	api := &API{
		wardrobe: fakeWardrobeService{err: fmt.Errorf("%w: 请填写单品名称、类别与颜色", wardrobe.ErrValidation)},
		logger:   discardLogger(),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/wardrobe/items", api.createWardrobeItem)
	req := httptest.NewRequest(http.MethodPost, "/v1/wardrobe/items", strings.NewReader(`{"name":"","category":"dress","color":"白"}`))
	req = req.WithContext(context.WithValue(req.Context(), userKey, domain.User{ID: "user-1"}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	assertStatus(t, rec, http.StatusBadRequest)
	assertJSONPath(t, rec, "error.code", "validation_error")
	assertJSONPath(t, rec, "error.message", "请填写单品名称、类别与颜色")
}
