package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/today"
)

// TodayService 是今日方案的最小依赖。
type TodayService interface {
	Context(ctx context.Context, city string, schedule string) domain.TodayContext
	Generate(ctx context.Context, userID string, input today.CreateInput) (today.Plan, error)
	Current(ctx context.Context, userID string) (today.Plan, error)
	Activate(ctx context.Context, userID string, id string) (today.Plan, error)
	Feedback(ctx context.Context, userID string, id string, feedback string) (today.Plan, error)
}

var errTodayUnavailable = errors.New("today service unavailable")

func (a *API) getTodayContext(w http.ResponseWriter, r *http.Request) {
	if a.today == nil {
		a.internalError(w, r, errTodayUnavailable)
		return
	}
	writeData(w, http.StatusOK, a.today.Context(r.Context(), r.URL.Query().Get("city"), r.URL.Query().Get("schedule")))
}

func (a *API) getTodayPlan(w http.ResponseWriter, r *http.Request) {
	if a.today == nil {
		a.internalError(w, r, errTodayUnavailable)
		return
	}
	item, err := a.today.Current(r.Context(), currentUser(r).ID)
	if errors.Is(err, repository.ErrNotFound) {
		writeData(w, http.StatusOK, nil)
		return
	}
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (a *API) createTodayPlan(w http.ResponseWriter, r *http.Request) {
	if a.today == nil {
		a.internalError(w, r, errTodayUnavailable)
		return
	}
	var input struct {
		ReportID string `json:"report_id"`
		City     string `json:"city"`
		Schedule string `json:"schedule"`
		Refresh  bool   `json:"refresh"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	item, err := a.today.Generate(r.Context(), currentUser(r).ID, today.CreateInput{
		City: input.City, Schedule: input.Schedule, ReportID: input.ReportID, Refresh: input.Refresh,
	})
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	// 契约 TodayPlanAccepted：{data, operation}——客户端凭 operation.id 轮询
	// 搭配图渲染进度（与 hair 预览同一最小 DTO 形状）。
	writeDataOperation(w, http.StatusCreated, item, operationDTO{
		ID: item.Operation.ID, Kind: string(item.Operation.Kind), Status: string(item.Operation.Status),
	})
}

func (a *API) activateTodayPlan(w http.ResponseWriter, r *http.Request) {
	if a.today == nil {
		a.internalError(w, r, errTodayUnavailable)
		return
	}
	item, err := a.today.Activate(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (a *API) feedbackTodayPlan(w http.ResponseWriter, r *http.Request) {
	if a.today == nil {
		a.internalError(w, r, errTodayUnavailable)
		return
	}
	var input struct {
		Feedback string `json:"feedback"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	item, err := a.today.Feedback(r.Context(), currentUser(r).ID, r.PathValue("id"), input.Feedback)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}
