package execution

import (
	"context"

	"github.com/zhanshimian/server/internal/domain"
)

// PutSelection 规范化请求、计算请求哈希,并委托 repository 创建或重放一次选择。
func (s *Service) PutSelection(
	ctx context.Context, userID, planSetID string, input PutSelectionInput,
) (selection domain.PlanSelection, created bool, err error) {
	planSetID, variantID, publicationID, requestHash, err := normalizeSelection(planSetID, input)
	if err != nil {
		return domain.PlanSelection{}, false, err
	}
	return s.repo.CreateSelection(ctx, CreateSelectionCommand{
		UserID:              userID,
		PlanSetID:           planSetID,
		PlanVariantID:       variantID,
		RenderPublicationID: publicationID,
		IdempotencyKey:      input.IdempotencyKey,
		RequestHash:         requestHash,
	})
}
