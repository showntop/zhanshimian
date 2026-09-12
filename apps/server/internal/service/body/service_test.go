package body

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

type stubRepo struct {
	created    domain.CreatedBodyPresentation
	createErr  error
	createArgs []string
	stored     domain.StoredBodyPresentation
	storedErr  error
	task       domain.Task
	taskErr    error
}

func (r *stubRepo) CreateBodyPresentation(_ context.Context, userID string, input domain.BodyPresentationInput, maxAttempts int) (domain.CreatedBodyPresentation, error) {
	r.createArgs = []string{userID, input.BodyMediaID, input.FaceMediaID}
	return r.created, r.createErr
}

func (r *stubRepo) GetBodyPresentation(_ context.Context, _, _ string) (domain.StoredBodyPresentation, error) {
	return r.stored, r.storedErr
}

func (r *stubRepo) ListBodyPresentationStatus(_ context.Context, _ string) (*domain.StoredBodyPresentation, *domain.StoredBodyPresentation, *domain.StoredBodyPresentation, error) {
	return nil, nil, nil, nil
}

func (r *stubRepo) BodyOrbitTaskState(_ context.Context, _, _ string) (domain.Task, error) {
	return r.task, r.taskErr
}

func (r *stubRepo) GetBodyOrbitWork(_ context.Context, _, _ string) (domain.BodyPresentationInput, error) {
	return domain.BodyPresentationInput{}, nil
}

func (r *stubRepo) ApplyBodyOrbitResult(_ context.Context, _, _ string, _ int, _ []float64, _ []string, _ string) error {
	return nil
}

type stubAssets struct {
	assets []domain.MediaAsset
	err    error
}

func (a *stubAssets) GetReadyAssets(_ context.Context, _ string, _ []string) ([]domain.MediaAsset, error) {
	return a.assets, a.err
}

type stubSigner struct{}

func (stubSigner) Sign(_ context.Context, key string) (string, error) { return "signed://" + key, nil }

type stubBilling struct {
	reserved   []string
	reserveErr error
}

func (b *stubBilling) Reserve(_ context.Context, userID, operationID string, product domain.Product, units int) (domain.Reservation, error) {
	b.reserved = append(b.reserved, operationID+":"+string(product))
	return domain.Reservation{}, b.reserveErr
}

type stubGenerator struct{}

func (stubGenerator) Generate(_ context.Context, _ OrbitInput) (OrbitOutput, error) {
	return OrbitOutput{VideoData: []byte("mp4"), MIMEType: "video/mp4", Duration: 3 * time.Second, ProviderVersion: "demo-body-orbit-v1"}, nil
}

const (
	bodyMediaID = "11111111-1111-1111-1111-111111111111"
	faceMediaID = "22222222-2222-2222-2222-222222222222"
)

func readyAssets() []domain.MediaAsset {
	return []domain.MediaAsset{
		{ID: bodyMediaID, Purpose: domain.MediaPurposeBody},
		{ID: faceMediaID, Purpose: domain.MediaPurposeFace},
	}
}

func newService(repo *stubRepo, assets AssetReader, billing Billing, gen Generator) *Service {
	return New(repo, assets, stubSigner{}, billing, gen, 3)
}

func TestCreateRequiresGenerator(t *testing.T) {
	svc := newService(&stubRepo{}, &stubAssets{assets: readyAssets()}, nil, nil)
	_, err := svc.Create(context.Background(), "user-1", domain.BodyPresentationInput{BodyMediaID: bodyMediaID, FaceMediaID: faceMediaID})
	if !errors.Is(err, ErrCapabilityUnavailable) {
		t.Fatalf("err = %v, want ErrCapabilityUnavailable", err)
	}
}

func TestCreateRejectsMalformedMediaIDs(t *testing.T) {
	svc := newService(&stubRepo{}, &stubAssets{assets: readyAssets()}, nil, stubGenerator{})
	_, err := svc.Create(context.Background(), "user-1", domain.BodyPresentationInput{BodyMediaID: "not-a-uuid", FaceMediaID: faceMediaID})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

func TestCreateMapsMissingMediaToValidation(t *testing.T) {
	svc := newService(&stubRepo{}, &stubAssets{err: repository.ErrNotFound}, nil, stubGenerator{})
	_, err := svc.Create(context.Background(), "user-1", domain.BodyPresentationInput{BodyMediaID: bodyMediaID, FaceMediaID: faceMediaID})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

func TestCreateRejectsWrongPurpose(t *testing.T) {
	assets := readyAssets()
	assets[0].Purpose = domain.MediaPurposeFace // body 槽位给了正脸
	svc := newService(&stubRepo{}, &stubAssets{assets: assets}, nil, stubGenerator{})
	_, err := svc.Create(context.Background(), "user-1", domain.BodyPresentationInput{BodyMediaID: bodyMediaID, FaceMediaID: faceMediaID})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

func TestCreateReservesCreditsOnce(t *testing.T) {
	repo := &stubRepo{created: domain.CreatedBodyPresentation{
		Presentation: domain.StoredBodyPresentation{Presentation: domain.BodyPresentation{ID: "p-1"}},
		Operation:    domain.Operation{ID: "op-1"},
	}}
	billing := &stubBilling{}
	svc := newService(repo, &stubAssets{assets: readyAssets()}, billing, stubGenerator{})
	if _, err := svc.Create(context.Background(), "user-1", domain.BodyPresentationInput{BodyMediaID: bodyMediaID, FaceMediaID: faceMediaID}); err != nil {
		t.Fatal(err)
	}
	if len(billing.reserved) != 1 || billing.reserved[0] != "op-1:body_orbit" {
		t.Fatalf("reserved = %v", billing.reserved)
	}
}

func TestCreateReusedSkipsReserve(t *testing.T) {
	repo := &stubRepo{created: domain.CreatedBodyPresentation{
		Presentation: domain.StoredBodyPresentation{Presentation: domain.BodyPresentation{ID: "p-1"}},
		Operation:    domain.Operation{ID: "op-1"},
		Reused:       true,
	}}
	billing := &stubBilling{}
	svc := newService(repo, &stubAssets{assets: readyAssets()}, billing, stubGenerator{})
	if _, err := svc.Create(context.Background(), "user-1", domain.BodyPresentationInput{BodyMediaID: bodyMediaID, FaceMediaID: faceMediaID}); err != nil {
		t.Fatal(err)
	}
	if len(billing.reserved) != 0 {
		t.Fatalf("reused create must not reserve, got %v", billing.reserved)
	}
}

func TestProjectMapsTaskStates(t *testing.T) {
	cases := []struct {
		status   domain.TaskStatus
		want     string
		progress int
	}{
		{domain.TaskQueued, "queued", 0},
		{domain.TaskLeased, "processing", 42},
		{domain.TaskRetryWait, "processing", 42},
		{domain.TaskSucceeded, "completed", 100},
		{domain.TaskFailed, "failed", 0},
	}
	for _, tc := range cases {
		item := &domain.StoredBodyPresentation{Presentation: domain.BodyPresentation{ID: "p-1"}}
		svc := newService(&stubRepo{task: domain.Task{Status: tc.status, ProgressBPS: tc.progress * 100}}, nil, nil, stubGenerator{})
		if err := svc.project(context.Background(), "user-1", item); err != nil {
			t.Fatal(err)
		}
		if item.Presentation.Status != tc.want || item.Presentation.Progress != tc.progress {
			t.Fatalf("status %s → got (%s,%d), want (%s,%d)", tc.status, item.Presentation.Status, item.Presentation.Progress, tc.want, tc.progress)
		}
	}
}

func TestProjectSignsVideoAndFrameURLs(t *testing.T) {
	repo := &stubRepo{taskErr: repository.ErrNotFound}
	svc := newService(repo, nil, nil, stubGenerator{})
	item := &domain.StoredBodyPresentation{
		Presentation: domain.BodyPresentation{
			ID:    "p-1",
			Orbit: domain.BodyOrbitView{Frames: []domain.OrbitFrame{{Yaw: 0}, {Yaw: 90}}},
		},
		VideoStorageKey: "orbit/video.mp4",
		FrameKeys:       []string{"orbit/f0.jpg", "orbit/f1.jpg"},
	}
	if err := svc.project(context.Background(), "user-1", item); err != nil {
		t.Fatal(err)
	}
	if item.Presentation.Orbit.VideoURL != "signed://orbit/video.mp4" {
		t.Fatalf("video url = %q", item.Presentation.Orbit.VideoURL)
	}
	if item.Presentation.Orbit.Frames[1].URL != "signed://orbit/f1.jpg" {
		t.Fatalf("frame url = %q", item.Presentation.Orbit.Frames[1].URL)
	}
	if item.Presentation.Status != "completed" {
		t.Fatalf("completed presentation without task must project completed, got %q", item.Presentation.Status)
	}
}

func TestStatusReportsAvailability(t *testing.T) {
	on := newService(&stubRepo{}, nil, nil, stubGenerator{})
	off := newService(&stubRepo{}, nil, nil, nil)
	gotOn, err := on.Status(context.Background(), "user-1")
	if err != nil || !gotOn.Available {
		t.Fatalf("available = %+v, err = %v", gotOn, err)
	}
	gotOff, err := off.Status(context.Background(), "user-1")
	if err != nil || gotOff.Available {
		t.Fatalf("available = %+v, err = %v", gotOff, err)
	}
}
