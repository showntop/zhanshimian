package home

import (
	"context"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
)

type readerSpy struct {
	snapshot Snapshot
	calls    int
	err      error
}

func (r *readerSpy) ReadHome(context.Context, string, time.Time) (Snapshot, error) {
	r.calls++
	return r.snapshot, r.err
}

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

func TestBootstrapUsesSingleReadModelCall(t *testing.T) {
	reader := &readerSpy{snapshot: Snapshot{ActiveOperations: []domain.OperationRef{}}}
	svc := New(reader, fixedClock{now: time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)})

	got, err := svc.Bootstrap(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if reader.calls != 1 {
		t.Fatalf("reader calls = %d, want 1", reader.calls)
	}
	if got.ActiveOperations == nil {
		t.Fatal("ActiveOperations must never be nil")
	}
}

func TestBootstrapNormalizesNilOperations(t *testing.T) {
	reader := &readerSpy{snapshot: Snapshot{}}
	svc := New(reader, fixedClock{now: time.Now()})

	got, err := svc.Bootstrap(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ActiveOperations == nil {
		t.Fatal("nil ActiveOperations was not normalized to an empty slice")
	}
}
