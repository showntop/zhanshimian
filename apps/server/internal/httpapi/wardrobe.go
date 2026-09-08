package httpapi

import (
	"net/http"

	"github.com/zhanshimian/server/internal/domain"
)

func (a *API) listWardrobeItems(w http.ResponseWriter, r *http.Request) {
	items, err := a.service.ListWardrobeItems(r.Context(), currentUser(r).ID)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (a *API) createWardrobeItem(w http.ResponseWriter, r *http.Request) {
	var input domain.WardrobeItemInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	item, err := a.service.CreateWardrobeItem(r.Context(), currentUser(r).ID, input)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, item)
}

func (a *API) deleteWardrobeItem(w http.ResponseWriter, r *http.Request) {
	if err := a.service.DeleteWardrobeItem(r.Context(), currentUser(r).ID, r.PathValue("id")); err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"deleted": true})
}

// POST /v1/wardrobe/outfits —— 客户端显式组合 item_ids，服务端冻结当前上下文。
func (a *API) createWardrobeOutfit(w http.ResponseWriter, r *http.Request) {
	var input domain.WardrobeOutfitInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	item, err := a.service.CreateWardrobeOutfit(r.Context(), currentUser(r).ID, input)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, item)
}

func (a *API) wearWardrobeOutfit(w http.ResponseWriter, r *http.Request) {
	item, err := a.service.MarkWardrobeOutfitWorn(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}
