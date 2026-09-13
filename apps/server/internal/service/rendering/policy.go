package rendering

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sort"

	"github.com/zhanshimian/server/internal/domain"
	providerai "github.com/zhanshimian/server/internal/provider/ai"
)

// 质量评估 reason code(稳定集合,模型只能从中选择)。
const (
	ReasonTechnicalDecode              = "technical_decode"
	ReasonTechnicalSize                = "technical_size"
	ReasonMultiplePeople               = "multiple_people"
	ReasonWatermark                    = "watermark"
	ReasonTextOverlay                  = "text_overlay"
	ReasonIdentityDrift                = "identity_drift"
	ReasonIdentityUnknown              = "identity_unknown"
	ReasonAnatomyHead                  = "anatomy_head"
	ReasonAnatomyTorso                 = "anatomy_torso"
	ReasonAnatomyArms                  = "anatomy_arms"
	ReasonAnatomyLegs                  = "anatomy_legs"
	ReasonAnatomyHands                 = "anatomy_hands"
	ReasonCompositionHeadCrop          = "composition_head_crop"
	ReasonCompositionPoseChanged       = "composition_pose_changed"
	ReasonCompositionBackgroundChanged = "composition_background_changed"
	ReasonSemanticHairMismatch         = "semantic_hair_mismatch"
	ReasonSemanticMakeupMismatch       = "semantic_makeup_mismatch"
	ReasonSemanticOutfitMismatch       = "semantic_outfit_mismatch"
	ReasonUnrequestedSkinToneChange    = "unrequested_skin_tone_change"
	ReasonUnrequestedAgeChange         = "unrequested_age_change"
	ReasonUnrequestedBodyChange        = "unrequested_body_change"
	ReasonQualitySchemaInvalid         = "quality_schema_invalid"
)

// qualityReasonCodes 是合法 reason code 的闭集;未知 code 使整个评估
// 变为 error 并失败关闭。
var qualityReasonCodes = map[string]bool{
	ReasonTechnicalDecode: true, ReasonTechnicalSize: true,
	ReasonMultiplePeople: true, ReasonWatermark: true, ReasonTextOverlay: true,
	ReasonIdentityDrift: true, ReasonIdentityUnknown: true,
	ReasonAnatomyHead: true, ReasonAnatomyTorso: true, ReasonAnatomyArms: true,
	ReasonAnatomyLegs: true, ReasonAnatomyHands: true,
	ReasonCompositionHeadCrop: true, ReasonCompositionPoseChanged: true,
	ReasonCompositionBackgroundChanged: true,
	ReasonSemanticHairMismatch:         true, ReasonSemanticMakeupMismatch: true,
	ReasonSemanticOutfitMismatch:    true,
	ReasonUnrequestedSkinToneChange: true, ReasonUnrequestedAgeChange: true,
	ReasonUnrequestedBodyChange: true,
	ReasonQualitySchemaInvalid:  true,
}

// QualityPolicy 是 render-quality-policy.v1.json 的加载形态。
type QualityPolicy struct {
	Version     string         `json:"version"`
	Technical   map[string]any `json:"technical"`
	Identity    QualityStage   `json:"identity"`
	Anatomy     QualityStage   `json:"anatomy"`
	Composition QualityStage   `json:"composition"`
	Semantic    QualityStage   `json:"semantic"`
}

// QualityStage 是单个质量阶段的阈值。
type QualityStage struct {
	MinimumConfidence float64 `json:"minimum_confidence"`
}

// LoadQualityPolicy 从磁盘加载并校验策略文件。
func LoadQualityPolicy(path string) (QualityPolicy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return QualityPolicy{}, err
	}
	var policy QualityPolicy
	if err := json.Unmarshal(data, &policy); err != nil {
		return QualityPolicy{}, err
	}
	if policy.Version == "" {
		return QualityPolicy{}, errors.New("quality policy misses version")
	}
	return policy, nil
}

// QualityGateChain 按固定顺序执行五类门禁:技术、身份、人体、构图、图文。
// 第一个失败阶段终止链路;无法确认即失败关闭。
type QualityGateChain struct {
	evaluator QualityEvaluator
	policy    QualityPolicy
}

// QualityEvaluator 是结构化质量评估的窄端口。
type QualityEvaluator interface {
	Evaluate(ctx context.Context, request providerai.QualityRequest) (providerai.QualityResult, error)
}

func NewQualityGate(evaluator QualityEvaluator, policy QualityPolicy) *QualityGateChain {
	return &QualityGateChain{evaluator: evaluator, policy: policy}
}

// DecisionFor 把质量判定映射到候选预算语义:Candidate 1 的可重试失败是
// retry,Candidate 2 的任何失败都是 reject。
func DecisionFor(ordinal int, retryable bool) string {
	if ordinal <= 1 && retryable {
		return string(domain.QualityDecisionRetry)
	}
	return string(domain.QualityDecisionReject)
}

// Evaluate 产生一个不可变的质量判定:排序去重的 reason codes、仅内部
// 分数,以及完成到哪个阶段。
func (g *QualityGateChain) Evaluate(ctx context.Context, input QualityInput) (QualityResult, error) {
	request := providerai.QualityRequest{
		RenderRunID: input.Run.ID,
		Candidate:   toProviderImage(input.Image, "candidate"),
		Body:        input.Body,
		Face:        input.Face,
		Spec:        input.Spec,
	}
	result, err := g.evaluator.Evaluate(ctx, request)
	if err != nil {
		// Provider 技术错误原样上抛:task runner 重试,不消耗候选预算。
		return QualityResult{}, err
	}

	var reasonCodes []string
	completed := 0
	add := func(code string) { reasonCodes = append(reasonCodes, code) }

	// 技术:本地 JPEG 检查 + 视觉字段。
	if input.Image.MIMEType != "image/jpeg" {
		add(ReasonTechnicalDecode)
	}
	if input.Image.Width < 512 || input.Image.Height < 512 {
		add(ReasonTechnicalSize)
	}
	technicalStage := result.Technical
	if !technicalStage.Pass {
		reasonCodes = append(reasonCodes, technicalStage.ReasonCodes...)
	}
	if len(reasonCodes) == 0 {
		completed++
	}
	if len(reasonCodes) > 0 {
		return g.decide(input, reasonCodes, completed, result)
	}

	// 身份。
	identity := result.Identity
	if confidenceOf(identity) < g.policy.Identity.MinimumConfidence {
		add(ReasonIdentityUnknown)
	} else if !identity.Pass {
		reasonCodes = append(reasonCodes, identity.ReasonCodes...)
	}
	if len(reasonCodes) == 0 {
		completed++
	} else {
		return g.decide(input, reasonCodes, completed, result)
	}

	// 人体。
	anatomy := result.Anatomy
	if confidenceOf(anatomy) < g.policy.Anatomy.MinimumConfidence {
		add(ReasonAnatomyTorso)
	} else if !anatomy.Pass {
		reasonCodes = append(reasonCodes, anatomy.ReasonCodes...)
	}
	if len(reasonCodes) == 0 {
		completed++
	} else {
		return g.decide(input, reasonCodes, completed, result)
	}

	// 构图。
	composition := result.Composition
	if confidenceOf(composition) < g.policy.Composition.MinimumConfidence {
		add(ReasonCompositionHeadCrop)
	} else if !composition.Pass {
		reasonCodes = append(reasonCodes, composition.ReasonCodes...)
	}
	if len(reasonCodes) == 0 {
		completed++
	} else {
		return g.decide(input, reasonCodes, completed, result)
	}

	// 图文。
	semantic := result.Semantic
	if confidenceOf(semantic) < g.policy.Semantic.MinimumConfidence {
		add(ReasonSemanticOutfitMismatch)
	} else if !semantic.Pass {
		reasonCodes = append(reasonCodes, semantic.ReasonCodes...)
	}
	if len(reasonCodes) == 0 {
		completed++
		return QualityResult{
			Decision:              string(domain.QualityDecisionPass),
			ReasonCodes:           []string{},
			InternalScores:        scoresJSON(result),
			EvaluatorInvocationID: result.Meta.InvocationID,
			CompletedStages:       completed,
		}, nil
	}
	return g.decide(input, reasonCodes, completed, result)
}

func (g *QualityGateChain) decide(input QualityInput, reasonCodes []string, completed int, result providerai.QualityResult) (QualityResult, error) {
	cleaned := make([]string, 0, len(reasonCodes))
	seen := map[string]bool{}
	for _, code := range reasonCodes {
		if !qualityReasonCodes[code] {
			// 未知 code:整个评估失败关闭。
			return QualityResult{
				Decision:              string(domain.QualityDecisionError),
				ReasonCodes:           []string{ReasonQualitySchemaInvalid},
				InternalScores:        scoresJSON(result),
				EvaluatorInvocationID: result.Meta.InvocationID,
				CompletedStages:       completed,
			}, nil
		}
		if !seen[code] {
			seen[code] = true
			cleaned = append(cleaned, code)
		}
	}
	sort.Strings(cleaned)
	decision := DecisionFor(candidateOrdinal(input), isRetryableQuality(cleaned))
	return QualityResult{
		Decision:              decision,
		ReasonCodes:           cleaned,
		InternalScores:        scoresJSON(result),
		EvaluatorInvocationID: result.Meta.InvocationID,
		CompletedStages:       completed,
	}, nil
}

func candidateOrdinal(input QualityInput) int {
	return input.Candidate.Ordinal
}

// isRetryableQuality 判定失败是否可补生成:身份未知等结构性失败可重试。
func isRetryableQuality(codes []string) bool {
	for _, code := range codes {
		if code == ReasonIdentityUnknown || code == ReasonTechnicalDecode || code == ReasonTechnicalSize {
			return true
		}
		switch code {
		case ReasonIdentityDrift, ReasonAnatomyHead, ReasonAnatomyTorso, ReasonAnatomyArms,
			ReasonAnatomyLegs, ReasonAnatomyHands, ReasonCompositionHeadCrop,
			ReasonCompositionPoseChanged, ReasonCompositionBackgroundChanged,
			ReasonSemanticHairMismatch, ReasonSemanticMakeupMismatch, ReasonSemanticOutfitMismatch,
			ReasonUnrequestedSkinToneChange, ReasonUnrequestedAgeChange, ReasonUnrequestedBodyChange,
			ReasonMultiplePeople, ReasonWatermark, ReasonTextOverlay:
			return true
		}
	}
	return false
}

func confidenceOf(stage providerai.StageResult) float64 {
	return stage.Confidence
}

func scoresJSON(result providerai.QualityResult) json.RawMessage {
	scores := domain.RenderEvaluationScores{
		Technical:   result.Technical.Confidence,
		Identity:    result.Identity.Confidence,
		Anatomy:     result.Anatomy.Confidence,
		Composition: result.Composition.Confidence,
		Semantic:    result.Semantic.Confidence,
	}
	encoded, err := json.Marshal(scores)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return encoded
}

func toProviderImage(image NormalizedJPEG, role string) providerai.ImageInput {
	return providerai.ImageInput{
		Role:     role,
		MIMEType: image.MIMEType,
		Width:    image.Width,
		Height:   image.Height,
		Data:     image.Data,
	}
}
