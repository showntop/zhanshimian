package provider

// 方案组生成（plan_group）：报告与方案解耦后，general 三套方案的文字
// 由本生成器从已落库的报告内容派生——AI 个性化（输入 findings/标签/
// 优先建议，输出贴合报告的三套方案），真路由不可用时回退 demo 模板。
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/zhanshimian/server/internal/domain"
)

// PlanGroupInput is the report digest a plan group is derived from.
type PlanGroupInput struct {
	ImpressionTags []string
	PriorityTitle  string
	PriorityCopy   string
	Findings       []domain.Finding
}

// PlanGroupOutput carries the authored general plans plus provider identity.
type PlanGroupOutput struct {
	Plans           []domain.Plan
	ProviderVersion string
}

type PlanGroupGenerator interface {
	Generate(context.Context, PlanGroupInput) (PlanGroupOutput, error)
}

// ---- Routed（AI 个性化） ----

type RoutedPlanGroupGenerator struct {
	runtime *AIRuntime
}

func NewRoutedPlanGroupGenerator(runtime *AIRuntime) (*RoutedPlanGroupGenerator, error) {
	if runtime == nil || !runtime.HasRoute(CapabilityAppearanceAnalysis) {
		return nil, errors.New("appearance analysis AI route is required")
	}
	return &RoutedPlanGroupGenerator{runtime: runtime}, nil
}

func (g *RoutedPlanGroupGenerator) Generate(ctx context.Context, input PlanGroupInput) (PlanGroupOutput, error) {
	result, err := g.runtime.Structured(ctx, CapabilityAppearanceAnalysis, StructuredRequest{
		Instructions: planGroupInstructions, Prompt: planGroupPrompt(input),
		SchemaName: CapabilityAppearanceAnalysis, Schema: planGroupSchema(), MaxOutputTokens: 3600,
		Validate: func(data []byte) error {
			payload, err := decodePlanGroupPayload(data)
			if err != nil {
				return err
			}
			return validatePlanGroupPayload(payload)
		},
	})
	if err != nil {
		return PlanGroupOutput{}, err
	}
	payload, err := decodePlanGroupPayload(result.JSON)
	if err != nil {
		return PlanGroupOutput{}, fmt.Errorf("decode structured plan group: %w", err)
	}
	if err := validatePlanGroupPayload(payload); err != nil {
		return PlanGroupOutput{}, err
	}
	return planGroupPayloadToDomain(payload, result.Meta.ProviderVersion())
}

const planGroupInstructions = "你是审慎、尊重用户的私人形象顾问。基于给定的形象分析报告撰写三套提升方案：只回应报告中明确提到的可提升点，不评价颜值，不推断健康、族裔、年龄或身份。每套方案必须具体、温和、可执行，步骤要与报告的可提升点一一对应。"

func planGroupPrompt(input PlanGroupInput) string {
	findings := make([]string, 0, len(input.Findings))
	for _, finding := range input.Findings {
		findings = append(findings, fmt.Sprintf("- %s（%s，来自%s）：%s", finding.Label, finding.Category, photoKindName(finding.Photo), finding.Detail))
	}
	return fmt.Sprintf(`请基于以下形象分析报告，为用户生成中文的三套提升方案。
当前印象：%s。
最优先建议：%s。%s
可提升点：
%s
输出严格 3 套方案，slug 必须分别是 sharp、warm、natural，恰好一套 recommended 为 true。每套方案包含 hair、makeup、outfit 三个步骤和可执行细节，步骤内容必须针对上面的可提升点给出具体解法，不要写通用套话。顶层输出一个 JSON 对象，不要输出数组、Markdown 或额外解释。`,
		strings.Join(input.ImpressionTags, "、"), input.PriorityTitle, input.PriorityCopy, strings.Join(findings, "\n"))
}

// ---- payload 契约（复用 analysisPlan 形状） ----

type planGroupPayload struct {
	Plans []analysisPlan `json:"plans"`
}

func decodePlanGroupPayload(raw []byte) (planGroupPayload, error) {
	var tree any
	if err := json.Unmarshal(raw, &tree); err != nil {
		return planGroupPayload{}, err
	}
	encoded, err := json.Marshal(collapseNestedStringArrays(tree))
	if err != nil {
		return planGroupPayload{}, err
	}
	var payload planGroupPayload
	if err := json.Unmarshal(encoded, &payload); err != nil {
		return planGroupPayload{}, err
	}
	return payload, nil
}

func validatePlanGroupPayload(payload planGroupPayload) error {
	if len(payload.Plans) != 3 {
		return fmt.Errorf("plan group output must contain exactly 3 plans, got %d", len(payload.Plans))
	}
	allowedSlugs := map[string]bool{"sharp": true, "warm": true, "natural": true}
	recommended := 0
	seenSlugs := map[string]bool{}
	for _, plan := range payload.Plans {
		if !allowedSlugs[plan.Slug] || seenSlugs[plan.Slug] || !safeText(plan.Name) || !safeText(plan.Descriptor) || !safeText(plan.Why) || len(plan.OutcomeTags) != 3 || len(plan.DifferenceTags) != 3 || len(plan.Steps) != 3 {
			return fmt.Errorf("plan group output contains invalid plan %q: name=%q outcome_tags=%d difference_tags=%d steps=%d (want slug sharp/warm/natural unique, 3/3/3)", plan.Slug, preview(plan.Name), len(plan.OutcomeTags), len(plan.DifferenceTags), len(plan.Steps))
		}
		seenSlugs[plan.Slug] = true
		if plan.Recommended {
			recommended++
		}
		seenCategories := map[string]bool{}
		for _, step := range plan.Steps {
			if !map[string]bool{"hair": true, "makeup": true, "outfit": true}[step.Category] || seenCategories[step.Category] || !safeText(step.Title) || !safeText(step.Summary) || len(step.Details) < 2 || len(step.Details) > 4 {
				return fmt.Errorf("plan group output contains invalid step in plan %q: category=%q details=%d (want hair/makeup/outfit unique, 2-4) title=%q", plan.Slug, step.Category, len(step.Details), preview(step.Title))
			}
			seenCategories[step.Category] = true
			for _, detail := range step.Details {
				if !safeText(detail.Label) || !safeText(detail.Value) {
					return fmt.Errorf("plan group output contains unsafe or empty detail in plan %q step %q: label=%q value=%q", plan.Slug, step.Category, preview(detail.Label), preview(detail.Value))
				}
			}
		}
	}
	if recommended != 1 {
		return fmt.Errorf("plan group output must recommend exactly one plan, got %d", recommended)
	}
	return nil
}

func planGroupPayloadToDomain(payload planGroupPayload, providerVersion string) (PlanGroupOutput, error) {
	imageURLs := map[string]string{"sharp": "/assets/looks/sharp.png", "warm": "/assets/looks/warm.png", "natural": "/assets/looks/natural.png"}
	output := PlanGroupOutput{ProviderVersion: providerVersion}
	for index, plan := range payload.Plans {
		domainPlan := domain.Plan{
			Name: plan.Name, Slug: plan.Slug, ImageURL: imageURLs[plan.Slug], Recommended: plan.Recommended,
			Descriptor: plan.Descriptor, Why: plan.Why, OutcomeTags: plan.OutcomeTags,
			DifferenceTags: plan.DifferenceTags, Sort: index + 1,
		}
		for stepIndex, step := range plan.Steps {
			details, err := json.Marshal(step.Details)
			if err != nil {
				return PlanGroupOutput{}, err
			}
			domainPlan.Steps = append(domainPlan.Steps, domain.PlanStep{
				Category: step.Category, Title: step.Title, Summary: step.Summary, Details: details, Sort: stepIndex + 1,
			})
		}
		output.Plans = append(output.Plans, domainPlan)
	}
	return output, nil
}

func planGroupSchema() map[string]any {
	stringSchema := map[string]any{"type": "string", "minLength": 1, "maxLength": 160}
	stringArray3 := map[string]any{"type": "array", "items": stringSchema, "minItems": 3, "maxItems": 3}
	detail := objectSchema(map[string]any{"label": stringSchema, "value": stringSchema}, "label", "value")
	step := objectSchema(map[string]any{
		"category": map[string]any{"type": "string", "enum": []string{"hair", "makeup", "outfit"}},
		"title":    stringSchema, "summary": stringSchema,
		"details": map[string]any{"type": "array", "items": detail, "minItems": 2, "maxItems": 4},
	}, "category", "title", "summary", "details")
	plan := objectSchema(map[string]any{
		"name":        stringSchema,
		"slug":        map[string]any{"type": "string", "enum": []string{"sharp", "warm", "natural"}},
		"recommended": map[string]any{"type": "boolean"},
		"descriptor":  stringSchema, "why": stringSchema,
		"outcome_tags": stringArray3, "difference_tags": stringArray3,
		"steps": map[string]any{"type": "array", "items": step, "minItems": 3, "maxItems": 3},
	}, "name", "slug", "recommended", "descriptor", "why", "outcome_tags", "difference_tags", "steps")
	return objectSchema(map[string]any{
		"plans": map[string]any{"type": "array", "items": plan, "minItems": 3, "maxItems": 3},
	}, "plans")
}

// ---- Demo（从报告内容参数化的本地模板） ----

type DemoPlanGroupGenerator struct{}

func NewDemoPlanGroupGenerator() *DemoPlanGroupGenerator { return &DemoPlanGroupGenerator{} }

func (d *DemoPlanGroupGenerator) Generate(_ context.Context, input PlanGroupInput) (PlanGroupOutput, error) {
	// 按 category 归拢报告可提升点，让模板步骤引用真实结论而非通用话术。
	byCategory := map[string][]string{}
	for _, finding := range input.Findings {
		byCategory[finding.Category] = append(byCategory[finding.Category], finding.Label)
	}
	join := func(category string) string {
		if items := byCategory[category]; len(items) > 0 {
			return strings.Join(items, "、")
		}
		return "报告未提到"
	}
	baseSteps := func(hairTitle, makeupTitle, outfitTitle string) []domain.PlanStep {
		return []domain.PlanStep{
			{Category: "hair", Title: hairTitle, Summary: "针对「" + join("hair") + "」给出可执行的发型调整。", Details: json.RawMessage(`[{"label":"重点","value":"按报告可提升点执行"},{"label":"幅度","value":"小步调整，先易后难"}]`), Sort: 1},
			{Category: "makeup", Title: makeupTitle, Summary: "针对「" + join("makeup") + "」强化眉眼与气色。", Details: json.RawMessage(`[{"label":"重点","value":"控制妆感强度"},{"label":"顺序","value":"先眉眼后唇色"}]`), Sort: 2},
			{Category: "outfit", Title: outfitTitle, Summary: "针对「" + join("outfit") + "」与配色调整轮廓。", Details: json.RawMessage(`[{"label":"重点","value":"优先现有衣物"},{"label":"配色","value":"深外套配明亮内搭"}]`), Sort: 3},
		}
	}
	why := "针对报告里的「" + join("hair") + "」等问题逐一给出解法，优先" + input.PriorityTitle + "。"
	return PlanGroupOutput{
		ProviderVersion: "demo-plan-group-v1",
		Plans: []domain.Plan{
			{Name: "清晰利落", Slug: "sharp", ImageURL: "/assets/looks/sharp.png", Recommended: true, Descriptor: "更精神 · 更可信 · 更有边界感", Why: why, OutcomeTags: []string{"精神感提升", "专业感更强", "保留亲和力"}, DifferenceTags: []string{"发型更利落", "眉眼更清晰", "肩线更干净"}, Sort: 1, Steps: baseSteps("抬高重心，收干净线条", "清晰眉形 · 克制眼妆", "合肩版型 · 明亮内搭")},
			{Name: "温柔明亮", Slug: "warm", ImageURL: "/assets/looks/warm.png", Descriptor: "更明亮 · 更柔和 · 更有亲和力", Why: why + "用柔和的光泽和弧度保留亲和力。", OutcomeTags: []string{"亲和力提升", "气色更明亮", "保持专业"}, DifferenceTags: []string{"柔和卷度", "浅暖配色", "轻盈妆感"}, Sort: 2, Steps: baseSteps("柔和弧度，保留蓬松", "轻薄底妆 · 提亮气色", "轻盈层次 · 柔和配色")},
			{Name: "松弛知性", Slug: "natural", ImageURL: "/assets/looks/natural.png", Descriptor: "更自然 · 更舒展 · 更耐看", Why: why + "保留熟悉感，只整理重点。", OutcomeTags: []string{"自然度提升", "日常好执行", "低维护"}, DifferenceTags: []string{"轻量妆感", "低饱和", "柔和线条"}, Sort: 3, Steps: baseSteps("自然偏分，减少贴头皮", "弱化妆感，只提气色", "舒展版型 · 一处颜色重点")},
		},
	}, nil
}
