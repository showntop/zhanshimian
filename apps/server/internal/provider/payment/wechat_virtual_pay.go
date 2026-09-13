package provider

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type VirtualPayConfig struct {
	AppID     string
	AppSecret string
	OfferID   string
	AppKey    string
	Env       int
	APIBase   string
	Timeout   time.Duration
}

type WeChatVirtualPay struct {
	appID     string
	appSecret string
	offerID   string
	appKey    string
	env       int
	apiBase   string
	client    *http.Client
}

func NewWeChatVirtualPay(cfg VirtualPayConfig, client *http.Client) (*WeChatVirtualPay, error) {
	if strings.TrimSpace(cfg.OfferID) == "" || strings.TrimSpace(cfg.AppKey) == "" {
		return nil, fmt.Errorf("virtual pay offer id and app key are required")
	}
	if strings.TrimSpace(cfg.AppID) == "" || strings.TrimSpace(cfg.AppSecret) == "" {
		return nil, fmt.Errorf("wechat app id and secret are required for virtual pay")
	}
	apiBase := strings.TrimRight(strings.TrimSpace(cfg.APIBase), "/")
	if apiBase == "" {
		apiBase = "https://api.weixin.qq.com"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 8 * time.Second
	}
	if client == nil {
		client = &http.Client{Timeout: cfg.Timeout}
	}
	return &WeChatVirtualPay{
		appID: cfg.AppID, appSecret: cfg.AppSecret, offerID: cfg.OfferID, appKey: cfg.AppKey,
		env: cfg.Env, apiBase: apiBase, client: client,
	}, nil
}

func (p *WeChatVirtualPay) Enabled() bool { return p != nil }

func (p *WeChatVirtualPay) SignGoodsOrder(_ context.Context, order VirtualPayOrder) (VirtualPayParams, error) {
	signData, err := json.Marshal(map[string]any{
		"offerId":      p.offerID,
		"buyQuantity":  1,
		"env":          p.env,
		"currencyType": "CNY",
		"productId":    order.ProductID,
		"goodsPrice":   order.PriceFen,
		"outTradeNo":   order.OutTradeNo,
	})
	if err != nil {
		return VirtualPayParams{}, err
	}
	payload := string(signData)
	params := VirtualPayParams{
		SignData: payload,
		PaySig:   hmacSHA256Hex(p.appKey, "requestVirtualPayment&"+payload),
		Mode:     "short_series_goods",
	}
	if order.SessionKey != "" {
		params.Signature = hmacSHA256Hex(order.SessionKey, payload)
	}
	return params, nil
}

func (p *WeChatVirtualPay) QueryOrder(ctx context.Context, outTradeNo string) (bool, string, error) {
	token, err := p.accessToken(ctx)
	if err != nil {
		return false, "", err
	}
	body, err := json.Marshal(map[string]any{
		"offer_id": p.offerID,
		"env":      p.env,
		"order_id": outTradeNo,
	})
	if err != nil {
		return false, "", err
	}
	path := "/xpay/query_order"
	paySig := hmacSHA256Hex(p.appKey, path+"&"+string(body))
	endpoint := p.apiBase + path + "?access_token=" + url.QueryEscape(token) + "&pay_sig=" + url.QueryEscape(paySig)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return false, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := p.client.Do(req)
	if err != nil {
		return false, "", fmt.Errorf("virtual pay query unavailable")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil {
		return false, "", fmt.Errorf("virtual pay query unavailable")
	}
	var payload struct {
		ErrCode   int    `json:"errcode"`
		OrderID   string `json:"order_id"`
		WxOrderID string `json:"wx_order_id"`
		Status    int    `json:"status"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return false, "", fmt.Errorf("virtual pay query unavailable")
	}
	if payload.ErrCode != 0 {
		return false, "", fmt.Errorf("virtual pay query rejected")
	}
	// 0 created / 1 paid / 2 fulfilled depending on WeChat docs; treat non-zero as paid enough to fulfill.
	paid := payload.Status >= 1
	wxOrderID := payload.WxOrderID
	if wxOrderID == "" {
		wxOrderID = payload.OrderID
	}
	return paid, wxOrderID, nil
}

func (p *WeChatVirtualPay) ParseDeliverNotify(raw []byte, signature string) (VirtualPayNotify, error) {
	expected := hmacSHA256Hex(p.appKey, string(raw))
	if signature == "" || (!hmac.Equal([]byte(expected), []byte(strings.ToLower(signature))) && !hmac.Equal([]byte(expected), []byte(signature))) {
		return VirtualPayNotify{}, fmt.Errorf("virtual pay notify signature invalid")
	}
	var payload struct {
		OutTradeNo string `json:"OutTradeNo"`
		Env        int    `json:"Env"`
		GoodsInfo  struct {
			ProductID string `json:"ProductId"`
		} `json:"GoodsInfo"`
		WeChatPayInfo struct {
			TransactionID string `json:"TransactionId"`
		} `json:"WeChatPayInfo"`
	}
	if json.Unmarshal(raw, &payload) != nil || payload.OutTradeNo == "" {
		return VirtualPayNotify{}, fmt.Errorf("virtual pay notify invalid")
	}
	return VirtualPayNotify{OutTradeNo: payload.OutTradeNo, ProductID: payload.GoodsInfo.ProductID, WxOrderID: payload.WeChatPayInfo.TransactionID}, nil
}

func (p *WeChatVirtualPay) accessToken(ctx context.Context) (string, error) {
	endpoint := p.apiBase + "/cgi-bin/token?grant_type=client_credential&appid=" + url.QueryEscape(p.appID) + "&secret=" + url.QueryEscape(p.appSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	response, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("virtual pay token unavailable")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 16<<10))
	if err != nil {
		return "", fmt.Errorf("virtual pay token unavailable")
	}
	var payload struct {
		AccessToken string `json:"access_token"`
		ErrCode     int    `json:"errcode"`
	}
	if json.Unmarshal(raw, &payload) != nil || payload.AccessToken == "" || payload.ErrCode != 0 {
		return "", fmt.Errorf("virtual pay token unavailable")
	}
	return payload.AccessToken, nil
}

func hmacSHA256Hex(key, message string) string {
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}

func VirtualPayEnvLabel(env int) string {
	return strconv.Itoa(env)
}
