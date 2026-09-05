package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/example/jianwo/server/internal/domain"
)

// TodayPlanGrounding carries everything the planner may use: the user's
// appearance report when one exists, today's real context, the style variant
// selected by the regenerate counter, and the title the user has already seen
// so a regeneration can take a different angle.
type TodayPlanGrounding struct {
	Report        *domain.Report
	Context       domain.TodayContext
	Variant       int
	PreviousTitle string
}

type TodayPlanOutput struct {
	Title   string
	Summary string
	Steps   []domain.TodayPlanStep
}

type TodayPlanner interface {
	Generate(context.Context, TodayPlanGrounding) (TodayPlanOutput, error)
}

// DemoTodayPlanner serves the bundled template rotation for local
// development. Production routing must configure the today_plan capability so
// this provider is never selected there.
type DemoTodayPlanner struct{}

func NewDemoTodayPlanner() *DemoTodayPlanner { return &DemoTodayPlanner{} }

func (*DemoTodayPlanner) Generate(_ context.Context, grounding TodayPlanGrounding) (TodayPlanOutput, error) {
	styles := []struct {
		title, summary, hair, makeup, outfit string
	}{
		{"轻薄利落，清爽不单薄", "保持自然亲和，用清晰肩线和明亮内搭提升精神感。", "轻盈侧分 · 发根蓬松", "轻透底妆 · 眉眼更清晰", "米白外套 · 明亮内搭 · 垂感长裤"},
		{"柔和明亮，今天更有气色", "减少沉重配色，用柔和弧度与一处明亮颜色保持亲近感。", "空气微卷 · 发尾有弧度", "提亮眼下 · 柔和唇色", "浅色针织 · 高腰下装 · 小体积包"},
		{"舒服自然，也保持完整", "以现有衣橱为主，只整理比例、发根与鞋包三个重点。", "自然偏分 · 整理耳侧", "均匀肤色 · 保留原生感", "舒展上装 · 清楚腰线 · 轻便鞋履"},
	}
	variant := grounding.Variant % len(styles)
	if variant < 0 {
		variant = 0
	}
	style := styles[variant]
	return TodayPlanOutput{
		Title: style.title, Summary: style.summary,
		Steps: []domain.TodayPlanStep{
			{Category: "hair", Label: "发型", Title: style.hair, Copy: "先抬高发根，再整理耳侧轮廓，约 3 分钟。"},
			{Category: "makeup", Label: "妆造", Title: style.makeup, Copy: "控制妆感，把重点放在气色与眉眼对比。"},
			{Category: "outfit", Label: "穿搭", Title: style.outfit, Copy: "优先使用已有衣物，先处理配色和腰线。"},
		},
	}, nil
}

type RoutedTodayPlanner struct{ runtime *AIRuntime }

func NewRoutedTodayPlanner(runtime *AIRuntime) (*RoutedTodayPlanner, error) {
	if runtime == nil || !runtime.HasRoute(CapabilityTodayPlan) {
		return nil, errors.New("today plan AI route is required")
	}
	return &RoutedTodayPlanner{runtime: runtime}, nil
}

const todayPlanInstructions = "你是审慎、尊重用户的私人形象顾问。根据用户的形象档案和今天的城市、天气、日程，给出一套今天就能执行的造型方案。语气像顾问而不像教程：尊重用户现有条件，建议具体、轻量、当天能完成，优先利用已有衣物。不打分，不评价颜值和身体，不制造焦虑，不编造用户没有的单品。"

func (p *RoutedTodayPlanner) Generate(ctx context.Context, grounding TodayPlanGrounding) (TodayPlanOutput, error) {
	result, err := p.runtime.Structured(ctx, CapabilityTodayPlan, StructuredRequest{
		Instructions: todayPlanInstructions, Prompt: todayPlanPrompt(grounding),
		SchemaName: CapabilityTodayPlan, Schema: todayPlanSchema(), MaxOutputTokens: 1600,
		Validate: func(data []byte) error {
			var candidate todayPlanPayload
			if err := json.Unmarshal(data, &candidate); err != nil {
				return err
			}
			return validateTodayPlanPayload(candidate)
		},
	})
	if err != nil {
		return TodayPlanOutput{}, err
	}
	var payload todayPlanPayload
	if err := json.Unmarshal(result.JSON, &payload); err != nil {
		return TodayPlanOutput{}, fmt.Errorf("decode structured today plan: %w", err)
	}
	if err := validateTodayPlanPayload(payload); err != nil {
		return TodayPlanOutput{}, err
	}
	return payload.toOutput(), nil
}

type todayPlanPayload struct {
	Title   string                 `json:"title"`
	Summary string                 `json:"summary"`
	Steps   []todayPlanStepPayload `json:"steps"`
}

type todayPlanStepPayload struct {
	Category string `json:"category"`
	Label    string `json:"label"`
	Title    string `json:"title"`
	Copy     string `json:"copy"`
}

var todayPlanCategoryOrder = map[string]int{"hair": 0, "makeup": 1, "outfit": 2}

func (p todayPlanPayload) toOutput() TodayPlanOutput {
	steps := make([]domain.TodayPlanStep, 0, len(p.Steps))
	for _, category := range []string{"hair", "makeup", "outfit"} {
		for _, step := range p.Steps {
			if step.Category == category {
				steps = append(steps, domain.TodayPlanStep{Category: step.Category, Label: step.Label, Title: step.Title, Copy: step.Copy})
			}
		}
	}
	return TodayPlanOutput{Title: p.Title, Summary: p.Summary, Steps: steps}
}

func validateTodayPlanPayload(payload todayPlanPayload) error {
	if !safeText(payload.Title) || !safeText(payload.Summary) || len(payload.Steps) != 3 {
		return errors.New("today plan provider output is incomplete or unsafe")
	}
	seen := map[string]bool{}
	for _, step := range payload.Steps {
		if _, ok := todayPlanCategoryOrder[step.Category]; !ok || seen[step.Category] {
			return errors.New("today plan provider output has invalid step categories")
		}
		seen[step.Category] = true
		if !safeText(step.Label) || !safeText(step.Title) || !safeText(step.Copy) {
			return errors.New("today plan provider output contains unsafe step")
		}
	}
	return nil
}

func todayPlanSchema() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false,
		"required": []string{"title", "summary", "steps"},
		"properties": map[string]any{
			"title":   map[string]any{"type": "string", "minLength": 1, "maxLength": 24},
			"summary": map[string]any{"type": "string", "minLength": 1, "maxLength": 80},
			"steps": map[string]any{"type": "array", "minItems": 3, "maxItems": 3, "items": map[string]any{
				"type": "object", "additionalProperties": false, "required": []string{"category", "label", "title", "copy"},
				"properties": map[string]any{
					"category": map[string]any{"type": "string", "enum": []string{"hair", "makeup", "outfit"}},
					"label":    map[string]any{"type": "string", "minLength": 1, "maxLength": 8},
					"title":    map[string]any{"type": "string", "minLength": 1, "maxLength": 36},
					"copy":     map[string]any{"type": "string", "minLength": 1, "maxLength": 80},
				},
			}},
		},
	}
}

func todayPlanPrompt(grounding TodayPlanGrounding) string {
	context := grounding.Context
	parts := make([]string, 0, 4)
	line := fmt.Sprintf("今天是%s，%s", context.Date, context.DayType)
	if context.City != "" {
		line += "，用户在" + context.City
	}
	if context.Condition != "" {
		line += fmt.Sprintf("，天气%s，气温约 %d°C", context.Condition, context.Temperature)
	}
	line += "，今日日程是" + context.Schedule + "。"
	parts = append(parts, line+"请结合这些信息给出一套今天立即可执行的方案：一个整体标题、一句摘要，以及发型、妆造、穿搭三步建议，每步说明具体做法。")
	if grounding.Report != nil {
		report := grounding.Report
		findings := make([]string, 0, len(report.Findings))
		for _, finding := range report.Findings {
			findings = append(findings, finding.Label)
		}
		parts = append(parts, fmt.Sprintf("用户已有形象档案：印象标签为%s；当前最高优先级是「%s」（%s）；已识别的可提升点包括：%s。方案要与档案方向保持连续，优先把最高优先级落成今天的轻量动作。",
			strings.Join(report.ImpressionTags, "、"), report.PriorityTitle, report.PriorityCopy, strings.Join(findings, "、")))
	}
	if grounding.PreviousTitle != "" {
		parts = append(parts, "用户今天已经看过方案「"+grounding.PreviousTitle+"」，本次请换一个不同的侧重点和标题，不要复述之前的内容。")
	}
	return strings.Join(parts, "\n")
}
