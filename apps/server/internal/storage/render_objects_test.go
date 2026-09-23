package storage

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func newMemoryObjectStore(t *testing.T) ObjectStorage {
	t.Helper()
	local, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return local
}

func candidateInput(userID, runID, candidateID string, data []byte) CandidateObjectInput {
	return CandidateObjectInput{UserID: userID, RunID: runID, CandidateID: candidateID, Data: data}
}

func TestRenderObjectStoreSeparatesQuarantineAndPublishedPrefixes(t *testing.T) {
	base := newMemoryObjectStore(t)
	store := NewRenderObjectStore(base)
	ctx := context.Background()
	candidate, err := store.PutCandidate(ctx, candidateInput("user-1", "run-1", "candidate-1", []byte("jpeg")))
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Key != "users/user-1/render-quarantine/run-1/candidate-1.jpg" {
		t.Fatalf("candidate key = %q", candidate.Key)
	}
	published, err := store.Promote(ctx, PromoteObjectInput{
		UserID: "user-1", PublicationID: "publication-1",
		SourceKey: candidate.Key, ExpectedSHA256: candidate.SHA256,
	})
	if err != nil {
		t.Fatal(err)
	}
	if published.Key != "users/user-1/render-published/publication-1.jpg" {
		t.Fatalf("published key = %q", published.Key)
	}
	if published.SHA256 != candidate.SHA256 {
		t.Fatalf("promote changed bytes: %s vs %s", published.SHA256, candidate.SHA256)
	}
}

func TestRenderObjectStoreRejectsOverwrite(t *testing.T) {
	store := NewRenderObjectStore(newMemoryObjectStore(t))
	ctx := context.Background()
	if _, err := store.PutCandidate(ctx, candidateInput("user-1", "run-1", "candidate-1", []byte("first"))); err != nil {
		t.Fatal(err)
	}
	_, err := store.PutCandidate(ctx, candidateInput("user-1", "run-1", "candidate-1", []byte("second")))
	if !hasRenderCode(err, ErrCodeObjectExists) {
		t.Fatalf("overwrite err = %v, want object_exists", err)
	}
}

func TestRenderObjectStoreRejectsCrossUserPromote(t *testing.T) {
	store := NewRenderObjectStore(newMemoryObjectStore(t))
	ctx := context.Background()
	candidate, err := store.PutCandidate(ctx, candidateInput("user-a", "run-1", "candidate-1", []byte("jpeg")))
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Promote(ctx, PromoteObjectInput{
		UserID: "user-b", PublicationID: "publication-1",
		SourceKey: candidate.Key, ExpectedSHA256: candidate.SHA256,
	})
	if !hasRenderCode(err, ErrCodeCrossUserObject) {
		t.Fatalf("err = %v, want cross_user_object", err)
	}
}

func TestRenderObjectStorePromoteHashMismatchCleansPublishedKey(t *testing.T) {
	base := newMemoryObjectStore(t)
	store := NewRenderObjectStore(base)
	ctx := context.Background()
	candidate, err := store.PutCandidate(ctx, candidateInput("user-1", "run-1", "candidate-1", []byte("jpeg")))
	if err != nil {
		t.Fatal(err)
	}
	// 篡改源对象内容,模拟 copy 后 hash 不一致。
	if err := base.Delete(ctx, candidate.Key); err != nil {
		t.Fatal(err)
	}
	if _, err := base.Save(ctx, candidate.Key, bytes.NewReader([]byte("tampered"))); err != nil {
		t.Fatal(err)
	}
	_, err = store.Promote(ctx, PromoteObjectInput{
		UserID: "user-1", PublicationID: "publication-1",
		SourceKey: candidate.Key, ExpectedSHA256: candidate.SHA256,
	})
	if !hasRenderCode(err, ErrCodeObjectHashMismatch) {
		t.Fatalf("err = %v, want object_hash_mismatch", err)
	}
	if _, err := base.Open(ctx, PublishedKey("user-1", "publication-1")); err == nil {
		t.Fatal("published key survived hash mismatch")
	}
}

func hasRenderCode(err error, code string) bool {
	var target *renderObjectError
	if !errors.As(err, &target) {
		return false
	}
	return target.Code == code
}
