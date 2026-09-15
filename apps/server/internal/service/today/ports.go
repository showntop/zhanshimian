package today

import (
	"context"
	"time"

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

// MediaSigner 给今日方案的发布媒体解析可读 URL（COS 短时签名，
// 本地存储由适配器回退公开路径）。与 home.MediaSigner 同形。
type MediaSigner interface {
	SignedURL(ctx context.Context, objectKey string) (url string, expiresAt time.Time, err error)
}
