package feedback

import (
	"context"
	"fmt"

	"github.com/zhanshimian/server/internal/domain"
)

// CreateGenerationFeedback 规范化请求、计算请求哈希,并委托 repository 保存一条
// Generation 反馈。Generation 反馈绝不形成偏好记忆,确认码恒为 AckFeedbackRecorded。
func (s *Service) CreateGenerationFeedback(
	ctx context.Context, userID string, input CreateGenerationFeedbackInput,
) (feedback GenerationFeedback, created bool, err error) {
	publicationID, tags, comment, mediaAssetID, requestHash, err := normalizeGenerationFeedback(input)
	if err != nil {
		return GenerationFeedback{}, false, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	feedback, created, err = s.repo.CreateGenerationFeedback(ctx, CreateGenerationFeedbackCommand{
		UserID:         userID,
		PublicationID:  publicationID,
		Tags:           tags,
		Comment:        comment,
		MediaAssetID:   mediaAssetID,
		IdempotencyKey: input.IdempotencyKey,
		RequestHash:    requestHash,
	})
	if err != nil {
		return GenerationFeedback{}, false, err
	}
	feedback.AcknowledgementCode = domain.AckFeedbackRecorded
	return feedback, created, nil
}

// CreateExecutionFeedback 规范化请求、计算请求哈希,并委托 repository 保存一条
// Execution 反馈。只有结构化偏好能形成记忆;确认码在事务提交后依据已返回的
// 记忆精确给出,绝不承诺未写库的记忆。
func (s *Service) CreateExecutionFeedback(
	ctx context.Context, userID string, input CreateExecutionFeedbackInput,
) (feedback ExecutionFeedback, created bool, err error) {
	executionID, tags, comment, mediaAssetID, preference, requestHash, err := normalizeExecutionFeedback(input)
	if err != nil {
		return ExecutionFeedback{}, false, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	feedback, created, err = s.repo.CreateExecutionFeedback(ctx, CreateExecutionFeedbackCommand{
		UserID:         userID,
		ExecutionID:    executionID,
		Tags:           tags,
		Comment:        comment,
		MediaAssetID:   mediaAssetID,
		Preference:     preference,
		IdempotencyKey: input.IdempotencyKey,
		RequestHash:    requestHash,
	})
	if err != nil {
		return ExecutionFeedback{}, false, err
	}
	feedback.AcknowledgementCode = acknowledgementCodeForMemories(feedback.AppliedMemories)
	return feedback, created, nil
}
