package diagnostic

import (
	"context"
	"time"

	"github.com/zhanshimian/server/internal/domain"
)

// Grounding 是 Outfit/Purchase 诊断所需的只读 grounding：报告 + 档案 + 衣橱。
// kind 是诊断类型（outfit/purchase），由调用方显式传入，不在 Reader 内部猜测。
type Grounding struct {
	Report   *domain.ReportGrounding
	Profile  domain.ProfileSnapshot
	Wardrobe []domain.WardrobeGroundingItem
}

type Reader interface {
	ReadDiagnosticGrounding(ctx context.Context, userID string, reportID string, kind string) (Grounding, error)
}

// MediaSigner 给诊断源照片解析可读 URL（COS 短时签名，本地存储由适配器回退
// 公开路径）。与 home.MediaSigner 同形。
type MediaSigner interface {
	SignedURL(ctx context.Context, objectKey string) (url string, expiresAt time.Time, err error)
}
