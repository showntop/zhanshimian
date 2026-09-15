package httpapi

import "net/http"

// GET /v1/home/bootstrap —— 首页一屏聚合：档案摘要 + 当前报告 + 最近方案集 +
// 今日方案 + 进行中操作 + 权益摘要。service/home 组装契约 HomeBootstrap，
// 这里只透传。
func (a *API) homeBootstrap(w http.ResponseWriter, r *http.Request) {
	if a.home == nil {
		a.internalError(w, r, errHomeUnavailable)
		return
	}
	payload, err := a.home.Bootstrap(r.Context(), currentUser(r).ID)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, payload)
}
