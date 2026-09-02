package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ObjectStorage interface {
	Save(context.Context, string, io.Reader) (string, error)
	Open(context.Context, string) (io.ReadCloser, error)
	Delete(context.Context, string) error
}

// SignedURLStorage is implemented by private object stores that can grant
// short-lived read access without making the bucket public.
type SignedURLStorage interface {
	SignedURL(context.Context, string, time.Duration) (string, error)
}

// StoredURLRefresher is implemented by object stores whose persisted URLs can
// expire (signed URLs). RefreshURL returns a fresh URL when value is one of
// the store's own object URLs and reports whether the value was recognized.
type StoredURLRefresher interface {
	RefreshURL(value string, ttl time.Duration) (string, bool)
}

type Local struct{ Root string }

func NewLocal(root string) (*Local, error) {
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, fmt.Errorf("create upload directory: %w", err)
	}
	return &Local{Root: root}, nil
}

func (l *Local) Save(_ context.Context, key string, reader io.Reader) (string, error) {
	clean := filepath.Clean(strings.TrimPrefix(key, "/"))
	if clean == "." || strings.HasPrefix(clean, "..") {
		return "", fmt.Errorf("invalid storage key")
	}
	path := filepath.Join(l.Root, clean)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return "", err
	}
	defer file.Close()
	if _, err := io.Copy(file, reader); err != nil {
		return "", err
	}
	return clean, nil
}

func (l *Local) Open(_ context.Context, key string) (io.ReadCloser, error) {
	clean := filepath.Clean(strings.TrimPrefix(key, "/"))
	if clean == "." || strings.HasPrefix(clean, "..") {
		return nil, fmt.Errorf("invalid storage key")
	}
	return os.Open(filepath.Join(l.Root, clean))
}

func (l *Local) Delete(_ context.Context, key string) error {
	clean := filepath.Clean(strings.TrimPrefix(key, "/"))
	if clean == "." || strings.HasPrefix(clean, "..") {
		return fmt.Errorf("invalid storage key")
	}
	err := os.Remove(filepath.Join(l.Root, clean))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
