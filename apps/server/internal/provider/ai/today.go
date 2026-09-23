package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/zhanshimian/server/internal/service/today"
)

const CapabilityTodayPlan = "today_plan"

// StructuredTodayPlanner 走能力路由的今日方案生成器；实现 today.TodayPlanner。
type StructuredTodayPlanner struct{ runtime StructuredRuntime }

func NewTodayPlanner(runtime StructuredRuntime) *StructuredTodayPlanner {
	return &StructuredTodayPlanner{runtime: runtime}
}

var _ today.TodayPlanner = (*StructuredTodayPlanner)(nil)

const todayPlanInstructions = "你是审慎、尊重用户的私人形象顾问。根据用户的形象档案和今天的城市、天气、日程，给出一套今天就能执行的造型方案。语气像顾问而不像教程：尊重用户现有条件，建议具体、轻量、当天能完成，优先利用已有衣物。不打分，不评价颜值和身体，不制造焦虑，不编造用户没有的单品。"

func (p *StructuredTodayPlanner) Generate(ctx context.Context, input today.TodayPlanRequest) (today.TodayPlanOutput, error) {
	result, err := p.runtime.Structured(ctx, StructuredRequest{
		Capability:      CapabilityTodayPlan,
		Instructions:    todayPlanInstructions,
		Prompt:          todayPlanPrompt(input),
		SchemaName:      CapabilityTodayPlan,
		Schema:          todayPlanSchema(),
		MaxOutputTokens: 1600,
		Validate:        validateTodayPlanPayload,
	})
	if err != nil {
		return today.TodayPlanOutput{}, err
	}
	if err := validateTodayPlanPayload(result.JSON); err != nil {
		return today.TodayPlanOutput{}, err
	}
	var payload todayPlanPayload
	if err := json.Unmarshal(result.JSON, &payload); err != nil {
		return today.TodayPlanOutput{}, fmt.Errorf("decode structured today plan: %w", err)
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

func (p todayPlanPayload) toOutput() today.TodayPlanOutput {
	steps := make([]today.TodayPlanStep, 0, len(p.Steps))
	for _, category := range []string{"hair", "makeup", "outfit"} {
		for _, step := range p.Steps {
			if step.Category == category {
				steps = append(steps, today.TodayPlanStep{Category: step.Category, Label: step.Label, Title: step.Title, Copy: step.Copy})
			}
		}
	}
	return today.TodayPlanOutput{Title: p.Title, Summary: p.Summary, Steps: steps}
}

func validateTodayPlanPayload(data []byte) error {
	var payload todayPlanPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	if !aiSafeText(payload.Title) || !aiSafeText(payload.Summary) || len(payload.Steps) != 3 {
		return errors.New("today plan provider output is incomplete or unsafe")
	}
	seen := map[string]bool{}
	for _, step := range payload.Steps {
		if _, ok := todayPlanCategoryOrder[step.Category]; !ok || seen[step.Category] {
			return errors.New("today plan provider output has invalid step categories")
		}
		seen[step.Category] = true
		if !aiSafeText(step.Label) || !aiSafeText(step.Title) || !aiSafeText(step.Copy) {
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

func todayPlanPrompt(input today.TodayPlanRequest) string {
	parts := []string{fmt.Sprintf("用户所在城市是%s，天气%s、气温约 %d°C，今日日程是%s。请给出一套今天立即可执行的方案：一个整体标题、一句摘要，以及发型、妆造、穿搭三步建议，每步说明具体做法。",
		input.Weather.City, input.Weather.Condition, input.Weather.Temperature, input.Schedule)}
	if input.Weather.Condition == "" {
		parts[0] = "今天请给出一套立即可执行的方案：一个整体标题、一句摘要，以及发型、妆造、穿搭三步建议，每步说明具体做法。"
	}
	if len(input.Findings) > 0 {
		labels := make([]string, 0, len(input.Findings))
		for _, f := range input.Findings {
			labels = append(labels, f.Label)
		}
		parts = append(parts, fmt.Sprintf("用户已有形象档案，已识别的可提升点包括：%s。方案要与档案方向保持连续，优先把最高优先级落成今天的轻量动作。", strings.Join(labels, "、")))
	}
	if input.SelectedPlan != nil {
		parts = append(parts, fmt.Sprintf("用户最近选中的方案是「%s」（%s），今天的建议可以延续这个方向。", input.SelectedPlan.Name, input.SelectedPlan.Descriptor))
	}
	return strings.Join(parts, "\n")
}

func aiSafeText(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len([]rune(value)) > 160 {
		return false
	}
	for _, forbidden := range []string{"颜值", "丑", "身材", "评分", "肥胖", "缺陷", "整容", "种族", "族裔", "疾病", "诊断"} {
		if strings.Contains(value, forbidden) {
			return false
		}
	}
	return true
}
