package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/share"
)

// ShareService 是分享的最小依赖。
type ShareService interface {
	Create(ctx context.Context, userID string, input share.CreateInput) (share.Card, error)
	GetPublic(ctx context.Context, token string) (share.PublicView, error)
	Revoke(ctx context.Context, userID string, id string) error
}

var errShareUnavailable = errors.New("share service unavailable")

// POST /v1/shares —— 创建不可变分享快照（只存资产身份，不存 URL）。
func (a *API) createShare(w http.ResponseWriter, r *http.Request) {
	if a.share == nil {
		a.internalError(w, r, errShareUnavailable)
		return
	}
	var input struct {
		SourceType   string `json:"source_type"`
		SourceID     string `json:"source_id"`
		IncludePhoto bool   `json:"include_photo"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	card, err := a.share.Create(r.Context(), currentUser(r).ID, share.CreateInput{
		SourceType: input.SourceType, SourceID: input.SourceID, IncludePhoto: input.IncludePhoto,
	})
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, card)
}

// GET /v1/shares/{token} —— 公开读取（免 bearer）；撤销/过期/资产不再发布一律 404。
func (a *API) getShareByToken(w http.ResponseWriter, r *http.Request) {
	if a.share == nil {
		a.internalError(w, r, errShareUnavailable)
		return
	}
	view, err := a.share.GetPublic(r.Context(), r.PathValue("token"))
	if errors.Is(err, share.ErrShareGone) || errors.Is(err, repository.ErrNotFound) {
		writeError(w, r, http.StatusNotFound, "not_found", "分享内容不存在或已被撤销")
		return
	}
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, view)
}

// DELETE /v1/shares/{id} —— 撤销即失效。
func (a *API) deleteShare(w http.ResponseWriter, r *http.Request) {
	if a.share == nil {
		a.internalError(w, r, errShareUnavailable)
		return
	}
	if err := a.share.Revoke(r.Context(), currentUser(r).ID, r.PathValue("id")); err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"revoked": true})
}
