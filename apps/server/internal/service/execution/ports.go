package execution

import (
	"context"

	"github.com/zhanshimian/server/internal/domain"
)

// ErrIdempotencyConflict 复用 domain 哨兵,postgres 适配器返回同一指针,
// 服务侧 errors.Is 能直接命中。
var ErrIdempotencyConflict = domain.ErrIdempotencyConflict

// ErrInvalidSnapshot 表示从 PlanVariant 复制出的步骤快照不满足
// 恰好 hair/makeup/outfit 且 position 唯一的约束。
var ErrInvalidSnapshot = domain.ErrInvalidSnapshot

// CreateSelectionCommand 复用 domain 的跨边界 DTO。
type CreateSelectionCommand = domain.CreateSelectionCommand

// CreateExecutionCommand 复用 domain 的跨边界 DTO。
type CreateExecutionCommand = domain.CreateExecutionCommand

// SelectionRepository 拥有独立、追加式的选择事实。
type SelectionRepository interface {
	CreateSelection(ctx context.Context, command CreateSelectionCommand) (domain.PlanSelection, bool, error)
	GetSelection(ctx context.Context, userID, selectionID string) (domain.PlanSelection, error)
}

// ExecutionRepository 从一次 Selection 快照出一次不可变 Execution。
type ExecutionRepository interface {
	CreateExecutionFromSelection(ctx context.Context, command CreateExecutionCommand) (domain.Execution, bool, error)
	GetExecution(ctx context.Context, userID, executionID string) (domain.Execution, error)
}

// Repository 是 execution 服务所需的完整端口集合。
type Repository interface {
	SelectionRepository
	ExecutionRepository
}

// PutSelectionInput 是创建或重放一次选择所需的全部输入。
type PutSelectionInput struct {
	PlanVariantID       string
	RenderPublicationID *string
	IdempotencyKey      string
}

// CreateExecutionInput 是快照一次选择所需的全部输入。
type CreateExecutionInput struct {
	IdempotencyKey string
}

// Service 只依赖 Repository 这一个端口。
type Service struct {
	repo Repository
}

// New 用最小端口集合构造 execution 服务。
func New(repo Repository) *Service {
	return &Service{repo: repo}
}
