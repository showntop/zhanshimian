package wardrobe

import (
	"context"

	"github.com/zhanshimian/server/internal/domain"
)

// Grounding 是衣橱组合生成所需的质量核心 grounding：当前报告 + 已选方案。
type Grounding struct {
	CurrentReportID string
	SelectedPlan    *domain.PlanVariantGrounding
}

type Reader interface {
	ReadWardrobeGrounding(ctx context.Context, userID string) (Grounding, error)
}
