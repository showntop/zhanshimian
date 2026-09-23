package media

import (
	"context"
	"time"

	"io"

	"github.com/zhanshimian/server/internal/domain"
)

type Repository interface {
	CreateUploadIntent(context.Context, domain.CreateUploadIntent) (domain.UploadIntent, error)
	GetUploadIntent(context.Context, string, string) (domain.UploadIntent, error)
	CompleteUploadIntent(context.Context, domain.CompleteUploadIntent) (domain.MediaAsset, bool, error)
}

type ObjectStore interface {
	PresignUpload(context.Context, domain.UploadIntent, time.Duration) (domain.UploadGrant, error)
	HeadObject(context.Context, string) (domain.ObjectMetadata, error)
	// Open 读取对象字节：上传完成时要嗅探真实内容（客户端声明的 MIME
	// 按扩展名猜测，可能是错的）。
	Open(context.Context, string) (io.ReadCloser, error)
}
