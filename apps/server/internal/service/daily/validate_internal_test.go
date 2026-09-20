package daily

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// validateOutput / buildVisual 是包内纯函数：用内部测试直接驱动。

func TestBuildVisualRejectsNonHexTone(t *testing.T) {
	if _, err := buildVisual(VisualDraft{Modality: "swatch", Items: []VisualItem{{Label: "不是色值", Tone: "red"}}}); err == nil {
		t.Fatal("expected error for non-hex tone")
	}
}

func TestBuildVisualSwatch(t *testing.T) {
	visual, err := buildVisual(VisualDraft{
		Modality: "swatch", Alt: "四种白色并排",
		Items: []VisualItem{
			{Label: "本白", Tone: "#FBFBF8", State: "pick"},
			{Label: "米白", Tone: "#EFEADC", State: "drop"},
		},
	})
	if err != nil {
		t.Fatalf("build swatch: %v", err)
	}
	if visual.Modality != "swatch" || len(visual.Spec["items"].([]map[string]any)) != 2 {
		t.Fatalf("visual = %#v", visual)
	}
}

func TestBuildVisualCompareAndDiagram(t *testing.T) {
	compare, err := buildVisual(VisualDraft{
		Modality: "compare", Alt: "对比", Marker: "肩点位置",
		Left:  &VisualSide{Label: "落肩", Tone: "#8A8F83"},
		Right: &VisualSide{Label: "肩线", Tone: "#8E9A83", Layered: true},
	})
	if err != nil {
		t.Fatalf("build compare: %v", err)
	}
	if compare.Spec["marker"] != "肩点位置" || compare.Spec["left"] == nil {
		t.Fatalf("compare = %#v", compare)
	}

	diagram, err := buildVisual(VisualDraft{
		Modality: "diagram", Kind: "body", Alt: "位置示意",
		Items: []VisualItem{{Label: "贴近脸的内搭", State: "avoid"}, {Label: "外套", State: "pick"}},
	})
	if err != nil {
		t.Fatalf("build diagram: %v", err)
	}
	if diagram.Modality != "diagram" || diagram.Spec["kind"] != "body" {
		t.Fatalf("diagram = %#v", diagram)
	}

	if _, err := buildVisual(VisualDraft{Modality: "diagram", Kind: "circle", Items: []VisualItem{{Label: "a"}, {Label: "b"}}}); !errors.Is(err, errStructure) {
		t.Fatalf("expected errStructure, got %v", err)
	}
}

func TestValidateOutputThreeGates(t *testing.T) {
	output := goodOutputForTest(1)
	if problems := validateOutput(output, 1); len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}

	// 黑名单：共享词源（颜值）与每日补充词源（显胖）都要拦。
	blacklisted := output
	blacklisted.Topic = "你的颜值亮点"
	if problems := validateOutput(blacklisted, 1); !hasError(problems, errBlacklist) {
		t.Fatalf("shared blacklist missed: %v", problems)
	}
	dailyBannedHit := output
	dailyBannedHit.Topic = "显胖预警"
	if problems := validateOutput(dailyBannedHit, 1); !hasError(problems, errBlacklist) {
		t.Fatalf("daily blacklist missed: %v", problems)
	}

	// 事实追溯：引用必须落在输入集内且非空。
	noRefs := output
	noRefs.Refs = nil
	if problems := validateOutput(noRefs, 1); !hasError(problems, errTrace) {
		t.Fatalf("fact trace missed: %v", problems)
	}
	outOfRange := output
	outOfRange.Refs = []int{5}
	if problems := validateOutput(outOfRange, 1); !hasError(problems, errTrace) {
		t.Fatalf("fact trace range missed: %v", problems)
	}

	// 结构：长度超限与空字段。
	toolong := output
	toolong.Lead = string(make([]rune, 80))
	if problems := validateOutput(toolong, 1); !hasError(problems, errStructure) {
		t.Fatalf("structure length missed: %v", problems)
	}
}

func goodOutputForTest(refs ...int) ContentOutput {
	return ContentOutput{
		Topic: "冬天的白，不止一种",
		Lead:  "本白、米白、奶油白，上身差很多。",
		Fit:   "你是冷调肤色，本白贴着皮肤气色往上走。",
		Why:   "白色也有色温，先看冷暖再看明度。",
		Visual: VisualDraft{
			Modality: "swatch", Alt: "四种白色并排",
			Items: []VisualItem{
				{Label: "本白", Tone: "#FBFBF8", State: "pick"},
				{Label: "米白", Tone: "#EFEADC", State: "drop"},
			},
		},
		Refs: refs,
	}
}

func TestRetryHintTranslatesProblems(t *testing.T) {
	hint := retryHint([]error{
		fmt.Errorf("%w: 命中共享禁则词表", errBlacklist),
	})
	if hint == "" || !strings.Contains(hint, "修正") {
		t.Fatalf("hint = %q", hint)
	}
}
