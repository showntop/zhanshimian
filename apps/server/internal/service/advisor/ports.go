package advisor

import (
	"context"
	"time"

	"github.com/zhanshimian/server/internal/domain"
)

// Grounding 是顾问对话所需的只读上下文：档案 + 报告 + 已选方案 + 衣橱 + 反馈记忆。
// 只读，不写质量核心表，不返回签名 URL。
type Grounding struct {
	Profile      domain.ProfileSnapshot
	Report       *domain.ReportGrounding
	SelectedPlan *domain.PlanVariantGrounding
	Wardrobe     []domain.WardrobeGroundingItem
	Feedback     []domain.FeedbackMemoryItem
}

type Reader interface {
	ReadAdvisorGrounding(ctx context.Context, userID string) (Grounding, error)
}

// UsageGate 是顾问用量闸：仓储自计数（billing_usage 台账），不扣次数。
// 形状与 billing.OrdersRepository.ApplyBilling 一致（*postgres.Store 直接实现）。
type UsageGate interface {
	ApplyBilling(ctx context.Context, userID string, now time.Time, activeLooks int, decide func(domain.BillingSnapshot) (domain.BillingDecision, error)) error
}
