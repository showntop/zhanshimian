package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/testutil"
)

func TestBeginIdempotencyStartsThenReplays(t *testing.T) {
	store, userID := newIdempotencyStore(t)
	in := beginInput(userID, "key-1", strings.Repeat("a", 64))
	first, outcome, err := store.BeginIdempotency(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != domain.IdempotencyBeginStarted {
		t.Fatalf("first outcome = %s, want started", outcome)
	}
	body := json.RawMessage(`{"data":{"id":"asset-1"}}`)
	if err := store.CompleteIdempotency(context.Background(), userID, in.Key, 201, body); err != nil {
		t.Fatal(err)
	}
	replay, outcome, err := store.BeginIdempotency(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != domain.IdempotencyBeginReplay {
		t.Fatalf("second outcome = %s, want replay", outcome)
	}
	if replay.ID != first.ID || replay.ResponseStatus != 201 {
		t.Fatalf("replay record = %#v first=%#v", replay, first)
	}
	var want, got any
	if err := json.Unmarshal(body, &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(replay.ResponseBody, &got); err != nil {
		t.Fatalf("replay body %s: %v", replay.ResponseBody, err)
	}
	wantRaw, _ := json.Marshal(want)
	gotRaw, _ := json.Marshal(got)
	if string(wantRaw) != string(gotRaw) {
		t.Fatalf("replay body = %s want %s", gotRaw, wantRaw)
	}
}

func TestBeginIdempotencyConflictAndInProgress(t *testing.T) {
	store, userID := newIdempotencyStore(t)
	in := beginInput(userID, "key-1", strings.Repeat("a", 64))
	if _, outcome, err := store.BeginIdempotency(context.Background(), in); err != nil || outcome != domain.IdempotencyBeginStarted {
		t.Fatalf("start outcome=%s err=%v", outcome, err)
	}
	_, outcome, err := store.BeginIdempotency(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != domain.IdempotencyBeginInProgress {
		t.Fatalf("same fingerprint outcome = %s, want in_progress", outcome)
	}
	conflictIn := in
	conflictIn.RequestFingerprint = strings.Repeat("b", 64)
	_, outcome, err = store.BeginIdempotency(context.Background(), conflictIn)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != domain.IdempotencyBeginConflict {
		t.Fatalf("different fingerprint outcome = %s, want conflict", outcome)
	}
}

func TestAbortIdempotencyAllowsRetry(t *testing.T) {
	store, userID := newIdempotencyStore(t)
	in := beginInput(userID, "key-1", strings.Repeat("c", 64))
	if _, _, err := store.BeginIdempotency(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if err := store.AbortIdempotency(context.Background(), userID, in.Key); err != nil {
		t.Fatal(err)
	}
	_, outcome, err := store.BeginIdempotency(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != domain.IdempotencyBeginStarted {
		t.Fatalf("after abort outcome = %s, want started", outcome)
	}
}

func TestBeginIdempotencyTwentyConcurrentSameRequest(t *testing.T) {
	store, userID := newIdempotencyStore(t)
	in := beginInput(userID, "key-race", strings.Repeat("d", 64))
	var started, inProgress, other atomic.Int32
	var wg sync.WaitGroup
	const n = 20
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, outcome, err := store.BeginIdempotency(context.Background(), in)
			if err != nil {
				t.Errorf("BeginIdempotency: %v", err)
				other.Add(1)
				return
			}
			switch outcome {
			case domain.IdempotencyBeginStarted:
				started.Add(1)
			case domain.IdempotencyBeginInProgress:
				inProgress.Add(1)
			default:
				other.Add(1)
			}
		}()
	}
	wg.Wait()
	if started.Load() != 1 || inProgress.Load() != 19 || other.Load() != 0 {
		t.Fatalf("started=%d in_progress=%d other=%d", started.Load(), inProgress.Load(), other.Load())
	}
}

func newIdempotencyStore(t *testing.T) (*Store, string) {
	t.Helper()
	store := New(testutil.NewPostgres(t))
	return store, insertUser(t, store)
}

func beginInput(userID, key, fingerprint string) domain.BeginIdempotency {
	return domain.BeginIdempotency{
		UserID:             userID,
		Key:                key,
		RequestFingerprint: fingerprint,
		Scope:              "POST /v1/media/upload-intents",
		ExpiresAt:          time.Now().Add(24 * time.Hour),
	}
}
