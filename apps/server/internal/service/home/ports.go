package home

import (
	"context"
	"time"

	"github.com/zhanshimian/server/internal/domain"
)

// Snapshot 是首页一屏聚合的只读投影：档案摘要 + 最新报告 + 今日方案 +
// 进行中操作 + 最近方案 + 权益。所有字段都是值对象，不含 repository row。
type Snapshot struct {
	Profile          *domain.ProfileSummary  `json:"profile,omitempty"`
	CurrentReport    *domain.ReportCard      `json:"current_report,omitempty"`
	Today            *domain.TodayCard       `json:"today,omitempty"`
	RecentPlan       *domain.PlanVariantCard `json:"recent_plan,omitempty"`
	ActiveOperations []domain.OperationRef   `json:"active_operations"`
	Billing          *domain.BillingSummary  `json:"billing,omitempty"`
}

// Reader 是首页聚合的单一方法 Reader：只读稳定投影，不写任何质量核心表。
type Reader interface {
	ReadHome(ctx context.Context, userID string, now time.Time) (Snapshot, error)
}
