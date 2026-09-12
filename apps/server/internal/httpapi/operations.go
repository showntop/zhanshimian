package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/zhanshimian/server/internal/domain"
)

type operationDTO struct {
	ID            string  `json:"id"`
	Kind          string  `json:"kind"`
	SubjectType   string  `json:"subject_type,omitempty"`
	SubjectID     string  `json:"subject_id,omitempty"`
	Status        string  `json:"status"`
	ProgressBPS   int     `json:"progress_bps"`
	StageCode     string  `json:"stage_code,omitempty"`
	PublicMessage string  `json:"public_message,omitempty"`
	ErrorCode     string  `json:"error_code,omitempty"`
	TraceID       string  `json:"trace_id,omitempty"`
	Retryable     bool    `json:"retryable"`
	ResultType    string  `json:"result_type,omitempty"`
	ResultID      string  `json:"result_id,omitempty"`
	CreatedAt     string  `json:"created_at,omitempty"`
	UpdatedAt     string  `json:"updated_at,omitempty"`
	FinishedAt    *string `json:"finished_at,omitempty"`
}

func (a *API) getOperation(w http.ResponseWriter, r *http.Request) {
	if a.operations == nil {
		a.internalError(w, r, errOperationUnavailable)
		return
	}
	op, err := a.operations.Get(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, publicOperation(op))
}

func (a *API) listOperations(w http.ResponseWriter, r *http.Request) {
	if a.operations == nil {
		a.internalError(w, r, errOperationUnavailable)
		return
	}
	raw := strings.TrimSpace(r.URL.Query().Get("ids"))
	ops, err := a.operations.GetMany(r.Context(), currentUser(r).ID, splitIDs(raw))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	items := make([]operationDTO, 0, len(ops))
	for _, op := range ops {
		items = append(items, publicOperation(op))
	}
	writeData(w, http.StatusOK, items)
}

func splitIDs(raw string) []string {
	if raw == "" {
		return nil
	}
	return strings.Split(raw, ",")
}

func publicOperation(op domain.Operation) operationDTO {
	dto := operationDTO{
		ID:            op.ID,
		Kind:          string(op.Kind),
		SubjectType:   op.SubjectType,
		SubjectID:     op.SubjectID,
		Status:        string(op.Status),
		ProgressBPS:   op.ProgressBPS,
		StageCode:     op.StageCode,
		PublicMessage: op.PublicMessage,
		ErrorCode:     op.ErrorCode,
		TraceID:       op.TraceID,
		Retryable:     op.Retryable,
		ResultType:    op.ResultType,
		ResultID:      op.ResultID,
		CreatedAt:     formatPublicTime(op.CreatedAt),
		UpdatedAt:     formatPublicTime(op.UpdatedAt),
	}
	if op.FinishedAt != nil {
		value := formatPublicTime(*op.FinishedAt)
		if value != "" {
			dto.FinishedAt = &value
		}
	}
	return dto
}

func formatPublicTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format("2006-01-02T15:04:05Z")
}
