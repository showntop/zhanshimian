// Package httpapi 只做协议翻译：路由注册、鉴权中间件、请求解码与响应包装。
// 业务规则全部在 service 层；按域拆分的 handler 见同包各文件。
package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zhanshimian/server/internal/provider"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/account"
	"github.com/zhanshimian/server/internal/service/advisor"
	"github.com/zhanshimian/server/internal/service/billing"
	"github.com/zhanshimian/server/internal/service/body"
	"github.com/zhanshimian/server/internal/service/diagnostic"
	"github.com/zhanshimian/server/internal/service/media"
	"github.com/zhanshimian/server/internal/service/operation"
	"github.com/zhanshimian/server/internal/service/wardrobe"
	"github.com/zhanshimian/server/internal/storage"
)

// New 注册全部路由（契约见 contracts/openapi.yaml）。
func New(deps Dependencies, logger *slog.Logger, devLoginEnabled bool, runtime RuntimeInfo) http.Handler {
	api := &API{
		media: deps.Media, operations: deps.Operations,
		body:         deps.Body,
		home:         deps.Home,
		deleteObject: deps.DeleteObject,
		assessment:   deps.Assessment,
		planning:     deps.Planning,
		renders:      deps.Renders,
		execution:    deps.Execution,
		feedback:     deps.Feedback,
		today:        deps.Today,
		wardrobe:     deps.Wardrobe,
		advisor:      deps.Advisor,
		diagnostic:   deps.Diagnostic,
		share:        deps.Share,
		hair:         deps.Hair,
		account:      deps.Account,
		billing:      deps.Billing,
		events:       deps.Events,
		jobs:         deps.Jobs,
		demo:         deps.Demo,
		idempotency:  deps.Idempotency,
		logger:       logger, devLoginEnabled: devLoginEnabled, runtime: runtime,
	}
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
	mux.Handle("PATCH /v1/me", api.auth(http.HandlerFunc(api.updateMe)))
	mux.Handle("GET /v1/me/profile", api.auth(http.HandlerFunc(api.getMyProfile)))
	mux.Handle("PUT /v1/me/profile", api.auth(http.HandlerFunc(api.updateMyProfile)))
	mux.Handle("DELETE /v1/me/data", api.auth(http.HandlerFunc(api.deleteData)))

	mux.Handle("GET /v1/operations/{id}", api.auth(http.HandlerFunc(api.getOperation)))
	mux.Handle("GET /v1/operations", api.auth(http.HandlerFunc(api.listOperations)))
	mux.Handle("GET /v1/home/bootstrap", api.auth(http.HandlerFunc(api.homeBootstrap)))

	mux.Handle("POST /v1/media/upload-intents", api.auth(api.requireIdempotencyWithReplayGuard(http.HandlerFunc(api.createUploadIntent), uploadGrantStillFresh)))
	mux.Handle("POST /v1/media/upload-intents/{id}/complete", api.auth(api.requireIdempotency(http.HandlerFunc(api.completeUploadIntent))))
	mux.Handle("POST /v1/media/demo", api.auth(http.HandlerFunc(api.createDemoMedia)))

	mux.Handle("GET /v1/reports/current", api.auth(http.HandlerFunc(api.getCurrentPublishedReport)))
	mux.Handle("GET /v1/reports/{id}", api.auth(http.HandlerFunc(api.getPublishedReport)))

	mux.Handle("POST /v1/assessments", api.auth(api.requireIdempotency(http.HandlerFunc(api.createAssessment))))
	mux.Handle("POST /v1/plan-sets", api.auth(api.requireIdempotency(http.HandlerFunc(api.createPlanSet))))
	mux.Handle("GET /v1/plan-sets", api.auth(http.HandlerFunc(api.listPlanSets)))
	mux.Handle("GET /v1/plan-sets/{id}", api.auth(http.HandlerFunc(api.getPlanSet)))
	mux.Handle("POST /v1/plan-variants/{id}/render-runs", api.auth(api.requireIdempotency(http.HandlerFunc(api.createRenderRun))))
	mux.Handle("GET /v1/render-runs/{id}", api.auth(http.HandlerFunc(api.getRenderRun)))
	mux.Handle("PUT /v1/plan-sets/{id}/selection", api.auth(api.requireIdempotency(http.HandlerFunc(api.putSelection))))
	mux.Handle("POST /v1/selections/{id}/executions", api.auth(api.requireIdempotency(http.HandlerFunc(api.createExecution))))
	mux.Handle("GET /v1/executions/{id}", api.auth(http.HandlerFunc(api.getExecution)))
	mux.Handle("POST /v1/executions/{id}/events", api.auth(api.requireIfMatch(api.requireIdempotency(http.HandlerFunc(api.appendExecutionEvent)))))
	mux.Handle("POST /v1/generation-feedback", api.auth(api.requireIdempotency(http.HandlerFunc(api.createGenerationFeedback))))
	mux.Handle("POST /v1/execution-feedback", api.auth(api.requireIdempotency(http.HandlerFunc(api.createExecutionFeedback))))

	mux.Handle("POST /v1/diagnostics", api.auth(http.HandlerFunc(api.createDiagnostic)))
	mux.Handle("GET /v1/diagnostics/latest", api.auth(http.HandlerFunc(api.getLatestDiagnostic)))
	mux.Handle("GET /v1/diagnostics/{id}", api.auth(http.HandlerFunc(api.getDiagnostic)))
	mux.Handle("PATCH /v1/diagnostics/{id}", api.auth(http.HandlerFunc(api.patchDiagnostic)))
	mux.Handle("GET /v1/hairstyles", api.auth(http.HandlerFunc(api.listHairstyles)))

	mux.Handle("POST /v1/hair-previews", api.auth(api.requireIdempotency(http.HandlerFunc(api.createHairPreview))))
	mux.Handle("GET /v1/hair-previews", api.auth(http.HandlerFunc(api.listHairPreviews)))
	mux.Handle("GET /v1/hair-previews/active", api.auth(http.HandlerFunc(api.getActiveHairPreview)))
	mux.Handle("GET /v1/hair-previews/{id}", api.auth(http.HandlerFunc(api.getHairPreview)))
	mux.Handle("POST /v1/hair-previews/{id}/save", api.auth(http.HandlerFunc(api.saveHairPreview)))

	mux.Handle("GET /v1/today/context", api.auth(http.HandlerFunc(api.getTodayContext)))
	mux.Handle("GET /v1/today/plans/current", api.auth(http.HandlerFunc(api.getTodayPlan)))
	mux.Handle("POST /v1/today/plans", api.auth(api.requireIdempotency(http.HandlerFunc(api.createTodayPlan))))
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

	mux.Handle("POST /v1/body-presentations", api.auth(http.HandlerFunc(api.createBodyPresentation)))
	mux.Handle("GET /v1/body-presentations/status", api.auth(http.HandlerFunc(api.getBodyPresentationStatus)))
	mux.Handle("GET /v1/body-presentations/{id}", api.auth(http.HandlerFunc(api.getBodyPresentation)))

	mux.Handle("POST /v1/events", api.auth(http.HandlerFunc(api.trackProductEvent)))

	mux.Handle("GET /v1/billing/me", api.auth(http.HandlerFunc(api.getBillingMe)))
	mux.Handle("POST /v1/billing/orders", api.auth(http.HandlerFunc(api.createBillingOrder)))
	mux.Handle("POST /v1/billing/orders/{id}/sync", api.auth(http.HandlerFunc(api.syncBillingOrder)))
	mux.HandleFunc("POST /v1/billing/notify", api.billingNotify)
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
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID, Idempotency-Key")
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
		user, err := a.account.Authenticate(r.Context(), token)
		if err != nil {
			writeError(w, r, http.StatusUnauthorized, "unauthorized", "登录已失效，请重新进入应用")
			return
		}
		ctx := context.WithValue(r.Context(), userKey, user)
		ctx = context.WithValue(ctx, tokenKey, token)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
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
	if a.jobs != nil {
		if jobs, err := a.jobs.HealthJobs(r.Context()); err == nil {
			payload["jobs"] = jobs
		} else {
			payload["jobs"] = nil
		}
	} else {
		payload["jobs"] = nil
	}
	writeData(w, http.StatusOK, payload)
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
	case errors.Is(err, account.ErrRateLimited):
		writeError(w, r, http.StatusTooManyRequests, "rate_limited", strings.TrimPrefix(err.Error(), account.ErrRateLimited.Error()+": "))
	case errors.Is(err, billing.ErrInsufficientCredits):
		writeError(w, r, http.StatusPaymentRequired, "insufficient_credits", strings.TrimPrefix(err.Error(), billing.ErrInsufficientCredits.Error()+": "))
	case errors.Is(err, billing.ErrRateLimited):
		// 用量日限/时限/并发限与下单限流同一公开形状（旧线 service.ErrRateLimited）。
		writeError(w, r, http.StatusTooManyRequests, "rate_limited", strings.TrimPrefix(err.Error(), billing.ErrRateLimited.Error()+": "))
	case errors.Is(err, advisor.ErrRateLimited):
		writeError(w, r, http.StatusTooManyRequests, "rate_limited", strings.TrimPrefix(err.Error(), advisor.ErrRateLimited.Error()+": "))
	case errors.Is(err, advisor.ErrValidation):
		writeError(w, r, http.StatusBadRequest, "validation_error", strings.TrimPrefix(err.Error(), advisor.ErrValidation.Error()+": "))
	case errors.Is(err, wardrobe.ErrValidation):
		writeError(w, r, http.StatusBadRequest, "validation_error", strings.TrimPrefix(err.Error(), wardrobe.ErrValidation.Error()+": "))
	case errors.Is(err, billing.ErrPaymentUnavailable):
		writeError(w, r, http.StatusServiceUnavailable, "payment_unavailable", "购买暂未开通")
	case errors.Is(err, diagnostic.ErrPhotoRejected):
		// 照片门禁拒识：422 + 用户可读原因，客户端据此给「换一张」空态
		writeError(w, r, http.StatusUnprocessableEntity, "photo_rejected", strings.TrimPrefix(err.Error(), diagnostic.ErrPhotoRejected.Error()+": "))
	case errors.Is(err, account.ErrValidation):
		writeError(w, r, http.StatusBadRequest, "validation_error", strings.TrimPrefix(err.Error(), account.ErrValidation.Error()+": "))
	case errors.Is(err, media.ErrValidation):
		writeError(w, r, http.StatusBadRequest, "validation_error", strings.TrimPrefix(err.Error(), media.ErrValidation.Error()+": "))
	case errors.Is(err, operation.ErrValidation):
		writeError(w, r, http.StatusBadRequest, "validation_error", strings.TrimPrefix(err.Error(), operation.ErrValidation.Error()+": "))
	case errors.Is(err, media.ErrUploadMetadataMismatch):
		writeError(w, r, http.StatusBadRequest, "validation_error", "上传文件与申报信息不一致")
	case errors.Is(err, storage.ErrDirectUploadUnavailable):
		writeError(w, r, http.StatusServiceUnavailable, "service_unavailable", "当前存储不支持直传")
	case errors.Is(err, storage.ErrObjectNotFound):
		writeError(w, r, http.StatusBadRequest, "validation_error", "还没有收到完整上传")
	case errors.Is(err, body.ErrCapabilityUnavailable):
		writeError(w, r, http.StatusServiceUnavailable, "capability_unavailable",
			strings.TrimPrefix(err.Error(), body.ErrCapabilityUnavailable.Error()+": "))
	case errors.Is(err, body.ErrValidation):
		writeError(w, r, http.StatusBadRequest, "validation_error", strings.TrimPrefix(err.Error(), body.ErrValidation.Error()+": "))
	case errors.Is(err, repository.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "没有找到对应内容")
	case errors.Is(err, repository.ErrConflict):
		writeError(w, r, http.StatusConflict, "conflict", "上传意图已失效")
	case errors.Is(err, account.ErrForbidden):
		writeError(w, r, http.StatusForbidden, "forbidden", "没有访问权限")
	default:
		a.internalError(w, r, err)
	}
}
