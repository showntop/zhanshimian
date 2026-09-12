package storage

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
)

func TestCOSBuildsPrefixedSignedURL(t *testing.T) {
	objects, err := NewCOS(COSConfig{
		BucketURL: "https://jianwo-123.cos.ap-shanghai.myqcloud.com",
		SecretID:  "secret-id", SecretKey: "secret-key", KeyPrefix: "release",
	})
	if err != nil {
		t.Fatal(err)
	}
	value, err := objects.SignedURL(context.Background(), "user/photo.webp", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(value, "/release/user/photo.webp") || !strings.Contains(value, "q-signature=") {
		t.Fatalf("unexpected signed URL: %s", value)
	}
}

func TestCOSRejectsUnsafeObjectKey(t *testing.T) {
	objects, err := NewCOS(COSConfig{BucketURL: "https://bucket.example.com", SecretID: "id", SecretKey: "key"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := objects.SignedURL(context.Background(), "../secret", time.Minute); err == nil {
		t.Fatal("expected unsafe object key to be rejected")
	}
}

func TestCOSPresignUploadIncludesRequiredHeaders(t *testing.T) {
	objects, err := NewCOS(COSConfig{
		BucketURL: "https://jianwo-123.cos.ap-shanghai.myqcloud.com",
		SecretID:  "secret-id", SecretKey: "secret-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("b", 64)
	grant, err := objects.PresignUpload(context.Background(), domain.UploadIntent{
		ObjectKey: "users/u1/uploads/i1", MIMEType: "image/jpeg", ByteSize: 20, SHA256: sha,
	}, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if grant.Method != "PUT" {
		t.Fatalf("method = %s", grant.Method)
	}
	if grant.Headers["Content-Type"] != "image/jpeg" || grant.Headers["Content-Length"] != "20" || grant.Headers["x-cos-meta-sha256"] != sha {
		t.Fatalf("headers = %#v", grant.Headers)
	}
	if !strings.Contains(grant.URL, "/users/u1/uploads/i1") || !strings.Contains(grant.URL, "q-signature=") {
		t.Fatalf("unexpected signed URL: %s", grant.URL)
	}
	if grant.ExpiresAt.Before(time.Now().Add(14 * time.Minute)) {
		t.Fatalf("expires_at too soon: %s", grant.ExpiresAt)
	}
}

func TestCOSHeadObjectReturnsMetadata(t *testing.T) {
	sha := strings.Repeat("c", 64)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead || r.URL.Path != "/users/u1/uploads/i1" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Content-Length", "32")
		w.Header().Set("x-cos-meta-sha256", sha)
	}))
	t.Cleanup(server.Close)
	objects, err := NewCOS(COSConfig{
		BucketURL: server.URL, SecretID: "id", SecretKey: "key", HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	meta, err := objects.HeadObject(context.Background(), "users/u1/uploads/i1")
	if err != nil {
		t.Fatal(err)
	}
	if meta.ObjectKey != "users/u1/uploads/i1" || meta.MIMEType != "image/png" || meta.ByteSize != 32 || meta.SHA256 != sha {
		t.Fatalf("meta = %#v", meta)
	}
}

func TestCOSHeadObjectNotFound(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	objects, err := NewCOS(COSConfig{
		BucketURL: server.URL, SecretID: "id", SecretKey: "key", HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = objects.HeadObject(context.Background(), "users/u1/uploads/missing")
	if !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("error = %v, want ErrObjectNotFound", err)
	}
}

func TestLocalDirectUploadUnavailable(t *testing.T) {
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.PresignUpload(context.Background(), domain.UploadIntent{ObjectKey: "users/u1/uploads/i1"}, time.Minute)
	if !errors.Is(err, ErrDirectUploadUnavailable) {
		t.Fatalf("PresignUpload error = %v", err)
	}
	_, err = store.HeadObject(context.Background(), "users/u1/uploads/i1")
	if !errors.Is(err, ErrDirectUploadUnavailable) {
		t.Fatalf("HeadObject error = %v", err)
	}
}
