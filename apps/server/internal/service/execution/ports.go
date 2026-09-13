package execution

import (
	"context"
	"errors"
	"time"

	"github.com/zhanshimian/server/internal/domain"
)

// ErrValidation 标记请求本身不合法的失败:标识不是 UUID、幂等键长度越界、
// step 事件缺少 step_id、occurred_at 超出允许窗口等。传输层据此返回 400
// 而不是 500,消息沿用 "<哨兵>: <细节>" 的既有约定。
var ErrValidation = errors.New("validation error")

// ErrIdempotencyConflict 复用 domain 哨兵,postgres 适配器返回同一指针,
// 服务侧 errors.Is 能直接命中。
var ErrIdempotencyConflict = domain.ErrIdempotencyConflict

// ErrInvalidSnapshot 表示从 PlanVariant 复制出的步骤快照不满足
// 恰好 hair/makeup/outfit 且 position 唯一的约束。
var ErrInvalidSnapshot = domain.ErrInvalidSnapshot

// ErrVersionConflict 表示事件的 ExpectedVersion 与当前版本不一致。
var ErrVersionConflict = domain.ErrVersionConflict

// CreateSelectionCommand 复用 domain 的跨边界 DTO。
type CreateSelectionCommand = domain.CreateSelectionCommand

// CreateExecutionCommand 复用 domain 的跨边界 DTO。
type CreateExecutionCommand = domain.CreateExecutionCommand

// AppendEventCommand 复用 domain 的跨边界 DTO。
type AppendEventCommand = domain.AppendEventCommand

// AppendEventResult 复用 domain 的跨边界返回类型。
type AppendEventResult = domain.AppendEventResult

// SelectionRepository 拥有独立、追加式的选择事实。
type SelectionRepository interface {
	CreateSelection(ctx context.Context, command CreateSelectionCommand) (domain.PlanSelection, bool, error)
	GetSelection(ctx context.Context, userID, selectionID string) (domain.PlanSelection, error)
}

// ExecutionRepository 从一次 Selection 快照出一次不可变 Execution,并追加幂等事件。
type ExecutionRepository interface {
	CreateExecutionFromSelection(ctx context.Context, command CreateExecutionCommand) (domain.Execution, bool, error)
	GetExecution(ctx context.Context, userID, executionID string) (domain.Execution, error)
	AppendExecutionEvent(ctx context.Context, command AppendEventCommand) (AppendEventResult, error)
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

// AppendEventInput 是追加一条执行事件所需的全部输入。
type AppendEventInput struct {
	ClientEventID   string
	Type            domain.ExecutionEventType
	StepID          *string
	OccurredAt      time.Time
	ExpectedVersion int
}

// Service 只依赖 Repository 这一个端口。
type Service struct {
	repo Repository
}

// New 用最小端口集合构造 execution 服务。
func New(repo Repository) *Service {
	return &Service{repo: repo}
}
