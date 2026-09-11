package httpapi

import (
	"net/http"

	"github.com/zhanshimian/server/internal/domain"
)

// POST /v1/diagnostics —— 同步诊断（outfit / purchase），201。
func (a *API) createDiagnostic(w http.ResponseWriter, r *http.Request) {
	var input domain.DiagnosticInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	result, err := a.service.RunDiagnostic(r.Context(), currentUser(r).ID, input)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, result)
}

// GET /v1/diagnostics/latest?kind= —— 该类型最近一条（无则 404）。
func (a *API) getLatestDiagnostic(w http.ResponseWriter, r *http.Request) {
	result, err := a.service.LatestDiagnostic(r.Context(), currentUser(r).ID, r.URL.Query().Get("kind"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, result)
}

// GET /v1/diagnostics/{id} —— 单条诊断（越权 404）。
func (a *API) getDiagnostic(w http.ResponseWriter, r *http.Request) {
	result, err := a.service.GetDiagnostic(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, result)
}

// PATCH /v1/diagnostics/{id} —— {saved:bool}。
func (a *API) patchDiagnostic(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Saved bool `json:"saved"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	result, err := a.service.SetDiagnosticSaved(r.Context(), currentUser(r).ID, r.PathValue("id"), input.Saved)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, result)
}

// GET /v1/hairstyles?report_id= —— 发型推荐（纯读；缺省取最新报告，无报告 404）。
func (a *API) listHairstyles(w http.ResponseWriter, r *http.Request) {
	items, err := a.service.Hairstyles(r.Context(), currentUser(r).ID, r.URL.Query().Get("report_id"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}
