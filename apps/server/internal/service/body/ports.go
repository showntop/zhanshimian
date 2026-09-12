package body

import (
	"context"

	"github.com/zhanshimian/server/internal/domain"
)

// Repository 是 3D 形象 Lite 的存储依赖（postgres.Store 实现）。
type Repository interface {
	CreateBodyPresentation(ctx context.Context, userID string, input domain.BodyPresentationInput, maxAttempts int) (domain.CreatedBodyPresentation, error)
	GetBodyPresentation(ctx context.Context, userID, id string) (domain.StoredBodyPresentation, error)
	ListBodyPresentationStatus(ctx context.Context, userID string) (active, completed, failed *domain.StoredBodyPresentation, err error)
	BodyOrbitTaskState(ctx context.Context, userID, presentationID string) (domain.Task, error)
	GetBodyOrbitWork(ctx context.Context, userID, id string) (domain.BodyPresentationInput, error)
	ApplyBodyOrbitResult(ctx context.Context, id, videoKey string, durationMS int, yaws []float64, keys []string, providerVersion string) error
}

// AssetReader 校验输入媒体的归属与用途。
type AssetReader interface {
	GetReadyAssets(ctx context.Context, userID string, ids []string) ([]domain.MediaAsset, error)
}

// URLSigner 把 COS object key 投影成带时限的签名 URL（本地存储退化为
// /uploads/ 公开路径，由 bootstrap 适配）。
type URLSigner interface {
	Sign(ctx context.Context, objectKey string) (string, error)
}

// Billing 是创建时的额度预扣（与 assessment 同一 Reserve 模型）。
type Billing interface {
	Reserve(ctx context.Context, userID, operationID string, product domain.Product, units int) (domain.Reservation, error)
}
