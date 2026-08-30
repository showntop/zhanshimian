package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/example/jianwo/server/internal/domain"
)

type OutfitAdvisor interface {
	Diagnose(context.Context, domain.ToolInput) (domain.ToolResult, error)
}

type DemoOutfitAdvisor struct{}

func NewDemoOutfitAdvisor() *DemoOutfitAdvisor { return &DemoOutfitAdvisor{} }

func (*DemoOutfitAdvisor) Diagnose(_ context.Context, input domain.ToolInput) (domain.ToolResult, error) {
	sceneNames := map[string]string{"general": "当前场景", "daily": "日常", "interview": "面试", "wedding": "婚礼", "date": "约会"}
	return domain.ToolResult{
		Kind: "outfit", Scene: input.Scene, Conclusion: "整体方向对了，先改一处",
		PriorityTitle: "把深色内搭换成象牙白",
		PriorityCopy:  fmt.Sprintf("不换整套衣服，就能让上半身更轻盈，也更适合%s。", sceneNames[input.Scene]),
		Tags:          []string{"预计 3 分钟", "无需购买新衣", "变化明显"}, ProviderVersion: "demo-outfit-v1",
		Findings: []domain.ToolFinding{
			{Label: "上身配色偏沉", Category: "color", Tone: "improve", AnchorX: .34, AnchorY: .38},
			{Label: "肩线不够清晰", Category: "silhouette", Tone: "improve", AnchorX: .67, AnchorY: .30},
			{Label: "腰线可以上移", Category: "proportion", Tone: "optional", AnchorX: .45, AnchorY: .64},
		},
	}, nil
}

type OpenAIOutfitAdvisor struct {
	config OpenAIConfig
	loader MediaLoader
	client *http.Client
}

func NewOpenAIOutfitAdvisor(config OpenAIConfig, loader MediaLoader, client *http.Client) (*OpenAIOutfitAdvisor, error) {
	if strings.TrimSpace(config.APIKey) == "" {
		return nil, errors.New("OPENAI_API_KEY is required for openai outfit diagnosis provider")
	}
	if loader == nil {
		return nil, errors.New("media loader is required for outfit diagnosis provider")
	}
	if config.BaseURL == "" {
		config.BaseURL = "https://api.openai.com/v1"
	}
	if config.Model == "" {
		config.Model = "gpt-5-mini"
	}
	if config.Timeout <= 0 {
		config.Timeout = 60 * time.Second
	}
	if client == nil {
		client = &http.Client{Timeout: config.Timeout}
	}
	return &OpenAIOutfitAdvisor{config: config, loader: loader, client: client}, nil
}

type outfitPayload struct {
	Conclusion    string          `json:"conclusion"`
	PriorityTitle string          `json:"priority_title"`
	PriorityCopy  string          `json:"priority_copy"`
	Tags          []string        `json:"tags"`
	Findings      []outfitFinding `json:"findings"`
}

type outfitFinding struct {
	Label    string  `json:"label"`
	Category string  `json:"category"`
	Tone     string  `json:"tone"`
	AnchorX  float64 `json:"anchor_x"`
	AnchorY  float64 `json:"anchor_y"`
}

func (a *OpenAIOutfitAdvisor) Diagnose(ctx context.Context, input domain.ToolInput) (domain.ToolResult, error) {
	images, err := a.loader.Load(ctx, []string{input.MediaID})
	if err != nil {
		return domain.ToolResult{}, fmt.Errorf("load outfit photo: %w", err)
	}
	if len(images) != 1 {
		return domain.ToolResult{}, fmt.Errorf("expected one outfit photo")
	}
	content := []map[string]any{
		{"type": "input_text", "text": outfitPrompt(input)},
		{"type": "input_image", "detail": "high", "image_url": dataURL(images[0].MIMEType, images[0].Data)},
	}
	body := map[string]any{
		"model": a.config.Model, "store": false,
		"instructions":      "你是审慎、尊重用户的私人穿搭顾问。只分析照片中可见的服装颜色、廓形、比例、材质和搭配关系；不评价身材，不推断年龄、健康、身份或经济状况。优先给出无需购买新衣的调整建议。",
		"input":             []map[string]any{{"role": "user", "content": content}},
		"text":              map[string]any{"format": map[string]any{"type": "json_schema", "name": "outfit_diagnosis", "strict": true, "schema": outfitSchema()}},
		"max_output_tokens": 1800,
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return domain.ToolResult{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(a.config.BaseURL, "/")+"/responses", bytes.NewReader(encoded))
	if err != nil {
		return domain.ToolResult{}, err
	}
	request.Header.Set("Authorization", "Bearer "+a.config.APIKey)
	request.Header.Set("Content-Type", "application/json")
	response, err := a.client.Do(request)
	if err != nil {
		return domain.ToolResult{}, fmt.Errorf("outfit diagnosis request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return domain.ToolResult{}, fmt.Errorf("outfit diagnosis returned status %d", response.StatusCode)
	}
	var apiResponse responsesAPIResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&apiResponse); err != nil {
		return domain.ToolResult{}, fmt.Errorf("decode outfit diagnosis: %w", err)
	}
	if apiResponse.Error != nil || (apiResponse.Status != "" && apiResponse.Status != "completed") {
		return domain.ToolResult{}, fmt.Errorf("outfit diagnosis did not complete")
	}
	outputText, refusal := responseText(apiResponse)
	if refusal != "" || outputText == "" {
		return domain.ToolResult{}, fmt.Errorf("outfit diagnosis contained no result")
	}
	var payload outfitPayload
	if err := json.Unmarshal([]byte(outputText), &payload); err != nil {
		return domain.ToolResult{}, fmt.Errorf("decode structured outfit diagnosis: %w", err)
	}
	if err := validateOutfitPayload(payload); err != nil {
		return domain.ToolResult{}, err
	}
	result := domain.ToolResult{
		Kind: "outfit", Scene: input.Scene, Conclusion: payload.Conclusion, PriorityTitle: payload.PriorityTitle,
		PriorityCopy: payload.PriorityCopy, Tags: payload.Tags, ProviderVersion: "openai-responses-outfit:" + a.config.Model,
	}
	for _, finding := range payload.Findings {
		result.Findings = append(result.Findings, domain.ToolFinding{Label: finding.Label, Category: finding.Category, Tone: finding.Tone, AnchorX: finding.AnchorX, AnchorY: finding.AnchorY})
	}
	return result, nil
}

func validateOutfitPayload(payload outfitPayload) error {
	return validateDiagnosisPayload(payload, map[string]bool{"improve": true, "positive": true, "optional": true})
}

func validatePurchasePayload(payload outfitPayload) error {
	return validateDiagnosisPayload(payload, map[string]bool{"positive": true, "caution": true, "optional": true})
}

func validateDiagnosisPayload(payload outfitPayload, tones map[string]bool) error {
	if !safeText(payload.Conclusion) || !safeText(payload.PriorityTitle) || !safeText(payload.PriorityCopy) || len(payload.Tags) != 3 || len(payload.Findings) != 3 {
		return fmt.Errorf("outfit provider output is incomplete or unsafe")
	}
	categories := map[string]bool{"color": true, "silhouette": true, "proportion": true, "fabric": true, "styling": true}
	for _, tag := range payload.Tags {
		if !safeText(tag) {
			return fmt.Errorf("outfit provider output contains unsafe tag")
		}
	}
	for _, finding := range payload.Findings {
		if !safeText(finding.Label) || !categories[finding.Category] || !tones[finding.Tone] || finding.AnchorX < 0 || finding.AnchorX > 1 || finding.AnchorY < 0 || finding.AnchorY > 1 {
			return fmt.Errorf("outfit provider output contains invalid finding")
		}
	}
	return nil
}

func outfitSchema() map[string]any {
	return diagnosisSchema([]string{"improve", "positive", "optional"})
}

func purchaseSchema() map[string]any {
	return diagnosisSchema([]string{"positive", "caution", "optional"})
}

func diagnosisSchema(tones []string) map[string]any {
	finding := map[string]any{"type": "object", "additionalProperties": false, "required": []string{"label", "category", "tone", "anchor_x", "anchor_y"}, "properties": map[string]any{
		"label":    map[string]any{"type": "string", "minLength": 1, "maxLength": 36},
		"category": map[string]any{"type": "string", "enum": []string{"color", "silhouette", "proportion", "fabric", "styling"}},
		"tone":     map[string]any{"type": "string", "enum": tones},
		"anchor_x": map[string]any{"type": "number", "minimum": 0, "maximum": 1}, "anchor_y": map[string]any{"type": "number", "minimum": 0, "maximum": 1},
	}}
	return map[string]any{"type": "object", "additionalProperties": false,
		"required": []string{"conclusion", "priority_title", "priority_copy", "tags", "findings"},
		"properties": map[string]any{
			"conclusion":     map[string]any{"type": "string", "minLength": 1, "maxLength": 40},
			"priority_title": map[string]any{"type": "string", "minLength": 1, "maxLength": 48},
			"priority_copy":  map[string]any{"type": "string", "minLength": 1, "maxLength": 120},
			"tags":           map[string]any{"type": "array", "minItems": 3, "maxItems": 3, "items": map[string]any{"type": "string", "minLength": 1, "maxLength": 24}},
			"findings":       map[string]any{"type": "array", "minItems": 3, "maxItems": 3, "items": finding},
		},
	}
}

func outfitPrompt(input domain.ToolInput) string {
	scenes := map[string]string{"general": "通用", "daily": "日常", "interview": "面试", "wedding": "婚礼", "date": "约会"}
	return "请诊断这张" + scenes[input.Scene] + "场景的正面全身穿搭照。只指出三处可见的颜色、廓形、比例、材质或搭配信息，并选出最值得先改的一处。建议优先利用现有衣服，通过卷袖、塞衣角、换内搭、调整腰线、配色或鞋包完成，不评价人的身体。" + toolContextPrompt(input.Context)
}

func toolContextPrompt(context *domain.ToolContext) string {
	if context == nil {
		return ""
	}
	parts := make([]string, 0, 2)
	if context.PriorityTitle != "" {
		parts = append(parts, fmt.Sprintf("用户已有形象档案：当前印象标签为%s；既有最高优先级是%s（%s）。把它作为搭配连续性的参考，但若与本次图片证据冲突，以本次可见证据为准。", strings.Join(context.ImpressionTags, "、"), context.PriorityTitle, context.PriorityCopy))
	}
	if len(context.Wardrobe) > 0 {
		items := make([]string, 0, len(context.Wardrobe))
		for _, item := range context.Wardrobe {
			items = append(items, item.Color+item.Name+"（"+item.Category+"）")
		}
		parts = append(parts, "用户已录入的衣橱单品只有："+strings.Join(items, "、")+"。搭配建议优先引用这些真实单品；没有录入的单品只能标为可选基础款，不得假装用户已经拥有。")
	}
	if len(parts) == 0 {
		return ""
	}
	return "\n" + strings.Join(parts, "\n")
}

type FallbackOutfitAdvisor struct{ primary, fallback OutfitAdvisor }

func NewFallbackOutfitAdvisor(primary, fallback OutfitAdvisor) *FallbackOutfitAdvisor {
	return &FallbackOutfitAdvisor{primary: primary, fallback: fallback}
}

func (a *FallbackOutfitAdvisor) Diagnose(ctx context.Context, input domain.ToolInput) (domain.ToolResult, error) {
	result, err := a.primary.Diagnose(ctx, input)
	if err == nil {
		return result, nil
	}
	return a.fallback.Diagnose(ctx, input)
}
