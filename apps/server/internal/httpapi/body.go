package httpapi

import (
	"net/http"

	"github.com/zhanshimian/server/internal/domain"
)

// createBodyPresentation 创建 3D 形象 Lite：202 + 公开操作投影（轮询走
// GET /v1/operations/{id}，与其他任务型端点一致）。
func (a *API) createBodyPresentation(w http.ResponseWriter, r *http.Request) {
	var input domain.BodyPresentationInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	created, err := a.body.Create(r.Context(), currentUser(r).ID, input)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeDataOperation(w, http.StatusAccepted, created.Presentation.Presentation, publicOperation(created.Operation))
}

func (a *API) getBodyPresentationStatus(w http.ResponseWriter, r *http.Request) {
	item, err := a.body.Status(r.Context(), currentUser(r).ID)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (a *API) getBodyPresentation(w http.ResponseWriter, r *http.Request) {
	item, err := a.body.Get(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}
