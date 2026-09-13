package provider

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	// ErrSmsConfig marks a provider that cannot be constructed or called
	// because deployment secrets are missing.
	ErrSmsConfig = errors.New("sms provider configuration error")
	// ErrSmsRejected marks a send refused by the provider for a business
	// reason (content/signature/rate rules) — retrying with the same request
	// will not help.
	ErrSmsRejected = errors.New("sms send rejected by provider")
	// ErrSmsUnavailable marks transient provider/network failures.
	ErrSmsUnavailable = errors.New("sms provider unavailable")
)

// SmsSender isolates the outside world of verification-code delivery.
type SmsSender interface {
	Send(ctx context.Context, phone, code string) error
	Name() string
	// ReturnsCode reports whether the provider surfaces the code to the
	// caller. Only the development console transport ever does; production
	// providers must return false so dev_code never leaks into responses.
	ReturnsCode() bool
}

// ConsoleSms logs the code instead of sending anything. In development the
// fixed code 888888 is used and returned to the caller so e2e and local
// debugging work without an SMS account; production must never use it.
type ConsoleSms struct {
	logger  *slog.Logger
	devMode bool
}

func NewConsoleSms(logger *slog.Logger, devMode bool) *ConsoleSms {
	if logger == nil {
		logger = slog.Default()
	}
	return &ConsoleSms{logger: logger, devMode: devMode}
}

// DevSmsCode is the fixed verification code in development.
const DevSmsCode = "888888"

func (c *ConsoleSms) Send(_ context.Context, phone, code string) error {
	c.logger.Info("sms code (console provider)", "phone", phone, "code", code)
	return nil
}

func (c *ConsoleSms) Name() string    { return "console" }
func (c *ConsoleSms) ReturnsCode() bool { return c.devMode }

// AliyunSmsConfig carries everything the Aliyun SendSms RPC needs. Keys are
// read from the environment by the config layer, never stored in routing
// files.
type AliyunSmsConfig struct {
	AccessKeyID     string
	AccessKeySecret string
	SignName        string
	TemplateCode    string
	RegionID        string
	Endpoint        string
	Timeout         time.Duration
}

// AliyunSms signs requests the way Aliyun RPC APIs expect (HMAC-SHA1 over the
// RFC3986-percent-encoded, sorted query string) and maps provider error codes
// to the three transport errors above.
type AliyunSms struct {
	cfg    AliyunSmsConfig
	client *http.Client
}

func NewAliyunSms(cfg AliyunSmsConfig, client *http.Client) (*AliyunSms, error) {
	if strings.TrimSpace(cfg.AccessKeyID) == "" || strings.TrimSpace(cfg.AccessKeySecret) == "" ||
		strings.TrimSpace(cfg.SignName) == "" || strings.TrimSpace(cfg.TemplateCode) == "" {
		return nil, fmt.Errorf("%w: ALIYUN_SMS keys, sign and template code are required", ErrSmsConfig)
	}
	if strings.TrimSpace(cfg.RegionID) == "" {
		cfg.RegionID = "cn-hangzhou"
	}
	if strings.TrimSpace(cfg.Endpoint) == "" {
		cfg.Endpoint = "https://dysmsapi.aliyuncs.com"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	if client == nil {
		client = &http.Client{Timeout: cfg.Timeout}
	}
	return &AliyunSms{cfg: cfg, client: client}, nil
}

func (a *AliyunSms) Name() string      { return "aliyun" }
func (a *AliyunSms) ReturnsCode() bool { return false }

func (a *AliyunSms) Send(ctx context.Context, phone, code string) error {
	params := map[string]string{
		"AccessKeyId":      a.cfg.AccessKeyID,
		"Action":           "SendSms",
		"Format":           "JSON",
		"PhoneNumbers":     phone,
		"RegionId":         a.cfg.RegionID,
		"SignatureMethod":  "HMAC-SHA1",
		"SignatureNonce":   uuid.NewString(),
		"SignatureVersion": "1.0",
		"SignName":         a.cfg.SignName,
		"TemplateCode":     a.cfg.TemplateCode,
		"TemplateParam":    `{"code":"` + code + `"}`,
		"Timestamp":        time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		"Version":          "2017-05-25",
	}
	endpoint := a.cfg.Endpoint + "/?Signature=" + url.QueryEscape(aliyunSign(a.cfg.AccessKeySecret, params)) + "&" + encodeAliyunQuery(params)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ErrSmsUnavailable
	}
	response, err := a.client.Do(request)
	if err != nil {
		return ErrSmsUnavailable
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 8<<10))
	if response.StatusCode != http.StatusOK {
		return ErrSmsUnavailable
	}
	var payload struct {
		Code    string `json:"Code"`
		Message string `json:"Message"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return ErrSmsUnavailable
	}
	if payload.Code == "" || payload.Code == "OK" {
		return nil
	}
	// Business rule violations (frequency caps, signature/template mismatch,
	// blacklisted content) are permanent for this request.
	switch {
	case strings.Contains(payload.Code, "LIMIT_CONTROL"), strings.Contains(payload.Code, "SIGNATURE"),
		strings.Contains(payload.Code, "TEMPLATE"), strings.Contains(payload.Code, "PARAM"):
		return fmt.Errorf("%w: aliyun %s: %s", ErrSmsRejected, payload.Code, payload.Message)
	default:
		return fmt.Errorf("%w: aliyun %s: %s", ErrSmsUnavailable, payload.Code, payload.Message)
	}
}

// encodeAliyunQuery serializes params exactly as the signature input expects:
// key-sorted, RFC3986 percent-encoded, joined with &.
func encodeAliyunQuery(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, rfc3986Encode(key)+"="+rfc3986Encode(params[key]))
	}
	return strings.Join(pairs, "&")
}

func rfc3986Encode(value string) string {
	encoded := url.QueryEscape(value)
	// QueryEscape turns space into "+" and leaves "~" alone is fine; RFC3986
	// wants %20 and treats "~" as unreserved, which matches.
	return strings.ReplaceAll(encoded, "+", "%20")
}

func aliyunSign(secret string, params map[string]string) string {
	stringToSign := "GET&" + rfc3986Encode("/") + "&" + rfc3986Encode(encodeAliyunQuery(params))
	mac := hmac.New(sha1.New, []byte(secret+"&"))
	mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
