package service

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/provider"
	"github.com/zhanshimian/server/internal/repository"
)

type bodyOrbitRepoStub struct {
	repository.Repository
	assets       []domain.MediaAsset
	activeCount  int
	countedTypes []string
	createCalled bool
	created      domain.BodyPresentation
	createdTask  *domain.Task
	createErr    error
	applyN       int
}

func (r *bodyOrbitRepoStub) GetMediaAssetsForUser(_ context.Context, _ string, _ []string) ([]domain.MediaAsset, error) {
	return r.assets, nil
}

func (r *bodyOrbitRepoStub) CreateBodyPresentation(_ context.Context, _ string, _ domain.BodyPresentationInput) (domain.BodyPresentation, *domain.Task, error) {
	r.createCalled = true
	return r.created, r.createdTask, r.createErr
}

func (r *bodyOrbitRepoStub) CountActiveTasksByTypes(_ context.Context, _ string, types []string) (int, error) {
	r.countedTypes = append([]string{}, types...)
	return r.activeCount, nil
}

func (r *bodyOrbitRepoStub) ApplyBilling(context.Context, string, time.Time, int, func(domain.BillingSnapshot) (domain.BillingDecision, error)) error {
	r.applyN++
	return nil
}

func (r *bodyOrbitRepoStub) LatestTasksByRef(context.Context, string, domain.TaskType, string, []string) (map[string]domain.Task, error) {
	return map[string]domain.Task{}, nil
}

type fakeOrbitGen struct{}

func (fakeOrbitGen) Generate(context.Context, provider.OrbitInput) (provider.OrbitOutput, error) {
	return provider.OrbitOutput{}, nil
}

func newBodyOrbitService(repo repository.Repository, orbit provider.OrbitGenerator) *Service {
	return New(repo, nil, nil, "http://127.0.0.1", time.Hour, 1, slog.Default(), ProviderOptions{Orbit: orbit})
}

func TestCreateBodyPresentationRequiresGenerator(t *testing.T) {
	repo := &bodyOrbitRepoStub{}
	svc := New(repo, nil, nil, "http://127.0.0.1", time.Hour, 1, slog.Default())
	_, _, err := svc.CreateBodyPresentation(context.Background(), "user-1", domain.BodyPresentationInput{
		BodyMediaID: "11111111-1111-1111-1111-111111111111",
		FaceMediaID: "22222222-2222-2222-2222-222222222222",
	})
	if !errors.Is(err, ErrCapabilityUnavailable) {
		t.Fatalf("err=%v", err)
	}
	if repo.createCalled {
		t.Fatal("nil generator must not write")
	}
}

func TestCreateBodyPresentationRejectsWrongKind(t *testing.T) {
	repo := &bodyOrbitRepoStub{assets: []domain.MediaAsset{
		{ID: "11111111-1111-1111-1111-111111111111", Kind: "outfit"},
		{ID: "22222222-2222-2222-2222-222222222222", Kind: "face"},
	}}
	svc := newBodyOrbitService(repo, &fakeOrbitGen{})
	_, _, err := svc.CreateBodyPresentation(context.Background(), "user-1", domain.BodyPresentationInput{
		BodyMediaID: "11111111-1111-1111-1111-111111111111",
		FaceMediaID: "22222222-2222-2222-2222-222222222222",
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err=%v", err)
	}
}

func TestCreateBodyPresentationSkipsChargeWhenActive(t *testing.T) {
	const activeID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	repo := &bodyOrbitRepoStub{
		assets: []domain.MediaAsset{
			{ID: "11111111-1111-1111-1111-111111111111", Kind: "body"},
			{ID: "22222222-2222-2222-2222-222222222222", Kind: "face"},
		},
		activeCount: 1,
		created:     domain.BodyPresentation{ID: activeID},
		createdTask: &domain.Task{ID: "task-active", Type: string(domain.TaskTypeBodyOrbit)},
	}
	svc := newBodyOrbitService(repo, &fakeOrbitGen{})
	item, _, err := svc.CreateBodyPresentation(context.Background(), "user-1", domain.BodyPresentationInput{
		BodyMediaID: "11111111-1111-1111-1111-111111111111",
		FaceMediaID: "22222222-2222-2222-2222-222222222222",
	})
	if err != nil {
		t.Fatalf("CreateBodyPresentation: %v", err)
	}
	if !repo.createCalled {
		t.Fatal("expected repo CreateBodyPresentation")
	}
	if repo.applyN != 0 {
		t.Fatalf("in-flight body_orbit must not authorize, applyN=%d", repo.applyN)
	}
	if item.ID != activeID {
		t.Fatalf("id=%s", item.ID)
	}
}

func TestAuthorizeIncludesBodyOrbit(t *testing.T) {
	repo := &bodyOrbitRepoStub{}
	svc := &Service{repo: repo}
	if _, err := svc.authorize(context.Background(), "user-1", domainActionLook, "", 1); err != nil {
		t.Fatalf("authorize: %v", err)
	}
	found := false
	for _, typ := range repo.countedTypes {
		if typ == string(domain.TaskTypeBodyOrbit) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("CountActiveTasksByTypes types=%v", repo.countedTypes)
	}
}
