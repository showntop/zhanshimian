package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/example/jianwo/server/internal/domain"
)

type OpenAIConfig struct {
	APIKey  string
	BaseURL string
	Model   string
	Timeout time.Duration
}

type OpenAIAnalyzer struct {
	config OpenAIConfig
	loader MediaLoader
	client *http.Client
}

func NewOpenAIAnalyzer(config OpenAIConfig, loader MediaLoader, client *http.Client) (*OpenAIAnalyzer, error) {
	if strings.TrimSpace(config.APIKey) == "" {
		return nil, errors.New("OPENAI_API_KEY is required for openai provider")
	}
	if loader == nil {
		return nil, errors.New("media loader is required for openai provider")
	}
	if config.BaseURL == "" {
		config.BaseURL = "https://api.openai.com/v1"
	}
	if config.Model == "" {
		config.Model = "gpt-5-mini"
	}
	if config.Timeout <= 0 {
		config.Timeout = 90 * time.Second
	}
	if client == nil {
		client = &http.Client{Timeout: config.Timeout}
	}
	return &OpenAIAnalyzer{config: config, loader: loader, client: client}, nil
}

type responseContent struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	Refusal string `json:"refusal"`
}

type responsesAPIResponse struct {
	Status string `json:"status"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
	Output []struct {
		Type    string            `json:"type"`
		Content []responseContent `json:"content"`
	} `json:"output"`
}

type analysisDetail struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type analysisStep struct {
	Category string           `json:"category"`
	Title    string           `json:"title"`
	Summary  string           `json:"summary"`
	Details  []analysisDetail `json:"details"`
}

type analysisPlan struct {
	Name           string         `json:"name"`
	Slug           string         `json:"slug"`
	Recommended    bool           `json:"recommended"`
	Descriptor     string         `json:"descriptor"`
	Why            string         `json:"why"`
	OutcomeTags    []string       `json:"outcome_tags"`
	DifferenceTags []string       `json:"difference_tags"`
	Steps          []analysisStep `json:"steps"`
}

type analysisFinding struct {
	Label    string  `json:"label"`
	Category string  `json:"category"`
	Severity string  `json:"severity"`
	AnchorX  float64 `json:"anchor_x"`
	AnchorY  float64 `json:"anchor_y"`
}

type analysisPayload struct {
	ImpressionTags []string          `json:"impression_tags"`
	PriorityTitle  string            `json:"priority_title"`
	PriorityCopy   string            `json:"priority_copy"`
	Findings       []analysisFinding `json:"findings"`
	Plans          []analysisPlan    `json:"plans"`
}

func (a *OpenAIAnalyzer) Analyze(ctx context.Context, input domain.CreateAnalysisInput) (domain.AnalysisOutput, error) {
	images, err := a.loader.Load(ctx, input.MediaIDs)
	if err != nil {
		return domain.AnalysisOutput{}, fmt.Errorf("load analysis photos: %w", err)
	}
	if len(images) != 3 {
		return domain.AnalysisOutput{}, fmt.Errorf("expected three analysis photos, got %d", len(images))
	}
	content := []map[string]any{{"type": "input_text", "text": analysisPrompt(input)}}
	for _, image := range images {
		content = append(content,
			map[string]any{"type": "input_text", "text": "照片类型：" + photoKindName(image.Kind)},
			map[string]any{"type": "input_image", "detail": "high", "image_url": dataURL(image.MIMEType, image.Data)},
		)
	}
	body := map[string]any{
		"model":        a.config.Model,
		"store":        false,
		"instructions": "你是审慎、尊重用户的私人形象顾问。只分析照片中可见的造型、比例、色彩和轮廓，不评价颜值，不推断健康、族裔、年龄、人格或社会身份。所有建议必须具体、温和、可执行。",
		"input":        []map[string]any{{"role": "user", "content": content}},
		"text": map[string]any{"format": map[string]any{
			"type": "json_schema", "name": "appearance_analysis", "strict": true, "schema": analysisSchema(),
		}},
		"max_output_tokens": 6000,
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return domain.AnalysisOutput{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(a.config.BaseURL, "/")+"/responses", bytes.NewReader(encoded))
	if err != nil {
		return domain.AnalysisOutput{}, err
	}
	request.Header.Set("Authorization", "Bearer "+a.config.APIKey)
	request.Header.Set("Content-Type", "application/json")
	response, err := a.client.Do(request)
	if err != nil {
		return domain.AnalysisOutput{}, fmt.Errorf("vision request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return domain.AnalysisOutput{}, fmt.Errorf("vision request returned status %d", response.StatusCode)
	}
	var apiResponse responsesAPIResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&apiResponse); err != nil {
		return domain.AnalysisOutput{}, fmt.Errorf("decode vision response: %w", err)
	}
	if apiResponse.Error != nil {
		return domain.AnalysisOutput{}, fmt.Errorf("vision response failed")
	}
	if apiResponse.Status != "" && apiResponse.Status != "completed" {
		return domain.AnalysisOutput{}, fmt.Errorf("vision response did not complete")
	}
	outputText, refusal := responseText(apiResponse)
	if refusal != "" {
		return domain.AnalysisOutput{}, fmt.Errorf("vision request was refused")
	}
	if outputText == "" {
		return domain.AnalysisOutput{}, fmt.Errorf("vision response contained no structured output")
	}
	var payload analysisPayload
	if err := json.Unmarshal([]byte(outputText), &payload); err != nil {
		return domain.AnalysisOutput{}, fmt.Errorf("decode structured analysis: %w", err)
	}
	if err := validateAnalysisPayload(payload); err != nil {
		return domain.AnalysisOutput{}, err
	}
	return payload.toDomain(images, "openai-responses:"+a.config.Model)
}

func responseText(response responsesAPIResponse) (string, string) {
	for _, item := range response.Output {
		if item.Type != "message" {
			continue
		}
		for _, content := range item.Content {
			if content.Type == "refusal" && content.Refusal != "" {
				return "", content.Refusal
			}
			if content.Type == "output_text" && content.Text != "" {
				return content.Text, ""
			}
		}
	}
	return "", ""
}

func analysisPrompt(input domain.CreateAnalysisInput) string {
	return fmt.Sprintf(`请根据依次提供的正脸、侧脸和全身照，为用户生成中文形象分析。
场景：%s；职业：%s；身高：%d cm；预算：%s。
	只描述能从照片直接观察到的发型重心、眉眼对比、头肩比例、服装轮廓和配色。不要给出颜值或身材评分，不推断敏感属性。
	输出 3 个当前印象标签、4 个可提升点、一个最优先建议，以及严格 3 套方案。每套方案必须包含 hair、makeup、outfit 三个步骤和可执行细节。顶层必须输出一个 JSON 对象，不要输出数组、Markdown 或额外解释。
	每个可提升点必须包含 label、category（只能填 hair、makeup、outfit、color 之一）、severity（只能填 low、medium、high）、anchor_x 和 anchor_y（0 到 1 之间的小数，表示在照片上的相对位置，不要用百分比或像素）。3 套方案的 slug 必须分别是 sharp、warm、natural，且恰好一套 recommended 为 true。`, input.Scene, input.Profile.Role, input.Profile.HeightCM, input.Profile.Budget)
}

func photoKindName(kind string) string {
	names := map[string]string{"face": "正脸", "side": "侧脸", "body": "全身"}
	if value := names[kind]; value != "" {
		return value
	}
	return kind
}

func dataURL(mimeType string, data []byte) string {
	if mimeType == "" {
		mimeType = "image/jpeg"
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func (payload analysisPayload) toDomain(images []AnalysisImage, providerVersion string) (domain.AnalysisOutput, error) {
	currentURL := ""
	for _, image := range images {
		if currentURL == "" || image.Kind == "face" {
			currentURL = image.URL
		}
		if image.Kind == "face" {
			break
		}
	}
	output := domain.AnalysisOutput{
		CurrentImageURL: currentURL, ImpressionTags: payload.ImpressionTags,
		PriorityTitle: payload.PriorityTitle, PriorityCopy: payload.PriorityCopy,
		ProviderVersion: providerVersion,
	}
	for _, finding := range payload.Findings {
		output.Findings = append(output.Findings, domain.Finding{
			Label: finding.Label, Category: finding.Category, Severity: finding.Severity,
			AnchorX: finding.AnchorX, AnchorY: finding.AnchorY,
		})
	}
	imageURLs := map[string]string{"sharp": "/assets/looks/sharp.png", "warm": "/assets/looks/warm.png", "natural": "/assets/looks/natural.png"}
	for index, plan := range payload.Plans {
		domainPlan := domain.Plan{
			Name: plan.Name, Slug: plan.Slug, ImageURL: imageURLs[plan.Slug], Recommended: plan.Recommended,
			Descriptor: plan.Descriptor, Why: plan.Why, OutcomeTags: plan.OutcomeTags,
			DifferenceTags: plan.DifferenceTags, Sort: index + 1,
		}
		for stepIndex, step := range plan.Steps {
			details, err := json.Marshal(step.Details)
			if err != nil {
				return domain.AnalysisOutput{}, err
			}
			domainPlan.Steps = append(domainPlan.Steps, domain.PlanStep{
				Category: step.Category, Title: step.Title, Summary: step.Summary, Details: details, Sort: stepIndex + 1,
			})
		}
		output.Plans = append(output.Plans, domainPlan)
	}
	return output, nil
}

// preview trims model copy for log messages so validation errors stay one line.
func preview(value string) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) > 40 {
		return string(runes[:40]) + "…"
	}
	return string(runes)
}

func validateAnalysisPayload(payload analysisPayload) error {
	if len(payload.ImpressionTags) != 3 || len(payload.Findings) != 4 || len(payload.Plans) != 3 {
		return fmt.Errorf("provider output has invalid collection sizes: tags=%d findings=%d plans=%d, want 3/4/3", len(payload.ImpressionTags), len(payload.Findings), len(payload.Plans))
	}
	if !safeText(payload.PriorityTitle) || !safeText(payload.PriorityCopy) {
		return fmt.Errorf("provider output contains unsafe or empty priority copy: title=%q copy=%q", preview(payload.PriorityTitle), preview(payload.PriorityCopy))
	}
	allowedCategories := map[string]bool{"hair": true, "makeup": true, "outfit": true, "color": true}
	allowedSeverity := map[string]bool{"low": true, "medium": true, "high": true}
	for index, finding := range payload.Findings {
		if !safeText(finding.Label) || !allowedCategories[finding.Category] || !allowedSeverity[finding.Severity] || finding.AnchorX < 0 || finding.AnchorX > 1 || finding.AnchorY < 0 || finding.AnchorY > 1 {
			return fmt.Errorf("provider output contains invalid finding %d: category=%q severity=%q anchor=(%.2f,%.2f) label=%q", index+1, finding.Category, finding.Severity, finding.AnchorX, finding.AnchorY, preview(finding.Label))
		}
	}
	allowedSlugs := map[string]bool{"sharp": true, "warm": true, "natural": true}
	recommended := 0
	seenSlugs := map[string]bool{}
	for _, plan := range payload.Plans {
		if !allowedSlugs[plan.Slug] || seenSlugs[plan.Slug] || !safeText(plan.Name) || !safeText(plan.Descriptor) || !safeText(plan.Why) || len(plan.OutcomeTags) != 3 || len(plan.DifferenceTags) != 3 || len(plan.Steps) != 3 {
			return fmt.Errorf("provider output contains invalid plan %q: name=%q outcome_tags=%d difference_tags=%d steps=%d (want slug sharp/warm/natural unique, 3/3/3)", plan.Slug, preview(plan.Name), len(plan.OutcomeTags), len(plan.DifferenceTags), len(plan.Steps))
		}
		seenSlugs[plan.Slug] = true
		if plan.Recommended {
			recommended++
		}
		seenCategories := map[string]bool{}
		for _, step := range plan.Steps {
			if !map[string]bool{"hair": true, "makeup": true, "outfit": true}[step.Category] || seenCategories[step.Category] || !safeText(step.Title) || !safeText(step.Summary) || len(step.Details) < 2 || len(step.Details) > 4 {
				return fmt.Errorf("provider output contains invalid step in plan %q: category=%q details=%d (want hair/makeup/outfit unique, 2-4) title=%q", plan.Slug, step.Category, len(step.Details), preview(step.Title))
			}
			seenCategories[step.Category] = true
			for _, detail := range step.Details {
				if !safeText(detail.Label) || !safeText(detail.Value) {
					return fmt.Errorf("provider output contains unsafe or empty detail in plan %q step %q: label=%q value=%q", plan.Slug, step.Category, preview(detail.Label), preview(detail.Value))
				}
			}
		}
	}
	if recommended != 1 {
		return fmt.Errorf("provider output must recommend exactly one plan, got %d", recommended)
	}
	for _, tag := range payload.ImpressionTags {
		if !safeText(tag) {
			return fmt.Errorf("provider output contains invalid impression tag %q", preview(tag))
		}
	}
	return nil
}

func safeText(value string) bool {
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

func analysisSchema() map[string]any {
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
	finding := objectSchema(map[string]any{
		"label":    stringSchema,
		"category": map[string]any{"type": "string", "enum": []string{"hair", "makeup", "outfit", "color"}},
		"severity": map[string]any{"type": "string", "enum": []string{"low", "medium", "high"}},
		"anchor_x": map[string]any{"type": "number", "minimum": 0, "maximum": 1},
		"anchor_y": map[string]any{"type": "number", "minimum": 0, "maximum": 1},
	}, "label", "category", "severity", "anchor_x", "anchor_y")
	return objectSchema(map[string]any{
		"impression_tags": stringArray3,
		"priority_title":  stringSchema,
		"priority_copy":   stringSchema,
		"findings":        map[string]any{"type": "array", "items": finding, "minItems": 4, "maxItems": 4},
		"plans":           map[string]any{"type": "array", "items": plan, "minItems": 3, "maxItems": 3},
	}, "impression_tags", "priority_title", "priority_copy", "findings", "plans")
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": required}
}
