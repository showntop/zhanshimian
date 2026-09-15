package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/zhanshimian/server/internal/domain"
)

const (
	maxIdempotencyBodyBytes = 1 << 20
	idempotencyTTL          = 24 * time.Hour
)

type IdempotencyStore interface {
	BeginIdempotency(context.Context, domain.BeginIdempotency) (domain.IdempotencyRecord, domain.IdempotencyBeginOutcome, error)
	CompleteIdempotency(context.Context, string, string, int, json.RawMessage) error
	AbortIdempotency(context.Context, string, string) error
	InvalidateIdempotency(context.Context, string, string) error
}

func (a *API) requireIdempotency(next http.Handler) http.Handler {
	return a.requireIdempotencyWithReplayGuard(next, nil)
}

// replayFresh 非空时在重放前校验存储的响应是否仍然有效（如预签名上传 URL 未过期）；
// 返回 false 则作废旧记录、按新请求重新执行——重放一张过期 URL 只会让客户端必然失败。
func (a *API) requireIdempotencyWithReplayGuard(next http.Handler, replayFresh func(status int, body []byte) bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.idempotency == nil {
			a.internalError(w, r, errIdempotencyUnavailable)
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if !validIdempotencyKey(key) {
			writeError(w, r, http.StatusBadRequest, "idempotency_key_required", "请提供有效的幂等键")
			return
		}
		body, err := readIdempotencyBody(r)
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
			return
		}
		fingerprint := requestFingerprint(r.Method, r.URL.EscapedPath(), canonicalJSON(body))
		userID := currentUser(r).ID
		begin := domain.BeginIdempotency{
			UserID:             userID,
			Key:                key,
			RequestFingerprint: fingerprint,
			Scope:              r.Method + " " + r.URL.EscapedPath(),
			ExpiresAt:          time.Now().Add(idempotencyTTL),
		}
		for {
			record, outcome, err := a.idempotency.BeginIdempotency(r.Context(), begin)
			if err != nil {
				a.internalError(w, r, err)
				return
			}
			switch outcome {
			case domain.IdempotencyBeginReplay:
				if replayFresh != nil && !replayFresh(record.ResponseStatus, record.ResponseBody) {
					if err := a.idempotency.InvalidateIdempotency(r.Context(), userID, key); err != nil {
						a.internalError(w, r, err)
						return
					}
					continue
				}
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(record.ResponseStatus)
				_, _ = w.Write(record.ResponseBody)
				return
			case domain.IdempotencyBeginInProgress:
				writeError(w, r, http.StatusConflict, "idempotency_in_progress", "相同请求仍在处理中，请稍后重试")
				return
			case domain.IdempotencyBeginConflict:
				writeError(w, r, http.StatusConflict, "idempotency_conflict", "相同幂等键已被用于不同请求")
				return
			case domain.IdempotencyBeginStarted:
			default:
				a.internalError(w, r, errUnexpectedIdempotencyOutcome)
				return
			}
			break
		}

		captured := &captureResponseWriter{ResponseWriter: w, status: http.StatusOK}
		persistCtx := context.WithoutCancel(r.Context())
		shouldAbort := true
		defer func() {
			recovered := recover()
			produced2xx := captured.wroteHeader && captured.status >= 200 && captured.status < 300
			if recovered != nil {
				if !produced2xx {
					_ = a.idempotency.AbortIdempotency(persistCtx, userID, key)
				}
				panic(recovered)
			}
			if shouldAbort {
				_ = a.idempotency.AbortIdempotency(persistCtx, userID, key)
			}
		}()
		next.ServeHTTP(captured, r)
		if captured.status < 200 || captured.status >= 300 {
			return
		}
		shouldAbort = false
		_ = a.idempotency.CompleteIdempotency(persistCtx, userID, key, captured.status, captured.body.Bytes())
	})
}

func validIdempotencyKey(key string) bool {
	if key == "" || len(key) > 128 {
		return false
	}
	for i := 0; i < len(key); i++ {
		if key[i] < 0x21 || key[i] > 0x7e {
			return false
		}
	}
	return true
}

func readIdempotencyBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, maxIdempotencyBodyBytes+1))
	if err != nil {
		return nil, errors.New("请求内容无法读取")
	}
	if len(body) > maxIdempotencyBodyBytes {
		return nil, errors.New("请求内容过大")
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

func requestFingerprint(method, escapedPath string, canonicalBody []byte) string {
	sum := sha256.Sum256([]byte(method + "\n" + escapedPath + "\n" + string(canonicalBody)))
	return hex.EncodeToString(sum[:])
}

func canonicalJSON(raw []byte) []byte {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return trimmed
	}
	out, err := marshalCanonical(value)
	if err != nil {
		return trimmed
	}
	return out
}

func marshalCanonical(value any) ([]byte, error) {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		var buf bytes.Buffer
		buf.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			name, err := json.Marshal(key)
			if err != nil {
				return nil, err
			}
			buf.Write(name)
			buf.WriteByte(':')
			item, err := marshalCanonical(typed[key])
			if err != nil {
				return nil, err
			}
			buf.Write(item)
		}
		buf.WriteByte('}')
		return buf.Bytes(), nil
	case []any:
		var buf bytes.Buffer
		buf.WriteByte('[')
		for i, item := range typed {
			if i > 0 {
				buf.WriteByte(',')
			}
			encoded, err := marshalCanonical(item)
			if err != nil {
				return nil, err
			}
			buf.Write(encoded)
		}
		buf.WriteByte(']')
		return buf.Bytes(), nil
	default:
		return json.Marshal(typed)
	}
}

type captureResponseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	body        bytes.Buffer
}

func (w *captureResponseWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.status = code
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *captureResponseWriter) Write(p []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	_, _ = w.body.Write(p)
	return w.ResponseWriter.Write(p)
}

var errIdempotencyUnavailable = errors.New("idempotency store unavailable")
var errUnexpectedIdempotencyOutcome = errors.New("unexpected idempotency outcome")
