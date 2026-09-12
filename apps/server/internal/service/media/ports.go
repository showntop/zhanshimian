package media

import (
	"context"
	"time"

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
}
