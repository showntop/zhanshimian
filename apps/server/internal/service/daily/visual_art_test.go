package daily

import (
	"strings"
	"testing"

	"github.com/zhanshimian/server/internal/domain"
)

func artTestContent(category string) domain.DailyContent {
	return domain.DailyContent{
		UserID:   "u1",
		GenDate:  "2026-09-25",
		Category: category,
		Visual: domain.ContentVisual{
			Modality: "compare",
			Spec: map[string]any{
				"left":  map[string]any{"label": "左", "tone": "#8A8F83"},
				"right": map[string]any{"label": "右", "tone": "#8E9A83", "layered": true},
			},
			Alt: "测试对比",
		},
	}
}

// 有映射的分类：modality 不变，spec 只加 image + source_kind
func TestDecorateVisualAttachesArt(t *testing.T) {
	for _, category := range []string{"outfit", "fit", "proportion", "occasion", "howto", "color", "hair"} {
		content := decorateVisual(artTestContent(category), "https://api.example.com")
		if content.Visual.Modality != "compare" {
			t.Fatalf("%s: modality changed to %q", category, content.Visual.Modality)
		}
		image, _ := content.Visual.Spec["image"].(string)
		if !strings.HasPrefix(image, "https://api.example.com/assets/daily/dress/color/") {
			t.Fatalf("%s: image = %q", category, image)
		}
		if content.Visual.Spec["source_kind"] != "bundled_reference" {
			t.Fatalf("%s: source_kind = %v", category, content.Visual.Spec["source_kind"])
		}
		// 原有的程序化 spec 必须原样保留（色票/对比是信息图，插画不替代它）
		if _, ok := content.Visual.Spec["left"]; !ok {
			t.Fatalf("%s: original spec keys lost", category)
		}
	}
}

// 稳定抽样：同一 uid+date 同一张；记录里已有 image 时不覆盖（幂等）
func TestDecorateVisualStableAndIdempotent(t *testing.T) {
	first := decorateVisual(artTestContent("color"), "https://api.example.com")
	second := decorateVisual(artTestContent("color"), "https://api.example.com")
	if first.Visual.Spec["image"] != second.Visual.Spec["image"] {
		t.Fatalf("unstable pick: %v vs %v", first.Visual.Spec["image"], second.Visual.Spec["image"])
	}

	existing := artTestContent("outfit")
	existing.Visual.Spec["image"] = "https://api.example.com/assets/daily/dress/color/custom.png"
	decorated := decorateVisual(existing, "https://api.example.com")
	if decorated.Visual.Spec["image"] != "https://api.example.com/assets/daily/dress/color/custom.png" {
		t.Fatalf("existing image overwritten: %v", decorated.Visual.Spec["image"])
	}
}

// 无映射的分类与未配置素材基地址：原样返回，不造 URL
func TestDecorateVisualNoOp(t *testing.T) {
	for _, category := range []string{"makeup", "accessory", "fabric", "item", "general"} {
		content := decorateVisual(artTestContent(category), "https://api.example.com")
		if _, exists := content.Visual.Spec["image"]; exists {
			t.Fatalf("%s: unexpected image", category)
		}
	}
	content := decorateVisual(artTestContent("outfit"), "")
	if _, exists := content.Visual.Spec["image"]; exists {
		t.Fatal("empty assetBase must not produce image")
	}
}
