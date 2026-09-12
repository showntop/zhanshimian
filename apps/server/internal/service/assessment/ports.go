package assessment

import (
	"context"
	"encoding/json"

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
