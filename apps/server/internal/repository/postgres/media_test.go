package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/testutil"
)

func TestCompleteUploadIntentIsIdempotent(t *testing.T) {
	store, userID := newMediaStore(t)
	intent := insertIntent(t, store, userID, time.Now().Add(15*time.Minute))
	meta := metaFor(intent)
	first, created, err := store.CompleteUploadIntent(context.Background(), domain.CompleteUploadIntent{
		UserID: userID, IntentID: intent.ID, Metadata: meta,
	})
	if err != nil || !created {
		t.Fatalf("first complete: created=%v err=%v", created, err)
	}
	second, created, err := store.CompleteUploadIntent(context.Background(), domain.CompleteUploadIntent{
		UserID: userID, IntentID: intent.ID, Metadata: meta,
	})
	if err != nil || created {
		t.Fatalf("replay complete: created=%v err=%v", created, err)
	}
	if first.ID != second.ID {
		t.Fatalf("asset IDs differ: %s vs %s", first.ID, second.ID)
	}
	if countAssets(t, store, userID) != 1 {
		t.Fatalf("expected exactly one media asset")
	}
}

func TestCompleteUploadIntentCrossUserNotFound(t *testing.T) {
	store, userID := newMediaStore(t)
	otherID := insertUser(t, store)
	intent := insertIntent(t, store, userID, time.Now().Add(15*time.Minute))
	_, _, err := store.CompleteUploadIntent(context.Background(), domain.CompleteUploadIntent{
		UserID: otherID, IntentID: intent.ID, Metadata: metaFor(intent),
	})
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("cross-user complete error = %v, want ErrNotFound", err)
	}
	if countAssets(t, store, userID) != 0 {
		t.Fatal("cross-user complete created an asset")
	}
}

func TestCompleteUploadIntentRejectsExpired(t *testing.T) {
	store, userID := newMediaStore(t)
	intent := insertIntent(t, store, userID, time.Now().Add(-time.Minute))
	_, _, err := store.CompleteUploadIntent(context.Background(), domain.CompleteUploadIntent{
		UserID: userID, IntentID: intent.ID, Metadata: metaFor(intent),
	})
	if !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("expired complete error = %v, want ErrConflict", err)
	}
	if countAssets(t, store, userID) != 0 {
		t.Fatal("expired complete created an asset")
	}
}

func TestCreateUploadIntentStoresServerObjectKey(t *testing.T) {
	store, userID := newMediaStore(t)
	intentID := "11111111-1111-4111-8111-111111111111"
	intent, err := store.CreateUploadIntent(context.Background(), domain.CreateUploadIntent{
		ID:        intentID,
		UserID:    userID,
		Purpose:   domain.MediaPurposeFace,
		MIMEType:  "image/jpeg",
		ByteSize:  20,
		SHA256:    strings.Repeat("b", 64),
		ObjectKey: "users/" + userID + "/uploads/" + intentID,
		ExpiresAt: time.Now().Add(15 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.GetUploadIntent(context.Background(), userID, intent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ObjectKey != "users/"+userID+"/uploads/"+intentID {
		t.Fatalf("object key = %s", got.ObjectKey)
	}
	if got.Status != domain.UploadIntentPending {
		t.Fatalf("status = %s", got.Status)
	}
}

func newMediaStore(t *testing.T) (*Store, string) {
	t.Helper()
	store := New(testutil.NewPostgres(t))
	return store, insertUser(t, store)
}

func insertUser(t *testing.T, store *Store) string {
	t.Helper()
	var userID string
	if err := store.pool.QueryRow(context.Background(), `INSERT INTO users(nickname) VALUES('media-test') RETURNING id::text`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	return userID
}

func insertIntent(t *testing.T, store *Store, userID string, expiresAt time.Time) domain.UploadIntent {
	t.Helper()
	intent, err := store.CreateUploadIntent(context.Background(), domain.CreateUploadIntent{
		ID:        "22222222-2222-4222-8222-222222222222",
		UserID:    userID,
		Purpose:   domain.MediaPurposeBody,
		MIMEType:  "image/png",
		ByteSize:  32,
		SHA256:    strings.Repeat("c", 64),
		ObjectKey: "users/" + userID + "/uploads/22222222-2222-4222-8222-222222222222",
		ExpiresAt: expiresAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	return intent
}

func metaFor(intent domain.UploadIntent) domain.ObjectMetadata {
	return domain.ObjectMetadata{
		ObjectKey: intent.ObjectKey, MIMEType: intent.MIMEType,
		ByteSize: intent.ByteSize, SHA256: intent.SHA256,
	}
}

func countAssets(t *testing.T, store *Store, userID string) int {
	t.Helper()
	var count int
	if err := store.pool.QueryRow(context.Background(), `SELECT count(*) FROM media_assets WHERE user_id=$1::uuid`, userID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
