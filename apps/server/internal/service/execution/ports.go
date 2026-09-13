package execution

import (
	"context"

	"github.com/zhanshimian/server/internal/domain"
)

// ErrIdempotencyConflict 复用 domain 哨兵,postgres 适配器返回同一指针,
// 服务侧 errors.Is 能直接命中。
var ErrIdempotencyConflict = domain.ErrIdempotencyConflict

// CreateSelectionCommand 复用 domain 的跨边界 DTO。
type CreateSelectionCommand = domain.CreateSelectionCommand

// SelectionRepository 拥有独立、追加式的选择事实。
type SelectionRepository interface {
	CreateSelection(ctx context.Context, command CreateSelectionCommand) (domain.PlanSelection, bool, error)
	GetSelection(ctx context.Context, userID, selectionID string) (domain.PlanSelection, error)
}

// PutSelectionInput 是创建或重放一次选择所需的全部输入。
type PutSelectionInput struct {
	PlanVariantID       string
	RenderPublicationID *string
	IdempotencyKey      string
}

// Service 只依赖 SelectionRepository 这一个端口。
type Service struct {
	repo SelectionRepository
}

// New 用最小端口集合构造 execution 服务。
func New(repo SelectionRepository) *Service {
	return &Service{repo: repo}
}
