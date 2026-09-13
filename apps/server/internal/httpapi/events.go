package httpapi

import (
	"errors"
	"net/http"

	"github.com/zhanshimian/server/internal/domain"
)

var errEventsUnavailable = errors.New("events writer unavailable")

// POST /v1/events —— 埋点（静默失败：埋点永不影响业务流程）。
func (a *API) trackProductEvent(w http.ResponseWriter, r *http.Request) {
	if a.events == nil {
		a.internalError(w, r, errEventsUnavailable)
		return
	}
	var input domain.ProductEventInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	_ = a.events.TrackProductEvent(r.Context(), currentUser(r).ID, input)
	writeData(w, http.StatusAccepted, map[string]bool{"accepted": true})
}
