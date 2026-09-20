package daily_test

import (
	"context"
	"testing"

	"github.com/zhanshimian/server/internal/domain"
)

// 首屏由客户端内置主题兜底，prepare 回来后必须换成服务端脚本——
// 少了这一步，等待期永远只能播一份写死在客户端的列表，
// 而「服务端下发脚本、客户端只是播放器」的约定就断了。
func TestPrepareShipsRoamScriptOnMiss(t *testing.T) {
	service := newService(t, fakeReader{}, fakeKnowledge{}, newFakeContent(nil), &fakeRuns{}, &fakeCollections{}, &fakePlanner{})
	result, err := service.Prepare(context.Background(), "user-1", "")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if result.CacheHit {
		t.Fatal("内容池为空时不应命中缓存")
	}
	if result.Presentation == nil || len(result.Presentation.Stages) == 0 {
		t.Fatal("未命中时必须下发巡游脚本")
	}
	if result.Presentation.Stages[0].Kind != "roam_tour" {
		t.Fatalf("stage kind = %q, want roam_tour", result.Presentation.Stages[0].Kind)
	}
}

// 命中缓存时不播巡游（重进不该重看等待动画），因此不下发脚本。
func TestPrepareSkipsScriptOnCacheHit(t *testing.T) {
	content := newFakeContent(nil)
	service := newService(t, fakeReader{}, fakeKnowledge{}, content, &fakeRuns{}, &fakeCollections{}, &fakePlanner{})
	first, err := service.Prepare(context.Background(), "user-1", "")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	genDate := first.GenDate
	content.today["user-1|"+genDate] = domain.DailyContent{
		ID: "c1", UserID: "user-1", GenDate: genDate, Category: "color", Source: "generated",
	}

	hit, err := service.Prepare(context.Background(), "user-1", "")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !hit.CacheHit {
		t.Fatal("当天已有内容时应命中")
	}
	if hit.Presentation != nil {
		t.Fatal("命中缓存不应下发巡游脚本")
	}
}
