package storage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalStorageSaveAndDelete(t *testing.T) {
	root := t.TempDir()
	store, err := NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	key, err := store.Save(context.Background(), "user/photo.jpg", strings.NewReader("image"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, key)); err != nil {
		t.Fatalf("saved file missing: %v", err)
	}
	opened, err := store.Open(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	opened.Close()
	if err := store.Delete(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, key)); !os.IsNotExist(err) {
		t.Fatalf("expected deletion, got %v", err)
	}
}

func TestLocalStorageRejectsTraversal(t *testing.T) {
	store, _ := NewLocal(t.TempDir())
	if _, err := store.Save(context.Background(), "../escape", strings.NewReader("x")); err == nil {
		t.Fatal("expected traversal to be rejected")
	}
}
