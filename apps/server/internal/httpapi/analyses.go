package httpapi

import (
	"net/http"

	"github.com/zhanshimian/server/internal/domain"
)

// POST /v1/analyses —— 202 {data: Analysis, task}：异步分析入统一任务队列。
func (a *API) createAnalysis(w http.ResponseWriter, r *http.Request) {
	var input domain.CreateAnalysisInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	analysis, task, err := a.service.CreateAnalysis(r.Context(), currentUser(r).ID, input)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeDataTask(w, http.StatusAccepted, analysis, domainTaskRef(task))
}

func (a *API) getCurrentAnalysis(w http.ResponseWriter, r *http.Request) {
	item, err := a.service.GetCurrentAnalysis(r.Context(), currentUser(r).ID)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (a *API) getAnalysis(w http.ResponseWriter, r *http.Request) {
	item, err := a.service.GetAnalysis(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (a *API) getReport(w http.ResponseWriter, r *http.Request) {
	item, err := a.service.GetReport(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (a *API) getCurrentReport(w http.ResponseWriter, r *http.Request) {
	item, err := a.service.GetCurrentReport(r.Context(), currentUser(r).ID)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}
