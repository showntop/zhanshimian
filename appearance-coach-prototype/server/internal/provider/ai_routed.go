package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/example/jianwo/server/internal/domain"
)

const appearanceInstructions = "你是审慎、尊重用户的私人形象顾问。只分析照片中可见的造型、比例、色彩和轮廓，不评价颜值，不推断健康、族裔、年龄、人格或社会身份。所有建议必须具体、温和、可执行。"

type RoutedAnalyzer struct {
	runtime *AIRuntime
	loader  MediaLoader
}

func NewRoutedAnalyzer(runtime *AIRuntime, loader MediaLoader) (*RoutedAnalyzer, error) {
	if runtime == nil || !runtime.HasRoute(CapabilityAppearanceAnalysis) {
		return nil, errors.New("appearance analysis AI route is required")
	}
	if loader == nil {
		return nil, errors.New("media loader is required")
	}
	return &RoutedAnalyzer{runtime: runtime, loader: loader}, nil
}

func (a *RoutedAnalyzer) Analyze(ctx context.Context, input domain.CreateAnalysisInput) (domain.AnalysisOutput, error) {
	images, err := a.loader.Load(ctx, input.MediaIDs)
	if err != nil {
		return domain.AnalysisOutput{}, fmt.Errorf("load analysis photos: %w", err)
	}
	if len(images) != 3 {
		return domain.AnalysisOutput{}, fmt.Errorf("expected three analysis photos, got %d", len(images))
	}
	if a.runtime.HasRoute(CapabilityPhotoCheck) {
		if err := a.checkPhotos(ctx, images); err != nil {
			return domain.AnalysisOutput{}, err
		}
	}
	result, err := a.runtime.Structured(ctx, CapabilityAppearanceAnalysis, StructuredRequest{
		Instructions: appearanceInstructions, Prompt: analysisPrompt(input), Images: images,
		SchemaName: CapabilityAppearanceAnalysis, Schema: analysisSchema(), MaxOutputTokens: 6000,
		Validate: func(data []byte) error {
			var candidate analysisPayload
			if err := json.Unmarshal(data, &candidate); err != nil {
				return err
			}
			return validateAnalysisPayload(candidate)
		},
	})
	if err != nil {
		return domain.AnalysisOutput{}, err
	}
	var payload analysisPayload
	if err := json.Unmarshal(result.JSON, &payload); err != nil {
		return domain.AnalysisOutput{}, fmt.Errorf("decode structured analysis: %w", err)
	}
	if err := validateAnalysisPayload(payload); err != nil {
		return domain.AnalysisOutput{}, err
	}
	return payload.toDomain(images, result.Meta.ProviderVersion())
}

type RoutedToolAdvisor struct {
	runtime    *AIRuntime
	loader     MediaLoader
	capability string
	kind       string
}

func NewRoutedOutfitAdvisor(runtime *AIRuntime, loader MediaLoader) (*RoutedToolAdvisor, error) {
	return newRoutedToolAdvisor(runtime, loader, CapabilityOutfitDiagnosis, "outfit")
}

func NewRoutedPurchaseAdvisor(runtime *AIRuntime, loader MediaLoader) (*RoutedToolAdvisor, error) {
	return newRoutedToolAdvisor(runtime, loader, CapabilityPurchaseDiagnosis, "purchase")
}

func newRoutedToolAdvisor(runtime *AIRuntime, loader MediaLoader, capability, kind string) (*RoutedToolAdvisor, error) {
	if runtime == nil || !runtime.HasRoute(capability) {
		return nil, fmt.Errorf("%s AI route is required", capability)
	}
	if loader == nil {
		return nil, errors.New("media loader is required")
	}
	return &RoutedToolAdvisor{runtime: runtime, loader: loader, capability: capability, kind: kind}, nil
}

func (a *RoutedToolAdvisor) Diagnose(ctx context.Context, input domain.ToolInput) (domain.ToolResult, error) {
	images, err := a.loader.Load(ctx, []string{input.MediaID})
	if err != nil {
		return domain.ToolResult{}, fmt.Errorf("load %s photo: %w", a.kind, err)
	}
	if len(images) != 1 {
		return domain.ToolResult{}, fmt.Errorf("expected one %s photo", a.kind)
	}
	instructions := "你是审慎、尊重用户的私人穿搭顾问。只分析照片中可见的服装颜色、廓形、比例、材质和搭配关系；不评价身体，不推断年龄、健康、身份或经济状况。优先给出无需购买新衣的调整建议。"
	prompt := outfitPrompt(input)
	schema := outfitSchema()
	validator := validateOutfitPayload
	if a.kind == "purchase" {
		instructions = "你是克制的购买决策顾问。只依据商品图片中可见的颜色、版型、材质和适用场景判断；不编造品牌、价格、成分或尺寸，不制造购买焦虑。建议必须说明适合条件、搭配方法和不建议购买的情况。"
		prompt = purchasePrompt(input)
		schema = purchaseSchema()
		validator = validatePurchasePayload
	}
	result, err := a.runtime.Structured(ctx, a.capability, StructuredRequest{
		Instructions: instructions, Prompt: prompt, Images: images,
		SchemaName: a.capability, Schema: schema, MaxOutputTokens: 1800,
		Validate: func(data []byte) error {
			var candidate outfitPayload
			if err := json.Unmarshal(data, &candidate); err != nil {
				return err
			}
			return validator(candidate)
		},
	})
	if err != nil {
		return domain.ToolResult{}, err
	}
	var payload outfitPayload
	if err := json.Unmarshal(result.JSON, &payload); err != nil {
		return domain.ToolResult{}, fmt.Errorf("decode structured %s diagnosis: %w", a.kind, err)
	}
	if err := validator(payload); err != nil {
		return domain.ToolResult{}, err
	}
	output := domain.ToolResult{
		Kind: a.kind, Scene: input.Scene, Conclusion: payload.Conclusion,
		PriorityTitle: payload.PriorityTitle, PriorityCopy: payload.PriorityCopy,
		Tags: payload.Tags, ProviderVersion: result.Meta.ProviderVersion(),
	}
	for _, finding := range payload.Findings {
		output.Findings = append(output.Findings, domain.ToolFinding{Label: finding.Label, Category: finding.Category, Tone: finding.Tone, AnchorX: finding.AnchorX, AnchorY: finding.AnchorY})
	}
	return output, nil
}

func purchasePrompt(input domain.ToolInput) string {
	scenes := map[string]string{"general": "通用", "daily": "日常", "interview": "面试", "wedding": "婚礼", "date": "约会"}
	return "请判断图片中的商品是否值得用于用户的" + scenes[input.Scene] + "场景。给出一个明确但有条件的结论，指出三处可见依据，并选出最重要的购买建议。不要猜测图片中看不到的品牌、面料成分或价格；建议如何与现有基础款组合，并说明何种情况下不建议买。" + toolContextPrompt(input.Context)
}

type RoutedHairGenerator struct {
	runtime *AIRuntime
	loader  MediaLoader
}

func NewRoutedHairGenerator(runtime *AIRuntime, loader MediaLoader) (*RoutedHairGenerator, error) {
	if runtime == nil || !runtime.HasRoute(CapabilityHairEdit) {
		return nil, errors.New("hair edit AI route is required")
	}
	if loader == nil {
		return nil, errors.New("media loader is required")
	}
	return &RoutedHairGenerator{runtime: runtime, loader: loader}, nil
}

func (g *RoutedHairGenerator) Generate(ctx context.Context, input domain.HairPreviewInput) (HairPreviewOutput, error) {
	images, err := g.loader.Load(ctx, []string{input.MediaID})
	if err != nil {
		return HairPreviewOutput{}, fmt.Errorf("load hair source photo: %w", err)
	}
	if len(images) != 1 {
		return HairPreviewOutput{}, errors.New("expected one hair source photo")
	}
	result, err := g.runtime.EditImage(ctx, CapabilityHairEdit, ImageEditRequest{Prompt: hairPrompt(input.StyleID), Images: images, Size: "1024*1536", Quality: "high"})
	if err != nil {
		return HairPreviewOutput{}, err
	}
	return HairPreviewOutput{ImageData: result.Data, MIMEType: result.MIMEType, ProviderVersion: result.Meta.ProviderVersion()}, nil
}

type AdvisorChat interface {
	Reply(context.Context, string, any) (string, error)
}

type RoutedAdvisorChat struct{ runtime *AIRuntime }

func NewRoutedAdvisorChat(runtime *AIRuntime) (*RoutedAdvisorChat, error) {
	if runtime == nil || !runtime.HasRoute(CapabilityAdvisorChat) {
		return nil, errors.New("advisor chat AI route is required")
	}
	return &RoutedAdvisorChat{runtime: runtime}, nil
}

func (a *RoutedAdvisorChat) Reply(ctx context.Context, question string, grounding any) (string, error) {
	contextJSON, err := json.Marshal(grounding)
	if err != nil {
		return "", err
	}
	schema := map[string]any{"type": "object", "additionalProperties": false, "required": []string{"reply"}, "properties": map[string]any{"reply": map[string]any{"type": "string", "minLength": 1, "maxLength": 500}}}
	result, err := a.runtime.Structured(ctx, CapabilityAdvisorChat, StructuredRequest{
		Instructions: "你是用户的私人形象顾问。只能使用提供的形象报告、今日方案和衣橱信息回答；如果资料不足，明确说明需要什么，不要编造用户拥有的单品。回答中文、简洁、具体、可执行，不评价颜值和身体。",
		Prompt:       "用户问题：" + question + "\n可用上下文：" + string(contextJSON), SchemaName: CapabilityAdvisorChat, Schema: schema, MaxOutputTokens: 800,
		Validate: func(data []byte) error {
			var candidate struct {
				Reply string `json:"reply"`
			}
			if err := json.Unmarshal(data, &candidate); err != nil {
				return err
			}
			candidate.Reply = strings.TrimSpace(candidate.Reply)
			if candidate.Reply == "" || len([]rune(candidate.Reply)) > 500 {
				return errors.New("advisor model returned an invalid reply")
			}
			return nil
		},
	})
	if err != nil {
		return "", err
	}
	var payload struct {
		Reply string `json:"reply"`
	}
	if err := json.Unmarshal(result.JSON, &payload); err != nil {
		return "", err
	}
	payload.Reply = strings.TrimSpace(payload.Reply)
	if payload.Reply == "" || len([]rune(payload.Reply)) > 500 {
		return "", errors.New("advisor model returned an invalid reply")
	}
	return payload.Reply, nil
}
