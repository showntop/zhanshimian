package service

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

type diagnosticRepoStub struct {
	repository.Repository
	latest    domain.ToolResult
	latestErr error
	got       domain.ToolResult
	gotErr    error
	assets    []domain.MediaAsset
}

func (r *diagnosticRepoStub) LatestDiagnostic(context.Context, string, string) (domain.ToolResult, error) {
	return r.latest, r.latestErr
}

func (r *diagnosticRepoStub) GetDiagnostic(context.Context, string, string) (domain.ToolResult, error) {
	return r.got, r.gotErr
}

func (r *diagnosticRepoStub) GetMediaAssetsForUser(context.Context, string, []string) ([]domain.MediaAsset, error) {
	return r.assets, nil
}

func newDiagnosticService(repo repository.Repository) *Service {
	return New(repo, nil, nil, "http://localhost:58000", time.Hour, 1024, slog.Default())
}

func TestLatestDiagnosticRejectsUnknownKind(t *testing.T) {
	svc := newDiagnosticService(&diagnosticRepoStub{})
	_, err := svc.LatestDiagnostic(context.Background(), "user-1", "hair")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("want validation error, got %v", err)
	}
}

func TestLatestDiagnosticHydratesImageURL(t *testing.T) {
	svc := newDiagnosticService(&diagnosticRepoStub{
		latest: domain.ToolResult{ID: "d1", Kind: "outfit", Conclusion: "先改一处", MediaID: "m1"},
		assets: []domain.MediaAsset{{ID: "m1", Kind: "outfit", StorageKey: "uploads/outfit.jpg"}},
	})
	got, err := svc.LatestDiagnostic(context.Background(), "user-1", "outfit")
	if err != nil {
		t.Fatalf("LatestDiagnostic: %v", err)
	}
	if got.ID != "d1" || got.ImageURL == "" || got.MediaID != "m1" {
		t.Fatalf("missing hydration: %#v", got)
	}
}

func TestGetDiagnosticNotFound(t *testing.T) {
	svc := newDiagnosticService(&diagnosticRepoStub{gotErr: repository.ErrNotFound})
	_, err := svc.GetDiagnostic(context.Background(), "user-1", "not-a-uuid")
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("invalid id should 404, got %v", err)
	}
}
