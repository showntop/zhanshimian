// Package httpapi 只做协议翻译：路由注册、鉴权中间件、请求解码与响应包装。
// 业务规则全部在 service 层；按域拆分的 handler 见同包各文件。
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/provider"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service"
)

type API struct {
	service         *service.Service
	logger          *slog.Logger
	devLoginEnabled bool
	runtime         RuntimeInfo
}

type RuntimeInfo struct {
	Environment             string
	StorageProvider         string
	WeatherProvider         string
	WeChatLoginConfigured   bool
	WeChatAppConfigured     bool
	AppleLoginConfigured    bool
	SmsProvider             string
	AIRoutes                map[string]string
}

type contextKey string

const userKey contextKey = "user"
const tokenKey contextKey = "token"

// New 注册全部路由（契约见 contracts/openapi.yaml，49 路径 / 53 操作）。
func New(svc *service.Service, logger *slog.Logger, devLoginEnabled bool, runtime RuntimeInfo) http.Handler {
	api := &API{service: svc, logger: logger, devLoginEnabled: devLoginEnabled, runtime: runtime}
	mux := http.NewServeMux()

	// ---- 公开端点（免 bearer）：healthz、auth/*、分享公开读 ----
	mux.HandleFunc("GET /healthz", api.health)
	mux.HandleFunc("POST /v1/auth/dev", api.devLogin)
	mux.HandleFunc("POST /v1/auth/wechat", api.wechatLogin)
	mux.HandleFunc("POST /v1/auth/wechat-app", api.wechatAppLogin)
	mux.HandleFunc("POST /v1/auth/apple", api.appleLogin)
	mux.HandleFunc("POST /v1/auth/sms/request", api.smsRequest)
	mux.HandleFunc("POST /v1/auth/sms/verify", api.smsVerify)
	mux.HandleFunc("GET /v1/shares/{token}", api.getShareByToken)

	// ---- 认证态端点 ----
	mux.Handle("DELETE /v1/auth/session", api.auth(http.HandlerFunc(api.logout)))
	mux.Handle("GET /v1/me", api.auth(http.HandlerFunc(api.getMe)))
	mux.Handle("GET /v1/me/profile", api.auth(http.HandlerFunc(api.getMyProfile)))
	mux.Handle("PUT /v1/me/profile", api.auth(http.HandlerFunc(api.updateMyProfile)))
	mux.Handle("DELETE /v1/me/data", api.auth(http.HandlerFunc(api.deleteData)))

	mux.Handle("GET /v1/tasks/{id}", api.auth(http.HandlerFunc(api.getTask)))
	mux.Handle("GET /v1/tasks", api.auth(http.HandlerFunc(api.getTasks)))
	mux.Handle("GET /v1/home/bootstrap", api.auth(http.HandlerFunc(api.homeBootstrap)))

	mux.Handle("POST /v1/media", api.auth(http.HandlerFunc(api.uploadMedia)))
	mux.Handle("POST /v1/media/demo", api.auth(http.HandlerFunc(api.createDemoMedia)))

	mux.Handle("POST /v1/analyses", api.auth(http.HandlerFunc(api.createAnalysis)))
	mux.Handle("GET /v1/analyses/{id}", api.auth(http.HandlerFunc(api.getAnalysis)))
	mux.Handle("GET /v1/reports/current", api.auth(http.HandlerFunc(api.getCurrentReport)))
	mux.Handle("GET /v1/reports/{id}", api.auth(http.HandlerFunc(api.getReport)))

	mux.Handle("GET /v1/reports/{id}/plans", api.auth(http.HandlerFunc(api.listPlans)))
	mux.Handle("PUT /v1/reports/{id}/plans", api.auth(http.HandlerFunc(api.putReportPlans)))
	mux.Handle("POST /v1/plans/{id}/look/regenerate", api.auth(http.HandlerFunc(api.regeneratePlanLook)))
	mux.Handle("GET /v1/plans/{id}", api.auth(http.HandlerFunc(api.getPlan)))
	mux.Handle("POST /v1/plans/{id}/select", api.auth(http.HandlerFunc(api.selectPlan)))
	mux.Handle("GET /v1/plans/{id}/checklist", api.auth(http.HandlerFunc(api.getChecklist)))
	mux.Handle("PATCH /v1/plans/{id}/checklist/{itemId}", api.auth(http.HandlerFunc(api.patchChecklistItem)))
	mux.Handle("POST /v1/plans/{id}/feedback", api.auth(http.HandlerFunc(api.planFeedback)))

	mux.Handle("POST /v1/diagnostics", api.auth(http.HandlerFunc(api.createDiagnostic)))
	mux.Handle("PATCH /v1/diagnostics/{id}", api.auth(http.HandlerFunc(api.patchDiagnostic)))
	mux.Handle("GET /v1/hairstyles", api.auth(http.HandlerFunc(api.listHairstyles)))

	mux.Handle("POST /v1/hair-previews", api.auth(http.HandlerFunc(api.createHairPreview)))
	mux.Handle("GET /v1/hair-previews", api.auth(http.HandlerFunc(api.listSavedHairPreviews)))
	mux.Handle("GET /v1/hair-previews/{id}", api.auth(http.HandlerFunc(api.getHairPreview)))
	mux.Handle("POST /v1/hair-previews/{id}/save", api.auth(http.HandlerFunc(api.saveHairPreview)))

	mux.Handle("GET /v1/today/context", api.auth(http.HandlerFunc(api.getTodayContext)))
	mux.Handle("GET /v1/today/plans/current", api.auth(http.HandlerFunc(api.getTodayPlan)))
	mux.Handle("POST /v1/today/plans", api.auth(http.HandlerFunc(api.createTodayPlan)))
	mux.Handle("POST /v1/today/plans/{id}/activate", api.auth(http.HandlerFunc(api.activateTodayPlan)))
	mux.Handle("POST /v1/today/plans/{id}/feedback", api.auth(http.HandlerFunc(api.feedbackTodayPlan)))

	mux.Handle("POST /v1/shares", api.auth(http.HandlerFunc(api.createShare)))
	mux.Handle("DELETE /v1/shares/{id}", api.auth(http.HandlerFunc(api.deleteShare)))

	mux.Handle("GET /v1/wardrobe/items", api.auth(http.HandlerFunc(api.listWardrobeItems)))
	mux.Handle("POST /v1/wardrobe/items", api.auth(http.HandlerFunc(api.createWardrobeItem)))
	mux.Handle("DELETE /v1/wardrobe/items/{id}", api.auth(http.HandlerFunc(api.deleteWardrobeItem)))
	mux.Handle("POST /v1/wardrobe/outfits", api.auth(http.HandlerFunc(api.createWardrobeOutfit)))
	mux.Handle("POST /v1/wardrobe/outfits/{id}/wear", api.auth(http.HandlerFunc(api.wearWardrobeOutfit)))

	mux.Handle("POST /v1/advisor/messages", api.auth(http.HandlerFunc(api.sendAdvisorMessage)))
	mux.Handle("GET /v1/advisor/conversations/{id}/messages", api.auth(http.HandlerFunc(api.listAdvisorMessages)))
	mux.Handle("POST /v1/advisor/actions/{id}/apply", api.auth(http.HandlerFunc(api.applyAdvisorAction)))

	mux.Handle("POST /v1/events", api.auth(http.HandlerFunc(api.trackProductEvent)))
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
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
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
		token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		user, err := a.service.Authenticate(r.Context(), token)
		if err != nil {
			writeError(w, r, http.StatusUnauthorized, "unauthorized", "登录已失效，请重新进入应用")
			return
		}
		ctx := context.WithValue(r.Context(), userKey, user)
		ctx = context.WithValue(ctx, tokenKey, token)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func currentUser(r *http.Request) domain.User { return r.Context().Value(userKey).(domain.User) }

func currentToken(r *http.Request) string {
	token, _ := r.Context().Value(tokenKey).(string)
	return token
}

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	payload := map[string]any{
		"status":                      "ok",
		"environment":                 a.runtime.Environment,
		"storage_provider":            a.runtime.StorageProvider,
		"weather_provider":            a.runtime.WeatherProvider,
		"wechat_login_configured":     a.runtime.WeChatLoginConfigured,
		"wechat_app_login_configured": a.runtime.WeChatAppConfigured,
		"apple_login_configured":      a.runtime.AppleLoginConfigured,
		"sms_provider":                a.runtime.SmsProvider,
		"ai_routes":                   a.runtime.AIRoutes,
	}
	if jobs, err := a.service.HealthJobs(r.Context()); err == nil {
		payload["jobs"] = jobs
	} else {
		payload["jobs"] = nil
	}
	writeData(w, http.StatusOK, payload)
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
	case errors.Is(err, provider.ErrWeChatCodeRejected):
		writeError(w, r, http.StatusUnauthorized, "wechat_code_invalid", "微信登录凭证已失效，请重新进入应用")
	case errors.Is(err, provider.ErrWeChatRateLimited):
		writeError(w, r, http.StatusTooManyRequests, "wechat_login_limited", "登录请求过于频繁，请稍后再试")
	case errors.Is(err, provider.ErrWeChatUnavailable):
		writeError(w, r, http.StatusBadGateway, "wechat_unavailable", "微信登录服务暂时不可用，请稍后再试")
	case errors.Is(err, provider.ErrAppleTokenRejected):
		writeError(w, r, http.StatusUnauthorized, "apple_token_invalid", "Apple 登录凭证已失效，请重试")
	case errors.Is(err, provider.ErrAppleUnavailable):
		writeError(w, r, http.StatusBadGateway, "apple_unavailable", "Apple 登录服务暂时不可用，请稍后再试")
	case errors.Is(err, provider.ErrSmsRejected):
		writeError(w, r, http.StatusBadRequest, "validation_error", "短信发送被拒绝，请检查手机号后重试")
	case errors.Is(err, provider.ErrSmsUnavailable):
		writeError(w, r, http.StatusBadGateway, "sms_unavailable", "短信服务暂时不可用，请稍后再试")
	case errors.Is(err, provider.ErrSmsConfig):
		a.internalError(w, r, err)
	case errors.Is(err, service.ErrRateLimited):
		writeError(w, r, http.StatusTooManyRequests, "rate_limited", strings.TrimPrefix(err.Error(), service.ErrRateLimited.Error()+": "))
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

// taskRef 是 202/201 顶层附带的任务引用（契约 TaskRef：{id,type}）。
type taskRef struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// writeDataTask 写 {data, task} 双字段响应：异步创建（202）与带任务的今日方案（201）。
func writeDataTask(w http.ResponseWriter, status int, value any, task *taskRef) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	payload := map[string]any{"data": value}
	if task != nil {
		payload["task"] = task
	}
	_ = json.NewEncoder(w).Encode(payload)
}

func domainTaskRef(task *domain.Task) *taskRef {
	if task == nil {
		return nil
	}
	return &taskRef{ID: task.ID, Type: task.Type}
}

func viewTaskRef(view domain.TaskView) *taskRef {
	return &taskRef{ID: view.ID, Type: view.Type}
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": message, "request_id": r.Header.Get("X-Request-ID")}})
}
