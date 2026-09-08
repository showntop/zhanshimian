package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	ErrWeChatCodeRejected = errors.New("wechat login code rejected")
	ErrWeChatRateLimited  = errors.New("wechat login rate limited")
	ErrWeChatUnavailable  = errors.New("wechat login unavailable")
)

type WeChatIdentity struct {
	OpenID  string
	UnionID string
}

type WeChatAuthenticator interface {
	ExchangeCode(context.Context, string) (WeChatIdentity, error)
}

type WeChatConfig struct {
	AppID     string
	AppSecret string
	BaseURL   string
	Timeout   time.Duration
}

type WeChatCodeExchanger struct {
	appID     string
	appSecret string
	baseURL   string
	client    *http.Client
}

func NewWeChatCodeExchanger(cfg WeChatConfig, client *http.Client) (*WeChatCodeExchanger, error) {
	if strings.TrimSpace(cfg.AppID) == "" || strings.TrimSpace(cfg.AppSecret) == "" {
		return nil, fmt.Errorf("wechat app id and secret are required")
	}
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = "https://api.weixin.qq.com/sns/jscode2session"
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid wechat api url")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	if client == nil {
		client = &http.Client{Timeout: cfg.Timeout}
	}
	return &WeChatCodeExchanger{
		appID: strings.TrimSpace(cfg.AppID), appSecret: strings.TrimSpace(cfg.AppSecret),
		baseURL: baseURL, client: client,
	}, nil
}

func (w *WeChatCodeExchanger) ExchangeCode(ctx context.Context, code string) (WeChatIdentity, error) {
	code = strings.TrimSpace(code)
	if code == "" || len(code) > 256 {
		return WeChatIdentity{}, ErrWeChatCodeRejected
	}
	endpoint, err := url.Parse(w.baseURL)
	if err != nil {
		return WeChatIdentity{}, ErrWeChatUnavailable
	}
	query := endpoint.Query()
	query.Set("appid", w.appID)
	query.Set("secret", w.appSecret)
	query.Set("js_code", code)
	query.Set("grant_type", "authorization_code")
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return WeChatIdentity{}, ErrWeChatUnavailable
	}
	response, err := w.client.Do(req)
	if err != nil {
		// Do not wrap url.Error: it contains the request URL, including AppSecret.
		return WeChatIdentity{}, ErrWeChatUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
		return WeChatIdentity{}, ErrWeChatUnavailable
	}
	var payload struct {
		OpenID  string `json:"openid"`
		UnionID string `json:"unionid"`
		ErrCode int    `json:"errcode"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 64<<10))
	if err := decoder.Decode(&payload); err != nil {
		return WeChatIdentity{}, ErrWeChatUnavailable
	}
	switch payload.ErrCode {
	case 0:
		if strings.TrimSpace(payload.OpenID) == "" {
			return WeChatIdentity{}, ErrWeChatUnavailable
		}
		return WeChatIdentity{OpenID: payload.OpenID, UnionID: payload.UnionID}, nil
	case 40029, 40163:
		return WeChatIdentity{}, ErrWeChatCodeRejected
	case 45011, 40226:
		return WeChatIdentity{}, ErrWeChatRateLimited
	default:
		return WeChatIdentity{}, ErrWeChatUnavailable
	}
}

// WeChatAppExchanger handles the open-platform OAuth code flow used by the
// native apps (sns/oauth2/access_token). It implements the same
// WeChatAuthenticator surface as the mini-program exchanger; the service
// layer picks the instance per endpoint.
type WeChatAppExchanger struct {
	appID     string
	appSecret string
	baseURL   string
	client    *http.Client
}

func NewWeChatAppExchanger(cfg WeChatConfig, client *http.Client) (*WeChatAppExchanger, error) {
	if strings.TrimSpace(cfg.AppID) == "" || strings.TrimSpace(cfg.AppSecret) == "" {
		return nil, fmt.Errorf("wechat open platform app id and secret are required")
	}
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = "https://api.weixin.qq.com/sns/oauth2/access_token"
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid wechat api url")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	if client == nil {
		client = &http.Client{Timeout: cfg.Timeout}
	}
	return &WeChatAppExchanger{
		appID: strings.TrimSpace(cfg.AppID), appSecret: strings.TrimSpace(cfg.AppSecret),
		baseURL: baseURL, client: client,
	}, nil
}

// ExchangeCode trades an open-platform authorization code for the app-scope
// identity. The unionid is preferred as identifier when the app is bound to
// an open platform account, because it stays stable across that vendor's apps.
func (w *WeChatAppExchanger) ExchangeCode(ctx context.Context, code string) (WeChatIdentity, error) {
	code = strings.TrimSpace(code)
	if code == "" || len(code) > 256 {
		return WeChatIdentity{}, ErrWeChatCodeRejected
	}
	endpoint, err := url.Parse(w.baseURL)
	if err != nil {
		return WeChatIdentity{}, ErrWeChatUnavailable
	}
	query := endpoint.Query()
	query.Set("appid", w.appID)
	query.Set("secret", w.appSecret)
	query.Set("code", code)
	query.Set("grant_type", "authorization_code")
	endpoint.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return WeChatIdentity{}, ErrWeChatUnavailable
	}
	response, err := w.client.Do(request)
	if err != nil {
		// Do not wrap url.Error: it contains the request URL, including AppSecret.
		return WeChatIdentity{}, ErrWeChatUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
		return WeChatIdentity{}, ErrWeChatUnavailable
	}
	var payload struct {
		OpenID  string `json:"openid"`
		UnionID string `json:"unionid"`
		ErrCode int    `json:"errcode"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&payload) != nil {
		return WeChatIdentity{}, ErrWeChatUnavailable
	}
	identifier := payload.UnionID
	if identifier == "" {
		identifier = payload.OpenID
	}
	if payload.ErrCode == 0 && identifier == "" {
		return WeChatIdentity{}, ErrWeChatUnavailable
	}
	switch payload.ErrCode {
	case 0:
		return WeChatIdentity{OpenID: identifier, UnionID: payload.UnionID}, nil
	case 40029, 40163:
		return WeChatIdentity{}, ErrWeChatCodeRejected
	case 45011, 40226:
		return WeChatIdentity{}, ErrWeChatRateLimited
	default:
		return WeChatIdentity{}, ErrWeChatUnavailable
	}
}
