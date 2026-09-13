package today

import (
	"context"

	"github.com/zhanshimian/server/internal/domain"
)

// Grounding 是今日方案生成所需的质量核心 grounding：报告发现 + 已选方案 +
// 已发布媒体。只读，不返回签名 URL（由读取投影即时生成）。
type Grounding struct {
	ReportID     string
	Profile      domain.ProfileSnapshot
	Findings     []domain.FindingGrounding
	SelectedPlan *domain.PlanVariantGrounding
	Publication  *domain.PublishedMedia
}

type Reader interface {
	ReadTodayGrounding(ctx context.Context, userID string) (Grounding, error)
}
