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

const diagnosticInstructions = "你是审慎、尊重用户的穿搭与购买顾问。基于形象报告、用户档案和照片给出一句话判断、一条最高优先级建议和若干观察点；只描述看得到的事实，建议具体可执行。不用缺陷、严重度等评判词；不评价颜值和身体；不编造用户没有的单品；语气保持尊重（positive=适合保留，improve=可提升，optional=可参考，caution=注意）。" +
	"先做照片门禁：照片里看不到可诊断的主体时 decision=reject 并给出 reason_code，其余字段一律输出空字符串或空数组，绝不硬答；照片可诊断时 decision=pass，reason_code 输出空字符串。" +
	"穿搭诊断的主体是单一真人的全身或大半身穿搭：看不到人、插画、截图、多人主导、只到腰部以上、过糊、过暗一律拒识；可诊断时观察点只落在服装、鞋包与配色（category 用 outfit 或 color），不评发型妆容。" +
	"购买判断的主体是清晰可见的物品本身：看不清物品、插画、截图、过糊、过暗一律拒识；可诊断时观察点只落在物品与搭配方向（category 用 item、outfit 或 color）。"

func (d *StructuredDiagnostic) Diagnose(ctx context.Context, request diagnostic.DiagnosticRequest) (diagnostic.DiagnosticOutput, error) {
	capability := CapabilityOutfitDiagnosis
	if request.Kind == "purchase" {
		capability = CapabilityPurchaseDiagnosis
	}
	// 锚点（anchor_x/y）必须锚在真实照片上：源照片随请求发给视觉模型
	// （outfit_diagnosis/purchase_diagnosis 路由支持图输入），无图时宁可失败
	// 也不让模型凭空编造锚点（来源真实性红线）。
	images := make([]ImageInput, 0, len(request.Images))
	for _, image := range request.Images {
		images = append(images, ImageInput{
			AssetID: image.AssetID, Role: image.Role, MIMEType: image.MIMEType, Data: image.Data,
		})
	}
	result, err := d.runtime.Structured(ctx, StructuredRequest{
		Capability:      capability,
		Instructions:    diagnosticInstructions,
		Prompt:          diagnosticPrompt(request),
		Images:          images,
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
	Decision      string                 `json:"decision"`
	ReasonCode    string                 `json:"reason_code"`
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
		Rejected: p.Decision == "reject", ReasonCode: p.ReasonCode,
	}
}

var diagnosticTones = map[string]bool{"positive": true, "improve": true, "optional": true, "caution": true}

var diagnosticRejectReasons = map[string]bool{
	"no_person": true, "no_product": true, "illustration": true, "screenshot": true,
	"multiple_people": true, "body_not_head_to_calf": true, "too_blurry": true, "too_dark": true,
}

func validateDiagnosticPayload(data []byte) error {
	var payload diagnosticPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	if payload.Decision != "pass" && payload.Decision != "reject" {
		return errors.New("diagnostic provider output has invalid decision")
	}
	// 拒识分支：reason_code 必须合法，内容字段本就为空，不做内容校验
	if payload.Decision == "reject" {
		if !diagnosticRejectReasons[payload.ReasonCode] {
			return errors.New("diagnostic provider output has invalid reject reason")
		}
		return nil
	}
	if payload.ReasonCode != "" {
		return errors.New("diagnostic provider output has unexpected reason_code")
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
		"required": []string{"decision", "reason_code", "conclusion", "priority_title", "priority_copy", "tags", "findings", "options"},
		"properties": map[string]any{
			"decision":    map[string]any{"type": "string", "enum": []string{"pass", "reject"}},
			"reason_code": map[string]any{"type": "string", "enum": []string{"", "no_person", "no_product", "illustration", "screenshot", "multiple_people", "body_not_head_to_calf", "too_blurry", "too_dark"}},
			// 内容字段允许空串：门禁拒识（decision=reject）时它们本就为空
			"conclusion":     map[string]any{"type": "string", "maxLength": 120},
			"priority_title": map[string]any{"type": "string", "maxLength": 40},
			"priority_copy":  map[string]any{"type": "string", "maxLength": 160},
			"tags":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 6},
			"findings": map[string]any{"type": "array", "maxItems": 6, "items": map[string]any{
				"type": "object", "additionalProperties": false, "required": []string{"label", "category", "tone"},
				"properties": map[string]any{
					"label":    map[string]any{"type": "string", "minLength": 1, "maxLength": 40},
					"category": map[string]any{"type": "string", "enum": []string{"outfit", "color", "item"}},
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
