package httpapi

import (
	"net/http"
	"strings"
)

// GET /v1/tasks/{id} —— 统一任务状态（照片不合格时 error.photo_reasons 逐图中文原因）。
func (a *API) getTask(w http.ResponseWriter, r *http.Request) {
	task, err := a.service.GetTask(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, task)
}

// GET /v1/tasks?ids=a,b,c —— 批量任务状态（首页任务横轨一次请求，≤20 个）。
func (a *API) getTasks(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.URL.Query().Get("ids"))
	if raw == "" {
		writeError(w, r, http.StatusBadRequest, "validation_error", "ids 参数不能为空")
		return
	}
	ids := strings.Split(raw, ",")
	if len(ids) > 20 {
		writeError(w, r, http.StatusBadRequest, "validation_error", "一次最多查询 20 个任务")
		return
	}
	tasks, err := a.service.GetTasksByIDs(r.Context(), currentUser(r).ID, ids)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, tasks)
}
