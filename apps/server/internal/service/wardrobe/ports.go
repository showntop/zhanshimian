package wardrobe

import (
	"context"
	"time"

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

// MediaSigner 给衣橱单品照片解析可读 URL（COS 短时签名，本地存储由适配器
// 回退公开路径）。与 home.MediaSigner 同形。
type MediaSigner interface {
	SignedURL(ctx context.Context, objectKey string) (url string, expiresAt time.Time, err error)
}
