package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/service/diagnostic"
)

const (
	CapabilityOutfitDiagnosis   = "outfit_diagnosis"
	CapabilityPurchaseDiagnosis = "purchase_diagnosis"
)

// StructuredDiagnostic 走能力路由的诊断；实现 diagnostic.Advisor。
type StructuredDiagnostic struct{ runtime StructuredRuntime }

func NewDiagnostic(runtime StructuredRuntime) *StructuredDiagnostic {
	return &StructuredDiagnostic{runtime: runtime}
}

var _ diagnostic.Advisor = (*StructuredDiagnostic)(nil)

const diagnosticInstructions = "你是审慎、尊重用户的穿搭与购买顾问。基于形象报告、用户档案和照片给出一句话判断、一条最高优先级建议和若干观察点；只描述看得到的事实，建议具体可执行。不用缺陷、严重度等评判词；不评价颜值和身体；不编造用户没有的单品；语气保持尊重（positive=适合保留，improve=可提升，optional=可参考，caution=注意）。"

func (d *StructuredDiagnostic) Diagnose(ctx context.Context, request diagnostic.DiagnosticRequest) (diagnostic.DiagnosticOutput, error) {
	capability := CapabilityOutfitDiagnosis
	if request.Kind == "purchase" {
		capability = CapabilityPurchaseDiagnosis
	}
	result, err := d.runtime.Structured(ctx, StructuredRequest{
		Capability:      capability,
		Instructions:    diagnosticInstructions,
		Prompt:          diagnosticPrompt(request),
		SchemaName:      capability,
		Schema:          diagnosticSchema(),
		MaxOutputTokens: 1600,
		Validate:        validateDiagnosticPayload,
	})
	if err != nil {
		return diagnostic.DiagnosticOutput{}, err
	}
	if err := validateDiagnosticPayload(result.JSON); err != nil {
		return diagnostic.DiagnosticOutput{}, err
	}
	var payload diagnosticPayload
	if err := json.Unmarshal(result.JSON, &payload); err != nil {
		return diagnostic.DiagnosticOutput{}, fmt.Errorf("decode diagnostic output: %w", err)
	}
	return payload.toOutput(), nil
}

type diagnosticPayload struct {
	Conclusion    string                 `json:"conclusion"`
	PriorityTitle string                 `json:"priority_title"`
	PriorityCopy  string                 `json:"priority_copy"`
	Tags          []string               `json:"tags"`
	Findings      []diagnosticFinding    `json:"findings"`
	Options       []diagnosticOptionPayl `json:"options"`
}

type diagnosticFinding struct {
	Label    string   `json:"label"`
	Category string   `json:"category"`
	Tone     string   `json:"tone"`
	AnchorX  *float64 `json:"anchor_x"`
	AnchorY  *float64 `json:"anchor_y"`
}

type diagnosticOptionPayl struct {
	Name   string   `json:"name"`
	Note   string   `json:"note"`
	Reason string   `json:"reason"`
	Tags   []string `json:"tags"`
}

func (p diagnosticPayload) toOutput() diagnostic.DiagnosticOutput {
	findings := make([]domain.DiagnosisFinding, 0, len(p.Findings))
	for _, f := range p.Findings {
		findings = append(findings, domain.DiagnosisFinding{Label: f.Label, Category: f.Category, Tone: f.Tone, AnchorX: f.AnchorX, AnchorY: f.AnchorY})
	}
	// options 的参考媒体由后续渲染链补齐；诊断文字阶段只给方向。
	options := make([]domain.DiagnosisOption, 0, len(p.Options))
	for _, o := range p.Options {
		options = append(options, domain.DiagnosisOption{Name: o.Name, Note: o.Note, Reason: o.Reason, Tags: o.Tags})
	}
	return diagnostic.DiagnosticOutput{
		Conclusion: p.Conclusion, PriorityTitle: p.PriorityTitle, PriorityCopy: p.PriorityCopy,
		Tags: p.Tags, Findings: findings, Options: options,
	}
}

var diagnosticTones = map[string]bool{"positive": true, "improve": true, "optional": true, "caution": true}

func validateDiagnosticPayload(data []byte) error {
	var payload diagnosticPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	if !aiSafeText(payload.Conclusion) || !aiSafeText(payload.PriorityTitle) || !aiSafeText(payload.PriorityCopy) {
		return errors.New("diagnostic provider output is incomplete or unsafe")
	}
	for _, f := range payload.Findings {
		if !aiSafeText(f.Label) || !diagnosticTones[f.Tone] {
			return errors.New("diagnostic provider output has invalid finding")
		}
	}
	for _, o := range payload.Options {
		if !aiSafeText(o.Name) {
			return errors.New("diagnostic provider output has invalid option")
		}
	}
	return nil
}

func diagnosticSchema() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false,
		"required": []string{"conclusion", "priority_title", "priority_copy", "tags", "findings", "options"},
		"properties": map[string]any{
			"conclusion":     map[string]any{"type": "string", "minLength": 1, "maxLength": 120},
			"priority_title": map[string]any{"type": "string", "minLength": 1, "maxLength": 40},
			"priority_copy":  map[string]any{"type": "string", "minLength": 1, "maxLength": 160},
			"tags":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 6},
			"findings": map[string]any{"type": "array", "maxItems": 6, "items": map[string]any{
				"type": "object", "additionalProperties": false, "required": []string{"label", "category", "tone"},
				"properties": map[string]any{
					"label":    map[string]any{"type": "string", "minLength": 1, "maxLength": 40},
					"category": map[string]any{"type": "string"},
					"tone":     map[string]any{"type": "string", "enum": []string{"positive", "improve", "optional", "caution"}},
					"anchor_x": map[string]any{"type": "number", "minimum": 0, "maximum": 1},
					"anchor_y": map[string]any{"type": "number", "minimum": 0, "maximum": 1},
				},
			}},
			"options": map[string]any{"type": "array", "maxItems": 4, "items": map[string]any{
				"type": "object", "additionalProperties": false, "required": []string{"name"},
				"properties": map[string]any{
					"name":   map[string]any{"type": "string", "minLength": 1, "maxLength": 60},
					"note":   map[string]any{"type": "string", "maxLength": 120},
					"reason": map[string]any{"type": "string", "maxLength": 160},
					"tags":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 6},
				},
			}},
		},
	}
}

func diagnosticPrompt(request diagnostic.DiagnosticRequest) string {
	parts := []string{fmt.Sprintf("诊断类型是%s", request.Kind)}
	if request.Scene != "" {
		parts[0] += "，场景是" + request.Scene
	}
	parts[0] += "。请给出一句话判断（conclusion）、一条最高优先级建议（priority_title + priority_copy）、若干观察点和可选方向。"
	if request.Report != nil {
		labels := make([]string, 0, len(request.Report.Findings))
		for _, f := range request.Report.Findings {
			labels = append(labels, f.Label)
		}
		parts = append(parts, fmt.Sprintf("用户已有形象档案：印象标签为%s；当前最高优先级是「%s」；可提升点包括：%s。诊断要与档案方向保持连续。",
			joinTags(request.Report.ImpressionTags), request.Report.PriorityTitle, strings.Join(labels, "、")))
	}
	if len(request.Wardrobe) > 0 {
		items := make([]string, 0, len(request.Wardrobe))
		for _, item := range request.Wardrobe {
			items = append(items, item.Name)
		}
		parts = append(parts, "用户衣橱已有："+strings.Join(items, "、")+"。建议优先用已有单品。")
	}
	return strings.Join(parts, "\n")
}

func joinTags(tags []string) string {
	out := ""
	for i, tag := range tags {
		if i > 0 {
			out += "、"
		}
		out += tag
	}
	return out
}
