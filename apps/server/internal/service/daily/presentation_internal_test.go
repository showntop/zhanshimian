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
	quick := settlePresentation(content, 2000)
	long := settlePresentation(content, 30000)
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
	p := settlePresentation(content, 8000)
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

func TestRoamCoversEveryTheme(t *testing.T) {
	p := roamPresentation()
	if len(p.Stages) != 1 || p.Stages[0].Kind != "roam_tour" {
		t.Fatalf("roam = %+v", p.Stages)
	}
	themes, ok := p.Stages[0].Params["themes"].([]map[string]any)
	// general 是「归不进七格」的兜底格，没有自己的视觉语言，巡游不单列它。
	want := len(allCategories) - 1
	if !ok || len(themes) != want {
		t.Fatalf("themes = %v, want %d", p.Stages[0].Params["themes"], want)
	}
	for _, theme := range themes {
		if theme["form"] == "" || theme["label"] == "" {
			t.Fatalf("theme missing form/label: %v", theme)
		}
	}
}
