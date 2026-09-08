package httpapi

import (
	"net/http"

	"github.com/zhanshimian/server/internal/domain"
)

func (a *API) createShare(w http.ResponseWriter, r *http.Request) {
	var input domain.ShareCardInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	card, err := a.service.CreateShareCard(r.Context(), currentUser(r).ID, input)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, card)
}

// GET /v1/shares/{token} —— 公开（免鉴权）读取分享快照，只暴露 ShareView 投影。
func (a *API) getShareByToken(w http.ResponseWriter, r *http.Request) {
	card, err := a.service.GetShareCard(r.Context(), r.PathValue("token"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, domain.ShareView{
		SourceType:   card.SourceType,
		Snapshot:     card.Snapshot,
		IncludePhoto: card.IncludePhoto,
		ExpiresAt:    card.ExpiresAt,
	})
}

// DELETE /v1/shares/{id} —— 撤销分享（语义：删除该分享资源）。
func (a *API) deleteShare(w http.ResponseWriter, r *http.Request) {
	if err := a.service.DeleteShareCard(r.Context(), currentUser(r).ID, r.PathValue("id")); err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"revoked": true})
}
