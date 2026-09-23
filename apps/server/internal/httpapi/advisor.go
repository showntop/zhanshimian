package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/service/advisor"
)

// AdvisorService 是顾问对话的最小依赖。
type AdvisorService interface {
	Send(ctx context.Context, userID string, input advisor.SendInput) (domain.AdvisorMessage, error)
	List(ctx context.Context, userID string) ([]domain.AdvisorMessage, error)
	ApplyAction(ctx context.Context, userID string, actionID string) (domain.AdvisorAction, error)
}

var errAdvisorUnavailable = errors.New("advisor service unavailable")

func (a *API) sendAdvisorMessage(w http.ResponseWriter, r *http.Request) {
	if a.advisor == nil {
		a.internalError(w, r, errAdvisorUnavailable)
		return
	}
	var input struct {
		Content string `json:"content"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	// 输入闸（旧线语义）：1–500 字，超限 400，不进入服务与计费。
	if strings.TrimSpace(input.Content) == "" || len([]rune(input.Content)) > 500 {
		writeError(w, r, http.StatusBadRequest, "validation_error", "请输入 1–500 字的问题")
		return
	}
	item, err := a.advisor.Send(r.Context(), currentUser(r).ID, advisor.SendInput{Content: input.Content})
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, item)
}

func (a *API) listAdvisorMessages(w http.ResponseWriter, r *http.Request) {
	if a.advisor == nil {
		a.internalError(w, r, errAdvisorUnavailable)
		return
	}
	items, err := a.advisor.List(r.Context(), currentUser(r).ID)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (a *API) applyAdvisorAction(w http.ResponseWriter, r *http.Request) {
	if a.advisor == nil {
		a.internalError(w, r, errAdvisorUnavailable)
		return
	}
	item, err := a.advisor.ApplyAction(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}
