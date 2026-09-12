package assessment

import (
	"context"
	"encoding/json"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository/postgres"
)

type AssetReader interface {
	GetReadyAssets(ctx context.Context, userID string, ids []string) ([]domain.MediaAsset, error)
}

type ProfileReader interface {
	Snapshot(ctx context.Context, userID string) (json.RawMessage, error)
}

type Repository interface {
	CreateOrReuseAssessment(ctx context.Context, params postgres.CreateAssessmentParams) (postgres.CreatedAssessment, error)
	GetReport(ctx context.Context, userID, reportID string) (postgres.AssessmentReport, error)
	GetCurrentReport(ctx context.Context, userID string) (postgres.AssessmentReport, error)
}

type MediaPresenter interface {
	Present(ctx context.Context, asset domain.MediaAsset) (PresentedMedia, error)
}

var _ Repository = (*postgres.Store)(nil)
