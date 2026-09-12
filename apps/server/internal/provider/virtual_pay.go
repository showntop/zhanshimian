package provider

import "context"

type VirtualPayOrder struct {
	OutTradeNo string
	ProductID  string
	PriceFen   int
	SessionKey string
}

type VirtualPayParams struct {
	SignData  string
	PaySig    string
	Signature string
	Mode      string
}

type VirtualPayNotify struct {
	OutTradeNo string
	ProductID  string
	WxOrderID  string
}

type VirtualPayer interface {
	Enabled() bool
	SignGoodsOrder(context.Context, VirtualPayOrder) (VirtualPayParams, error)
	QueryOrder(ctx context.Context, outTradeNo string) (paid bool, wxOrderID string, err error)
	ParseDeliverNotify(raw []byte, signature string) (VirtualPayNotify, error)
}

type WeChatSession struct {
	OpenID     string
	SessionKey string
}

type WeChatSessionExchanger interface {
	ExchangeSession(context.Context, string) (WeChatSession, error)
}
