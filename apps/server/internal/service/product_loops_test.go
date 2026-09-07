package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/example/jianwo/server/internal/domain"
	"github.com/example/jianwo/server/internal/provider"
	"github.com/example/jianwo/server/internal/repository"
)

func TestAdvisorReplyUsesWardrobeAndTodayContext(t *testing.T) {
	today := domain.TodayPlan{ID: "today-1", Title: "轻薄利落", Context: domain.TodayContext{City: "杭州", Condition: "小雨", Temperature: 22, Schedule: "通勤"}}
	wardrobe := []domain.WardrobeItem{{ID: "item-1", Name: "针织衫", Color: "米白"}, {ID: "item-2", Name: "长裤", Color: "藏蓝"}}
	reply, actions := advisorReply("只用现有衣橱", advisorGrounding{Today: &today, Wardrobe: wardrobe})
	if !strings.Contains(reply, "米白针织衫") || !strings.Contains(reply, "藏蓝长裤") {
		t.Fatalf("reply is not grounded in wardrobe: %q", reply)
	}
	if len(actions) != 1 || !strings.Contains(string(actions[0].Payload), "today-1") || !strings.Contains(string(actions[0].Payload), "item-1") {
		t.Fatalf("action payload lost grounding: %#v", actions)
	}
}

func TestAdvisorReplyFallsBackWhenWardrobeIsEmpty(t *testing.T) {
	reply, _ := advisorReply("只用现有衣橱", advisorGrounding{})
	if !strings.Contains(reply, "先添加 2–3 件") {
		t.Fatalf("empty wardrobe should produce an actionable fallback: %q", reply)
	}
}

func TestValidEventName(t *testing.T) {
	for _, name := range []string{"page_view", "today_plan_generate", "advisor_action_apply"} {
		if !validEventName(name) {
			t.Fatalf("valid event rejected: %q", name)
		}
	}
	for _, name := range []string{"", "A", "Page View", "page-view"} {
		if validEventName(name) {
			t.Fatalf("invalid event accepted: %q", name)
		}
	}
}

type todayPlanRepoStub struct {
	repository.Repository
	existing    domain.TodayPlan
	existingErr error
	saved       domain.TodayPlan
}

func (r *todayPlanRepoStub) GetTodayPlan(context.Context, string) (domain.TodayPlan, error) {
	return r.existing, r.existingErr
}

func (r *todayPlanRepoStub) SaveTodayPlan(_ context.Context, _ string, plan domain.TodayPlan) (domain.TodayPlan, error) {
	plan.ID = "today-1"
	r.saved = plan
	return plan, nil
}

type failingWeatherStub struct{}

func (failingWeatherStub) Current(context.Context, string) (provider.Weather, error) {
	return provider.Weather{}, provider.ErrWeatherUnavailable
}

type failingTodayPlanner struct{ err error }

func (p failingTodayPlanner) Generate(context.Context, provider.TodayPlanGrounding) (provider.TodayPlanOutput, error) {
	return provider.TodayPlanOutput{}, p.err
}

func TestGenerateTodayPlanWithDemoPlannerReturnsTemplatePlan(t *testing.T) {
	repo := &todayPlanRepoStub{existingErr: repository.ErrNotFound}
	service := &Service{repo: repo, weather: provider.NewDemoWeatherProvider(), todayPlanner: provider.NewDemoTodayPlanner()}
	plan, err := service.GenerateTodayPlan(context.Background(), "user-1", domain.TodayPlanInput{City: "杭州"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Title == "" || plan.Summary == "" || plan.ImageURL != "/assets/plans/sharp.jpg" || plan.RegenerateCount != 0 {
		t.Fatalf("unexpected demo plan: %#v", plan)
	}
	if plan.Context.City != "杭州" {
		t.Fatalf("context lost the requested city: %#v", plan.Context)
	}
	if len(plan.Steps) != 3 || plan.Steps[0].Category != "hair" || plan.Steps[1].Category != "makeup" || plan.Steps[2].Category != "outfit" {
		t.Fatalf("demo plan steps are incomplete: %#v", plan.Steps)
	}
}

func TestGenerateTodayPlanRotatesVariantOnRefresh(t *testing.T) {
	repo := &todayPlanRepoStub{existing: domain.TodayPlan{Title: "旧方案", RegenerateCount: 0}}
	service := &Service{repo: repo, weather: provider.NewDemoWeatherProvider(), todayPlanner: provider.NewDemoTodayPlanner()}
	plan, err := service.GenerateTodayPlan(context.Background(), "user-1", domain.TodayPlanInput{City: "杭州", Refresh: true})
	if err != nil {
		t.Fatal(err)
	}
	if plan.RegenerateCount != 1 || plan.ImageURL != "/assets/plans/warm.jpg" {
		t.Fatalf("refresh did not rotate the variant: %#v", plan)
	}
}

// 数据真实性：真实 provider 失败时必须返回 error，不能回退成模板假数据。
func TestGenerateTodayPlanReturnsErrorWhenPlannerFails(t *testing.T) {
	repo := &todayPlanRepoStub{existingErr: repository.ErrNotFound}
	service := &Service{repo: repo, weather: provider.NewDemoWeatherProvider(), todayPlanner: failingTodayPlanner{err: errors.New("model unavailable")}}
	if _, err := service.GenerateTodayPlan(context.Background(), "user-1", domain.TodayPlanInput{City: "杭州"}); err == nil {
		t.Fatal("planner failure must surface as an error, not a template plan")
	}
	if repo.saved.Title != "" {
		t.Fatalf("failed generation must not be persisted: %#v", repo.saved)
	}
}

// 天气失败时降级为城市+日期类型+日程的上下文，不再返回 error。
func TestBuildTodayContextDegradesWhenWeatherFails(t *testing.T) {
	service := &Service{weather: failingWeatherStub{}}
	contexts, err := service.buildTodayContext(context.Background(), "杭州", "")
	if err != nil {
		t.Fatalf("weather failure must not break today context: %v", err)
	}
	if contexts.City != "杭州" || contexts.Condition != "" {
		t.Fatalf("degraded context should keep only the requested city: %#v", contexts)
	}
	if contexts.DayType == "" || contexts.Schedule == "" || contexts.Date == "" {
		t.Fatalf("degraded context lost date type or schedule: %#v", contexts)
	}
}

func TestAbsoluteURLKeepsBundledAssetsAndExpandsUploads(t *testing.T) {
	service := &Service{publicBaseURL: "http://localhost:58000"}
	if got := service.absoluteURL("/assets/plans/sharp.webp"); got != "/assets/plans/sharp.webp" {
		t.Fatalf("bundled asset became remote URL: %q", got)
	}
	if got := service.absoluteURL("/uploads/example.webp"); got != "http://localhost:58000/uploads/example.webp" {
		t.Fatalf("upload URL was not expanded: %q", got)
	}
}

func TestHydrateShareSnapshotResolvesStoredImageURL(t *testing.T) {
	service := &Service{publicBaseURL: "https://api.example.test"}
	snapshot := service.hydrateShareSnapshot([]byte(`{"title":"方案","image_url":"/uploads/user/plan.jpg"}`))
	if !strings.Contains(string(snapshot), `"image_url":"https://api.example.test/uploads/user/plan.jpg"`) {
		t.Fatalf("relative snapshot URL was not expanded: %s", snapshot)
	}
	// Bundled assets must stay relative for the mini-program renderer.
	snapshot = service.hydrateShareSnapshot([]byte(`{"title":"今日","image_url":"/assets/plans/sharp.jpg"}`))
	if !strings.Contains(string(snapshot), `"image_url":"/assets/plans/sharp.jpg"`) {
		t.Fatalf("bundled snapshot URL must stay relative: %s", snapshot)
	}
}

func TestHydrateShareSnapshotRefreshesStaleSignedURL(t *testing.T) {
	stale := "https://bucket.cos.ap-guangzhou.myqcloud.com/user/plan.jpg?sign=expired"
	service := &Service{
		publicBaseURL: "https://api.example.test",
		storage:       refreshStorageStub{refreshed: map[string]string{stale: "https://bucket.cos.ap-guangzhou.myqcloud.com/user/plan.jpg?sign=fresh"}},
	}
	snapshot := service.hydrateShareSnapshot([]byte(`{"title":"方案","image_url":"` + stale + `"}`))
	if !strings.Contains(string(snapshot), "sign=fresh") {
		t.Fatalf("stale signed snapshot URL was not refreshed: %s", snapshot)
	}
}
