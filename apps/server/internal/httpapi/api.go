package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/service/assessment"
	"github.com/zhanshimian/server/internal/service/body"
	"github.com/zhanshimian/server/internal/service/home"
	"github.com/zhanshimian/server/internal/service/media"
	"github.com/zhanshimian/server/internal/service/operation"
)

type API struct {
	media           *media.Service
	operations      *operation.Service
	body            *body.Service
	assessment      *assessment.Service
	home            HomeService
	planning        planSetService
	renders         renderService
	execution       executionService
	feedback        feedbackService
	today           TodayService
	wardrobe        WardrobeService
	advisor         AdvisorService
	diagnostic      DiagnosticService
	share           ShareService
	hair            HairService
	account         AccountService
	billing         BillingService
	events          EventWriter
	jobs            JobsReader
	demo            DemoMediaCreator
	deleteObject    func(key string) error
	idempotency     IdempotencyStore
	logger          *slog.Logger
	devLoginEnabled bool
	runtime         RuntimeInfo
}

// HomeService 是首页聚合的最小依赖：一次调用返回契约 HomeBootstrap 投影。
type HomeService interface {
	Bootstrap(context.Context, string) (home.Bootstrap, error)
}

// Dependencies 承载全部 HTTP 依赖；New 只做路由注册与协议翻译。
type Dependencies struct {
	Media        *media.Service
	Operations   *operation.Service
	Body         *body.Service
	Home         HomeService
	Idempotency  IdempotencyStore
	DeleteObject func(key string) error

	Assessment *assessment.Service
	Planning   planSetService
	Renders    renderService
	Execution  executionService
	Feedback   feedbackService
	Today      TodayService
	Wardrobe   WardrobeService
	Advisor    AdvisorService
	Diagnostic DiagnosticService
	Share      ShareService
	Hair       HairService
	Account    AccountService
	Billing    BillingService
	Events     EventWriter
	Jobs       JobsReader
	Demo       DemoMediaCreator
}

type RuntimeInfo struct {
	Environment           string
	StorageProvider       string
	WeatherProvider       string
	WeChatLoginConfigured bool
	WeChatAppConfigured   bool
	AppleLoginConfigured  bool
	SmsProvider           string
	AIRoutes              map[string]string
}

var errMediaUnavailable = errors.New("media service unavailable")
var errOperationUnavailable = errors.New("operation service unavailable")
var errHomeUnavailable = errors.New("home service unavailable")

type contextKey string

const userKey contextKey = "user"
const tokenKey contextKey = "token"

func currentUser(r *http.Request) domain.User { return r.Context().Value(userKey).(domain.User) }

func currentToken(r *http.Request) string {
	token, _ := r.Context().Value(tokenKey).(string)
	return token
}

func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("请求内容格式不正确")
	}
	return nil
}

func (a *API) internalError(w http.ResponseWriter, r *http.Request, err error) {
	if a.logger != nil {
		a.logger.Error("request failed", "path", r.URL.Path, "error", err)
	}
	writeError(w, r, http.StatusInternalServerError, "internal_error", "服务暂时不可用，请稍后重试")
}

func writeData(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": value})
}

func writeDataOperation(w http.ResponseWriter, status int, value any, op operationDTO) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": value, "operation": op})
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
		"code": code, "message": message, "request_id": r.Header.Get("X-Request-ID"), "retryable": errorRetryable(code, status),
	}})
}

func errorRetryable(code string, status int) bool {
	switch code {
	case "idempotency_in_progress":
		return true
	case "idempotency_conflict", "idempotency_key_required":
		return false
	case "version_conflict":
		// 只有 CAS 版本冲突是"重读后重试即可成功"的冲突。
		return true
	}
	return status >= 500 || status == http.StatusTooManyRequests
}
