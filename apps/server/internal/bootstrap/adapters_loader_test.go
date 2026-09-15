package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/zhanshimian/server/internal/domain"
)

// fakeProcessedStorage 模拟开通/未开通数据万象两种对象库。
type fakeProcessedStorage struct {
	processedData []byte
	processedErr  error
	openData      []byte
}

func (f fakeProcessedStorage) Save(context.Context, string, io.Reader) (string, error) {
	return "", errors.New("not implemented")
}

func (f fakeProcessedStorage) Open(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(f.openData)), nil
}

func (f fakeProcessedStorage) Delete(context.Context, string) error { return nil }

func (f fakeProcessedStorage) OpenProcessed(_ context.Context, _, _ string) (io.ReadCloser, error) {
	if f.processedErr != nil {
		return nil, f.processedErr
	}
	return io.NopCloser(bytes.NewReader(f.processedData)), nil
}

// 回归：PNG 资产经数据万象下载时处理后产出 JPEG 字节，MIME 必须按真实
// 格式标记为 image/jpeg，否则技术校验报 photo_mime_mismatch。
func TestImageLoaderMarksCIProcessedBytesAsJPEG(t *testing.T) {
	store := fakeProcessedStorage{processedData: []byte("ci-jpeg-bytes"), openData: []byte("original-png")}
	images, err := imageLoader{objects: store}.Load(context.Background(), []domain.PhotoSetItem{{
		Role:  domain.PhotoRoleFace,
		Asset: domain.MediaAsset{ObjectKey: "u/uploads/x", MIMEType: "image/png"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(images))
	}
	if images[0].MIMEType != "image/jpeg" {
		t.Fatalf("CI-processed bytes must be marked image/jpeg, got %s", images[0].MIMEType)
	}
	if string(images[0].Data) != "ci-jpeg-bytes" {
		t.Fatalf("expected CI-processed bytes, got %q", images[0].Data)
	}
}

func TestImageLoaderKeepsDeclaredMIMEWithoutCI(t *testing.T) {
	store := fakeProcessedStorage{processedErr: errors.New("ci disabled"), openData: []byte("original-png")}
	images, err := imageLoader{objects: store}.Load(context.Background(), []domain.PhotoSetItem{{
		Role:  domain.PhotoRoleFace,
		Asset: domain.MediaAsset{ObjectKey: "u/uploads/x", MIMEType: "image/png"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(images))
	}
	if images[0].MIMEType != "image/png" {
		t.Fatalf("fallback original bytes must keep declared mime, got %s", images[0].MIMEType)
	}
	if string(images[0].Data) != "original-png" {
		t.Fatalf("expected original bytes on CI fallback, got %q", images[0].Data)
	}
}
