package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/execution"
)

// executionService is the narrow Execution surface the transport may call.
type executionService interface {
	PutSelection(ctx context.Context, userID, planSetID string, input execution.PutSelectionInput) (domain.PlanSelection, bool, error)
	CreateExecution(ctx context.Context, userID, selectionID string, input execution.CreateExecutionInput) (domain.Execution, bool, error)
	GetExecution(ctx context.Context, userID, executionID string) (domain.Execution, error)
	AppendEvent(ctx context.Context, userID, executionID string, input execution.AppendEventInput) (execution.AppendEventResult, error)
}

var errExecutionUnavailable = errors.New("execution service unavailable")

// ifMatchKey 保存 requireIfMatch 解析出的期望版本号。
const ifMatchKey contextKey = "ifMatch"

type putSelectionRequest struct {
	PlanVariantID       string  `json:"plan_variant_id"`
	RenderPublicationID *string `json:"render_publication_id,omitempty"`
}

type appendExecutionEventRequest struct {
	ClientEventID string                    `json:"client_event_id"`
	Type          domain.ExecutionEventType `json:"type"`
	StepID        *string                   `json:"step_id,omitempty"`
	OccurredAt    time.Time                 `json:"occurred_at"`
}

func (a *API) putSelection(w http.ResponseWriter, r *http.Request) {
	if a.execution == nil {
		a.internalError(w, r, errExecutionUnavailable)
		return
	}
	var input putSelectionRequest
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", "请求内容格式不正确")
		return
	}
	selection, created, err := a.execution.PutSelection(r.Context(), currentUser(r).ID, r.PathValue("id"), execution.PutSelectionInput{
		PlanVariantID:       input.PlanVariantID,
		RenderPublicationID: input.RenderPublicationID,
		IdempotencyKey:      r.Header.Get("Idempotency-Key"),
	})
	if err != nil {
		a.writeExecutionError(w, r, err)
		return
	}
	writeData(w, createdStatus(created), selection)
}

func (a *API) createExecution(w http.ResponseWriter, r *http.Request) {
	if a.execution == nil {
		a.internalError(w, r, errExecutionUnavailable)
		return
	}
	// 请求体是可选的空对象;只用于传输校验。
	if r.ContentLength > 0 {
		var input struct{}
		if err := decodeJSON(r, &input); err != nil {
			writeError(w, r, http.StatusBadRequest, "validation_error", "请求内容格式不正确")
			return
		}
	}
	snapshot, created, err := a.execution.CreateExecution(r.Context(), currentUser(r).ID, r.PathValue("id"), execution.CreateExecutionInput{
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
	})
	if err != nil {
		a.writeExecutionError(w, r, err)
		return
	}
	writeDataVersioned(w, createdStatus(created), snapshot, snapshot.Version)
}

func (a *API) getExecution(w http.ResponseWriter, r *http.Request) {
	if a.execution == nil {
		a.internalError(w, r, errExecutionUnavailable)
		return
	}
	snapshot, err := a.execution.GetExecution(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		a.writeExecutionError(w, r, err)
		return
	}
	writeDataVersioned(w, http.StatusOK, snapshot, snapshot.Version)
}

func (a *API) appendExecutionEvent(w http.ResponseWriter, r *http.Request) {
	if a.execution == nil {
		a.internalError(w, r, errExecutionUnavailable)
		return
	}
	expectedVersion, ok := r.Context().Value(ifMatchKey).(int)
	if !ok {
		// 直接注册 handler 而未挂 requireIfMatch 时的兜底,契约仍是 428。
		writeError(w, r, http.StatusPreconditionRequired, "precondition_required", "请携带 If-Match 后重试")
		return
	}
	var input appendExecutionEventRequest
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", "请求内容格式不正确")
		return
	}
	result, err := a.execution.AppendEvent(r.Context(), currentUser(r).ID, r.PathValue("id"), execution.AppendEventInput{
		ClientEventID:   input.ClientEventID,
		Type:            input.Type,
		StepID:          input.StepID,
		OccurredAt:      input.OccurredAt,
		ExpectedVersion: expectedVersion,
	})
	if err != nil {
		a.writeExecutionError(w, r, err)
		return
	}
	// 客户端用响应体的 execution.version 发下一次事件,因此 ETag 与它同源。
	status := http.StatusCreated
	if result.Replayed {
		status = http.StatusOK
	}
	writeDataVersioned(w, status, map[string]any{
		"event":     result.Event,
		"execution": result.Execution,
	}, result.Execution.Version)
}

// requireIfMatch 校验 If-Match 携带的正整数版本号并放进 context。它必须包在
// requireIdempotency 外层:缺少前置条件时契约要求 428,而不是幂等中间件的 400。
func (a *API) requireIfMatch(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := strings.TrimSpace(r.Header.Get("If-Match"))
		if raw == "" || raw == "*" {
			writeError(w, r, http.StatusPreconditionRequired, "precondition_required", "请携带 If-Match 后重试")
			return
		}
		version, err := parseVersionETag(raw)
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "validation_error", `If-Match 必须形如 "3"`)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ifMatchKey, version)))
	})
}

func (a *API) writeExecutionError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, execution.ErrValidation):
		writeError(w, r, http.StatusBadRequest, "validation_error",
			strings.TrimPrefix(err.Error(), execution.ErrValidation.Error()+": "))
	case errors.Is(err, repository.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "没有找到对应内容")
	case errors.Is(err, execution.ErrIdempotencyConflict):
		writeError(w, r, http.StatusConflict, "idempotency_conflict", "相同幂等键已被用于不同请求")
	case errors.Is(err, domain.ErrInvalidTransition):
		writeError(w, r, http.StatusConflict, "invalid_transition", "当前执行状态不允许该操作")
	case errors.Is(err, domain.ErrExecutionNotCompleted):
		writeError(w, r, http.StatusConflict, "execution_not_completed", "请先完成全部步骤再提交反馈")
	case errors.Is(err, execution.ErrVersionConflict):
		writeError(w, r, http.StatusPreconditionFailed, "version_conflict", "执行已更新，请刷新后重试")
	default:
		// ErrInvalidSnapshot 属于服务端数据完整性问题,按 500 处理。
		a.internalError(w, r, err)
	}
}

func createdStatus(created bool) int {
	if created {
		return http.StatusCreated
	}
	return http.StatusOK
}

func writeDataVersioned(w http.ResponseWriter, status int, value any, version int) {
	w.Header().Set("ETag", versionETag(version))
	writeData(w, status, value)
}

func versionETag(version int) string { return `"` + strconv.Itoa(version) + `"` }

// parseVersionETag 只接受 "<正整数>" 这一种形状(引号可省略)。
func parseVersionETag(raw string) (int, error) {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimPrefix(trimmed, `"`)
	trimmed = strings.TrimSuffix(trimmed, `"`)
	version, err := strconv.Atoi(trimmed)
	if err != nil || version < 1 {
		return 0, errors.New("invalid if-match")
	}
	return version, nil
}
