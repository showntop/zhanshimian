package httpapi

import (
	"net/http"

	"github.com/zhanshimian/server/internal/domain"
)

// POST /v1/hair-previews —— 202 {data: HairPreview, task}：异步发型预览。
func (a *API) createHairPreview(w http.ResponseWriter, r *http.Request) {
	var input domain.HairPreviewInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	preview, task, err := a.service.CreateHairPreview(r.Context(), currentUser(r).ID, input)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeDataTask(w, http.StatusAccepted, preview, domainTaskRef(task))
}

// GET /v1/hair-previews/active —— 当前进行中的发型预览（无则 404）：
// 客户端本地引用丢失（清缓存/换设备）时据此恢复任务展示。
func (a *API) getActiveHairPreview(w http.ResponseWriter, r *http.Request) {
	preview, err := a.service.GetActiveHairPreview(r.Context(), currentUser(r).ID)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, preview)
}

func (a *API) getHairPreview(w http.ResponseWriter, r *http.Request) {
	preview, err := a.service.GetHairPreview(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, preview)
}

func (a *API) listSavedHairPreviews(w http.ResponseWriter, r *http.Request) {
	items, err := a.service.ListSavedHairPreviews(r.Context(), currentUser(r).ID)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (a *API) saveHairPreview(w http.ResponseWriter, r *http.Request) {
	preview, err := a.service.SaveHairPreview(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, preview)
}
