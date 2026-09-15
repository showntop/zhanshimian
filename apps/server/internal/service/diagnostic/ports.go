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
	// ReadDiagnosticMedia 按资产 ID 读诊断源照片的对象定位；越权/不存在/已删除
	// 一律 ErrNotFound（与 home.MediaObjectInfo 同一语义），绝不允许无图诊断
	// 静默发生。
	ReadDiagnosticMedia(ctx context.Context, userID string, assetID string) (domain.MediaInput, error)
}

// Image 是发给视觉模型的一张照片输入（字节已经过预算约束）。
type Image struct {
	AssetID  string
	Role     string
	MIMEType string
	Data     []byte
}

// ImageLoader 把诊断源照片的对象字节加载为视觉模型图片输入
// （bootstrap 装配：数据万象下载时压缩 + ConstrainVisionImage 本地兜底，
// 与 assessment imageLoader 同一链路）。
type ImageLoader interface {
	Load(ctx context.Context, media domain.MediaInput) (Image, error)
}

// MediaSigner 给诊断源照片解析可读 URL（COS 短时签名，本地存储由适配器回退
// 公开路径）。与 home.MediaSigner 同形。
type MediaSigner interface {
	SignedURL(ctx context.Context, objectKey string) (url string, expiresAt time.Time, err error)
}
