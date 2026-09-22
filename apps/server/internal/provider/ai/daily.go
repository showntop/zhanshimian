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
// daily.ContentPlanner。一次成文：模型基于用户语境自行决定今天讲什么，
// 参考事实只是可选素材，不强制引用；分类由模型自报（手册各格或 general）。
type StructuredDailyContentPlanner struct{ runtime StructuredRuntime }

func NewDailyContentPlanner(runtime StructuredRuntime) *StructuredDailyContentPlanner {
	return &StructuredDailyContentPlanner{runtime: runtime}
}

var _ daily.ContentPlanner = (*StructuredDailyContentPlanner)(nil)

const dailyContentInstructions = "你是形象顾问的内容主编，每天为一位用户写一条今日形象建议——穿搭、发型、妆容、配饰都可能，由你判断今天哪个角度最有话说。给出一条具体的、今天就能用的建议。语气克制、肯定式，不评价身材外貌，不打分，不制造焦虑，不提品牌价格。输出 JSON。"

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
		Category:  strings.TrimSpace(payload.Category),
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
	Topic    string                    `json:"topic"`
	Lead     string                    `json:"lead"`
	Fit      string                    `json:"fit"`
	Why      string                    `json:"why"`
	Category string                    `json:"category"`
	Visual   dailyContentVisualPayload `json:"visual"`
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
	switch payload.Category {
	case "color", "fit", "proportion", "fabric", "occasion", "howto", "outfit", "hair", "makeup", "accessory", "general":
	default:
		return fmt.Errorf("daily content provider output has invalid category %q", payload.Category)
	}
	switch payload.Visual.Modality {
	case "swatch", "compare", "diagram":
	default:
		return fmt.Errorf("daily content provider output has unsupported modality")
	}
	return nil
}

func dailyContentSchema() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false,
		"required": []string{"topic", "lead", "fit", "why", "category", "visual"},
		"properties": map[string]any{
			"topic": map[string]any{"type": "string", "minLength": 1, "maxLength": 24},
			"lead":  map[string]any{"type": "string", "minLength": 1, "maxLength": 60},
			"fit":   map[string]any{"type": "string", "minLength": 1, "maxLength": 90},
			"why":   map[string]any{"type": "string", "minLength": 1, "maxLength": 60},
			"category": map[string]any{"type": "string",
				"enum": []string{"color", "fit", "proportion", "fabric", "occasion", "howto", "outfit", "hair", "makeup", "accessory", "general"}},
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

// dailyContentPrompt 语境注入：模型自己决定讲什么，参考事实可选。
func dailyContentPrompt(input daily.ContentRequest) string {
	parts := []string{}
	if input.ContextText != "" {
		parts = append(parts, "【今日语境】"+input.ContextText)
	}
	if input.GeneText != "" {
		parts = append(parts, "【用户特征】"+input.GeneText+"。适配说明要贴这个特征。")
	}
	if input.InterestText != "" {
		parts = append(parts, "【用户兴趣】"+input.InterestText+"。可以贴，但不必迎合。")
	}
	if input.HistoryText != "" {
		parts = append(parts, "【近期已推】"+input.HistoryText)
	}
	if len(input.ReferenceFacts) > 0 {
		lines := make([]string, 0, len(input.ReferenceFacts))
		for _, fact := range input.ReferenceFacts {
			line := "  - " + fact.Fact
			if fact.Boundary != "" {
				line += "（边界：" + fact.Boundary + "）"
			}
			lines = append(lines, line)
		}
		parts = append(parts, "【参考观点】以下是可以参考的专业事实，可用可不用，观点要自己消化：\n"+strings.Join(lines, "\n"))
	}
	parts = append(parts, "【输出】一条完整的今日建议：topic（≤12字，杂志式选题）、lead（≤40字导语）、fit（≤60字，结合用户特征的适配说明）、why（≤40字，一句原理）、category（按内容主体归格：color/fit/proportion/fabric/occasion/howto/outfit 讲穿着，hair=发型方向、makeup=妆容要点、accessory=鞋包首饰的选法与呼应，确实跨格才用 general）、visual（swatch/compare/diagram 之一，给出可程序化绘制的参数）。今天必须是一个新主题，也不必总停在穿着上——发型、妆容、配饰同样是今天的候选角度。")
	if input.RetryHint != "" {
		parts = append(parts, "【修正提示】"+input.RetryHint)
	}
	return strings.Join(parts, "\n")
}
