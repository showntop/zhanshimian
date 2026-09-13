package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/account"
	"github.com/zhanshimian/server/internal/service/media"
	"github.com/zhanshimian/server/internal/storage"
)

func TestUploadIntentCreateReturnsPendingGrant(t *testing.T) {
	api, _ := newMediaAPI(t, matchingHTTPStore())
	userID := uuid.NewString()
	rec := doJSON(t, api, userID, http.MethodPost, "/v1/media/upload-intents", map[string]any{
		"purpose": "face", "mime_type": "image/jpeg", "byte_size": 20, "sha256": strings.Repeat("b", 64),
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data uploadIntentDTO `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.ID == "" || payload.Data.Purpose != "face" || payload.Data.Status != "pending" {
		t.Fatalf("intent = %#v", payload.Data)
	}
	if payload.Data.Upload.Method != "PUT" || payload.Data.Upload.URL == "" || payload.Data.Upload.Headers["Content-Type"] != "image/jpeg" {
		t.Fatalf("upload = %#v", payload.Data.Upload)
	}
}

func TestUploadIntentCompleteFirst201Replay200(t *testing.T) {
	api, _ := newMediaAPI(t, matchingHTTPStore())
	userID := uuid.NewString()
	intentID := createIntentID(t, api, userID)
	first := doJSON(t, api, userID, http.MethodPost, "/v1/media/upload-intents/"+intentID+"/complete", map[string]any{})
	if first.Code != http.StatusCreated {
		t.Fatalf("first status = %d body=%s", first.Code, first.Body.String())
	}
	second := doJSON(t, api, userID, http.MethodPost, "/v1/media/upload-intents/"+intentID+"/complete", map[string]any{})
	if second.Code != http.StatusOK {
		t.Fatalf("replay status = %d body=%s", second.Code, second.Body.String())
	}
	if assetID(t, first.Body.Bytes()) != assetID(t, second.Body.Bytes()) {
		t.Fatal("replay returned a different asset id")
	}
}

func TestUploadIntentCompleteCrossUserNotFound(t *testing.T) {
	api, _ := newMediaAPI(t, matchingHTTPStore())
	owner := uuid.NewString()
	intentID := createIntentID(t, api, owner)
	rec := doJSON(t, api, uuid.NewString(), http.MethodPost, "/v1/media/upload-intents/"+intentID+"/complete", map[string]any{})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCompleteUploadRejectsHeadMismatch(t *testing.T) {
	store := matchingHTTPStore()
	store.head = domain.ObjectMetadata{
		ObjectKey: "mismatch", MIMEType: "image/png", ByteSize: 21, SHA256: strings.Repeat("a", 64),
	}
	api, _ := newMediaAPI(t, store)
	userID := uuid.NewString()
	intentID := createIntentID(t, api, userID)
	rec := doJSON(t, api, userID, http.MethodPost, "/v1/media/upload-intents/"+intentID+"/complete", map[string]any{})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestNewWiresMediaAndLoginToken(t *testing.T) {
	repo := newSessionMediaRepo()
	local, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	objects := combinedObjectStore{ObjectStorage: local, ObjectStore: matchingHTTPStore()}
	handler := New(newTestDependencies(repo, objects), discardLogger(), true, RuntimeInfo{})

	login := httptest.NewRecorder()
	loginReq := httptest.NewRequest(http.MethodPost, "/v1/auth/dev", strings.NewReader(`{"nickname":"wired"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(login, loginReq)
	if login.Code != http.StatusCreated {
		t.Fatalf("login status = %d body=%s", login.Code, login.Body.String())
	}
	var session struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if session.Data.Token == "" {
		t.Fatalf("login token is empty: %s", login.Body.String())
	}

	rec := httptest.NewRecorder()
	body, err := json.Marshal(map[string]any{
		"purpose": "face", "mime_type": "image/jpeg", "byte_size": 20, "sha256": strings.Repeat("b", 64),
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/media/upload-intents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+session.Data.Token)
	req.Header.Set("Idempotency-Key", "wire-media-1")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create intent via New status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUploadIntentLocalReturns503(t *testing.T) {
	local, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	api := &API{media: media.New(newHTTPRepoFake(), local, 10<<20, 15*time.Minute), logger: discardLogger()}
	rec := doJSON(t, api, uuid.NewString(), http.MethodPost, "/v1/media/upload-intents", map[string]any{
		"purpose": "face", "mime_type": "image/jpeg", "byte_size": 20, "sha256": strings.Repeat("b", 64),
	})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func newMediaAPI(t *testing.T, store *httpObjectStore) (*API, *httpRepoFake) {
	t.Helper()
	repo := newHTTPRepoFake()
	return &API{media: media.New(repo, store, 10<<20, 15*time.Minute), logger: discardLogger()}, repo
}

func createIntentID(t *testing.T, api *API, userID string) string {
	t.Helper()
	rec := doJSON(t, api, userID, http.MethodPost, "/v1/media/upload-intents", map[string]any{
		"purpose": "face", "mime_type": "image/jpeg", "byte_size": 20, "sha256": strings.Repeat("b", 64),
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create intent status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Data.ID
}

func doJSON(t *testing.T, api *API, userID, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), userKey, domain.User{ID: userID}))
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/media/upload-intents", api.createUploadIntent)
	mux.HandleFunc("POST /v1/media/upload-intents/{id}/complete", api.completeUploadIntent)
	mux.ServeHTTP(rec, req)
	return rec
}

func assetID(t *testing.T, raw []byte) string {
	t.Helper()
	var payload struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Data.ID
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type httpRepoFake struct {
	intents map[string]domain.UploadIntent
	assets  map[string]domain.MediaAsset
}

func newHTTPRepoFake() *httpRepoFake {
	return &httpRepoFake{intents: map[string]domain.UploadIntent{}, assets: map[string]domain.MediaAsset{}}
}

func (r *httpRepoFake) CreateUploadIntent(_ context.Context, in domain.CreateUploadIntent) (domain.UploadIntent, error) {
	intent := domain.UploadIntent{
		ID: in.ID, UserID: in.UserID, Purpose: in.Purpose, MIMEType: in.MIMEType,
		ByteSize: in.ByteSize, SHA256: in.SHA256, ObjectKey: in.ObjectKey,
		Status: domain.UploadIntentPending, ExpiresAt: in.ExpiresAt, Version: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	r.intents[in.UserID+"/"+in.ID] = intent
	return intent, nil
}

func (r *httpRepoFake) GetUploadIntent(_ context.Context, userID, intentID string) (domain.UploadIntent, error) {
	intent, ok := r.intents[userID+"/"+intentID]
	if !ok {
		return domain.UploadIntent{}, repository.ErrNotFound
	}
	return intent, nil
}

func (r *httpRepoFake) CompleteUploadIntent(ctx context.Context, in domain.CompleteUploadIntent) (domain.MediaAsset, bool, error) {
	intent, err := r.GetUploadIntent(ctx, in.UserID, in.IntentID)
	if err != nil {
		return domain.MediaAsset{}, false, err
	}
	if intent.Status == domain.UploadIntentCompleted && intent.CompletedMediaAssetID != "" {
		return r.assets[intent.CompletedMediaAssetID], false, nil
	}
	asset := domain.MediaAsset{
		ID: uuid.NewString(), UserID: in.UserID, Origin: domain.MediaOriginUserUpload,
		Purpose: intent.Purpose, ObjectKey: in.Metadata.ObjectKey, SHA256: in.Metadata.SHA256,
		MIMEType: in.Metadata.MIMEType, ByteSize: in.Metadata.ByteSize,
		State: domain.MediaStateReady, DisplayKind: domain.DisplayKindOriginal, CreatedAt: time.Now(),
	}
	r.assets[asset.ID] = asset
	intent.Status = domain.UploadIntentCompleted
	intent.CompletedMediaAssetID = asset.ID
	r.intents[in.UserID+"/"+in.IntentID] = intent
	return asset, true, nil
}

type httpObjectStore struct {
	head    domain.ObjectMetadata
	objects map[string]domain.ObjectMetadata
}

func matchingHTTPStore() *httpObjectStore {
	return &httpObjectStore{objects: map[string]domain.ObjectMetadata{}}
}

func (s *httpObjectStore) PresignUpload(_ context.Context, intent domain.UploadIntent, ttl time.Duration) (domain.UploadGrant, error) {
	if s.objects != nil {
		s.objects[intent.ObjectKey] = domain.ObjectMetadata{
			ObjectKey: intent.ObjectKey, MIMEType: intent.MIMEType,
			ByteSize: intent.ByteSize, SHA256: intent.SHA256,
		}
	}
	return domain.UploadGrant{
		Method: "PUT", URL: "https://private-cos.example/signed",
		Headers: map[string]string{
			"Content-Type": intent.MIMEType, "Content-Length": strconv.FormatInt(intent.ByteSize, 10),
			"x-cos-meta-sha256": intent.SHA256,
		},
		ExpiresAt: time.Now().Add(ttl),
	}, nil
}

func (s *httpObjectStore) HeadObject(_ context.Context, key string) (domain.ObjectMetadata, error) {
	if s.head.ObjectKey != "" || s.head.MIMEType != "" {
		return s.head, nil
	}
	meta, ok := s.objects[key]
	if !ok {
		return domain.ObjectMetadata{}, fmt.Errorf("missing object")
	}
	return meta, nil
}

type combinedObjectStore struct {
	storage.ObjectStorage
	media.ObjectStore
}

type sessionMediaRepo struct {
	repository.Repository
	*httpRepoFake
	*memoryIdempotencyStore
	users    map[string]domain.User
	sessions map[string]string
}

func newSessionMediaRepo() *sessionMediaRepo {
	return &sessionMediaRepo{
		httpRepoFake:           newHTTPRepoFake(),
		memoryIdempotencyStore: newMemoryIdempotencyStore(),
		users:                  map[string]domain.User{},
		sessions:               map[string]string{},
	}
}

func (r *sessionMediaRepo) CreateDevUser(_ context.Context, nickname string) (domain.User, error) {
	user := domain.User{ID: uuid.NewString(), Nickname: nickname, CreatedAt: time.Now()}
	r.users[user.ID] = user
	return user, nil
}

func (r *sessionMediaRepo) CreateSession(_ context.Context, userID string, digest []byte, _ time.Time) error {
	r.sessions[string(digest)] = userID
	return nil
}

func (r *sessionMediaRepo) UserByTokenDigest(_ context.Context, digest []byte) (domain.User, error) {
	userID, ok := r.sessions[string(digest)]
	if !ok {
		return domain.User{}, repository.ErrNotFound
	}
	user, ok := r.users[userID]
	if !ok {
		return domain.User{}, repository.ErrNotFound
	}
	return user, nil
}

// newTestDependencies 供 httpapi 测试构造最小 Dependencies：
// 登录走 account（repo 提供 identity/session 端口），媒体走 media.Service。
func newTestDependencies(repo interface {
	repository.Repository
	media.Repository
	IdempotencyStore
}, objects storage.ObjectStorage) Dependencies {
	deps := Dependencies{
		Idempotency: repo,
		Account: account.New(repo, repo, repo, repo, repo, nil, nil, nil, nil,
			stubAvatarResolver{}, nil, account.Config{SessionTTL: time.Hour}),
	}
	if objectStore, ok := objects.(media.ObjectStore); ok {
		deps.Media = media.New(repo, objectStore, 10<<20, time.Hour)
	}
	return deps
}

// stubAvatarResolver 测试头像解析：key 原样返回。
type stubAvatarResolver struct{}

func (stubAvatarResolver) ResolveAssetURL(objectKey string) string { return objectKey }
