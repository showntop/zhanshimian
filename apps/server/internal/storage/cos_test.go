package storage

import (
	"context"
	"strings"
	"testing"
	"time"
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
