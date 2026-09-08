package httpapi

import (
	"errors"
	"net/http"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

func (a *API) getMe(w http.ResponseWriter, r *http.Request) {
	account, err := a.service.GetAccount(r.Context(), currentUser(r))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, account)
}

func (a *API) getMyProfile(w http.ResponseWriter, r *http.Request) {
	profile, err := a.service.GetProfile(r.Context(), currentUser(r).ID)
	if errors.Is(err, repository.ErrNotFound) {
		// 从未填写过补充资料：契约允许 data 为 null。
		writeData(w, http.StatusOK, nil)
		return
	}
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	if profile == (domain.UserProfile{}) {
		writeData(w, http.StatusOK, nil)
		return
	}
	writeData(w, http.StatusOK, profile)
}

func (a *API) updateMyProfile(w http.ResponseWriter, r *http.Request) {
	var input domain.UserProfile
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	profile, err := a.service.UpdateProfile(r.Context(), currentUser(r).ID, input)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, profile)
}

func (a *API) deleteData(w http.ResponseWriter, r *http.Request) {
	if err := a.service.DeleteUserData(r.Context(), currentUser(r).ID); err != nil {
		a.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
