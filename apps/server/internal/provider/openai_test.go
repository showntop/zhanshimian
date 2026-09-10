package provider

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"
)

type stubMediaLoader struct {
	images []AnalysisImage
	err    error
}

func (l stubMediaLoader) Load(_ context.Context, _ []string) ([]AnalysisImage, error) {
	return l.images, l.err
}

func TestValidateAnalysisPayloadRejectsUnsafeCopy(t *testing.T) {
	payload := validAnalysisPayload()
	payload.PriorityCopy = "根据颜值给你一个评分"
	if err := validateAnalysisPayload(payload); err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("expected unsafe output rejection, got %v", err)
	}
}

func TestValidateAnalysisPayloadRejectsUnknownPhoto(t *testing.T) {
	payload := validAnalysisPayload()
	payload.Findings[2].Photo = "portrait"
	err := validateAnalysisPayload(payload)
	if err == nil || !strings.Contains(err.Error(), "invalid finding") {
		t.Fatalf("expected invalid finding rejection, got %v", err)
	}
}

// 复现线上 kimi-k3 报错：impression_tags 输出成嵌套数组
// （json: cannot unmarshal array into ... impression_tags of type string）。
// 归一化应拍平嵌套并让校验通过，而不是整次调用作废。
func TestDecodeAnalysisPayloadNormalizesNestedTagArrays(t *testing.T) {
	payload := validAnalysisPayload()
	payload.ImpressionTags = []string{"自然亲和", "稳重克制", "线条柔和"}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var tree map[string]any
	if err := json.Unmarshal(encoded, &tree); err != nil {
		t.Fatal(err)
	}
	tree["impression_tags"] = []any{[]any{"自然亲和"}, []any{"稳重克制", "线条柔和"}}
	nested, err := json.Marshal(tree)
	if err != nil {
		t.Fatal(err)
	}

	decoded, normalized, err := decodeAnalysisPayload(nested)
	if err != nil {
		t.Fatalf("nested tags should decode after normalization: %v", err)
	}
	if !normalized {
		t.Fatal("normalization should be reported")
	}
	if len(decoded.ImpressionTags) != 3 || decoded.ImpressionTags[0] != "自然亲和" || decoded.ImpressionTags[2] != "线条柔和" {
		t.Fatalf("unexpected normalized tags: %#v", decoded.ImpressionTags)
	}
	if err := validateAnalysisPayload(decoded); err != nil {
		t.Fatalf("normalized payload should pass validation: %v", err)
	}
}

func TestSeparateAnchorsSpreadsCrowdedSamePhotoAnchors(t *testing.T) {
	findings := []analysisFinding{
		{Label: "肩颈线条可更利落", Category: "outfit", Photo: "body", AnchorX: .5, AnchorY: .3},
		{Label: "上身配色偏沉", Category: "color", Photo: "body", AnchorX: .52, AnchorY: .34},
		{Label: "发型重心偏低", Category: "hair", Photo: "face", AnchorX: .5, AnchorY: .3},
		{Label: "眉眼对比稍弱", Category: "makeup", Photo: "face", AnchorX: .95, AnchorY: .99},
	}
	separateAnchors(findings)
	for i, finding := range findings {
		if finding.AnchorX < 0.03 || finding.AnchorX > 0.97 || finding.AnchorY < 0.03 || finding.AnchorY > 0.97 {
			t.Fatalf("finding %d escaped bounds: (%.2f,%.2f)", i, finding.AnchorX, finding.AnchorY)
		}
		for j := 0; j < i; j++ {
			if findings[j].Photo != finding.Photo {
				continue
			}
			if math.Hypot(finding.AnchorX-findings[j].AnchorX, finding.AnchorY-findings[j].AnchorY) < minAnchorGap {
				t.Fatalf("findings %d and %d on %s stayed closer than %.2f: (%.2f,%.2f) vs (%.2f,%.2f)",
					i, j, finding.Photo, minAnchorGap, finding.AnchorX, finding.AnchorY, findings[j].AnchorX, findings[j].AnchorY)
			}
		}
	}
	// face 锚点 (0.5,0.3) 与 body 锚点互不影响;越界锚点被钳回边界内
	if findings[2].AnchorX != .5 || findings[2].AnchorY != .3 {
		t.Fatalf("face anchor should keep its position, got (%.2f,%.2f)", findings[2].AnchorX, findings[2].AnchorY)
	}
}

func validAnalysisPayload() analysisPayload {
	steps := []analysisStep{
		{Category: "hair", Title: "抬高发型重心", Summary: "增加颅顶空气感", Details: []analysisDetail{{Label: "颅顶", Value: "增加蓬松度"}, {Label: "长度", Value: "发尾到锁骨"}}},
		{Category: "makeup", Title: "强化眉眼轮廓", Summary: "保持自然低饱和", Details: []analysisDetail{{Label: "眉形", Value: "清晰眉峰"}, {Label: "眼部", Value: "贴根部眼线"}}},
		{Category: "outfit", Title: "使用清晰肩线", Summary: "明亮内搭减轻沉重感", Details: []analysisDetail{{Label: "肩线", Value: "选择合肩版型"}, {Label: "配色", Value: "象牙白内搭"}}},
	}
	return analysisPayload{
		ImpressionTags: []string{"自然亲和", "稳重克制", "线条柔和"},
		PriorityTitle:  "先提升头肩区域的利落感", PriorityCopy: "抬高发型重心并露出肩颈，整体会更精神。",
		Findings: []analysisFinding{
			{Label: "发型重心偏低", Category: "hair", Severity: "medium", Detail: "颅顶贴头皮，抬高蓬松度会更利落。", Photo: "face", AnchorX: .6, AnchorY: .14},
			{Label: "眉眼对比稍弱", Category: "makeup", Severity: "low", Detail: "眉形偏淡，清晰眉峰可增强聚焦。", Photo: "face", AnchorX: .42, AnchorY: .28},
			{Label: "肩颈线条可更利落", Category: "outfit", Severity: "medium", Detail: "落肩版型下移肩线，合肩剪裁更精神。", Photo: "body", AnchorX: .66, AnchorY: .49},
			{Label: "上身配色偏沉", Category: "color", Severity: "low", Detail: "深色压低明度，浅色内搭可提亮。", Photo: "body", AnchorX: .31, AnchorY: .68},
		},
		Plans: []analysisPlan{
			{Name: "清晰利落", Slug: "sharp", Recommended: true, Descriptor: "精神可信", Why: "强化头肩区域", OutcomeTags: []string{"精神", "可信", "利落"}, DifferenceTags: []string{"发型", "眉眼", "肩线"}, Steps: steps},
			{Name: "温柔明亮", Slug: "warm", Descriptor: "明亮柔和", Why: "保留自然亲和", OutcomeTags: []string{"亲和", "明亮", "柔和"}, DifferenceTags: []string{"卷度", "配色", "妆感"}, Steps: steps},
			{Name: "松弛知性", Slug: "natural", Descriptor: "自然耐看", Why: "改动少且容易执行", OutcomeTags: []string{"自然", "知性", "耐看"}, DifferenceTags: []string{"偏分", "低饱和", "轻量"}, Steps: steps},
		},
	}
}
