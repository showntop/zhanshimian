package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zhanshimian/server/internal/service/daily"
)

const CapabilityDailyContent = "daily_content"

// StructuredDailyContentPlanner 走能力路由的每日内容生成器；实现
// daily.ContentPlanner。grounding 生成：输出必须能追溯到输入的知识事实，
// 不引入事实里没有的品牌/价格/材质。
type StructuredDailyContentPlanner struct{ runtime StructuredRuntime }

func NewDailyContentPlanner(runtime StructuredRuntime) *StructuredDailyContentPlanner {
	return &StructuredDailyContentPlanner{runtime: runtime}
}

var _ daily.ContentPlanner = (*StructuredDailyContentPlanner)(nil)

const dailyContentInstructions = "你是形象顾问的内容编辑。基于给定的【知识事实】为用户组装今日一条建议。规则：每个事实性断言必须来自【知识事实】，禁止引入其中没有的品牌、价格、材质或商品；语气克制、肯定式，不评价身材外貌，不打分，不制造焦虑；不用警示词；输出 JSON。"

func (p *StructuredDailyContentPlanner) Generate(ctx context.Context, input daily.ContentRequest) (daily.ContentOutput, error) {
	result, err := p.runtime.Structured(ctx, StructuredRequest{
		Capability:      CapabilityDailyContent,
		Instructions:    dailyContentInstructions,
		Prompt:          dailyContentPrompt(input),
		SchemaName:      CapabilityDailyContent,
		Schema:          dailyContentSchema(),
		MaxOutputTokens: 1200,
		Validate:        validateDailyContentPayload,
	})
	if err != nil {
		return daily.ContentOutput{}, err
	}
	if err := validateDailyContentPayload(result.JSON); err != nil {
		return daily.ContentOutput{}, err
	}
	var payload dailyContentPayload
	if err := json.Unmarshal(result.JSON, &payload); err != nil {
		return daily.ContentOutput{}, fmt.Errorf("decode structured daily content: %w", err)
	}
	output := daily.ContentOutput{
		Topic:     strings.TrimSpace(payload.Topic),
		Lead:      strings.TrimSpace(payload.Lead),
		Fit:       strings.TrimSpace(payload.Fit),
		Why:       strings.TrimSpace(payload.Why),
		Refs:      payload.Refs,
		ModelKey:  result.Meta.ModelKey,
		LatencyMS: result.Meta.LatencyMS,
	}
	output.Visual = daily.VisualDraft{
		Modality: payload.Visual.Modality,
		Kind:     payload.Visual.Kind,
		Alt:      strings.TrimSpace(payload.Visual.Alt),
		Marker:   payload.Visual.Marker,
	}
	for _, item := range payload.Visual.Items {
		output.Visual.Items = append(output.Visual.Items, daily.VisualItem{
			Label: strings.TrimSpace(item.Label), Tone: strings.TrimSpace(item.Tone), State: item.State,
		})
	}
	if payload.Visual.Left != nil {
		output.Visual.Left = &daily.VisualSide{
			Label:   strings.TrimSpace(payload.Visual.Left.Label),
			Tone:    strings.TrimSpace(payload.Visual.Left.Tone),
			Layered: payload.Visual.Left.Layered,
		}
	}
	if payload.Visual.Right != nil {
		output.Visual.Right = &daily.VisualSide{
			Label:   strings.TrimSpace(payload.Visual.Right.Label),
			Tone:    strings.TrimSpace(payload.Visual.Right.Tone),
			Layered: payload.Visual.Right.Layered,
		}
	}
	output.EstimatedCost = result.Meta.EstimatedCostCNY
	return output, nil
}

type dailyContentPayload struct {
	Topic  string                    `json:"topic"`
	Lead   string                    `json:"lead"`
	Fit    string                    `json:"fit"`
	Why    string                    `json:"why"`
	Visual dailyContentVisualPayload `json:"visual"`
	Refs   []int                     `json:"refs"`
}

type dailyContentVisualPayload struct {
	Modality string                    `json:"modality"`
	Kind     string                    `json:"kind"`
	Alt      string                    `json:"alt"`
	Marker   string                    `json:"marker"`
	Items    []dailyContentItemPayload `json:"items"`
	Left     *dailyContentSidePayload  `json:"left"`
	Right    *dailyContentSidePayload  `json:"right"`
}

type dailyContentItemPayload struct {
	Label string `json:"label"`
	Tone  string `json:"tone"`
	State string `json:"state"`
}

type dailyContentSidePayload struct {
	Label   string `json:"label"`
	Tone    string `json:"tone"`
	Layered bool   `json:"layered"`
}

func validateDailyContentPayload(data []byte) error {
	var payload dailyContentPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	if !aiSafeText(payload.Topic) || !aiSafeText(payload.Lead) || !aiSafeText(payload.Fit) || !aiSafeText(payload.Why) {
		return fmt.Errorf("daily content provider output is incomplete or unsafe")
	}
	switch payload.Visual.Modality {
	case "swatch", "compare", "diagram":
	default:
		return fmt.Errorf("daily content provider output has unsupported modality")
	}
	if len(payload.Refs) == 0 {
		return fmt.Errorf("daily content provider output is missing fact references")
	}
	return nil
}

func dailyContentSchema() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false,
		"required": []string{"topic", "lead", "fit", "why", "visual", "refs"},
		"properties": map[string]any{
			"topic": map[string]any{"type": "string", "minLength": 1, "maxLength": 24},
			"lead":  map[string]any{"type": "string", "minLength": 1, "maxLength": 60},
			"fit":   map[string]any{"type": "string", "minLength": 1, "maxLength": 90},
			"why":   map[string]any{"type": "string", "minLength": 1, "maxLength": 60},
			"refs": map[string]any{"type": "array", "minItems": 1, "maxItems": 5,
				"items": map[string]any{"type": "integer", "minimum": 1}},
			"visual": map[string]any{"type": "object", "additionalProperties": false,
				"required": []string{"modality", "alt"},
				"properties": map[string]any{
					"modality": map[string]any{"type": "string", "enum": []string{"swatch", "compare", "diagram"}},
					// kind：diagram 用（body=身体位置分区，scale=单向刻度）
					"kind":   map[string]any{"type": "string", "enum": []string{"body", "scale"}},
					"alt":    map[string]any{"type": "string", "minLength": 1, "maxLength": 80},
					"marker": map[string]any{"type": "string", "maxLength": 8},
					"items": map[string]any{"type": "array", "maxItems": 5, "items": map[string]any{
						"type": "object", "additionalProperties": false, "required": []string{"label"},
						"properties": map[string]any{
							"label": map[string]any{"type": "string", "minLength": 1, "maxLength": 12},
							"tone":  map[string]any{"type": "string", "pattern": "^#[0-9A-Fa-f]{6}$"},
							"state": map[string]any{"type": "string", "enum": []string{"pick", "drop", "avoid"}},
						},
					}},
					"left":  dailyContentSideSchema(),
					"right": dailyContentSideSchema(),
				},
			},
		},
	}
}

func dailyContentSideSchema() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false,
		"required": []string{"label"},
		"properties": map[string]any{
			"label":   map[string]any{"type": "string", "minLength": 1, "maxLength": 12},
			"tone":    map[string]any{"type": "string", "pattern": "^#[0-9A-Fa-f]{6}$"},
			"layered": map[string]any{"type": "boolean"},
		},
	}
}

// dailyContentPrompt 知识条目逐条注入（方案 §2.4 的 prompt 骨架）。
func dailyContentPrompt(input daily.ContentRequest) string {
	parts := []string{"【知识事实】"}
	for _, fact := range input.Facts {
		line := fmt.Sprintf("  %d. %s", fact.Index, fact.Fact)
		if fact.Boundary != "" {
			line += fmt.Sprintf("（边界：%s；domain: %s）", fact.Boundary, fact.Domain)
		}
		parts = append(parts, line)
	}
	if input.GeneText != "" {
		parts = append(parts, "【用户特征】"+input.GeneText)
	}
	if input.ContextText != "" {
		parts = append(parts, input.ContextText)
	}
	if input.HistoryText != "" {
		parts = append(parts, "【历史摘要】"+input.HistoryText)
	}
	if input.Angle != "" {
		parts = append(parts, "【选题角度】"+input.Angle)
	}
	if input.Category != "" {
		parts = append(parts, fmt.Sprintf("这条内容归入手册的「%s」格，内容要围绕知识事实展开。", input.Category))
	}
	parts = append(parts, "【输出】topic（≤12字，杂志式选题，不个性化）、lead（≤40字导语）、fit（≤60字，结合用户特征给出适配说明）、why（≤40字，一句原理）、refs（引用的知识条目序号，至少 1 个）、visual（swatch/compare/diagram 之一，给出可程序化绘制的参数）。")
	if input.RetryHint != "" {
		parts = append(parts, "【修正提示】"+input.RetryHint)
	}
	return strings.Join(parts, "\n")
}
