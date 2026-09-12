package httpapi

import (
	"net/http"

	"github.com/zhanshimian/server/internal/domain"
)

func (a *API) createBodyPresentation(w http.ResponseWriter, r *http.Request) {
	var input domain.BodyPresentationInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	item, task, err := a.service.CreateBodyPresentation(r.Context(), currentUser(r).ID, input)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeDataTask(w, http.StatusAccepted, item, domainTaskRef(task))
}

func (a *API) getBodyPresentationStatus(w http.ResponseWriter, r *http.Request) {
	item, err := a.service.GetBodyPresentationStatus(r.Context(), currentUser(r).ID)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (a *API) getBodyPresentation(w http.ResponseWriter, r *http.Request) {
	item, err := a.service.GetBodyPresentation(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}
