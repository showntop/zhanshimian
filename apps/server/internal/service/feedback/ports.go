package feedback

import (
	"context"

	"github.com/zhanshimian/server/internal/domain"
)

// ErrIdempotencyConflict 复用 domain 哨兵,postgres 适配器返回同一指针,
// 服务侧 errors.Is 能直接命中。
var ErrIdempotencyConflict = domain.ErrIdempotencyConflict

// ErrExecutionNotCompleted 复用 domain 哨兵;Execution 反馈只允许落在已完成
// (completed)的执行上。
var ErrExecutionNotCompleted = domain.ErrExecutionNotCompleted

// GenerationFeedback 复用 domain 类型。
type GenerationFeedback = domain.GenerationFeedback

// CreateGenerationFeedbackCommand 复用 domain 的跨边界 DTO。
type CreateGenerationFeedbackCommand = domain.CreateGenerationFeedbackCommand

// PreferenceMemory 复用 domain 类型。
type PreferenceMemory = domain.PreferenceMemory

// ExecutionFeedback 复用 domain 类型。
type ExecutionFeedback = domain.ExecutionFeedback

// CreateExecutionFeedbackCommand 复用 domain 的跨边界 DTO。
type CreateExecutionFeedbackCommand = domain.CreateExecutionFeedbackCommand

// FeedbackRepository 保存已发布 Generation 或已完成 Execution 的反馈,
// 并派生完整发布/执行链路。
type FeedbackRepository interface {
	CreateGenerationFeedback(ctx context.Context, command CreateGenerationFeedbackCommand) (GenerationFeedback, bool, error)
	CreateExecutionFeedback(ctx context.Context, command CreateExecutionFeedbackCommand) (ExecutionFeedback, bool, error)
}

// Repository 是 feedback 服务所需的完整端口集合。
type Repository interface {
	FeedbackRepository
}

// CreateGenerationFeedbackInput 是保存一条 Generation 反馈所需的全部输入。
type CreateGenerationFeedbackInput struct {
	PublicationID  string
	Tags           []domain.Tag
	Comment        string
	MediaAssetID   *string
	IdempotencyKey string
}

// CreateExecutionFeedbackInput 是保存一条 Execution 反馈所需的全部输入;Preference
// 是唯一能形成偏好记忆的结构化输入,自由文本永不进入。
type CreateExecutionFeedbackInput struct {
	ExecutionID    string
	Tags           []domain.Tag
	Comment        string
	MediaAssetID   *string
	Preference     domain.StructuredPreference
	IdempotencyKey string
}

// Service 只依赖 Repository 这一个端口。
type Service struct {
	repo Repository
}

// New 用最小端口集合构造 feedback 服务。
func New(repo Repository) *Service {
	return &Service{repo: repo}
}
