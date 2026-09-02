package service

import (
	"context"
	"testing"
	"time"

	"github.com/example/jianwo/server/internal/domain"
	"github.com/example/jianwo/server/internal/repository"
	"github.com/example/jianwo/server/internal/storage"
)

type analysisMediaRepositoryStub struct {
	repository.Repository
	analysis domain.Analysis
	report   domain.Report
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
		analysis: domain.Analysis{ID: "analysis-1", MediaIDs: []string{"face-1", "side-1", "body-1"}},
		assets: []domain.MediaAsset{
			{ID: "face-1", Kind: "face", StorageKey: "user/face.jpg"},
			{ID: "side-1", Kind: "side", StorageKey: "user/side.jpg"},
			{ID: "body-1", Kind: "body", StorageKey: "user/body.jpg"},
		},
	}
	service := &Service{repo: repo, publicBaseURL: "https://api.example.test"}
	analysis, err := service.GetAnalysis(context.Background(), "user-1", "analysis-1")
	if err != nil {
		t.Fatal(err)
	}
	if analysis.PreviewImageURL != "https://api.example.test/uploads/user/body.jpg" {
		t.Fatalf("unexpected preview image: %q", analysis.PreviewImageURL)
	}
	if len(analysis.Media) != 3 || analysis.Media[1].URL != "https://api.example.test/uploads/user/side.jpg" {
		t.Fatalf("analysis media was not hydrated: %#v", analysis.Media)
	}
}

func TestAnalysisMediaUsesBundledDemoAsset(t *testing.T) {
	service := &Service{publicBaseURL: "https://api.example.test"}
	url := service.mediaAssetURL(domain.MediaAsset{Kind: "face", StorageKey: "demo/face.png"})
	if url != "/assets/looks/natural.png" {
		t.Fatalf("unexpected demo URL: %q", url)
	}
}

func TestAnalysisPreviewURLPrefersBodyPhoto(t *testing.T) {
	repo := analysisMediaRepositoryStub{assets: []domain.MediaAsset{
		{ID: "body-1", Kind: "body", StorageKey: "user/body.jpg"},
		{ID: "face-1", Kind: "face", StorageKey: "user/face.jpg"},
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

type refreshStorageStub struct {
	storage.ObjectStorage
	refreshed map[string]string
}

func (s refreshStorageStub) RefreshURL(value string, _ time.Duration) (string, bool) {
	if refreshed, ok := s.refreshed[value]; ok {
		return refreshed, true
	}
	return "", false
}

func (s analysisMediaRepositoryStub) GetReport(_ context.Context, _, _ string) (domain.Report, error) {
	return s.report, nil
}

func TestGetReportRefreshesExpiredSignedURL(t *testing.T) {
	stale := "https://bucket.cos.ap-guangzhou.myqcloud.com/user/face.jpg?sign=expired"
	repo := analysisMediaRepositoryStub{report: domain.Report{ID: "report-1", CurrentImageURL: stale}}
	service := &Service{
		repo:          repo,
		publicBaseURL: "https://api.example.test",
		storage:       refreshStorageStub{refreshed: map[string]string{stale: "https://bucket.cos.ap-guangzhou.myqcloud.com/user/face.jpg?sign=fresh"}},
	}
	report, err := service.GetReport(context.Background(), "user-1", "report-1")
	if err != nil {
		t.Fatal(err)
	}
	if report.CurrentImageURL != "https://bucket.cos.ap-guangzhou.myqcloud.com/user/face.jpg?sign=fresh" {
		t.Fatalf("stale signed URL was not refreshed: %q", report.CurrentImageURL)
	}
}

func TestGetReportExpandsRelativeStoredURL(t *testing.T) {
	repo := analysisMediaRepositoryStub{report: domain.Report{ID: "report-1", CurrentImageURL: "/uploads/user/face.jpg"}}
	service := &Service{repo: repo, publicBaseURL: "https://api.example.test"}
	report, err := service.GetReport(context.Background(), "user-1", "report-1")
	if err != nil {
		t.Fatal(err)
	}
	if report.CurrentImageURL != "https://api.example.test/uploads/user/face.jpg" {
		t.Fatalf("relative stored URL was not expanded: %q", report.CurrentImageURL)
	}
}
