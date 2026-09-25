package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/service/daily"
)

type fakeDailyService struct {
	prepare  daily.PrepareResult
	generate daily.GenerateResult
}

func (f fakeDailyService) Prepare(context.Context, string, string) (daily.PrepareResult, error) {
	return f.prepare, nil
}

func (f fakeDailyService) Generate(context.Context, string, string) (daily.GenerateResult, error) {
	return f.generate, nil
}

func (f fakeDailyService) CreateCollection(context.Context, string, string, string) (domain.DailyCollection, error) {
	return domain.DailyCollection{}, nil
}

func (f fakeDailyService) ListCollections(context.Context, string, string, int) ([]domain.DailyCollection, error) {
	return nil, nil
}

func (f fakeDailyService) HistoryContents(context.Context, string, int) ([]domain.DailyContent, error) {
	return nil, nil
}

func (f fakeDailyService) UpdateCollection(context.Context, string, string, string, string) (domain.DailyCollection, error) {
	return domain.DailyCollection{}, nil
}

func (f fakeDailyService) DeleteCollection(context.Context, string, string) error { return nil }

func (f fakeDailyService) CollectionStats(context.Context, string) (daily.CollectionStats, error) {
	return daily.CollectionStats{}, nil
}

func dailyRequest(t *testing.T, api *API, target string, handler http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+target, handler)
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(`{}`))
	req = req.WithContext(context.WithValue(req.Context(), userKey, domain.User{ID: "user-1"}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// 等待动画是「服务端下发脚本、客户端只是播放器」：prepare 的响应必须带
// presentation，否则客户端只能播内置兜底——这正是首屏那个圆圈出现的原因。
func TestPrepareDailyResponseCarriesPresentation(t *testing.T) {
	api := &API{
		daily: fakeDailyService{prepare: daily.PrepareResult{
			GenDate:  "2026-09-20",
			CacheHit: false,
			Presentation: &daily.Presentation{
				Version: 1,
				Stages:  []daily.Stage{{Phase: "roam", Kind: "roam_tour"}},
			},
		}},
		logger: discardLogger(),
	}
	rec := dailyRequest(t, api, "/v1/daily/prepare", api.prepareDaily)
	assertStatus(t, rec, http.StatusOK)
	assertJSONPath(t, rec, "data.cache_hit", false)
	assertJSONPath(t, rec, "data.presentation.stages.0.kind", "roam_tour")
	assertJSONArrayLength(t, rec, "data.presentation.stages", 1)
}

// 收敛脚本同样要随 generate 下发：少了它，内容到位后就没有「定格在答案」那一幕。
func TestGenerateDailyResponseCarriesPresentation(t *testing.T) {
	api := &API{
		daily: fakeDailyService{generate: daily.GenerateResult{
			Source:  "generated",
			Content: domain.DailyContent{ID: "c1", Category: "outfit", Topic: "上短下长"},
			Presentation: &daily.Presentation{
				Version: 1,
				Stages: []daily.Stage{
					{Phase: "settle", Kind: "converge", Form: "outfit_blocks"},
					{Phase: "reveal", Kind: "sweep"},
				},
			},
		}},
		logger: discardLogger(),
	}
	rec := dailyRequest(t, api, "/v1/daily/generate", api.generateDaily)
	assertStatus(t, rec, http.StatusOK)
	assertJSONPath(t, rec, "data.source", "generated")
	assertJSONPath(t, rec, "data.presentation.stages.0.kind", "converge")
	assertJSONPath(t, rec, "data.presentation.stages.0.form", "outfit_blocks")
	assertJSONPath(t, rec, "data.presentation.stages.1.kind", "sweep")
}
