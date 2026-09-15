package operation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zhanshimian/server/internal/domain"
)

var ErrValidation = errors.New("validation error")

// stale 孤儿判定阈（旧线 staleAnalysis* 同款）：worker 活着时认领/心跳/重排/
// 进度上报都会刷新 operation 或其 task 的 updated_at（租约 ≤90s、心跳 ≤30s、
// 重试退避 ≤1min），长时间零活动只可能是 worker 整体死亡遗留。
// processing 阈必须大于任务租约+重试预算：render 单次超时 300s×3 次尝试
// +2 次退避 ≈ 17min 是单任务链理论上限，但其间心跳持续刷新 task 活动，
// 实际活动间隔 ≤1min，11min 已留足一个数量级余量；queued 用更宽的 20min，
// 覆盖并发槽被长任务占满时的排队等待。
const (
	staleProcessingIdle = 11 * time.Minute // running/retrying
	staleQueuedIdle     = 20 * time.Minute // accepted
)

// stale 失败写入的公开形状（S1b 模式：public_message + retryable=true，
// 用户能看到失败并重试）。
const (
	staleErrorCode     = "stale_orphan"
	stalePublicMessage = "处理时间过长，请重新发起"
)

type Service struct {
	reader Reader
	stale  StaleFailer
	logger *slog.Logger
}

func New(reader Reader) *Service {
	return &Service{reader: reader, logger: slog.Default()}
}

// WithStaleFailer 装配读路径的 stale 孤儿兜底（与 assessment.WithBilling 同一
// 链式做法）。Nil 容忍：未装配时读路径只读不写（单测/降级组装）。
func (s *Service) WithStaleFailer(failer StaleFailer) *Service {
	s.stale = failer
	return s
}

func (s *Service) Get(ctx context.Context, userID, id string) (domain.Operation, error) {
	op, err := s.reader.GetOperation(ctx, userID, strings.TrimSpace(id))
	if err != nil {
		return op, err
	}
	return s.failIfStale(ctx, op), nil
}

func (s *Service) GetMany(ctx context.Context, userID string, ids []string) ([]domain.Operation, error) {
	cleaned, err := normalizeIDs(ids)
	if err != nil {
		return nil, err
	}
	ops, err := s.reader.GetOperations(ctx, userID, cleaned)
	if err != nil {
		return nil, err
	}
	for i := range ops {
		ops[i] = s.failIfStale(ctx, ops[i])
	}
	return ops, nil
}

// failIfStale 对非终态 operation 做读路径 stale 兜底。应用侧先用 UpdatedAt
// 粗筛（免每次轮询都发 UPDATE）；仓储用库内时钟 + task 活动做权威判定，
// 应用/库时钟偏移不会误杀。兜底写失败不拖垮读路径（旧线同款降级）。
func (s *Service) failIfStale(ctx context.Context, op domain.Operation) domain.Operation {
	if s.stale == nil {
		return op
	}
	idle := staleIdleFor(op.Status)
	if idle <= 0 || time.Since(op.UpdatedAt) <= idle {
		return op
	}
	failed, err := s.stale.FailStaleOperation(ctx, op.UserID, op.ID, idle, staleErrorCode, stalePublicMessage)
	if err != nil {
		s.logger.Error("fail stale operation", "operation_id", op.ID, "error", err)
		return op
	}
	if !failed {
		return op
	}
	s.logger.Warn("failed stale orphan operation", "operation_id", op.ID, "kind", op.Kind, "status", op.Status)
	fresh, err := s.reader.GetOperation(ctx, op.UserID, op.ID)
	if err != nil {
		return op
	}
	return fresh
}

// staleIdleFor 返回各在途状态的孤儿判定阈；终态返回 0（不兜底）。
func staleIdleFor(status domain.OperationStatus) time.Duration {
	switch status {
	case domain.OperationRunning, domain.OperationRetrying:
		return staleProcessingIdle
	case domain.OperationAccepted:
		return staleQueuedIdle
	default:
		return 0
	}
}

func normalizeIDs(ids []string) ([]string, error) {
	if len(ids) < 1 || len(ids) > 20 {
		return nil, fmt.Errorf("%w: query 1 to 20 operation ids", ErrValidation)
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if _, err := uuid.Parse(id); err != nil {
			return nil, fmt.Errorf("%w: operation id is not a uuid", ErrValidation)
		}
		if _, ok := seen[id]; ok {
			return nil, fmt.Errorf("%w: duplicate operation id", ErrValidation)
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, nil
}
