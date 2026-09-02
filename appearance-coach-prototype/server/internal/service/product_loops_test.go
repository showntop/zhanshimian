package service

import (
	"strings"
	"testing"

	"github.com/example/jianwo/server/internal/domain"
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
