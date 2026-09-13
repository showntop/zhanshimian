package feedback

import (
	"context"

	"github.com/zhanshimian/server/internal/domain"
)

// CreateGenerationFeedback 规范化请求、计算请求哈希,并委托 repository 保存一条
// Generation 反馈。Generation 反馈绝不形成偏好记忆,确认码恒为 AckFeedbackRecorded。
func (s *Service) CreateGenerationFeedback(
	ctx context.Context, userID string, input CreateGenerationFeedbackInput,
) (feedback GenerationFeedback, created bool, err error) {
	publicationID, tags, comment, mediaAssetID, requestHash, err := normalizeGenerationFeedback(input)
	if err != nil {
		return GenerationFeedback{}, false, err
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
