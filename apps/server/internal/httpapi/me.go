package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

// profilePayload 是 /v1/me/profile 的线形 DTO，与冻结 OpenAPI UserProfile 一致：
// height_cm/role/budget 必填，体重/三围选填。持久化的 domain.UserProfile 不打
// 标签且测量项存于 preferences JSONB，故线形与领域模型在 HTTP 边界显式互转。
type profilePayload struct {
	HeightCM int      `json:"height_cm"`
	Role     string   `json:"role"`
	Budget   string   `json:"budget"`
	WeightKG *float64 `json:"weight_kg,omitempty"`
	BustCM   *float64 `json:"bust_cm,omitempty"`
	WaistCM  *float64 `json:"waist_cm,omitempty"`
	HipCM    *float64 `json:"hip_cm,omitempty"`
}

// toDomain 把线形 DTO 映射为领域模型：测量项打包成 Preferences 补丁（仅含
// 请求中提供的键），由仓储合并进 preferences JSONB。
func (p profilePayload) toDomain() (domain.UserProfile, error) {
	patch := map[string]float64{}
	if p.WeightKG != nil {
		patch["weight_kg"] = *p.WeightKG
	}
	if p.BustCM != nil {
		patch["bust_cm"] = *p.BustCM
	}
	if p.WaistCM != nil {
		patch["waist_cm"] = *p.WaistCM
	}
	if p.HipCM != nil {
		patch["hip_cm"] = *p.HipCM
	}
	raw, err := json.Marshal(patch)
	if err != nil {
		return domain.UserProfile{}, err
	}
	return domain.UserProfile{
		HeightCM:    p.HeightCM,
		Role:        p.Role,
		Budget:      p.Budget,
		Preferences: raw,
	}, nil
}

// payloadFromDomain 把领域模型映射回线形 DTO：从 Preferences 提取测量项，
// 内部键（feedback_memory 等）与未提供的测量键不上线。
func payloadFromDomain(profile domain.UserProfile) profilePayload {
	payload := profilePayload{HeightCM: profile.HeightCM, Role: profile.Role, Budget: profile.Budget}
	if len(profile.Preferences) == 0 {
		return payload
	}
	// preferences 是混合 JSONB（feedback_memory 等内部键与测量键共存），
	// 逐键解析，非数字值不影响其他键的投影。
	var prefs map[string]json.RawMessage
	if err := json.Unmarshal(profile.Preferences, &prefs); err != nil {
		return payload
	}
	measurement := func(key string) *float64 {
		raw, ok := prefs[key]
		if !ok {
			return nil
		}
		var v float64
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil
		}
		return &v
	}
	payload.WeightKG = measurement("weight_kg")
	payload.BustCM = measurement("bust_cm")
	payload.WaistCM = measurement("waist_cm")
	payload.HipCM = measurement("hip_cm")
	return payload
}

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
	if profile.Role == "" && profile.HeightCM == 0 {
		// 评估发布会回填仅含 current_report_id 的空行；视为「从未填写」。
		writeData(w, http.StatusOK, nil)
		return
	}
	writeData(w, http.StatusOK, payloadFromDomain(profile))
}

func (a *API) updateMyProfile(w http.ResponseWriter, r *http.Request) {
	if a.account == nil {
		a.internalError(w, r, errAccountUnavailable)
		return
	}
	var payload profilePayload
	if err := decodeJSON(r, &payload); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	input, err := payload.toDomain()
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	profile, err := a.account.UpdateProfile(r.Context(), currentUser(r).ID, input)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, payloadFromDomain(profile))
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
