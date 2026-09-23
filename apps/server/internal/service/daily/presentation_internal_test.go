package daily

import (
	"testing"

	"github.com/zhanshimian/server/internal/domain"
)

func TestAxesOfTargetComesFromVisualSpec(t *testing.T) {
	content := domain.DailyContent{
		Category: "proportion",
		Visual:   domain.ContentVisual{Modality: "diagram", Spec: map[string]any{"ratio": 0.42}},
	}
	axes := axesOf(content)
	if len(axes) == 0 {
		t.Fatal("no axes")
	}
	if got := axes[0].Target; got != 0.42 {
		t.Fatalf("target = %v, want 0.42（定格必须落在建议本身）", got)
	}
}

func TestAxesOfFallsBackToCategoryDefault(t *testing.T) {
	// spec 缺失或类型不对：退回分类默认值，绝不能空着让客户端去猜。
	content := domain.DailyContent{
		Category: "proportion",
		Visual:   domain.ContentVisual{Modality: "diagram", Spec: map[string]any{"ratio": "not-a-number"}},
	}
	if got := axesOf(content)[0].Target; got != 0.38 {
		t.Fatalf("target = %v, want 0.38", got)
	}
	empty := domain.DailyContent{Category: "outfit"}
	if got := axesOf(empty)[0].Target; got != "cropped" {
		t.Fatalf("target = %v, want cropped", got)
	}
}

func TestEveryCategoryHasAxesAndAForm(t *testing.T) {
	for _, category := range append([]string{CategoryGeneral}, allCategories...) {
		content := domain.DailyContent{Category: category}
		if len(axesOf(content)) == 0 {
			t.Fatalf("%s has no axes", category)
		}
		if formOfCategory(category) == "" {
			t.Fatalf("%s has no form", category)
		}
	}
}

func TestTempoGrowsWithElapsed(t *testing.T) {
	fast := tempoFor(1200)
	mid := tempoFor(8000)
	slow := tempoFor(40000)
	steps := func(m map[string]any) int { return m["steps"].(int) }
	if !(steps(fast) < steps(mid) && steps(mid) < steps(slow)) {
		t.Fatalf("steps should grow with elapsed: %d / %d / %d", steps(fast), steps(mid), steps(slow))
	}
}

func TestSettleRevealKindDependsOnElapsed(t *testing.T) {
	content := domain.DailyContent{ID: "c1", Category: "outfit"}
	// 空 assetBase：无素材时退回 CSS 形态收敛，这一支的行为保持不变。
	quick := settlePresentation(content, 2000, "")
	long := settlePresentation(content, 30000, "")
	revealOf := func(p Presentation) string {
		for _, stage := range p.Stages {
			if stage.Phase == "reveal" {
				return stage.Kind
			}
		}
		return ""
	}
	if revealOf(quick) != "sweep" {
		t.Fatalf("quick reveal = %q, want sweep", revealOf(quick))
	}
	if revealOf(long) != "develop" {
		t.Fatalf("long reveal = %q, want develop（等得久，揭晓值得慢一点）", revealOf(long))
	}
}

func TestSettleCarriesAxesDimsTempoAndBrakes(t *testing.T) {
	content := domain.DailyContent{ID: "c1", Category: "color"}
	p := settlePresentation(content, 8000, "")
	if len(p.Stages) != 2 {
		t.Fatalf("stages = %d, want 2 (settle + reveal)", len(p.Stages))
	}
	settle := p.Stages[0]
	if settle.Kind != "converge" || settle.Form != "swatch_bars" {
		t.Fatalf("settle = %+v", settle)
	}
	for _, key := range []string{"axes", "dims", "tempo", "brakes"} {
		if _, ok := settle.Params[key]; !ok {
			t.Fatalf("settle params missing %q", key)
		}
	}
}

func TestSettleFramesWhenAssetBaseConfigured(t *testing.T) {
	content := domain.DailyContent{ID: "c1", Category: "outfit"}
	p := settlePresentation(content, 8000, "https://api.example.com")
	if len(p.Stages) != 1 {
		t.Fatalf("stages = %d, want 1 (frames)", len(p.Stages))
	}
	stage := p.Stages[0]
	if stage.Phase != "settle" || stage.Kind != "frames" {
		t.Fatalf("stage = %+v", stage)
	}
	urls, ok := stage.Params["urls"].([]string)
	if !ok || len(urls) != revealFramesCount {
		t.Fatalf("urls = %v", stage.Params["urls"])
	}
	if urls[0] != "https://api.example.com/assets/daily/reveal/f_01.jpg" {
		t.Fatalf("urls[0] = %q", urls[0])
	}
	if urls[len(urls)-1] != "https://api.example.com/assets/daily/reveal/f_17.jpg" {
		t.Fatalf("last url = %q", urls[len(urls)-1])
	}
	if got := stage.Params["interval_ms"]; got != revealFrameMS {
		t.Fatalf("interval_ms = %v", got)
	}
	if got := stage.Params["hold_ms"]; got != revealHoldMS {
		t.Fatalf("hold_ms = %v", got)
	}
	if stage.DurationMS != revealFramesCount*revealFrameMS+revealHoldMS {
		t.Fatalf("duration = %d", stage.DurationMS)
	}
}

func TestRoamCoversEveryTheme(t *testing.T) {
	p := roamPresentation("u1", "2026-09-23", "")
	if len(p.Stages) != 1 || p.Stages[0].Kind != "sketch_tour" {
		t.Fatalf("roam = %+v", p.Stages)
	}
	themes, ok := p.Stages[0].Params["themes"].([]map[string]any)
	// 巡游只收录「有画法的分类」：七个经典分类。hair/makeup/accessory 与
	// general（兜底格）暂无自己的视觉语言，硬列会出现标签与画面不符。
	// 这里用显式清单而不是 allCategories 派生——分类集合会生长，巡游跟着
	// 混进没画法的格就是这次测试红的原因。
	if !ok || len(themes) != 7 {
		t.Fatalf("themes = %v, want 7", p.Stages[0].Params["themes"])
	}
	seen := map[string]bool{}
	for _, theme := range themes {
		if theme["form"] == "" || theme["label"] == "" || theme["theme"] == "" {
			t.Fatalf("theme missing theme/form/label: %v", theme)
		}
		seen[theme["theme"].(string)] = true
	}
	for _, key := range []string{"color", "fit", "proportion", "fabric", "occasion", "howto", "outfit"} {
		if !seen[key] {
			t.Fatalf("roam missing theme %q", key)
		}
	}
}
