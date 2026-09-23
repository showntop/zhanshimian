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

// MediaChecker 校验单品照片媒体的归属与用途：越权、不存在、已删除或
// 用途非 wardrobe 一律 ErrNotFound（与旧线 CreateWardrobeItem 的 media 校验一致）。
type MediaChecker interface {
	CheckWardrobeMedia(ctx context.Context, userID, assetID string) error
}

// MediaSigner 给衣橱单品照片解析可读 URL（COS 短时签名，本地存储由适配器
// 回退公开路径）。与 home.MediaSigner 同形。
type MediaSigner interface {
	SignedURL(ctx context.Context, objectKey string) (url string, expiresAt time.Time, err error)
}
