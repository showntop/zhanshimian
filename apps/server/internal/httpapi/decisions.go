package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/planning"
)

// decisionService is the narrow planning decision surface the transport may call.
type decisionService interface {
	PutVariantDecision(ctx context.Context, userID, planVariantID, decision string) (domain.PlanVariantDecision, error)
	DeleteVariantDecision(ctx context.Context, userID, planVariantID string) error
}

var errDecisionUnavailable = errors.New("planning decision service unavailable")

// putPlanVariantDecisionRequest 只接受一个字段;DisallowUnknownFields 让
// 多余字段在传输层就被拒绝。
type putPlanVariantDecisionRequest struct {
	Decision string `json:"decision"`
}

type planVariantDecisionDTO struct {
	PlanVariantID string `json:"plan_variant_id"`
	Decision      string `json:"decision"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

// putPlanVariantDecision 记录一条卡堆决策（喜欢/跳过）。UPSERT 语义:
// 首次写入与改主意都返回 200;幂等键防双击重放。
func (a *API) putPlanVariantDecision(w http.ResponseWriter, r *http.Request) {
	if a.decisions == nil {
		a.internalError(w, r, errDecisionUnavailable)
		return
	}
	var input putPlanVariantDecisionRequest
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", "请求内容格式不正确")
		return
	}
	decision, err := a.decisions.PutVariantDecision(r.Context(), currentUser(r).ID, r.PathValue("id"), input.Decision)
	if err != nil {
		a.writeDecisionError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, planVariantDecisionDTO{
		PlanVariantID: decision.PlanVariantID,
		Decision:      string(decision.Decision),
		CreatedAt:     formatPublicTime(decision.CreatedAt),
		UpdatedAt:     formatPublicTime(decision.UpdatedAt),
	})
}

// deletePlanVariantDecision 清除一条决策（撤销）。行不存在同样是 204——
// 撤销的语义是「回到未决」,不依赖之前真的决策过。本库 DELETE 不挂幂等
// 中间件（与 shares/wardrobe 一致）。
func (a *API) deletePlanVariantDecision(w http.ResponseWriter, r *http.Request) {
	if a.decisions == nil {
		a.internalError(w, r, errDecisionUnavailable)
		return
	}
	if err := a.decisions.DeleteVariantDecision(r.Context(), currentUser(r).ID, r.PathValue("id")); err != nil {
		a.writeDecisionError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) writeDecisionError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, planning.ErrInvalidDecision):
		writeError(w, r, http.StatusBadRequest, "validation_error", "决策只能是 like 或 skip")
	case errors.Is(err, repository.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "没有找到对应内容")
	default:
		a.internalError(w, r, err)
	}
}
