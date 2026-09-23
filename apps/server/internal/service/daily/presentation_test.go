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
	if result.Presentation.Stages[0].Kind != "sketch_tour" {
		t.Fatalf("stage kind = %q, want sketch_tour", result.Presentation.Stages[0].Kind)
	}
}

// 命中缓存时不播巡游（重进不该重看等待动画），但仍下发 roam 脚本——
// 只为带 params.variant：客户端据此提前预载换装素材。缓存命中的日子没有
// 等待期，generate 一返回就进收敛，预载晚一步（3s 闸）就只能回落序列帧
// 揭晓，洗牌永远上不了场。
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
	if hit.Presentation == nil || len(hit.Presentation.Stages) != 1 {
		t.Fatalf("命中缓存也要下发 roam 脚本（带 variant 供预载），got %#v", hit.Presentation)
	}
	if hit.Presentation.Stages[0].Phase != "roam" {
		t.Fatalf("roam phase = %s", hit.Presentation.Stages[0].Phase)
	}
	if _, ok := hit.Presentation.Stages[0].Params["variant"]; !ok {
		t.Fatal("roam 脚本必须带 variant，客户端靠它决定预载哪一套")
	}
}
