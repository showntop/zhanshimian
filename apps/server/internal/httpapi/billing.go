package httpapi

import (
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

func (a *API) getBillingMe(w http.ResponseWriter, r *http.Request) {
	summary, err := a.service.BillingSummary(r.Context(), currentUser(r).ID)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, summary)
}

func (a *API) createBillingOrder(w http.ResponseWriter, r *http.Request) {
	var input struct {
		SKUID string `json:"sku_id"`
		Code  string `json:"code"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	order, err := a.service.CreateBillingOrder(r.Context(), currentUser(r).ID, strings.TrimSpace(input.SKUID), strings.TrimSpace(input.Code))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, order)
}

func (a *API) syncBillingOrder(w http.ResponseWriter, r *http.Request) {
	orderID := r.PathValue("id")
	if _, err := uuid.Parse(orderID); err != nil {
		writeError(w, r, http.StatusNotFound, "not_found", "没有找到对应内容")
		return
	}
	order, err := a.service.SyncBillingOrder(r.Context(), currentUser(r).ID, orderID)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, order)
}

func (a *API) billingNotify(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", "通知内容无法读取")
		return
	}
	if err := a.service.HandleBillingNotify(r.Context(), raw, notifySignature(r)); err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, map[string]string{"status": "ok"})
}

func notifySignature(r *http.Request) string {
	for _, header := range []string{"X-Signature", "Wechatpay-Signature", "Pay-Sig", "X-Pay-Sig"} {
		if value := strings.TrimSpace(r.Header.Get(header)); value != "" {
			return value
		}
	}
	return strings.TrimSpace(r.URL.Query().Get("pay_sig"))
}
