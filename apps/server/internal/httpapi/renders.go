package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/rendering"
)

var (
	repositoryErrNotFound = repository.ErrNotFound
	errorsNew             = errors.New
	errorsIs              = errors.Is
)

// renderService is the narrow Rendering surface the transport may call.
type renderService interface {
	StartRun(ctx context.Context, cmd rendering.StartRunCommand) (rendering.StartRunResult, error)
	GetRun(ctx context.Context, userID, runID string) (domain.RenderRunView, error)
}

type createRenderRunRequest struct {
	Note string `json:"note,omitempty"`
}

func (a *API) createRenderRun(w http.ResponseWriter, r *http.Request) {
	if a.renders == nil {
		a.internalError(w, r, errRenderUnavailable)
		return
	}
	if r.Header.Get("Idempotency-Key") == "" {
		writeError(w, r, http.StatusBadRequest, "idempotency_key_required", "请携带 Idempotency-Key 后重试")
		return
	}
	// 请求体可选(空对象);只用于传输校验。
	if r.ContentLength > 0 {
		var input createRenderRunRequest
		if err := decodeJSON(r, &input); err != nil {
			writeError(w, r, http.StatusBadRequest, "validation_error", "请求内容格式不正确")
			return
		}
	}
	result, err := a.renders.StartRun(r.Context(), rendering.StartRunCommand{
		UserID:         currentUser(r).ID,
		PlanVariantID:  r.PathValue("id"),
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
	})
	if err != nil {
		a.writeRenderError(w, r, err)
		return
	}
	writeDataWithOperation(w, http.StatusAccepted, renderRunDTO{
		ID: result.Run.ID, PlanVariantID: result.Run.PlanVariantID,
		Generation: result.Run.Generation,
		Render:     renderStatusQueued(result.Operation),
	}, operationRefDTO{
		ID:     result.Operation.ID,
		Kind:   string(result.Operation.Kind),
		Status: string(result.Operation.Status),
	})
}

func (a *API) getRenderRun(w http.ResponseWriter, r *http.Request) {
	if a.renders == nil {
		a.internalError(w, r, errRenderUnavailable)
		return
	}
	view, err := a.renders.GetRun(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		a.writeRenderError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, renderRunDTO{
		ID: view.ID, PlanVariantID: view.PlanVariantID, Generation: view.Generation,
		Render: renderStatusDTO{
			State:         view.Render.State,
			Retryable:     view.Render.Retryable,
			OperationID:   nilIfEmpty(view.Render.OperationID),
			RenderRunID:   view.Render.RenderRunID,
			PublicationID: view.Render.PublicationID,
			Media:         renderMediaDTO(view.Render.Media),
		},
	})
}

func (a *API) writeRenderError(w http.ResponseWriter, r *http.Request, err error) {
	if errorsIs(err, rendering.ErrValidation) {
		writeError(w, r, http.StatusBadRequest, "validation_error", "渲染请求不完整，请重试")
		return
	}
	if errorsIs(err, repositoryErrNotFound) {
		writeError(w, r, http.StatusNotFound, "not_found", "没有找到对应内容")
		return
	}
	a.internalError(w, r, err)
}

var errRenderUnavailable = errorsNew("rendering service unavailable")

// ---- DTOs (frozen contract shapes; no provider/model/score fields) ----

type renderRunDTO struct {
	ID            string          `json:"id"`
	PlanVariantID string          `json:"plan_variant_id"`
	Generation    int             `json:"generation"`
	Render        renderStatusDTO `json:"render"`
}

func renderStatusQueued(operation domain.OperationRef) renderStatusDTO {
	return renderStatusDTO{
		State:       domain.RenderStateQueued,
		Retryable:   false,
		OperationID: nilIfEmpty(operation.ID),
	}
}

func renderMediaDTO(media *domain.RenderMediaView) *displayMediaDTO {
	if media == nil {
		return nil
	}
	return &displayMediaDTO{
		AssetID:      media.AssetID,
		URL:          media.URL,
		URLExpiresAt: formatPublicTime(media.URLExpiresAt),
		MIMEType:     "image/jpeg",
		SourceKind:   media.SourceKind,
		DisplayLabel: media.DisplayLabel,
	}
}

func nilIfEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
