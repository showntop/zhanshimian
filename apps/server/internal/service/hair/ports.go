package hair

import (
	"context"
	"time"

	"github.com/zhanshimian/server/internal/domain"
)

// Grounding 是发型建议/预览所需的质量核心 grounding：报告发现 + 正脸媒体引用。
type Grounding struct {
	ReportID string
	Findings []domain.FindingGrounding
	Face     domain.MediaInput
}

type Reader interface {
	ReadHairGrounding(ctx context.Context, userID string, reportID string) (Grounding, error)
	// ReadFaceMedia 取用户显式选择的正脸照（media_id）：校验归属、可用状态与
	// 可展示格式；不满足一律 NotFound（越权与不存在不可区分）。
	ReadFaceMedia(ctx context.Context, userID string, assetID string) (domain.MediaAsset, error)
}

// MediaSigner 给读模型即时签 URL（COS 短时签名 / 本地公网前缀回退），与
// body/share/外围读模型同一做法：库只存 object key，签名发生在读取投影时。
type MediaSigner interface {
	SignedURL(ctx context.Context, objectKey string) (url string, expiresAt time.Time, err error)
}
