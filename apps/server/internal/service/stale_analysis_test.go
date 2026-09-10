package service

import (
	"context"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

type staleAnalysisRepositoryStub struct {
	repository.Repository
	analysis domain.Analysis
	failed   []string // 记录 FailAnalysisPresentation 调用的 analysisID
}

func (s *staleAnalysisRepositoryStub) GetAnalysis(_ context.Context, _, _ string) (domain.Analysis, error) {
	return s.analysis, nil
}

func (s *staleAnalysisRepositoryStub) GetMediaAssetsForUser(_ context.Context, _ string, _ []string) ([]domain.MediaAsset, error) {
	return nil, nil
}

func (s *staleAnalysisRepositoryStub) FailAnalysisPresentation(_ context.Context, analysisID, _, _ string) error {
	s.failed = append(s.failed, analysisID)
	return nil
}

func TestGetAnalysisFailsStaleProcessingOrphan(t *testing.T) {
	repo := &staleAnalysisRepositoryStub{analysis: domain.Analysis{
		ID: "analysis-1", Status: "processing", Progress: 72,
		UpdatedAt: time.Now().Add(-staleAnalysisProcessingTimeout - time.Minute),
	}}
	service := &Service{repo: repo}
	analysis, err := service.GetAnalysis(context.Background(), "user-1", "analysis-1")
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Status != "failed" || analysis.ErrorMessage == "" {
		t.Fatalf("stale processing analysis should fail with message, got %#v", analysis)
	}
	if len(repo.failed) != 1 || repo.failed[0] != "analysis-1" {
		t.Fatalf("FailAnalysisPresentation should be called once, got %v", repo.failed)
	}
}

func TestGetAnalysisKeepsFreshProcessing(t *testing.T) {
	repo := &staleAnalysisRepositoryStub{analysis: domain.Analysis{
		ID: "analysis-1", Status: "processing", Progress: 72,
		UpdatedAt: time.Now().Add(-time.Minute),
	}}
	service := &Service{repo: repo}
	analysis, err := service.GetAnalysis(context.Background(), "user-1", "analysis-1")
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Status != "processing" || len(repo.failed) != 0 {
		t.Fatalf("fresh processing analysis must stay untouched, got status=%s failed=%v", analysis.Status, repo.failed)
	}
}

func TestGetAnalysisQueuedUsesWiderWindow(t *testing.T) {
	// queued 11 分钟：仍在宽限窗内，不误杀
	repo := &staleAnalysisRepositoryStub{analysis: domain.Analysis{
		ID: "analysis-1", Status: "queued",
		UpdatedAt: time.Now().Add(-staleAnalysisProcessingTimeout - time.Minute),
	}}
	service := &Service{repo: repo}
	analysis, err := service.GetAnalysis(context.Background(), "user-1", "analysis-1")
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Status != "queued" || len(repo.failed) != 0 {
		t.Fatalf("queued within wide window must stay untouched, got status=%s failed=%v", analysis.Status, repo.failed)
	}

	// queued 21 分钟：孤儿，落失败
	repo = &staleAnalysisRepositoryStub{analysis: domain.Analysis{
		ID: "analysis-2", Status: "queued",
		UpdatedAt: time.Now().Add(-staleAnalysisQueuedTimeout - time.Minute),
	}}
	service = &Service{repo: repo}
	analysis, err = service.GetAnalysis(context.Background(), "user-1", "analysis-2")
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Status != "failed" || len(repo.failed) != 1 {
		t.Fatalf("stale queued analysis should fail, got status=%s failed=%v", analysis.Status, repo.failed)
	}
}
