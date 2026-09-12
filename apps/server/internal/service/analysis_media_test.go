package service

import (
	"context"
	"testing"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

type analysisMediaRepositoryStub struct {
	repository.Repository
	analysis domain.Analysis
	assets   []domain.MediaAsset
}

func (s analysisMediaRepositoryStub) GetAnalysis(_ context.Context, _, _ string) (domain.Analysis, error) {
	return s.analysis, nil
}

func (s analysisMediaRepositoryStub) GetMediaAssetsForUser(_ context.Context, _ string, _ []string) ([]domain.MediaAsset, error) {
	return s.assets, nil
}

func TestGetAnalysisReturnsOwnedMediaPreview(t *testing.T) {
	repo := analysisMediaRepositoryStub{
		analysis: domain.Analysis{ID: "11111111-1111-1111-1111-111111111111", MediaIDs: []string{"face-1", "side-1", "body-1"}},
		assets: []domain.MediaAsset{
			{ID: "face-1", Purpose: domain.MediaPurposeFace, ObjectKey: "user/face.jpg"},
			{ID: "side-1", Purpose: domain.MediaPurposeSide, ObjectKey: "user/side.jpg"},
			{ID: "body-1", Purpose: domain.MediaPurposeBody, ObjectKey: "user/body.jpg"},
		},
	}
	service := &Service{repo: repo, publicBaseURL: "https://api.example.test"}
	analysis, err := service.GetAnalysis(context.Background(), "user-1", "11111111-1111-1111-1111-111111111111")
	if err != nil {
		t.Fatal(err)
	}
	if analysis.PreviewImageURL != "https://api.example.test/uploads/user/body.jpg" {
		t.Fatalf("unexpected preview image: %q", analysis.PreviewImageURL)
	}
	if len(analysis.Media) != 3 || analysis.Media[1].ID != "side-1" {
		t.Fatalf("analysis media was not hydrated: %#v", analysis.Media)
	}
}

func TestAnalysisMediaUsesBundledDemoAsset(t *testing.T) {
	service := &Service{publicBaseURL: "https://api.example.test"}
	url := service.mediaAssetURL(domain.MediaAsset{Purpose: domain.MediaPurposeFace, ObjectKey: "demo/face.png"})
	if url != "/assets/looks/natural.png" {
		t.Fatalf("unexpected demo URL: %q", url)
	}
}

func TestGetAnalysisMarksBundledDemoMedia(t *testing.T) {
	repo := analysisMediaRepositoryStub{
		analysis: domain.Analysis{ID: "11111111-1111-1111-1111-111111111111", MediaIDs: []string{"body-1"}},
		assets:   []domain.MediaAsset{{ID: "body-1", Purpose: domain.MediaPurposeBody, ObjectKey: "demo/body.png", Origin: domain.MediaOriginDemo}},
	}
	service := &Service{repo: repo, publicBaseURL: "https://api.example.test"}
	analysis, err := service.GetAnalysis(context.Background(), "user-1", "11111111-1111-1111-1111-111111111111")
	if err != nil {
		t.Fatal(err)
	}
	if len(analysis.Media) != 1 || analysis.Media[0].Origin != domain.MediaOriginDemo {
		t.Fatalf("demo media provenance was not preserved: %#v", analysis.Media)
	}
}

func TestAnalysisPreviewURLPrefersBodyPhoto(t *testing.T) {
	repo := analysisMediaRepositoryStub{assets: []domain.MediaAsset{
		{ID: "body-1", Purpose: domain.MediaPurposeBody, ObjectKey: "user/body.jpg"},
		{ID: "face-1", Purpose: domain.MediaPurposeFace, ObjectKey: "user/face.jpg"},
	}}
	service := &Service{repo: repo, publicBaseURL: "https://api.example.test"}
	url, err := service.analysisPreviewURL(context.Background(), "user-1", []string{"body-1", "face-1"})
	if err != nil {
		t.Fatal(err)
	}
	// The preview is persisted on the report row and re-expanded on read, so
	// it must stay relative rather than carrying a host or signature.
	if url != "/uploads/user/body.jpg" {
		t.Fatalf("expected relative body image, got %q", url)
	}
}

func (s analysisMediaRepositoryStub) GetReport(_ context.Context, _, _ string) (domain.AssessmentReport, error) {
	return domain.AssessmentReport{}, repository.ErrNotFound
}
