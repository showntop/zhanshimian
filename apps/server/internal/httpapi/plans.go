package httpapi

import (
	"net/http"

	"github.com/zhanshimian/server/internal/domain"
)

func (a *API) listPlans(w http.ResponseWriter, r *http.Request) {
	items, err := a.service.ListPlans(r.Context(), currentUser(r).ID, r.PathValue("id"), r.URL.Query().Get("scene"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

// PUT /v1/reports/{id}/plans —— 幂等确保该场景的三方案组存在：
// 已有组同步补图任务（200 + plans）；general 空组触发 plan_group
// 生成任务（202 + task ref），客户端轮询任务直到方案就绪。
func (a *API) putReportPlans(w http.ResponseWriter, r *http.Request) {
	var input domain.PlansUpsertInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	items, task, err := a.service.PutReportPlans(r.Context(), currentUser(r).ID, r.PathValue("id"), input)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	if task != nil {
		writeDataTask(w, http.StatusAccepted, items, viewTaskRef(*task))
		return
	}
	writeData(w, http.StatusOK, items)
}

// POST /v1/plans/{id}/look/regenerate —— 单方案图重试（202 + task）。
func (a *API) regeneratePlanLook(w http.ResponseWriter, r *http.Request) {
	plan, task, err := a.service.RegeneratePlanLook(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeDataTask(w, http.StatusAccepted, plan, viewTaskRef(task))
}

func (a *API) getPlan(w http.ResponseWriter, r *http.Request) {
	item, err := a.service.GetPlan(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (a *API) selectPlan(w http.ResponseWriter, r *http.Request) {
	if err := a.service.SelectPlan(r.Context(), currentUser(r).ID, r.PathValue("id")); err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]bool{"selected": true})
}

func (a *API) getChecklist(w http.ResponseWriter, r *http.Request) {
	items, err := a.service.GetChecklist(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (a *API) patchChecklistItem(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Completed bool `json:"completed"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	item, err := a.service.SetChecklistItem(r.Context(), currentUser(r).ID, r.PathValue("id"), r.PathValue("itemId"), input.Completed)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

// POST /v1/plans/{id}/feedback —— 201 {saved, message}：message 为服务端生成的个性化承诺文案（G3）。
func (a *API) planFeedback(w http.ResponseWriter, r *http.Request) {
	var input domain.FeedbackInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	ack, err := a.service.AddPlanFeedback(r.Context(), currentUser(r).ID, r.PathValue("id"), input)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, ack)
}
