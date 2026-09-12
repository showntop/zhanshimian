package media

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

func TestCompleteUploadRejectsHeadMismatch(t *testing.T) {
	repo := newMediaRepoFake()
	store := &objectStoreFake{head: domain.ObjectMetadata{
		ObjectKey: "users/u1/uploads/i1", MIMEType: "image/png",
		ByteSize: 21, SHA256: strings.Repeat("a", 64),
	}}
	svc := New(repo, store, 10<<20, 15*time.Minute)
	intent, err := svc.CreateUploadIntent(context.Background(), "u1", CreateIntentInput{
		Purpose:  domain.MediaPurposeFace,
		MIMEType: "image/jpeg",
		ByteSize: 20,
		SHA256:   strings.Repeat("b", 64),
	})
	if err != nil {
		t.Fatalf("CreateUploadIntent: %v", err)
	}
	_, err = svc.CompleteUploadIntent(context.Background(), "u1", intent.ID)
	if !errors.Is(err, ErrUploadMetadataMismatch) {
		t.Fatalf("CompleteUploadIntent error = %v, want ErrUploadMetadataMismatch", err)
	}
	if repo.completeCalls != 0 {
		t.Fatalf("completeCalls = %d, want 0", repo.completeCalls)
	}
}

func TestCompleteUploadIsIdempotent(t *testing.T) {
	repo := newMediaRepoFake()
	store := matchingObjectStore()
	svc := New(repo, store, 10<<20, 15*time.Minute)
	intent := createIntent(t, svc, "u1")
	first, err := svc.CompleteUploadIntent(context.Background(), "u1", intent.ID)
	if err != nil {
		t.Fatalf("first CompleteUploadIntent: %v", err)
	}
	second, err := svc.CompleteUploadIntent(context.Background(), "u1", intent.ID)
	if err != nil {
		t.Fatalf("second CompleteUploadIntent: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("asset IDs differ: %s vs %s", first.ID, second.ID)
	}
	if repo.insertAssetCalls != 1 {
		t.Fatalf("insertAssetCalls = %d, want 1", repo.insertAssetCalls)
	}
}

func TestCompleteUploadCrossUserNotFound(t *testing.T) {
	repo := newMediaRepoFake()
	store := matchingObjectStore()
	svc := New(repo, store, 10<<20, 15*time.Minute)
	intent := createIntent(t, svc, "u1")
	_, err := svc.CompleteUploadIntent(context.Background(), "u2", intent.ID)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("cross-user complete error = %v, want ErrNotFound", err)
	}
	if repo.completeCalls != 0 {
		t.Fatalf("completeCalls = %d, want 0", repo.completeCalls)
	}
}

func TestCreateUploadIntentObjectKeyUsesIntentID(t *testing.T) {
	repo := newMediaRepoFake()
	svc := New(repo, matchingObjectStore(), 10<<20, 15*time.Minute)
	userID := "u1"
	intent := createIntent(t, svc, userID)
	if intent.ID == "" {
		t.Fatal("intent ID is empty")
	}
	wantKey := "users/" + userID + "/uploads/" + intent.ID
	if intent.ObjectKey != wantKey {
		t.Fatalf("object key = %q, want %q", intent.ObjectKey, wantKey)
	}
	stored, err := repo.GetUploadIntent(context.Background(), userID, intent.ID)
	if err != nil {
		t.Fatalf("GetUploadIntent: %v", err)
	}
	if stored.ID != intent.ID {
		t.Fatalf("stored ID = %q, want %q", stored.ID, intent.ID)
	}
	if stored.ObjectKey != wantKey {
		t.Fatalf("stored object key = %q, want %q", stored.ObjectKey, wantKey)
	}
}

func TestCreateUploadIntentRejectsInvalidInput(t *testing.T) {
	svc := New(newMediaRepoFake(), matchingObjectStore(), 10<<20, 15*time.Minute)
	valid := CreateIntentInput{
		Purpose: domain.MediaPurposeFace, MIMEType: "image/jpeg",
		ByteSize: 20, SHA256: strings.Repeat("b", 64),
	}
	cases := []CreateIntentInput{
		{Purpose: "render_candidate", MIMEType: valid.MIMEType, ByteSize: valid.ByteSize, SHA256: valid.SHA256},
		{Purpose: valid.Purpose, MIMEType: "image/webp", ByteSize: valid.ByteSize, SHA256: valid.SHA256},
		{Purpose: valid.Purpose, MIMEType: valid.MIMEType, ByteSize: 0, SHA256: valid.SHA256},
		{Purpose: valid.Purpose, MIMEType: valid.MIMEType, ByteSize: 11 << 20, SHA256: valid.SHA256},
		{Purpose: valid.Purpose, MIMEType: valid.MIMEType, ByteSize: valid.ByteSize, SHA256: strings.Repeat("B", 64)},
	}
	for _, input := range cases {
		if _, err := svc.CreateUploadIntent(context.Background(), "u1", input); !errors.Is(err, ErrValidation) {
			t.Fatalf("CreateUploadIntent(%+v) error = %v, want ErrValidation", input, err)
		}
	}
}

func createIntent(t *testing.T, svc *Service, userID string) domain.UploadIntent {
	t.Helper()
	intent, err := svc.CreateUploadIntent(context.Background(), userID, CreateIntentInput{
		Purpose:  domain.MediaPurposeFace,
		MIMEType: "image/jpeg",
		ByteSize: 20,
		SHA256:   strings.Repeat("b", 64),
	})
	if err != nil {
		t.Fatalf("createIntent: %v", err)
	}
	return intent.UploadIntent
}

type mediaRepoFake struct {
	intents          map[string]domain.UploadIntent
	assets           map[string]domain.MediaAsset
	completeCalls    int
	insertAssetCalls int
}

func newMediaRepoFake() *mediaRepoFake {
	return &mediaRepoFake{
		intents: make(map[string]domain.UploadIntent),
		assets:  make(map[string]domain.MediaAsset),
	}
}

func (r *mediaRepoFake) CreateUploadIntent(_ context.Context, in domain.CreateUploadIntent) (domain.UploadIntent, error) {
	id := in.ID
	if id == "" {
		id = uuid.NewString()
	}
	key := in.ObjectKey
	if key == "" {
		key = fmt.Sprintf("users/%s/uploads/%s", in.UserID, id)
	}
	intent := domain.UploadIntent{
		ID: id, UserID: in.UserID, Purpose: in.Purpose,
		MIMEType: in.MIMEType, ByteSize: in.ByteSize, SHA256: in.SHA256,
		ObjectKey: key, Status: domain.UploadIntentPending,
		ExpiresAt: in.ExpiresAt, Version: 1, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	r.intents[in.UserID+"/"+id] = intent
	return intent, nil
}

func (r *mediaRepoFake) GetUploadIntent(_ context.Context, userID, intentID string) (domain.UploadIntent, error) {
	intent, ok := r.intents[userID+"/"+intentID]
	if !ok {
		return domain.UploadIntent{}, repository.ErrNotFound
	}
	return intent, nil
}

func (r *mediaRepoFake) CompleteUploadIntent(ctx context.Context, in domain.CompleteUploadIntent) (domain.MediaAsset, bool, error) {
	r.completeCalls++
	intent, err := r.GetUploadIntent(ctx, in.UserID, in.IntentID)
	if err != nil {
		return domain.MediaAsset{}, false, err
	}
	if intent.Status == domain.UploadIntentCompleted && intent.CompletedMediaAssetID != "" {
		return r.assets[intent.CompletedMediaAssetID], false, nil
	}
	r.insertAssetCalls++
	asset := domain.MediaAsset{
		ID: uuid.NewString(), UserID: in.UserID,
		Origin: domain.MediaOriginUserUpload, Purpose: intent.Purpose,
		ObjectKey: in.Metadata.ObjectKey, SHA256: in.Metadata.SHA256,
		MIMEType: in.Metadata.MIMEType, ByteSize: in.Metadata.ByteSize,
		State: domain.MediaStateReady, DisplayKind: domain.DisplayKindOriginal,
		CreatedAt: time.Now(),
	}
	r.assets[asset.ID] = asset
	intent.Status = domain.UploadIntentCompleted
	intent.CompletedMediaAssetID = asset.ID
	r.intents[in.UserID+"/"+in.IntentID] = intent
	return asset, true, nil
}

type objectStoreFake struct {
	head    domain.ObjectMetadata
	objects map[string]domain.ObjectMetadata
	err     error
}

func matchingObjectStore() *objectStoreFake {
	return &objectStoreFake{objects: map[string]domain.ObjectMetadata{}}
}

func (s *objectStoreFake) PresignUpload(_ context.Context, intent domain.UploadIntent, ttl time.Duration) (domain.UploadGrant, error) {
	if s.err != nil {
		return domain.UploadGrant{}, s.err
	}
	if s.objects != nil {
		s.objects[intent.ObjectKey] = domain.ObjectMetadata{
			ObjectKey: intent.ObjectKey, MIMEType: intent.MIMEType,
			ByteSize: intent.ByteSize, SHA256: intent.SHA256,
		}
	}
	return domain.UploadGrant{
		Method: "PUT",
		URL:    "https://private-cos.example/signed",
		Headers: map[string]string{
			"Content-Type":      intent.MIMEType,
			"Content-Length":    strconv.FormatInt(intent.ByteSize, 10),
			"x-cos-meta-sha256": intent.SHA256,
		},
		ExpiresAt: time.Now().Add(ttl),
	}, nil
}

func (s *objectStoreFake) HeadObject(_ context.Context, key string) (domain.ObjectMetadata, error) {
	if s.err != nil {
		return domain.ObjectMetadata{}, s.err
	}
	if s.head.ObjectKey != "" || s.head.MIMEType != "" || s.head.SHA256 != "" {
		return s.head, nil
	}
	meta, ok := s.objects[key]
	if !ok {
		return domain.ObjectMetadata{}, fmt.Errorf("missing object %s", key)
	}
	return meta, nil
}
