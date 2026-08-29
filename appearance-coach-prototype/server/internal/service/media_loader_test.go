package service

import (
	"context"
	"strings"
	"testing"

	"github.com/example/jianwo/server/internal/domain"
	"github.com/example/jianwo/server/internal/repository"
	"github.com/example/jianwo/server/internal/storage"
)

type mediaRepositoryStub struct {
	repository.Repository
	assets []domain.MediaAsset
}

func (s mediaRepositoryStub) GetMediaAssets(_ context.Context, _ []string) ([]domain.MediaAsset, error) {
	return s.assets, nil
}

func TestAnalysisMediaLoaderReadsProviderImages(t *testing.T) {
	objects, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		key  string
		data string
	}{{"user/face.jpg", "face-image"}, {"user/side.jpg", "side-image"}} {
		if _, err := objects.Save(context.Background(), item.key, strings.NewReader(item.data)); err != nil {
			t.Fatal(err)
		}
	}
	repo := mediaRepositoryStub{assets: []domain.MediaAsset{
		{ID: "face-id", Kind: "face", StorageKey: "user/face.jpg", MIMEType: "image/jpeg"},
		{ID: "side-id", Kind: "side", StorageKey: "user/side.jpg", MIMEType: "image/jpeg"},
	}}
	loader := NewAnalysisMediaLoader(repo, objects, "https://api.example.test/", 1024)

	images, err := loader.Load(context.Background(), []string{"face-id", "side-id"})
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 2 || string(images[0].Data) != "face-image" {
		t.Fatalf("unexpected loaded images: %#v", images)
	}
	if images[0].URL != "https://api.example.test/uploads/user/face.jpg" {
		t.Fatalf("unexpected public URL: %s", images[0].URL)
	}
}

func TestAnalysisMediaLoaderEnforcesProviderLimit(t *testing.T) {
	objects, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := objects.Save(context.Background(), "user/face.jpg", strings.NewReader("too-large")); err != nil {
		t.Fatal(err)
	}
	loader := NewAnalysisMediaLoader(mediaRepositoryStub{assets: []domain.MediaAsset{
		{ID: "face-id", Kind: "face", StorageKey: "user/face.jpg", MIMEType: "image/jpeg"},
	}}, objects, "http://localhost:8080", 3)

	if _, err := loader.Load(context.Background(), []string{"face-id"}); err == nil {
		t.Fatal("expected oversized provider image to be rejected")
	}
}
