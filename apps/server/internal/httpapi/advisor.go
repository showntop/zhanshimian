package httpapi

import (
	"net/http"

	"github.com/zhanshimian/server/internal/domain"
)

func (a *API) sendAdvisorMessage(w http.ResponseWriter, r *http.Request) {
	var input domain.AdvisorMessageInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	item, err := a.service.SendAdvisorMessage(r.Context(), currentUser(r).ID, input)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, item)
}

func (a *API) listAdvisorMessages(w http.ResponseWriter, r *http.Request) {
	items, err := a.service.ListAdvisorMessages(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (a *API) applyAdvisorAction(w http.ResponseWriter, r *http.Request) {
	item, err := a.service.ApplyAdvisorAction(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (a *API) trackProductEvent(w http.ResponseWriter, r *http.Request) {
	var input domain.ProductEventInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	if err := a.service.TrackProductEvent(r.Context(), currentUser(r).ID, input); err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusAccepted, map[string]bool{"accepted": true})
}
