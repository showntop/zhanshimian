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
	"github.com/zhanshimian/server/internal/service"
	"github.com/zhanshimian/server/internal/service/assessment"
	"github.com/zhanshimian/server/internal/service/home"
	"github.com/zhanshimian/server/internal/service/media"
	"github.com/zhanshimian/server/internal/service/operation"
)

type API struct {
	service         *service.Service
	media           *media.Service
	operations      *operation.Service
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
	idempotency     IdempotencyStore
	logger          *slog.Logger
	devLoginEnabled bool
	runtime         RuntimeInfo
}

// HomeService 是首页聚合的最小依赖：只读模型一次调用。
type HomeService interface {
	Bootstrap(context.Context, string) (home.Snapshot, error)
}

// Dependencies 承载质量核心服务的窄依赖。legacy *service.Service 仍由 New 的
// 旧参数传入（认证/账单/媒体等），这里只补 quality-core 五件套；旧路由删光后
// 再彻底移除 legacy 参数。
type Dependencies struct {
	Assessment *assessment.Service
	Planning   planSetService
	Renders    renderService
	Execution  executionService
	Feedback   feedbackService
	Today      TodayService
	Wardrobe   WardrobeService
	Advisor    AdvisorService
	Diagnostic DiagnosticService
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
