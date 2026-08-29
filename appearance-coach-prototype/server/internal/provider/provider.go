package provider

import (
	"context"
	"encoding/json"

	"github.com/example/jianwo/server/internal/domain"
)

type Analyzer interface {
	Analyze(context.Context, domain.CreateAnalysisInput) (domain.AnalysisOutput, error)
}

type AnalysisImage struct {
	ID       string
	Kind     string
	MIMEType string
	URL      string
	Data     []byte
}

type MediaLoader interface {
	Load(context.Context, []string) ([]AnalysisImage, error)
}

type DemoAnalyzer struct{}

func NewDemoAnalyzer() *DemoAnalyzer { return &DemoAnalyzer{} }

func (d *DemoAnalyzer) Analyze(_ context.Context, input domain.CreateAnalysisInput) (domain.AnalysisOutput, error) {
	hairDetails, _ := json.Marshal([]map[string]string{
		{"label": "颅顶", "value": "增加 2–3 cm 蓬松度"},
		{"label": "长度", "value": "发尾落在锁骨上方"},
		{"label": "分缝", "value": "6:4 侧分，弱化贴头皮"},
	})
	makeStep := func(category, title, summary string, details json.RawMessage, sort int) domain.PlanStep {
		return domain.PlanStep{Category: category, Title: title, Summary: summary, Details: details, Sort: sort}
	}
	baseSteps := []domain.PlanStep{
		makeStep("hair", "抬高重心，露出轮廓", "增加颅顶蓬松度，发尾落在锁骨上方", hairDetails, 1),
		makeStep("makeup", "强化眉眼对比", "眉形更清晰，眼妆保持低饱和", json.RawMessage(`[{"label":"眉形","value":"眉峰清晰但不过度上挑"},{"label":"眼部","value":"贴根部内眼线 + 自然睫毛"}]`), 2),
		makeStep("outfit", "清晰肩线 + 明亮内搭", "藏蓝套装搭配象牙白内搭", json.RawMessage(`[{"label":"肩线","value":"选择合肩版型"},{"label":"配色","value":"深外套 + 明亮内搭"}]`), 3),
	}
	current := "/assets/looks/natural.png"
	if len(input.MediaIDs) == 0 {
		current = "/assets/looks/natural.png"
	}
	return domain.AnalysisOutput{
		CurrentImageURL: current,
		ImpressionTags:  []string{"自然亲和", "稳重克制", "线条柔和"},
		PriorityTitle:   "先提升头肩区域的利落感",
		PriorityCopy:    "抬高发型重心、露出肩颈、加强眉眼轮廓，整体会更精神。",
		ProviderVersion: "demo-v1.0.0",
		Findings: []domain.Finding{
			{Label: "发型重心偏低", Category: "hair", Severity: "medium", AnchorX: .60, AnchorY: .14},
			{Label: "眉眼对比度稍弱", Category: "makeup", Severity: "low", AnchorX: .42, AnchorY: .28},
			{Label: "肩颈线条可更利落", Category: "outfit", Severity: "medium", AnchorX: .66, AnchorY: .49},
			{Label: "上身配色偏沉", Category: "color", Severity: "low", AnchorX: .31, AnchorY: .68},
		},
		Plans: []domain.Plan{
			{Name: "清晰利落", Slug: "sharp", ImageURL: "/assets/looks/sharp.png", Recommended: true, Descriptor: "更精神 · 更可信 · 更有边界感", Why: "抬高发型重心，强化眉眼轮廓，用清晰肩线改善头肩比例。", OutcomeTags: []string{"精神感提升", "专业感更强", "保留亲和力"}, DifferenceTags: []string{"发型更蓬松", "眉眼更清晰", "肩线更利落"}, Sort: 1, Steps: baseSteps},
			{Name: "温柔明亮", Slug: "warm", ImageURL: "/assets/looks/warm.png", Descriptor: "更明亮 · 更柔和 · 更有亲和力", Why: "通过浅暖配色和柔和卷度，让专业感更容易靠近。", OutcomeTags: []string{"亲和力提升", "气色更明亮", "保持专业"}, DifferenceTags: []string{"柔和卷度", "浅暖配色", "轻盈妆感"}, Sort: 2, Steps: baseSteps},
			{Name: "松弛知性", Slug: "natural", ImageURL: "/assets/looks/natural.png", Descriptor: "更自然 · 更舒展 · 更耐看", Why: "保留自然亲和，以轻量妆感和低饱和穿搭建立稳定风格。", OutcomeTags: []string{"自然度提升", "日常好执行", "低维护"}, DifferenceTags: []string{"轻量妆感", "低饱和", "柔和线条"}, Sort: 3, Steps: baseSteps},
		},
	}, nil
}
