package ai

import (
	"context"

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

// QualityEvaluator 在指定模型上执行一次结构化质量评估。
type QualityEvaluator interface {
	EvaluateQuality(ctx context.Context, modelID, capability string, request QualityRequest) (QualityResult, error)
}
