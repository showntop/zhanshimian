package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/zhanshimian/server/internal/service/diagnostic"
)

// DiagnosticService 是诊断的最小依赖。
type DiagnosticService interface {
	Run(ctx context.Context, userID string, input diagnostic.RunInput) (diagnostic.Diagnosis, error)
	Get(ctx context.Context, userID string, id string) (diagnostic.Diagnosis, error)
	Latest(ctx context.Context, userID string, kind string) (diagnostic.Diagnosis, error)
	SetSaved(ctx context.Context, userID string, id string, saved bool) (diagnostic.Diagnosis, error)
}

var errDiagnosticUnavailable = errors.New("diagnostic service unavailable")

// POST /v1/diagnostics —— 同步诊断（outfit / purchase），201。
func (a *API) createDiagnostic(w http.ResponseWriter, r *http.Request) {
	if a.diagnostic == nil {
		a.internalError(w, r, errDiagnosticUnavailable)
		return
	}
	var input struct {
		Kind         string `json:"kind"`
		Scene        string `json:"scene"`
		MediaAssetID string `json:"media_id"`
		ReportID     string `json:"report_id"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	result, err := a.diagnostic.Run(r.Context(), currentUser(r).ID, diagnostic.RunInput{
		Kind: input.Kind, Scene: input.Scene, MediaAssetID: input.MediaAssetID, ReportID: input.ReportID,
	})
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, result)
}

// GET /v1/diagnostics/latest?kind= —— 该类型最近一条（无则 404）。
func (a *API) getLatestDiagnostic(w http.ResponseWriter, r *http.Request) {
	if a.diagnostic == nil {
		a.internalError(w, r, errDiagnosticUnavailable)
		return
	}
	result, err := a.diagnostic.Latest(r.Context(), currentUser(r).ID, r.URL.Query().Get("kind"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, result)
}

// GET /v1/diagnostics/{id} —— 单条诊断（越权 404）。
func (a *API) getDiagnostic(w http.ResponseWriter, r *http.Request) {
	if a.diagnostic == nil {
		a.internalError(w, r, errDiagnosticUnavailable)
		return
	}
	result, err := a.diagnostic.Get(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, result)
}

// PATCH /v1/diagnostics/{id} —— {saved:bool}。
func (a *API) patchDiagnostic(w http.ResponseWriter, r *http.Request) {
	if a.diagnostic == nil {
		a.internalError(w, r, errDiagnosticUnavailable)
		return
	}
	var input struct {
		Saved bool `json:"saved"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	result, err := a.diagnostic.SetSaved(r.Context(), currentUser(r).ID, r.PathValue("id"), input.Saved)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, result)
}
