package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zhanshimian/server/internal/domain"
)

const (
	photoQualityInstructions = "你是严格的拍摄引导检查员。只判断三张照片是否符合声明的拍摄类型与清晰度，不评价人物外貌。"
	photoQualityPrompt       = `三张照片依次为 face、side、body。逐一判断是否为单一真人主体且符合角色：
- face：真实人物正对镜头，面部完整清晰，无严重遮挡；
- side：真实人物约 90° 侧脸，轮廓清晰；
- body：真实人物正面全身，至少覆盖头部到小腿。
风景、宠物、物品、截图、插画、多人主导或无法辨认人物的照片一律 reject。
decision 只能是 pass 或 reject；pass 时 reason_code 必须是空字符串；reject 时 reason_code 只能是 multiple_people、no_person、screenshot、illustration、pet、face_not_frontal、face_occluded、side_not_profile、body_not_head_to_calf、too_blurry、too_dark 之一。`

	identityInstructions = "你只判断三张照片是否为同一人，不评价外貌。"
	identityPrompt       = `三张照片依次为 face、side、body。判断是否为同一人。
decision 只能是 pass、reject 或 uncertain，并给出 0 到 1 的内部 confidence。不要输出外貌评分。`

	appearanceInstructions = `你是审慎、尊重用户的私人形象顾问。明确禁止外貌、身材、年龄、敏感属性评分，不推断健康、族裔、人格或社会身份。职业、预算、身高只能影响 recommendation，不得写入 visible_observation，也不得据此给外貌或身材打分。`

	appearancePrompt = `请根据依次提供的 face、side、body 三张原图生成中文形象分析。
只描述照片中可见的发型、妆容、服装轮廓和色彩。禁止外貌、身材、年龄、敏感属性评分。
若提供了职业、预算、身高，它们只能影响 recommendation，不能改变可见观察。
输出 impression_tags、priority_title、priority_copy，以及 3 到 6 条 finding。每条 finding 必须包含 key、category（hair|makeup|outfit|color）、label、visible_observation、recommendation、priority（1-3）、position、source_role（face|side|body）和完整 anchor（x,y,w,h）。`

	evidenceInstructions = `你是独立的视觉核验器。只对照三张原图和 draft finding 的可见观察做判断，不接收上一模型的自由文本解释。`
	evidencePromptPrefix = `三张原图依次为 face、side、body。对每条 draft finding 给出恰好一条 decision：key 必须与输入一致，supported 为布尔值，confidence 为 0 到 1。不要发明额外 finding，也不要省略任一 key。`
)

type PhotoQualityItem struct {
	Role       domain.PhotoRole `json:"role"`
	Decision   string           `json:"decision"`
	ReasonCode string           `json:"reason_code"`
}

type PhotoQualityResult struct {
	Photos []PhotoQualityItem
	Meta   InvocationMeta
}

type IdentityResult struct {
	Decision   string         `json:"decision"`
	Confidence float64        `json:"confidence"`
	Meta       InvocationMeta `json:"-"`
}

type ReportAnalysisResult struct {
	Draft domain.ReportDraft
	Meta  InvocationMeta
}

type EvidenceDecision struct {
	Key        string  `json:"key"`
	Supported  bool    `json:"supported"`
	Confidence float64 `json:"confidence"`
	ReasonCode string  `json:"reason_code"`
}

type EvidenceResult struct {
	Findings []EvidenceDecision
	Meta     InvocationMeta
}

type ReportAnalysisInput struct {
	Images     []ImageInput
	Profile    json.RawMessage
	Generation int
}

type AssessmentProviders struct {
	PhotoContent PhotoContentChecker
	Identity     IdentityChecker
	Analyzer     ReportAnalyzer
	Evidence     EvidenceVerifier
}

func NewAssessmentProviders(runtime Runtime) AssessmentProviders {
	return AssessmentProviders{
		PhotoContent: PhotoContentChecker{runtime: runtime},
		Identity:     IdentityChecker{runtime: runtime},
		Analyzer:     ReportAnalyzer{runtime: runtime},
		Evidence:     EvidenceVerifier{runtime: runtime},
	}
}

type PhotoContentChecker struct{ runtime Runtime }

func (c PhotoContentChecker) Check(ctx context.Context, images []ImageInput) (PhotoQualityResult, error) {
	result, err := c.runtime.Structured(ctx, StructuredRequest{
		Capability:      CapabilityPhotoQualityCheck,
		Instructions:    photoQualityInstructions,
		Prompt:          photoQualityPrompt,
		Images:          orderAssessmentImages(images),
		SchemaName:      CapabilityPhotoQualityCheck,
		Schema:          photoQualitySchema(),
		MaxOutputTokens: 400,
		Validate:        validatePhotoQualityPayload,
	})
	if err != nil {
		return PhotoQualityResult{}, err
	}
	var payload photoQualityPayload
	if err := json.Unmarshal(result.JSON, &payload); err != nil {
		return PhotoQualityResult{}, err
	}
	return PhotoQualityResult{Photos: payload.Photos, Meta: result.Meta}, nil
}

type IdentityChecker struct{ runtime Runtime }

func (c IdentityChecker) Check(ctx context.Context, images []ImageInput) (IdentityResult, error) {
	result, err := c.runtime.Structured(ctx, StructuredRequest{
		Capability:      CapabilityPhotoIdentityConsistency,
		Instructions:    identityInstructions,
		Prompt:          identityPrompt,
		Images:          orderAssessmentImages(images),
		SchemaName:      CapabilityPhotoIdentityConsistency,
		Schema:          identitySchema(),
		MaxOutputTokens: 200,
		Validate:        validateIdentityPayload,
	})
	if err != nil {
		return IdentityResult{}, err
	}
	var payload identityPayload
	if err := json.Unmarshal(result.JSON, &payload); err != nil {
		return IdentityResult{}, err
	}
	return IdentityResult{Decision: payload.Decision, Confidence: payload.Confidence, Meta: result.Meta}, nil
}

type ReportAnalyzer struct{ runtime Runtime }

func (a ReportAnalyzer) Analyze(ctx context.Context, input ReportAnalysisInput) (ReportAnalysisResult, error) {
	result, err := a.runtime.Structured(ctx, StructuredRequest{
		Capability:      CapabilityAppearanceAnalysis,
		Instructions:    appearanceInstructions,
		Prompt:          appearanceAnalysisPrompt(input),
		Images:          orderAssessmentImages(input.Images),
		SchemaName:      CapabilityAppearanceAnalysis,
		Schema:          reportSchema(),
		MaxOutputTokens: 6000,
		Validate:        validateReportPayload,
	})
	if err != nil {
		return ReportAnalysisResult{}, err
	}
	var payload reportPayload
	if err := json.Unmarshal(result.JSON, &payload); err != nil {
		return ReportAnalysisResult{}, err
	}
	return ReportAnalysisResult{Draft: payload.toDraft(), Meta: result.Meta}, nil
}

type EvidenceVerifier struct{ runtime Runtime }

func (v EvidenceVerifier) Verify(ctx context.Context, images []ImageInput, draft domain.ReportDraft) (EvidenceResult, error) {
	keys := findingKeys(draft)
	result, err := v.runtime.Structured(ctx, StructuredRequest{
		Capability:      CapabilityReportEvidenceVerification,
		Instructions:    evidenceInstructions,
		Prompt:          evidenceVerificationPrompt(draft),
		Images:          orderAssessmentImages(images),
		SchemaName:      CapabilityReportEvidenceVerification,
		Schema:          evidenceSchema(),
		MaxOutputTokens: 800,
		Validate: func(data []byte) error {
			return validateEvidencePayload(keys, data)
		},
	})
	if err != nil {
		return EvidenceResult{}, err
	}
	var payload evidencePayload
	if err := json.Unmarshal(result.JSON, &payload); err != nil {
		return EvidenceResult{}, err
	}
	return EvidenceResult{Findings: payload.Findings, Meta: result.Meta}, nil
}

func appearanceAnalysisPrompt(input ReportAnalysisInput) string {
	var b strings.Builder
	b.WriteString(appearancePrompt)
	if len(input.Profile) > 0 {
		b.WriteString("\n用户档案（职业、预算、身高只能影响 recommendation）：")
		b.Write(input.Profile)
	}
	if input.Generation > 1 {
		fmt.Fprintf(&b, "\n这是第 %d 轮补生成，请给出与上一轮不同的可见观察。", input.Generation)
	}
	return b.String()
}

func evidenceVerificationPrompt(draft domain.ReportDraft) string {
	var b strings.Builder
	b.WriteString(evidencePromptPrefix)
	b.WriteString("\n以下是 draft finding，仅核验可见观察是否被三张原图支持：\n")
	for _, finding := range draft.Findings {
		fmt.Fprintf(&b, "- key=%s source_role=%s observation=%s anchor=(%.4f,%.4f,%.4f,%.4f)\n",
			finding.Key, finding.SourceRole, finding.VisibleObservation,
			finding.Anchor.X, finding.Anchor.Y, finding.Anchor.W, finding.Anchor.H)
	}
	return b.String()
}

func findingKeys(draft domain.ReportDraft) []string {
	keys := make([]string, 0, len(draft.Findings))
	for _, finding := range draft.Findings {
		keys = append(keys, finding.Key)
	}
	return keys
}

func orderAssessmentImages(images []ImageInput) []ImageInput {
	byRole := make(map[string]ImageInput, len(images))
	for _, image := range images {
		byRole[image.Role] = image
	}
	ordered := make([]ImageInput, 0, 3)
	for _, role := range []string{"face", "side", "body"} {
		if image, ok := byRole[role]; ok {
			ordered = append(ordered, image)
		}
	}
	return ordered
}
