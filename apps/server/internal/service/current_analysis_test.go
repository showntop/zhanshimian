package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

type currentAnalysisRepoStub struct {
	repository.Repository
	tasks    []domain.Task
	analysis domain.Analysis
}

func (s currentAnalysisRepoStub) ActiveTasks(context.Context, string, int) ([]domain.Task, error) {
	return s.tasks, nil
}

func (s currentAnalysisRepoStub) GetAnalysis(_ context.Context, _, id string) (domain.Analysis, error) {
	if s.analysis.ID != id {
		return domain.Analysis{}, repository.ErrNotFound
	}
	return s.analysis, nil
}

func (s currentAnalysisRepoStub) GetMediaAssetsForUser(context.Context, string, []string) ([]domain.MediaAsset, error) {
	return nil, nil
}

func TestGetCurrentAnalysisReturnsInFlightAnalysis(t *testing.T) {
	const analysisID = "61105915-5e49-4aa5-b8b3-a9d0dd476ec9"
	payload, err := json.Marshal(domain.AnalysisTaskPayload{AnalysisID: analysisID})
	if err != nil {
		t.Fatal(err)
	}
	repo := currentAnalysisRepoStub{
		tasks: []domain.Task{{
			Type:      string(domain.TaskTypeAnalysis),
			Status:    domain.TaskProcessing,
			Payload:   payload,
			UpdatedAt: time.Now(),
		}},
		analysis: domain.Analysis{ID: analysisID, Status: domain.AnalysisProcessing, UpdatedAt: time.Now()},
	}
	got, err := (&Service{repo: repo}).GetCurrentAnalysis(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != analysisID {
		t.Fatalf("got %#v", got)
	}
}

func TestGetAnalysisRejectsNonUUID(t *testing.T) {
	_, err := (&Service{repo: currentAnalysisRepoStub{}}).GetAnalysis(context.Background(), "user-1", "current")
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected not found for sentinel id, got %v", err)
	}
}

func TestGetCurrentAnalysisNotFoundWithoutActiveTask(t *testing.T) {
	_, err := (&Service{repo: currentAnalysisRepoStub{}}).GetCurrentAnalysis(context.Background(), "user-1")
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}
