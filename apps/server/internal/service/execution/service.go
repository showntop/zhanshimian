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

// CreateExecution 规范化请求并委托 repository 从一次选择快照出一次执行。
func (s *Service) CreateExecution(
	ctx context.Context, userID, selectionID string, input CreateExecutionInput,
) (execution domain.Execution, created bool, err error) {
	selectionID, requestHash, err := normalizeExecution(selectionID, input)
	if err != nil {
		return domain.Execution{}, false, err
	}
	return s.repo.CreateExecutionFromSelection(ctx, CreateExecutionCommand{
		UserID:         userID,
		SelectionID:    selectionID,
		IdempotencyKey: input.IdempotencyKey,
		RequestHash:    requestHash,
	})
}

// GetExecution 委托 repository 按租户读取一次执行。
func (s *Service) GetExecution(ctx context.Context, userID, executionID string) (domain.Execution, error) {
	return s.repo.GetExecution(ctx, userID, executionID)
}
