package httpapi

import (
	"errors"
	"net"
	"net/http"

	"github.com/zhanshimian/server/internal/provider/identity"
	"github.com/zhanshimian/server/internal/service/account"
)

var errAccountUnavailable = errors.New("account service unavailable")

var _ AccountService = (*account.Service)(nil)

func (a *API) devLogin(w http.ResponseWriter, r *http.Request) {
	if a.account == nil {
		a.internalError(w, r, errAccountUnavailable)
		return
	}
	if !a.devLoginEnabled {
		writeError(w, r, http.StatusNotFound, "not_found", "接口不存在")
		return
	}
	var input struct {
		Nickname string `json:"nickname"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	session, err := a.account.DevLogin(r.Context(), input.Nickname)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, session)
}

func (a *API) wechatLogin(w http.ResponseWriter, r *http.Request) {
	if a.account == nil {
		a.internalError(w, r, errAccountUnavailable)
		return
	}
	var input struct {
		Code     string `json:"code"`
		Nickname string `json:"nickname"`
	}
	if err := decodeJSON(r, &input); err != nil || input.Code == "" {
		writeError(w, r, http.StatusBadRequest, "validation_error", "微信登录 code 不能为空")
		return
	}
	session, err := a.account.WeChatLogin(r.Context(), input.Code, input.Nickname)
	if errors.Is(err, identity.ErrWeChatUnavailable) && a.devLoginEnabled {
		session, err = a.account.DevLogin(r.Context(), input.Nickname)
	}
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, session)
}

func (a *API) wechatAppLogin(w http.ResponseWriter, r *http.Request) {
	if a.account == nil {
		a.internalError(w, r, errAccountUnavailable)
		return
	}
	var input struct {
		Code     string `json:"code"`
		Nickname string `json:"nickname"`
	}
	if err := decodeJSON(r, &input); err != nil || input.Code == "" {
		writeError(w, r, http.StatusBadRequest, "validation_error", "微信登录 code 不能为空")
		return
	}
	session, err := a.account.WeChatAppLogin(r.Context(), input.Code, input.Nickname)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, session)
}

func (a *API) appleLogin(w http.ResponseWriter, r *http.Request) {
	if a.account == nil {
		a.internalError(w, r, errAccountUnavailable)
		return
	}
	var input struct {
		IdentityToken string `json:"identity_token"`
		Nickname      string `json:"nickname"`
	}
	if err := decodeJSON(r, &input); err != nil || input.IdentityToken == "" {
		writeError(w, r, http.StatusBadRequest, "validation_error", "Apple 登录 identity_token 不能为空")
		return
	}
	session, err := a.account.AppleLogin(r.Context(), input.IdentityToken, input.Nickname)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, session)
}

func (a *API) smsRequest(w http.ResponseWriter, r *http.Request) {
	if a.account == nil {
		a.internalError(w, r, errAccountUnavailable)
		return
	}
	var input struct {
		Phone string `json:"phone"`
	}
	if err := decodeJSON(r, &input); err != nil || input.Phone == "" {
		writeError(w, r, http.StatusBadRequest, "validation_error", "手机号不能为空")
		return
	}
	devCode, err := a.account.RequestSmsCode(r.Context(), input.Phone, clientIP(r))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	payload := map[string]any{"sent": true, "cooldown_seconds": 60}
	if devCode != "" {
		// dev-only 扩展字段：ConsoleSms（开发环境）返回明文验证码便于联调与 e2e，生产恒为空。
		payload["dev_code"] = devCode
	}
	writeData(w, http.StatusCreated, payload)
}

func (a *API) smsVerify(w http.ResponseWriter, r *http.Request) {
	if a.account == nil {
		a.internalError(w, r, errAccountUnavailable)
		return
	}
	var input struct {
		Phone    string `json:"phone"`
		Code     string `json:"code"`
		Nickname string `json:"nickname"`
	}
	if err := decodeJSON(r, &input); err != nil || input.Phone == "" || input.Code == "" {
		writeError(w, r, http.StatusBadRequest, "validation_error", "手机号与验证码不能为空")
		return
	}
	session, err := a.account.VerifySmsCode(r.Context(), input.Phone, input.Code, input.Nickname)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, session)
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	if a.account == nil {
		a.internalError(w, r, errAccountUnavailable)
		return
	}
	if err := a.account.Logout(r.Context(), currentToken(r)); err != nil {
		a.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
