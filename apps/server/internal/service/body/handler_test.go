package body

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/media"
)

type handlerRepoStub struct {
	work        domain.BodyPresentationInput
	workErr     error
	assets      []domain.MediaAsset
	assetsErr   error
	applied     []appliedResult
	applyErr    error
	commitCalls int
}

type appliedResult struct {
	videoKey   string
	durationMS int
	yaws       []float64
	keys       []string
	version    string
}

func (r *handlerRepoStub) GetBodyOrbitWork(_ context.Context, _, _ string) (domain.BodyPresentationInput, error) {
	return r.work, r.workErr
}

func (r *handlerRepoStub) GetReadyAssets(_ context.Context, _ string, _ []string) ([]domain.MediaAsset, error) {
	return r.assets, r.assetsErr
}

func (r *handlerRepoStub) ApplyBodyOrbitResult(_ context.Context, _, videoKey string, durationMS int, yaws []float64, keys []string, version string) error {
	r.applied = append(r.applied, appliedResult{videoKey: videoKey, durationMS: durationMS, yaws: yaws, keys: keys, version: version})
	return r.applyErr
}

func (r *handlerRepoStub) CommitBodyOrbit(_ context.Context, _ domain.TaskLease, _ domain.TaskResult) (domain.CommitOutcome, error) {
	r.commitCalls++
	return domain.CommitApplied, nil
}

type objectStoreStub struct {
	data    map[string][]byte
	deleted []string
}

func newObjectStoreStub() *objectStoreStub { return &objectStoreStub{data: map[string][]byte{}} }

func (s *objectStoreStub) Open(_ context.Context, key string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader([]byte("photo:" + key))), nil
}

func (s *objectStoreStub) Save(_ context.Context, key string, reader io.Reader) (string, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	s.data[key] = data
	return key, nil
}

func (s *objectStoreStub) Delete(_ context.Context, key string) error {
	s.deleted = append(s.deleted, key)
	return nil
}

type frameExtractorStub struct {
	frames []media.Frame
	err    error
}

func (e frameExtractorStub) Extract(_ context.Context, _ []byte, _ time.Duration, n int) ([]media.Frame, error) {
	return e.frames, e.err
}

func makeFrames(n int) []media.Frame {
	frames := make([]media.Frame, n)
	for i := range frames {
		frames[i] = media.Frame{Yaw: media.OrbitYaw(i, n), JPEG: []byte{byte(i)}}
	}
	return frames
}

func newLease() domain.TaskLease {
	return domain.TaskLease{Task: domain.Task{
		ID: "task-1", UserID: "user-1", OperationID: "op-1", SubjectID: "pres-1",
	}}
}

func handlerWorkRepo() *handlerRepoStub {
	return &handlerRepoStub{
		work: domain.BodyPresentationInput{BodyMediaID: "body-1", FaceMediaID: "face-1"},
		assets: []domain.MediaAsset{
			{ID: "body-1", Purpose: domain.MediaPurposeBody, ObjectKey: "u/body.jpg", MIMEType: "image/jpeg"},
			{ID: "face-1", Purpose: domain.MediaPurposeFace, ObjectKey: "u/face.jpg", MIMEType: "image/jpeg"},
		},
	}
}

func TestExecuteAppliesSixteenFrames(t *testing.T) {
	repo := handlerWorkRepo()
	objects := newObjectStoreStub()
	handler := NewHandler(repo, objects, nil, stubGenerator{}, frameExtractorStub{frames: makeFrames(media.OrbitFrameCount)}, nil)

	result, err := handler.Execute(context.Background(), newLease())
	if err != nil {
		t.Fatal(err)
	}
	if result.Disposition != domain.TaskPublish || result.ResultID != "pres-1" {
		t.Fatalf("result = %+v", result)
	}
	if len(repo.applied) != 1 {
		t.Fatalf("applied = %v", repo.applied)
	}
	got := repo.applied[0]
	if len(got.keys) != media.OrbitFrameCount || len(got.yaws) != media.OrbitFrameCount {
		t.Fatalf("frames = %d", len(got.keys))
	}
	if got.videoKey != "user-1/generated/body-orbit/pres-1.mp4" {
		t.Fatalf("videoKey = %q", got.videoKey)
	}
	if got.durationMS != 3000 || got.version != "demo-body-orbit-v1" {
		t.Fatalf("duration/version = %d/%q", got.durationMS, got.version)
	}
}

func TestExecuteDropsSparseFrames(t *testing.T) {
	repo := handlerWorkRepo()
	objects := newObjectStoreStub()
	handler := NewHandler(repo, objects, nil, stubGenerator{}, frameExtractorStub{frames: makeFrames(media.OrbitMinKeepFrames - 1)}, nil)

	result, err := handler.Execute(context.Background(), newLease())
	if err != nil {
		t.Fatal(err)
	}
	if result.Disposition != domain.TaskPublish {
		t.Fatalf("result = %+v", result)
	}
	if len(repo.applied) != 1 || len(repo.applied[0].keys) != 0 {
		t.Fatalf("degraded run must keep video without frames: %+v", repo.applied)
	}
	// 视频仍在，抽帧未写。
	if _, ok := objects.data["user-1/generated/body-orbit/pres-1.mp4"]; !ok {
		t.Fatal("video missing from object store")
	}
}

func TestExecuteRejectsNonMP4(t *testing.T) {
	repo := handlerWorkRepo()
	badGen := GeneratorFunc(func(_ context.Context, _ OrbitInput) (OrbitOutput, error) {
		return OrbitOutput{VideoData: []byte("webm"), MIMEType: "video/webm"}, nil
	})
	handler := NewHandler(repo, newObjectStoreStub(), nil, badGen, frameExtractorStub{}, nil)

	result, err := handler.Execute(context.Background(), newLease())
	if err != nil {
		t.Fatal(err)
	}
	if result.Disposition != domain.TaskDomainFail || result.Failure.Code != "body_orbit_bad_video" {
		t.Fatalf("result = %+v", result)
	}
}

func TestExecuteFailsWhenGeneratorMissing(t *testing.T) {
	handler := NewHandler(handlerWorkRepo(), newObjectStoreStub(), nil, nil, frameExtractorStub{}, nil)
	result, err := handler.Execute(context.Background(), newLease())
	if err != nil {
		t.Fatal(err)
	}
	if result.Disposition != domain.TaskDomainFail || result.Failure.Code != "body_orbit_not_configured" {
		t.Fatalf("result = %+v", result)
	}
}

// GeneratorFunc 让测试快速拼装生成器行为。
type GeneratorFunc func(ctx context.Context, input OrbitInput) (OrbitOutput, error)

func (f GeneratorFunc) Generate(ctx context.Context, input OrbitInput) (OrbitOutput, error) {
	return f(ctx, input)
}
