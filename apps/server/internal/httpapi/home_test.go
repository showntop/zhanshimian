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
	bootstrap home.Bootstrap
	err       error
}

func (f fakeHomeService) Bootstrap(context.Context, string) (home.Bootstrap, error) {
	return f.bootstrap, f.err
}

func TestHomeBootstrapReturnsOperationsNotTasks(t *testing.T) {
	api := &API{
		home: fakeHomeService{bootstrap: home.Bootstrap{
			ProfileSummary: &domain.ProfileSummary{HeightCM: 170, Role: "设计师", Budget: "1000-3000"},
			ActiveOperations: []domain.OperationRef{
				{ID: "operation-1", Kind: domain.OperationRender, Status: domain.OperationRunning},
			},
			Billing: &domain.BillingSummary{Credits: 2, SKUs: []domain.BillingSKU{}},
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
	assertJSONPath(t, rec, "data.profile_summary.height_cm", float64(170))
	assertJSONPath(t, rec, "data.profile_summary.role", "设计师")
	assertJSONPath(t, rec, "data.billing.credits", float64(2))
	assertJSONDoesNotContainKey(t, rec, "active_tasks")
	assertJSONDoesNotContainKey(t, rec, "task")
	// 契约键是 profile_summary/report/plan_set/today_plan，不是旧读模型键。
	assertJSONDoesNotContainKey(t, rec, "current_report")
	assertJSONDoesNotContainKey(t, rec, "recent_plan")
	assertJSONDoesNotContainKey(t, rec, "today")
}
