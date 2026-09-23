package httpapi

import (
	"time"

	"github.com/zhanshimian/server/internal/domain"
)

type operationDTO struct {
	ID            string  `json:"id"`
	Kind          string  `json:"kind"`
	SubjectType   string  `json:"subject_type"`
	SubjectID     string  `json:"subject_id"`
	Status        string  `json:"status"`
	ProgressBPS   int     `json:"progress_bps"`
	StageCode     string  `json:"stage_code"`
	PublicMessage string  `json:"public_message"`
	ErrorCode     string  `json:"error_code,omitempty"`
	TraceID       *string `json:"trace_id,omitempty"`
	Retryable     bool    `json:"retryable"`
	ResultType    string  `json:"result_type,omitempty"`
	ResultID      string  `json:"result_id,omitempty"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
	FinishedAt    *string `json:"finished_at,omitempty"`
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
		Retryable:     op.Retryable,
		ResultType:    op.ResultType,
		ResultID:      op.ResultID,
		CreatedAt:     formatPublicTime(op.CreatedAt),
		UpdatedAt:     formatPublicTime(op.UpdatedAt),
	}
	if op.TraceID != "" || op.Status == domain.OperationFailed {
		traceID := op.TraceID
		dto.TraceID = &traceID
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
