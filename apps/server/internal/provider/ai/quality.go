package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/zhanshimian/server/internal/domain"
)

// QualityRequest 是质量评估的固定输入:candidate、body、face 三张图加上
// 完整 RenderSpec。
type QualityRequest struct {
	RenderRunID string
	Candidate   ImageInput
	Body        ImageInput
	Face        ImageInput
	Spec        domain.RenderDirective
}

// StageResult 是一个质量阶段的判定。
type StageResult struct {
	Pass        bool
	Confidence  float64
	ReasonCodes []string
}

// QualityResult 是五个阶段的完整输出。
type QualityResult struct {
	Technical   StageResult
	Identity    StageResult
	Anatomy     StageResult
	Composition StageResult
	Semantic    StageResult
	Meta        InvocationMeta
}

// qualityInstructions 与输出 Schema:五阶段判定,reason code 闭集。
const qualityInstructions = `你是严格的渲染质量核验器。对照 body 原图(构图与身体比例基准)、face 原图(身份参考)与候选效果图,依次判定技术、身份、人体、构图、图文五个阶段。identity_drift 等外部原因码必须与输入 candidate_reason_codes 一致;不得输出评分、不得输出图片。`

var qualityReasonCodesAllowed = map[string]bool{
	"multiple_people": true, "watermark": true, "text_overlay": true,
	"identity_drift": true,
	"anatomy_head":   true, "anatomy_torso": true, "anatomy_arms": true, "anatomy_legs": true, "anatomy_hands": true,
	"composition_head_crop": true, "composition_pose_changed": true, "composition_background_changed": true,
	"semantic_hair_mismatch": true, "semantic_makeup_mismatch": true, "semantic_outfit_mismatch": true,
	"unrequested_skin_tone_change": true, "unrequested_age_change": true, "unrequested_body_change": true,
}

type qualityStagePayload struct {
	Pass        bool     `json:"pass"`
	Confidence  float64  `json:"confidence"`
	ReasonCodes []string `json:"reason_codes"`
}

type qualityPayload struct {
	Technical   qualityStagePayload `json:"technical"`
	Identity    qualityStagePayload `json:"identity"`
	Anatomy     qualityStagePayload `json:"anatomy"`
	Composition qualityStagePayload `json:"composition"`
	Semantic    qualityStagePayload `json:"semantic"`
}

// StructuredQualityEvaluator 通过能力路由的结构化通道执行质量评估。
type StructuredQualityEvaluator struct {
	runtime StructuredRuntime
}

func NewStructuredQualityEvaluator(runtime StructuredRuntime) *StructuredQualityEvaluator {
	return &StructuredQualityEvaluator{runtime: runtime}
}

func (e *StructuredQualityEvaluator) Evaluate(ctx context.Context, request QualityRequest) (QualityResult, error) {
	if len(request.Candidate.Data) == 0 || len(request.Body.Data) == 0 || len(request.Face.Data) == 0 {
		return QualityResult{}, errors.New("quality evaluation requires candidate, body and face images")
	}
	specJSON, err := json.Marshal(request.Spec)
	if err != nil {
		return QualityResult{}, err
	}
	result, err := e.runtime.Structured(ctx, StructuredRequest{
		Capability:   CapabilityRenderQualityEvaluation,
		Instructions: qualityInstructions,
		Prompt: string(mustJSON(map[string]any{
			"render_run_id": request.RenderRunID,
			"render_spec":   json.RawMessage(specJSON),
		})),
		SchemaName:      CapabilityRenderQualityEvaluation,
		Schema:          QualitySchema(),
		MaxOutputTokens: 1200,
		Validate:        validateQualityPayload,
	})
	if err != nil {
		return QualityResult{}, err
	}
	return decodeQualityPayload(result.JSON, result.Meta)
}

func mustJSON(value any) []byte {
	encoded, _ := json.Marshal(value)
	return encoded
}

// QualitySchema 返回质量评估的严格输出 schema。
func QualitySchema() map[string]any {
	stage := func() map[string]any {
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"pass", "confidence", "reason_codes"},
			"properties": map[string]any{
				"pass":       map[string]any{"type": "boolean"},
				"confidence": map[string]any{"type": "number", "minimum": 0, "maximum": 1},
				"reason_codes": map[string]any{"type": "array", "maxItems": 8, "items": map[string]any{
					"enum": []string{
						"multiple_people", "watermark", "text_overlay",
						"identity_drift",
						"anatomy_head", "anatomy_torso", "anatomy_arms", "anatomy_legs", "anatomy_hands",
						"composition_head_crop", "composition_pose_changed", "composition_background_changed",
						"semantic_hair_mismatch", "semantic_makeup_mismatch", "semantic_outfit_mismatch",
						"unrequested_skin_tone_change", "unrequested_age_change", "unrequested_body_change",
					},
				}},
			},
		}
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"technical", "identity", "anatomy", "composition", "semantic"},
		"properties": map[string]any{
			"technical": stage(), "identity": stage(), "anatomy": stage(),
			"composition": stage(), "semantic": stage(),
		},
	}
}

func validateQualityPayload(data []byte) error {
	var payload qualityPayload
	decode := json.NewDecoder(strings.NewReader(string(data)))
	decode.DisallowUnknownFields()
	if err := decode.Decode(&payload); err != nil {
		return fmt.Errorf("%w: %v", ErrVerifierContract, err)
	}
	for _, stage := range []qualityStagePayload{payload.Technical, payload.Identity, payload.Anatomy, payload.Composition, payload.Semantic} {
		if stage.Confidence < 0 || stage.Confidence > 1 {
			return fmt.Errorf("%w: confidence out of range", ErrVerifierContract)
		}
		for _, code := range stage.ReasonCodes {
			if !qualityReasonCodesAllowed[code] {
				return fmt.Errorf("%w: unknown quality reason code %q", ErrVerifierContract, code)
			}
		}
	}
	return nil
}

func decodeQualityPayload(data []byte, meta InvocationMeta) (QualityResult, error) {
	var payload qualityPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return QualityResult{}, fmt.Errorf("%w: %v", ErrVerifierContract, err)
	}
	return QualityResult{
		Technical:   StageResult{Pass: payload.Technical.Pass, Confidence: payload.Technical.Confidence, ReasonCodes: payload.Technical.ReasonCodes},
		Identity:    StageResult{Pass: payload.Identity.Pass, Confidence: payload.Identity.Confidence, ReasonCodes: payload.Identity.ReasonCodes},
		Anatomy:     StageResult{Pass: payload.Anatomy.Pass, Confidence: payload.Anatomy.Confidence, ReasonCodes: payload.Anatomy.ReasonCodes},
		Composition: StageResult{Pass: payload.Composition.Pass, Confidence: payload.Composition.Confidence, ReasonCodes: payload.Composition.ReasonCodes},
		Semantic:    StageResult{Pass: payload.Semantic.Pass, Confidence: payload.Semantic.Confidence, ReasonCodes: payload.Semantic.ReasonCodes},
		Meta:        meta,
	}, nil
}
