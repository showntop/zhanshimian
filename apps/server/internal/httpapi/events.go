package httpapi

import (
	"net/http"

	"github.com/zhanshimian/server/internal/domain"
)

// POST /v1/events —— 埋点（legacy service 面，暂留至外围切换完成）。
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
