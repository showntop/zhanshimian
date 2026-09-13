package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/service/home"
)

type fakeHomeService struct {
	snapshot home.Snapshot
	err      error
}

func (f fakeHomeService) Bootstrap(context.Context, string) (home.Snapshot, error) {
	return f.snapshot, f.err
}

func TestHomeBootstrapReturnsOperationsNotTasks(t *testing.T) {
	api := &API{
		home: fakeHomeService{snapshot: home.Snapshot{
			ActiveOperations: []domain.OperationRef{
				{ID: "operation-1", Kind: domain.OperationRender, Status: domain.OperationRunning},
			},
		}},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/home/bootstrap", api.homeBootstrap)

	req := httptest.NewRequest(http.MethodGet, "/v1/home/bootstrap", nil)
	req = req.WithContext(context.WithValue(req.Context(), userKey, domain.User{ID: "user-1"}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assertStatus(t, rec, 200)
	assertJSONPath(t, rec, "data.active_operations.0.id", "operation-1")
	assertJSONPath(t, rec, "data.active_operations.0.kind", "render")
	assertJSONPath(t, rec, "data.active_operations.0.status", "running")
	assertJSONDoesNotContainKey(t, rec, "active_tasks")
	assertJSONDoesNotContainKey(t, rec, "look_task")
}
