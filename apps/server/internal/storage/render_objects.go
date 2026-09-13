package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
)

// 渲染对象隔离的稳定失败码。
const (
	ErrCodeObjectExists       = "object_exists"
	ErrCodeCrossUserObject    = "cross_user_object"
	ErrCodeObjectHashMismatch = "object_hash_mismatch"
)

// renderObjectError carries a stable code for the rendering workflow.
type renderObjectError struct {
	Code string
	Err  error
}

func (e *renderObjectError) Error() string { return e.Code }
func (e *renderObjectError) Unwrap() error { return e.Err }

func renderError(code string, err error) error {
	return &renderObjectError{Code: code, Err: err}
}

// RenderObjectStore isolates candidate bytes (quarantine prefix) from
// published bytes (published prefix). Both are create-only: an existing
// object is never overwritten.
type RenderObjectStore struct {
	base ObjectStorage
}

func NewRenderObjectStore(base ObjectStorage) *RenderObjectStore {
	return &RenderObjectStore{base: base}
}

// CandidateKey is the quarantine key of one candidate.
func CandidateKey(userID, runID, candidateID string) string {
	return fmt.Sprintf("users/%s/render-quarantine/%s/%s.jpg", userID, runID, candidateID)
}

// PublishedKey is the immutable published key of one publication.
func PublishedKey(userID, publicationID string) string {
	return fmt.Sprintf("users/%s/render-published/%s.jpg", userID, publicationID)
}

// StoredObject is one successful create-only write.
type StoredObject struct {
	Key      string
	SHA256   string
	ByteSize int64
}

// PutCandidate writes candidate bytes to the quarantine prefix.
func (s *RenderObjectStore) PutCandidate(ctx context.Context, userID, runID, candidateID string, data []byte) (StoredObject, error) {
	key := CandidateKey(userID, runID, candidateID)
	if err := s.writeExclusive(ctx, key, data); err != nil {
		return StoredObject{}, err
	}
	return storedOf(key, data), nil
}

// Promote copies a quarantined candidate to the published prefix, verifying
// the copied bytes against the expected hash. On mismatch the published
// object is deleted again — the quarantine copy stays for 30-day GC.
func (s *RenderObjectStore) Promote(ctx context.Context, userID, publicationID, sourceKey, expectedSHA256 string) (StoredObject, error) {
	if !belongsToFileUser(userID, sourceKey) {
		return StoredObject{}, renderError(ErrCodeCrossUserObject, fmt.Errorf("source key %q does not belong to user", sourceKey))
	}
	reader, err := s.base.Open(ctx, sourceKey)
	if err != nil {
		return StoredObject{}, err
	}
	data, err := io.ReadAll(reader)
	reader.Close()
	if err != nil {
		return StoredObject{}, err
	}
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if got != expectedSHA256 {
		return StoredObject{}, renderError(ErrCodeObjectHashMismatch, fmt.Errorf("source hash %s != expected %s", got, expectedSHA256))
	}
	key := PublishedKey(userID, publicationID)
	if err = s.writeExclusive(ctx, key, data); err != nil {
		return StoredObject{}, err
	}
	return storedOf(key, data), nil
}

// Delete removes an object; used only by the caller after a failed database
// CAS to clean up the just-created published object.
func (s *RenderObjectStore) Delete(ctx context.Context, key string) error {
	return s.base.Delete(ctx, key)
}

func (s *RenderObjectStore) writeExclusive(ctx context.Context, key string, data []byte) error {
	existing, err := s.base.Open(ctx, key)
	if err == nil {
		existing.Close()
		return renderError(ErrCodeObjectExists, fmt.Errorf("object %q already exists", key))
	}
	if _, err = s.base.Save(ctx, key, bytes.NewReader(data)); err != nil {
		// Local 存储用 O_EXCL 打开,并发/重复写入在这里暴露。
		return renderError(ErrCodeObjectExists, err)
	}
	return nil
}

func storedOf(key string, data []byte) StoredObject {
	sum := sha256.Sum256(data)
	return StoredObject{Key: key, SHA256: hex.EncodeToString(sum[:]), ByteSize: int64(len(data))}
}

func belongsToFileUser(userID, key string) bool {
	prefix := "users/" + userID + "/"
	return len(key) > len(prefix) && key[:len(prefix)] == prefix
}
