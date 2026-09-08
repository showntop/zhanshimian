package httpapi

import (
	"errors"
	"net/http"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

func (a *API) getTodayContext(w http.ResponseWriter, r *http.Request) {
	item, err := a.service.GetTodayContext(r.Context(), currentUser(r).ID, r.URL.Query().Get("city"), r.URL.Query().Get("schedule"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (a *API) getTodayPlan(w http.ResponseWriter, r *http.Request) {
	item, err := a.service.GetTodayPlan(r.Context(), currentUser(r).ID)
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

// POST /v1/today/plans —— 201 {data: TodayPlan, task}：今日方案文字即时生成，本人搭配图走任务。
func (a *API) createTodayPlan(w http.ResponseWriter, r *http.Request) {
	var input domain.TodayPlanInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	item, task, err := a.service.GenerateTodayPlan(r.Context(), currentUser(r).ID, input)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeDataTask(w, http.StatusCreated, item, domainTaskRef(task))
}

func (a *API) activateTodayPlan(w http.ResponseWriter, r *http.Request) {
	item, err := a.service.ActivateTodayPlan(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (a *API) feedbackTodayPlan(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Feedback string `json:"feedback"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	item, err := a.service.FeedbackTodayPlan(r.Context(), currentUser(r).ID, r.PathValue("id"), input.Feedback)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}
