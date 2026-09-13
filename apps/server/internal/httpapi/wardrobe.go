package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/zhanshimian/server/internal/service/wardrobe"
)

// WardrobeService 是衣橱的最小依赖。
type WardrobeService interface {
	ListItems(ctx context.Context, userID string) ([]wardrobe.Item, error)
	CreateItem(ctx context.Context, userID string, input wardrobe.CreateItemInput) (wardrobe.Item, error)
	DeleteItem(ctx context.Context, userID string, id string) error
	CreateOutfit(ctx context.Context, userID string, input wardrobe.CreateOutfitInput) (wardrobe.Outfit, error)
	WearOutfit(ctx context.Context, userID string, id string) (wardrobe.Outfit, error)
}

var errWardrobeUnavailable = errors.New("wardrobe service unavailable")

func (a *API) listWardrobeItems(w http.ResponseWriter, r *http.Request) {
	if a.wardrobe == nil {
		a.internalError(w, r, errWardrobeUnavailable)
		return
	}
	items, err := a.wardrobe.ListItems(r.Context(), currentUser(r).ID)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (a *API) createWardrobeItem(w http.ResponseWriter, r *http.Request) {
	if a.wardrobe == nil {
		a.internalError(w, r, errWardrobeUnavailable)
		return
	}
	var input struct {
		MediaAssetID string   `json:"media_id"`
		Name         string   `json:"name"`
		Category     string   `json:"category"`
		Color        string   `json:"color"`
		Season       string   `json:"season"`
		Formality    string   `json:"formality"`
		Scenes       []string `json:"scenes"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	item, err := a.wardrobe.CreateItem(r.Context(), currentUser(r).ID, wardrobe.CreateItemInput{
		MediaAssetID: input.MediaAssetID, Name: input.Name, Category: input.Category,
		Color: input.Color, Season: input.Season, Formality: input.Formality, Scenes: input.Scenes,
	})
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, item)
}

func (a *API) deleteWardrobeItem(w http.ResponseWriter, r *http.Request) {
	if a.wardrobe == nil {
		a.internalError(w, r, errWardrobeUnavailable)
		return
	}
	if err := a.wardrobe.DeleteItem(r.Context(), currentUser(r).ID, r.PathValue("id")); err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (a *API) createWardrobeOutfit(w http.ResponseWriter, r *http.Request) {
	if a.wardrobe == nil {
		a.internalError(w, r, errWardrobeUnavailable)
		return
	}
	var input struct {
		Title   string   `json:"title"`
		Note    string   `json:"note"`
		ItemIDs []string `json:"item_ids"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	outfit, err := a.wardrobe.CreateOutfit(r.Context(), currentUser(r).ID, wardrobe.CreateOutfitInput{
		Title: input.Title, Note: input.Note, ItemIDs: input.ItemIDs,
	})
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, outfit)
}

func (a *API) wearWardrobeOutfit(w http.ResponseWriter, r *http.Request) {
	if a.wardrobe == nil {
		a.internalError(w, r, errWardrobeUnavailable)
		return
	}
	item, err := a.wardrobe.WearOutfit(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}
