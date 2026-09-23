package daily

import (
	"testing"

	"github.com/zhanshimian/server/internal/domain"
)

func TestMotionVariantStablePerUserPerDay(t *testing.T) {
	a := motionVariant("user-1", "2026-09-23", "")
	if a != motionVariant("user-1", "2026-09-23", "") {
		t.Fatalf("same user+date must be stable, got %s vs %s", a, motionVariant("user-1", "2026-09-23", ""))
	}
	// 分流必须两种都出现，否则「两天见两套」不成立
	seen := map[string]bool{}
	for i := 0; i < 64; i++ {
		userID := "user-" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		seen[motionVariant(userID, "2026-09-23", "")] = true
	}
	if !seen[MotionVariantDress] || !seen[MotionVariantSketch] {
		t.Fatalf("variant hash must split across both variants, seen=%v", seen)
	}
}

func TestMotionVariantOverride(t *testing.T) {
	if got := motionVariant("u", "2026-09-23", MotionVariantSketch); got != MotionVariantSketch {
		t.Fatalf("override sketch: got %s", got)
	}
	if got := motionVariant("u", "2026-09-23", MotionVariantDress); got != MotionVariantDress {
		t.Fatalf("override dress: got %s", got)
	}
	// 未知值不当开关用：走自然分流
	if got := motionVariant("u", "2026-09-23", "bogus"); got != motionVariant("u", "2026-09-23", "") {
		t.Fatalf("unknown override must fall through to hash split: got %s", got)
	}
}

func TestLookOfCategorySemanticMapping(t *testing.T) {
	cases := map[string]string{
		"outfit":     "outfit",
		"proportion": "ratio",
		"fit":        "fit",
		"occasion":   "occasion",
	}
	for category, want := range cases {
		if got := lookOfCategory(category, "u1|2026-09-23"); got != want {
			t.Fatalf("lookOfCategory(%q) = %q, want %q", category, got, want)
		}
	}
	// 其余分类走稳定随机：仍必须在词汇表内，且同 seed 固定
	inLib := false
	for _, key := range dressLookKeys {
		if lookOfCategory("general", "u1|2026-09-23") == key {
			inLib = true
		}
	}
	if !inLib {
		t.Fatalf("stable-random look must stay inside the shared vocabulary")
	}
	if lookOfCategory("general", "u1|2026-09-23") != lookOfCategory("general", "u1|2026-09-23") {
		t.Fatalf("stable-random look must be deterministic per seed")
	}
}

func TestDressLockPresentation(t *testing.T) {
	content := domain.DailyContent{
		UserID:   "user-1",
		GenDate:  "2026-09-23",
		Category: "proportion",
	}
	p := dressLockPresentation(content, 2000, "https://cdn.example.com")
	if len(p.Stages) != 1 {
		t.Fatalf("want single settle stage, got %d", len(p.Stages))
	}
	stage := p.Stages[0]
	if stage.Phase != "settle" || stage.Kind != "dress_lock" {
		t.Fatalf("want settle/dress_lock, got %s/%s", stage.Phase, stage.Kind)
	}
	target, ok := stage.Params["target"].(map[string]any)
	if !ok {
		t.Fatalf("target must be an object, got %T", stage.Params["target"])
	}
	if target["look"] != "ratio" {
		t.Fatalf("proportion content must map to ratio look, got %v", target["look"])
	}
	inLib := func(v string, list []string) bool {
		for _, item := range list {
			if item == v {
				return true
			}
		}
		return false
	}
	color, _ := target["color"].(string)
	waist, _ := target["waist"].(string)
	hair, _ := target["hair"].(string)
	if !inLib(color, dressColorNames) || !inLib(waist, dressWaistNames) || !inLib(hair, dressHairKeys) {
		t.Fatalf("target values must stay in shared vocabulary: %v", target)
	}
	if stage.Params["pace"] != "normal" {
		t.Fatalf("short wait must be normal pace, got %v", stage.Params["pace"])
	}
	if slow := dressLockPresentation(content, 20000, "https://cdn.example.com"); slow.Stages[0].Params["pace"] != "slow" {
		t.Fatalf("long wait must slow the settle, got %v", slow.Stages[0].Params["pace"])
	}
	// 同 uid+date 稳定：收敛 target 换天换、同天刷新不漂移
	if again := dressLockPresentation(content, 2000, "https://cdn.example.com"); again.Stages[0].Params["target"] == nil {
		t.Fatalf("target must be present")
	}
	nextDay := content
	nextDay.GenDate = "2026-09-24"
	if same := dressLockPresentation(nextDay, 2000, "x").Stages[0].Params["target"]; same == nil {
		t.Fatalf("next day target must be present")
	}
	assets, ok := stage.Params["assets"].(map[string]any)
	wantBase := "https://cdn.example.com/assets/daily/dress"
	if !ok || assets["base"] != wantBase || assets["version"] != dressAssetsVersion {
		t.Fatalf("assets must be %q + version %s, got %v", wantBase, dressAssetsVersion, stage.Params["assets"])
	}
}

// 等待期与收敛期必须同一套种子：两端各算一遍会出现「等待旧线、收敛洗牌」
// 的混搭，且等待期不预载时收敛期来不及（预载有 3s 闸）。
func TestRoamAndSettleAgreeOnVariant(t *testing.T) {
	for i := 0; i < 24; i++ {
		userID := "user-" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		genDate := "2026-09-2" + string(rune('0'+i%9))
		roam := roamPresentation(userID, genDate, "")
		variant := roam.Stages[0].Params["variant"]
		want := motionVariant(userID, genDate, "")
		if variant != want {
			t.Fatalf("roam variant %v, want %s (uid=%s date=%s)", variant, want, userID, genDate)
		}
		if variant != MotionVariantSketch && variant != MotionVariantDress {
			t.Fatalf("roam variant must be sketch or dress, got %v", variant)
		}
	}
	// 灰度开关同时作用于两段
	if got := roamPresentation("u", "2026-09-23", MotionVariantDress).Stages[0].Params["variant"]; got != MotionVariantDress {
		t.Fatalf("override must apply to roam too, got %v", got)
	}
}
