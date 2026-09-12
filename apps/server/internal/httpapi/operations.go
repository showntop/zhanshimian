package httpapi

import (
	"net/http"
	"strings"
)

func (a *API) getOperation(w http.ResponseWriter, r *http.Request) {
	if a.operations == nil {
		a.internalError(w, r, errOperationUnavailable)
		return
	}
	op, err := a.operations.Get(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, publicOperation(op))
}

func (a *API) listOperations(w http.ResponseWriter, r *http.Request) {
	if a.operations == nil {
		a.internalError(w, r, errOperationUnavailable)
		return
	}
	raw := strings.TrimSpace(r.URL.Query().Get("ids"))
	ops, err := a.operations.GetMany(r.Context(), currentUser(r).ID, splitIDs(raw))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	items := make([]operationDTO, 0, len(ops))
	for _, op := range ops {
		items = append(items, publicOperation(op))
	}
	writeData(w, http.StatusOK, items)
}

func splitIDs(raw string) []string {
	if raw == "" {
		return nil
	}
	return strings.Split(raw, ",")
}
