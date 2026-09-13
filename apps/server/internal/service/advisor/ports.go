package advisor

import (
	"context"

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
