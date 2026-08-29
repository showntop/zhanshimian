package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/example/jianwo/server/internal/domain"
	"github.com/example/jianwo/server/internal/repository"
	"github.com/example/jianwo/server/internal/service"
	"github.com/google/uuid"
)

type API struct {
	service         *service.Service
	logger          *slog.Logger
	devLoginEnabled bool
	runtime         RuntimeInfo
}

type RuntimeInfo struct {
	AnalysisProvider        string
	FallbackEnabled         bool
	HairPreviewProvider     string
	OutfitDiagnosisProvider string
}

type contextKey string

const userKey contextKey = "user"

func New(svc *service.Service, logger *slog.Logger, devLoginEnabled bool, runtime RuntimeInfo) http.Handler {
	api := &API{service: svc, logger: logger, devLoginEnabled: devLoginEnabled, runtime: runtime}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", api.health)
	mux.HandleFunc("POST /v1/auth/dev", api.devLogin)
	mux.HandleFunc("POST /v1/auth/wechat", api.wechatLogin)
	mux.Handle("POST /v1/media", api.auth(http.HandlerFunc(api.uploadMedia)))
	mux.Handle("POST /v1/media/demo", api.auth(http.HandlerFunc(api.createDemoMedia)))
	mux.Handle("POST /v1/analyses", api.auth(http.HandlerFunc(api.createAnalysis)))
	mux.Handle("GET /v1/analyses/{id}", api.auth(http.HandlerFunc(api.getAnalysis)))
	mux.Handle("GET /v1/reports/{id}", api.auth(http.HandlerFunc(api.getReport)))
	mux.Handle("GET /v1/reports/{id}/plans", api.auth(http.HandlerFunc(api.listPlans)))
	mux.Handle("GET /v1/plans/{id}", api.auth(http.HandlerFunc(api.getPlan)))
	mux.Handle("POST /v1/plans/{id}/select", api.auth(http.HandlerFunc(api.selectPlan)))
	mux.Handle("GET /v1/plans/{id}/checklist", api.auth(http.HandlerFunc(api.getChecklist)))
	mux.Handle("PATCH /v1/checklist/{id}", api.auth(http.HandlerFunc(api.setChecklist)))
	mux.Handle("POST /v1/feedback", api.auth(http.HandlerFunc(api.addFeedback)))
	mux.Handle("POST /v1/tools/run", api.auth(http.HandlerFunc(api.runTool)))
	mux.Handle("POST /v1/tools/{id}/save", api.auth(http.HandlerFunc(api.saveToolResult)))
	mux.Handle("POST /v1/hair-previews", api.auth(http.HandlerFunc(api.createHairPreview)))
	mux.Handle("GET /v1/hair-previews", api.auth(http.HandlerFunc(api.listSavedHairPreviews)))
	mux.Handle("GET /v1/hair-previews/{id}", api.auth(http.HandlerFunc(api.getHairPreview)))
	mux.Handle("POST /v1/hair-previews/{id}/save", api.auth(http.HandlerFunc(api.saveHairPreview)))
	mux.Handle("DELETE /v1/me/data", api.auth(http.HandlerFunc(api.deleteData)))
	return requestMiddleware(logger, mux)
}

func requestMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = uuid.NewString()
		}
		r.Header.Set("X-Request-ID", requestID)
		w.Header().Set("X-Request-ID", requestID)
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
		logger.Info("http request", "request_id", requestID, "method", r.Method, "path", r.URL.Path, "latency_ms", time.Since(started).Milliseconds())
	})
}

func (a *API) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		user, err := a.service.Authenticate(r.Context(), value)
		if err != nil {
			writeError(w, r, http.StatusUnauthorized, "unauthorized", "登录已失效，请重新进入小程序")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, user)))
	})
}

func currentUser(r *http.Request) domain.User { return r.Context().Value(userKey).(domain.User) }

func (a *API) health(w http.ResponseWriter, _ *http.Request) {
	writeData(w, http.StatusOK, map[string]any{
		"status":                    "ok",
		"analysis_provider":         a.runtime.AnalysisProvider,
		"fallback_enabled":          a.runtime.FallbackEnabled,
		"hair_preview_provider":     a.runtime.HairPreviewProvider,
		"outfit_diagnosis_provider": a.runtime.OutfitDiagnosisProvider,
	})
}

func (a *API) devLogin(w http.ResponseWriter, r *http.Request) {
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
	session, err := a.service.DevLogin(r.Context(), input.Nickname)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, session)
}

func (a *API) wechatLogin(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Code     string `json:"code"`
		Nickname string `json:"nickname"`
	}
	if err := decodeJSON(r, &input); err != nil || input.Code == "" {
		writeError(w, r, http.StatusBadRequest, "validation_error", "微信登录 code 不能为空")
		return
	}
	if !a.devLoginEnabled {
		writeError(w, r, http.StatusNotImplemented, "wechat_adapter_required", "请配置微信 jscode2session 适配器")
		return
	}
	session, err := a.service.DevLogin(r.Context(), input.Nickname)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, session)
}

func (a *API) uploadMedia(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 11<<20)
	if err := r.ParseMultipartForm(11 << 20); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", "照片过大或上传格式错误")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", "请选择一张照片")
		return
	}
	defer file.Close()
	asset, err := a.service.UploadMedia(r.Context(), currentUser(r).ID, r.FormValue("kind"), header.Filename, multipartMIME(header), header.Size, file)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, asset)
}

func (a *API) createDemoMedia(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Kind string `json:"kind"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	asset, err := a.service.CreateDemoMedia(r.Context(), currentUser(r).ID, input.Kind)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, asset)
}

func multipartMIME(header *multipart.FileHeader) string {
	value := header.Header.Get("Content-Type")
	if value == "" {
		value = "application/octet-stream"
	}
	return value
}

func (a *API) createAnalysis(w http.ResponseWriter, r *http.Request) {
	var input domain.CreateAnalysisInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	analysis, err := a.service.CreateAnalysis(r.Context(), currentUser(r).ID, input)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusAccepted, analysis)
}

func (a *API) getAnalysis(w http.ResponseWriter, r *http.Request) {
	item, err := a.service.GetAnalysis(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}
func (a *API) getReport(w http.ResponseWriter, r *http.Request) {
	item, err := a.service.GetReport(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}
func (a *API) listPlans(w http.ResponseWriter, r *http.Request) {
	items, err := a.service.ListPlans(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
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
func (a *API) setChecklist(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Completed bool `json:"completed"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	item, err := a.service.SetChecklistItem(r.Context(), currentUser(r).ID, r.PathValue("id"), input.Completed)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}
func (a *API) addFeedback(w http.ResponseWriter, r *http.Request) {
	var input domain.FeedbackInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	if err := a.service.AddFeedback(r.Context(), currentUser(r).ID, input); err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, map[string]bool{"saved": true})
}
func (a *API) runTool(w http.ResponseWriter, r *http.Request) {
	var input domain.ToolInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	result, err := a.service.RunTool(r.Context(), currentUser(r).ID, input)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, result)
}
func (a *API) saveToolResult(w http.ResponseWriter, r *http.Request) {
	result, err := a.service.SaveToolResult(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, result)
}

func (a *API) createHairPreview(w http.ResponseWriter, r *http.Request) {
	var input domain.HairPreviewInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	preview, err := a.service.CreateHairPreview(r.Context(), currentUser(r).ID, input)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusAccepted, preview)
}

func (a *API) getHairPreview(w http.ResponseWriter, r *http.Request) {
	preview, err := a.service.GetHairPreview(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, preview)
}

func (a *API) listSavedHairPreviews(w http.ResponseWriter, r *http.Request) {
	items, err := a.service.ListSavedHairPreviews(r.Context(), currentUser(r).ID)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (a *API) saveHairPreview(w http.ResponseWriter, r *http.Request) {
	preview, err := a.service.SaveHairPreview(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, preview)
}
func (a *API) deleteData(w http.ResponseWriter, r *http.Request) {
	if err := a.service.DeleteUserData(r.Context(), currentUser(r).ID); err != nil {
		a.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("请求内容格式不正确")
	}
	return nil
}

func (a *API) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrValidation):
		writeError(w, r, http.StatusBadRequest, "validation_error", strings.TrimPrefix(err.Error(), service.ErrValidation.Error()+": "))
	case errors.Is(err, repository.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "没有找到对应内容")
	case errors.Is(err, service.ErrForbidden):
		writeError(w, r, http.StatusForbidden, "forbidden", "没有访问权限")
	default:
		a.internalError(w, r, err)
	}
}
func (a *API) internalError(w http.ResponseWriter, r *http.Request, err error) {
	a.logger.Error("request failed", "path", r.URL.Path, "error", err)
	writeError(w, r, http.StatusInternalServerError, "internal_error", "服务暂时不可用，请稍后重试")
}

func writeData(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": value})
}
func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": message, "request_id": r.Header.Get("X-Request-ID")}})
}
