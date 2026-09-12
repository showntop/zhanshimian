package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/storage"
)

func TestIdempotencyReplaysSuccessfulResponse(t *testing.T) {
	calls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		writeData(w, http.StatusCreated, map[string]string{"id": "asset-1"})
	})
	handler := authenticatedIdempotentHandler(t, next)
	first := requestWithKey(t, handler, "key-1", `{"purpose":"face"}`)
	second := requestWithKey(t, handler, "key-1", `{"purpose":"face"}`)
	if first.Code != http.StatusCreated {
		t.Fatalf("first status = %d body=%s", first.Code, first.Body.String())
	}
	if first.Body.String() != second.Body.String() {
		t.Fatalf("replay body mismatch\nfirst  %s\nsecond %s", first.Body.String(), second.Body.String())
	}
	if calls != 1 {
		t.Fatalf("handler calls = %d, want 1", calls)
	}
}

func TestIdempotencyRejectsSameKeyWithDifferentBody(t *testing.T) {
	handler := authenticatedIdempotentHandler(t, successfulCreateHandler())
	_ = requestWithKey(t, handler, "key-1", `{"purpose":"face"}`)
	conflict := requestWithKey(t, handler, "key-1", `{"purpose":"body"}`)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s", conflict.Code, conflict.Body.String())
	}
	if !strings.Contains(conflict.Body.String(), `"code":"idempotency_conflict"`) {
		t.Fatalf("missing conflict code: %s", conflict.Body.String())
	}
	if !strings.Contains(conflict.Body.String(), `"retryable":false`) {
		t.Fatalf("conflict should not be retryable: %s", conflict.Body.String())
	}
}

func TestIdempotencyRejectsInProgressSameRequest(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		writeData(w, http.StatusCreated, map[string]string{"id": "asset-1"})
	})
	handler := authenticatedIdempotentHandler(t, next)

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- requestWithKey(t, handler, "key-1", `{"purpose":"face"}`)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first request did not start")
	}
	inProgress := requestWithKey(t, handler, "key-1", `{"purpose":"face"}`)
	close(release)
	first := <-done
	if first.Code != http.StatusCreated {
		t.Fatalf("first status = %d body=%s", first.Code, first.Body.String())
	}
	if inProgress.Code != http.StatusConflict {
		t.Fatalf("in-progress status = %d body=%s", inProgress.Code, inProgress.Body.String())
	}
	if !strings.Contains(inProgress.Body.String(), `"code":"idempotency_in_progress"`) {
		t.Fatalf("missing in-progress code: %s", inProgress.Body.String())
	}
	if !strings.Contains(inProgress.Body.String(), `"retryable":true`) {
		t.Fatalf("in-progress should be retryable: %s", inProgress.Body.String())
	}
}

func TestIdempotencyTwentyConcurrentIdenticalRequests(t *testing.T) {
	var calls atomic.Int32
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		time.Sleep(20 * time.Millisecond)
		writeData(w, http.StatusCreated, map[string]string{"id": "asset-1"})
	})
	handler := authenticatedIdempotentHandler(t, next)

	const n = 20
	var wg sync.WaitGroup
	codes := make([]int, n)
	bodies := make([]string, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			rec := requestWithKey(t, handler, "key-concurrent", `{"purpose":"face"}`)
			codes[i] = rec.Code
			bodies[i] = rec.Body.String()
		}(i)
	}
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("handler calls = %d, want 1", got)
	}
	created := 0
	others := 0
	for i, code := range codes {
		switch code {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			if !strings.Contains(bodies[i], `"code":"idempotency_in_progress"`) {
				t.Fatalf("409 without in_progress: %s", bodies[i])
			}
			others++
		default:
			if !strings.Contains(bodies[i], `"id":"asset-1"`) {
				t.Fatalf("unexpected status %d body=%s", code, bodies[i])
			}
			others++
		}
	}
	if created < 1 || created+others != n {
		t.Fatalf("created=%d others=%d codes=%v", created, others, codes)
	}
}

func TestIdempotencyAllowsRetryAfterFailure(t *testing.T) {
	calls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			writeError(w, r, http.StatusBadRequest, "validation_error", "请求内容格式不正确")
			return
		}
		writeData(w, http.StatusCreated, map[string]string{"id": "asset-1"})
	})
	handler := authenticatedIdempotentHandler(t, next)
	first := requestWithKey(t, handler, "key-1", `{"purpose":"face"}`)
	second := requestWithKey(t, handler, "key-1", `{"purpose":"face"}`)
	if first.Code != http.StatusBadRequest {
		t.Fatalf("first status = %d", first.Code)
	}
	if second.Code != http.StatusCreated {
		t.Fatalf("retry status = %d body=%s", second.Code, second.Body.String())
	}
	if calls != 2 {
		t.Fatalf("handler calls = %d, want 2", calls)
	}
}

func TestIdempotencyAbortsOnPanic(t *testing.T) {
	calls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			panic("boom")
		}
		writeData(w, http.StatusCreated, map[string]string{"id": "asset-1"})
	})
	handler := authenticatedIdempotentHandler(t, next)
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic to propagate")
			}
		}()
		_ = requestWithKey(t, handler, "key-1", `{"purpose":"face"}`)
	}()
	second := requestWithKey(t, handler, "key-1", `{"purpose":"face"}`)
	if second.Code != http.StatusCreated {
		t.Fatalf("retry after panic status = %d body=%s", second.Code, second.Body.String())
	}
	if calls != 2 {
		t.Fatalf("handler calls = %d, want 2", calls)
	}
}

func TestIdempotencyDoesNotAbortAfter2xxWhenRequestCancelled(t *testing.T) {
	store := &cancelSensitiveStore{memoryIdempotencyStore: newMemoryIdempotencyStore()}
	api := &API{idempotency: store, logger: discardLogger()}
	calls := 0
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		writeData(w, http.StatusCreated, map[string]string{"id": "asset-1"})
		cancel()
	})
	handler := api.requireIdempotency(next)

	req := httptest.NewRequest(http.MethodPost, "/v1/media/upload-intents", strings.NewReader(`{"purpose":"face"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "req-idem-1")
	req.Header.Set("Idempotency-Key", "key-1")
	req = req.WithContext(context.WithValue(ctx, userKey, domain.User{ID: "u1"}))
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, req)
	if first.Code != http.StatusCreated {
		t.Fatalf("first status = %d body=%s", first.Code, first.Body.String())
	}
	if store.aborts != 0 {
		t.Fatalf("Abort called %d times after 2xx", store.aborts)
	}

	second := requestWithKey(t, handler, "key-1", `{"purpose":"face"}`)
	if calls != 1 {
		t.Fatalf("handler calls = %d, want 1 (second status=%d body=%s)", calls, second.Code, second.Body.String())
	}
	if second.Code != http.StatusCreated && !strings.Contains(second.Body.String(), `"code":"idempotency_in_progress"`) {
		t.Fatalf("replay status = %d body=%s", second.Code, second.Body.String())
	}
}

func TestIdempotencyRequiresVisibleASCIIKey(t *testing.T) {
	handler := authenticatedIdempotentHandler(t, successfulCreateHandler())
	for _, key := range []string{"", strings.Repeat("a", 129), "key\n1", "键"} {
		rec := requestWithKey(t, handler, key, `{"purpose":"face"}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("key %q status = %d body=%s", key, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"code":"idempotency_key_required"`) {
			t.Fatalf("key %q missing required code: %s", key, rec.Body.String())
		}
	}
}

func TestIdempotencyCanonicalJSONIgnoresKeyOrder(t *testing.T) {
	calls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		writeData(w, http.StatusCreated, map[string]string{"id": "asset-1"})
	})
	handler := authenticatedIdempotentHandler(t, next)
	first := requestWithKey(t, handler, "key-1", `{"purpose":"face","mime_type":"image/jpeg"}`)
	second := requestWithKey(t, handler, "key-1", `{"mime_type":"image/jpeg","purpose":"face"}`)
	if first.Code != http.StatusCreated || second.Code != http.StatusCreated {
		t.Fatalf("status first=%d second=%d", first.Code, second.Code)
	}
	if calls != 1 {
		t.Fatalf("handler calls = %d, want 1", calls)
	}
}

func TestNewRequiresIdempotencyKeyOnCreateRoutes(t *testing.T) {
	repo := newSessionMediaRepo()
	handler := newWiredMediaHandler(t, repo)

	login := httptest.NewRecorder()
	loginReq := httptest.NewRequest(http.MethodPost, "/v1/auth/dev", strings.NewReader(`{"nickname":"idem"}`))
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

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/media/upload-intents", strings.NewReader(`{"purpose":"face","mime_type":"image/jpeg","byte_size":20,"sha256":"`+strings.Repeat("b", 64)+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+session.Data.Token)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing key status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"idempotency_key_required"`) {
		t.Fatalf("missing required code: %s", rec.Body.String())
	}
}

func authenticatedIdempotentHandler(t *testing.T, next http.Handler) http.Handler {
	t.Helper()
	api := &API{idempotency: newMemoryIdempotencyStore(), logger: discardLogger()}
	return api.requireIdempotency(next)
}

func successfulCreateHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeData(w, http.StatusCreated, map[string]string{"id": "asset-1"})
	})
}

func requestWithKey(t *testing.T, handler http.Handler, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/media/upload-intents", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "req-idem-1")
	req.Header.Set("Idempotency-Key", key)
	req = req.WithContext(context.WithValue(req.Context(), userKey, domain.User{ID: "u1"}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

type memoryIdempotencyStore struct {
	mu   sync.Mutex
	rows map[string]domain.IdempotencyRecord
}

func newMemoryIdempotencyStore() *memoryIdempotencyStore {
	return &memoryIdempotencyStore{rows: map[string]domain.IdempotencyRecord{}}
}

func (s *memoryIdempotencyStore) rowKey(userID, key string) string {
	return userID + "\x00" + key
}

func (s *memoryIdempotencyStore) BeginIdempotency(_ context.Context, in domain.BeginIdempotency) (domain.IdempotencyRecord, domain.IdempotencyBeginOutcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.rowKey(in.UserID, in.Key)
	if existing, ok := s.rows[id]; ok {
		if !existing.ExpiresAt.After(time.Now()) {
			delete(s.rows, id)
		} else if existing.RequestFingerprint != in.RequestFingerprint {
			return existing, domain.IdempotencyBeginConflict, nil
		} else if existing.Status == domain.IdempotencyCompleted {
			return existing, domain.IdempotencyBeginReplay, nil
		} else {
			return existing, domain.IdempotencyBeginInProgress, nil
		}
	}
	rec := domain.IdempotencyRecord{
		ID:                 in.UserID + "/" + in.Key,
		UserID:             in.UserID,
		Key:                in.Key,
		RequestFingerprint: in.RequestFingerprint,
		Status:             domain.IdempotencyInProgress,
		Scope:              in.Scope,
		ExpiresAt:          in.ExpiresAt,
		CreatedAt:          time.Now(),
	}
	s.rows[id] = rec
	return rec, domain.IdempotencyBeginStarted, nil
}

func (s *memoryIdempotencyStore) CompleteIdempotency(_ context.Context, userID, key string, status int, body json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.rowKey(userID, key)
	rec, ok := s.rows[id]
	if !ok || rec.Status != domain.IdempotencyInProgress {
		return nil
	}
	rec.Status = domain.IdempotencyCompleted
	rec.ResponseStatus = status
	rec.ResponseBody = append(json.RawMessage(nil), body...)
	s.rows[id] = rec
	return nil
}

type cancelSensitiveStore struct {
	*memoryIdempotencyStore
	aborts int
}

func (s *cancelSensitiveStore) CompleteIdempotency(ctx context.Context, userID, key string, status int, body json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.memoryIdempotencyStore.CompleteIdempotency(ctx, userID, key, status, body)
}

func (s *cancelSensitiveStore) AbortIdempotency(ctx context.Context, userID, key string) error {
	s.aborts++
	return s.memoryIdempotencyStore.AbortIdempotency(ctx, userID, key)
}

func (s *memoryIdempotencyStore) AbortIdempotency(_ context.Context, userID, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.rowKey(userID, key)
	rec, ok := s.rows[id]
	if !ok || rec.Status != domain.IdempotencyInProgress {
		return nil
	}
	delete(s.rows, id)
	return nil
}

func newWiredMediaHandler(t *testing.T, repo *sessionMediaRepo) http.Handler {
	t.Helper()
	local, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	objects := combinedObjectStore{ObjectStorage: local, ObjectStore: matchingHTTPStore()}
	return New(newServiceForAPI(t, repo, objects), discardLogger(), true, RuntimeInfo{})
}
