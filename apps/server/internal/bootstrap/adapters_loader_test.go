package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/zhanshimian/server/internal/domain"
)

var (
	testJPEGBytes = append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{1}, 64)...)
	testPNGBytes  = append([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, bytes.Repeat([]byte{2}, 64)...)
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

// 回归：PNG 资产（声明 image/png）经数据万象下载时处理后产出 JPEG 字节，
// MIME 必须按字节内容标记为 image/jpeg，否则技术校验报 photo_mime_mismatch。
func TestImageLoaderMarksCIProcessedBytesAsJPEG(t *testing.T) {
	store := fakeProcessedStorage{processedData: testJPEGBytes, openData: testPNGBytes}
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
	if !bytes.Equal(images[0].Data, testJPEGBytes) {
		t.Fatalf("expected CI-processed bytes, got %x", images[0].Data[:8])
	}
}

// 回归：声明猜错（PNG 字节被标成 image/jpeg）时按字节内容纠正，旧库错误行自愈。
func TestImageLoaderCorrectsWrongDeclaredMIME(t *testing.T) {
	store := fakeProcessedStorage{processedErr: errors.New("ci disabled"), openData: testPNGBytes}
	images, err := imageLoader{objects: store}.Load(context.Background(), []domain.PhotoSetItem{{
		Role:  domain.PhotoRoleFace,
		Asset: domain.MediaAsset{ObjectKey: "u/uploads/x", MIMEType: "image/jpeg"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(images))
	}
	if images[0].MIMEType != "image/png" {
		t.Fatalf("PNG bytes wrongly declared jpeg must be sniffed to image/png, got %s", images[0].MIMEType)
	}
	if !bytes.Equal(images[0].Data, testPNGBytes) {
		t.Fatalf("expected original bytes on CI fallback, got %x", images[0].Data[:8])
	}
}
