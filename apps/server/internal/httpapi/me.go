package httpapi

import (
	"errors"
	"net/http"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

func (a *API) updateMe(w http.ResponseWriter, r *http.Request) {
	if a.account == nil {
		a.internalError(w, r, errAccountUnavailable)
		return
	}
	var input struct {
		Nickname      string `json:"nickname"`
		AvatarMediaID string `json:"avatar_media_id"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	account, err := a.account.UpdateAccount(r.Context(), currentUser(r), input.Nickname, input.AvatarMediaID)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, account)
}

func (a *API) getMe(w http.ResponseWriter, r *http.Request) {
	if a.account == nil {
		a.internalError(w, r, errAccountUnavailable)
		return
	}
	account, err := a.account.GetAccount(r.Context(), currentUser(r))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, account)
}

func (a *API) getMyProfile(w http.ResponseWriter, r *http.Request) {
	if a.account == nil {
		a.internalError(w, r, errAccountUnavailable)
		return
	}
	profile, err := a.account.GetProfile(r.Context(), currentUser(r).ID)
	if errors.Is(err, repository.ErrNotFound) {
		// 从未填写过补充资料：契约允许 data 为 null。
		writeData(w, http.StatusOK, nil)
		return
	}
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	if profile.ID == "" && profile.UserID == "" && profile.Role == "" && profile.HeightCM == 0 {
		writeData(w, http.StatusOK, nil)
		return
	}
	writeData(w, http.StatusOK, profile)
}

func (a *API) updateMyProfile(w http.ResponseWriter, r *http.Request) {
	if a.account == nil {
		a.internalError(w, r, errAccountUnavailable)
		return
	}
	var input domain.UserProfile
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	profile, err := a.account.UpdateProfile(r.Context(), currentUser(r).ID, input)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, profile)
}

func (a *API) deleteData(w http.ResponseWriter, r *http.Request) {
	if a.account == nil {
		a.internalError(w, r, errAccountUnavailable)
		return
	}
	if err := a.account.DeleteUserData(r.Context(), currentUser(r).ID, a.deleteObject); err != nil {
		a.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
