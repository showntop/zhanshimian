package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/media"
	"github.com/zhanshimian/server/internal/provider"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/storage"
)

type stubExtract struct {
	frames []media.Frame
	err    error
}

func (s stubExtract) Extract(context.Context, []byte, time.Duration, int) ([]media.Frame, error) {
	return s.frames, s.err
}

type stubOrbitGenerator struct {
	output provider.OrbitOutput
	err    error
}

func (g stubOrbitGenerator) Generate(context.Context, provider.OrbitInput) (provider.OrbitOutput, error) {
	return g.output, g.err
}

type bodyOrbitTaskRepo struct {
	repository.Repository
	work            domain.BodyPresentationInput
	workErr         error
	assets          []domain.MediaAsset
	applied         bool
	videoURL        string
	videoKey        string
	durationMS      int
	frames          []domain.OrbitFrame
	frameKeys       []string
	providerVersion string
}

func (r *bodyOrbitTaskRepo) GetBodyOrbitWork(context.Context, string, string) (domain.BodyPresentationInput, error) {
	if r.workErr != nil {
		return domain.BodyPresentationInput{}, r.workErr
	}
	return r.work, nil
}

func (r *bodyOrbitTaskRepo) GetMediaAssets(context.Context, []string) ([]domain.MediaAsset, error) {
	return r.assets, nil
}

func (r *bodyOrbitTaskRepo) UpdateTaskProgress(context.Context, string, int, string) error {
	return nil
}

func (r *bodyOrbitTaskRepo) ApplyBodyOrbitResult(_ context.Context, _, videoURL, videoKey string, durationMS int, frames []domain.OrbitFrame, frameKeys []string, providerVersion string) error {
	r.applied = true
	r.videoURL = videoURL
	r.videoKey = videoKey
	r.durationMS = durationMS
	r.frames = append([]domain.OrbitFrame{}, frames...)
	r.frameKeys = append([]string{}, frameKeys...)
	r.providerVersion = providerVersion
	return nil
}

func orbitFixtureMP4(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "assets", "demo", "body-orbit.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) <= 4 {
		t.Fatal("Task 5 fixture must be more than a 4-byte ftyp header")
	}
	return data
}

func stubOrbitFrames(n int) []media.Frame {
	frames := make([]media.Frame, n)
	for i := range frames {
		frames[i] = media.Frame{Yaw: media.OrbitYaw(i, n), JPEG: []byte{0xFF, 0xD8, 0xFF, byte(i)}}
	}
	return frames
}

func bodyOrbitTask(presentationID string) domain.Task {
	payload, _ := json.Marshal(domain.BodyOrbitTaskPayload{PresentationID: presentationID})
	return domain.Task{
		ID: "task-orbit-1", UserID: "user-1", Type: string(domain.TaskTypeBodyOrbit),
		Payload: payload,
	}
}

func newBodyOrbitTaskService(t *testing.T, repo *bodyOrbitTaskRepo, gen provider.OrbitGenerator, extractor media.Extractor) *Service {
	t.Helper()
	objects, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		key  string
		data string
	}{
		{"user-1/body.jpg", "body-bytes"},
		{"user-1/face.jpg", "face-bytes"},
	} {
		if _, err := objects.Save(context.Background(), item.key, strings.NewReader(item.data)); err != nil {
			t.Fatal(err)
		}
	}
	repo.work = domain.BodyPresentationInput{
		BodyMediaID: "11111111-1111-1111-1111-111111111111",
		FaceMediaID: "22222222-2222-2222-2222-222222222222",
	}
	repo.assets = []domain.MediaAsset{
		{ID: repo.work.BodyMediaID, Kind: "body", StorageKey: "user-1/body.jpg", MIMEType: "image/jpeg"},
		{ID: repo.work.FaceMediaID, Kind: "face", StorageKey: "user-1/face.jpg", MIMEType: "image/jpeg"},
	}
	svc := New(repo, objects, nil, "http://127.0.0.1", time.Hour, 1<<20, nil, ProviderOptions{Orbit: gen})
	svc.orbitExtractor = extractor
	return svc
}

func TestProcessBodyOrbitAppliesSixteenFrames(t *testing.T) {
	repo := &bodyOrbitTaskRepo{}
	video := orbitFixtureMP4(t)
	svc := newBodyOrbitTaskService(t, repo, stubOrbitGenerator{output: provider.OrbitOutput{
		VideoData: video, MIMEType: "video/mp4", Duration: 3 * time.Second, ProviderVersion: "demo-body-orbit-v1",
	}}, stubExtract{frames: stubOrbitFrames(16)})

	resultRef, err := svc.processBodyOrbit(context.Background(), bodyOrbitTask("pres-1"))
	if err != nil {
		t.Fatal(err)
	}
	if resultRef != "pres-1" {
		t.Fatalf("resultRef=%q", resultRef)
	}
	if !repo.applied {
		t.Fatal("expected ApplyBodyOrbitResult")
	}
	if repo.videoURL == "" {
		t.Fatal("videoURL must be non-empty")
	}
	if len(repo.frames) != 16 || len(repo.frameKeys) != 16 {
		t.Fatalf("frames=%d keys=%d", len(repo.frames), len(repo.frameKeys))
	}
}

func TestProcessBodyOrbitDropsSparseFrames(t *testing.T) {
	repo := &bodyOrbitTaskRepo{}
	svc := newBodyOrbitTaskService(t, repo, stubOrbitGenerator{output: provider.OrbitOutput{
		VideoData: orbitFixtureMP4(t), MIMEType: "video/mp4", Duration: 3 * time.Second,
	}}, stubExtract{frames: stubOrbitFrames(3)})

	if _, err := svc.processBodyOrbit(context.Background(), bodyOrbitTask("pres-1")); err != nil {
		t.Fatal(err)
	}
	if !repo.applied {
		t.Fatal("sparse extract must still apply the video")
	}
	if repo.videoURL == "" {
		t.Fatal("videoURL must be non-empty")
	}
	if len(repo.frames) != 0 {
		t.Fatalf("sparse frames must be dropped, got %d", len(repo.frames))
	}
}

func TestProcessBodyOrbitRejectsNonMP4(t *testing.T) {
	repo := &bodyOrbitTaskRepo{}
	svc := newBodyOrbitTaskService(t, repo, stubOrbitGenerator{output: provider.OrbitOutput{
		VideoData: orbitFixtureMP4(t), MIMEType: "video/webm", Duration: 3 * time.Second,
	}}, stubExtract{frames: stubOrbitFrames(16)})

	_, err := svc.processBodyOrbit(context.Background(), bodyOrbitTask("pres-1"))
	if err == nil {
		t.Fatal("expected permanent failure")
	}
	var permanent *permanentTaskError
	if !errors.As(err, &permanent) {
		t.Fatalf("want permanentTaskError, got %T %v", err, err)
	}
	if repo.applied {
		t.Fatal("unsupported MIME must not Apply")
	}
}
