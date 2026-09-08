package httpapi

import "net/http"

// GET /v1/home/bootstrap —— 首页一屏聚合：
// 档案摘要 + 最新报告 + 今日方案 + 进行中任务 + 最近方案，替代客户端 4 次并发请求。
func (a *API) homeBootstrap(w http.ResponseWriter, r *http.Request) {
	payload, err := a.service.HomeBootstrap(r.Context(), currentUser(r))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, payload)
}
