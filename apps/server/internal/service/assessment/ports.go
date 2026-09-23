package assessment

import (
	"context"
	"encoding/json"
	"time"

	"github.com/zhanshimian/server/internal/domain"
)

type AssetReader interface {
	GetReadyAssets(ctx context.Context, userID string, ids []string) ([]domain.MediaAsset, error)
}

type ProfileReader interface {
	Snapshot(ctx context.Context, userID string) (json.RawMessage, error)
}

type Repository interface {
	CreateOrReuseAssessment(ctx context.Context, params domain.CreateAssessmentParams) (domain.CreatedAssessment, error)
	GetReport(ctx context.Context, userID, reportID string) (domain.AssessmentReport, error)
	GetCurrentReport(ctx context.Context, userID string) (domain.AssessmentReport, error)
}

type MediaPresenter interface {
	Present(ctx context.Context, asset domain.MediaAsset) (PresentedMedia, error)
}

// UsageCounter 按用户计数既有 operation 行：用量日限的仓储自计数口径。
// 幂等复用不产生新行，天然不占当日名额。
type UsageCounter interface {
	CountOperationsCreatedSince(ctx context.Context, userID string, kinds []domain.OperationKind, subjectTypes []string, since time.Time) (int, error)
}
