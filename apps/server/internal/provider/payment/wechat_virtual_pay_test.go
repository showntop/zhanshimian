package provider

import (
	"context"
	"crypto/hmac"
	"strings"
	"testing"
)

func TestSignGoodsOrderUsesOfferAndSession(t *testing.T) {
	pay, err := NewWeChatVirtualPay(VirtualPayConfig{
		AppID: "wxapp", AppSecret: "secret", OfferID: "123456", AppKey: "app-key", Env: 0,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	params, err := pay.SignGoodsOrder(context.Background(), VirtualPayOrder{
		OutTradeNo: "order1", ProductID: "pack_3", PriceFen: 600, SessionKey: "session",
	})
	if err != nil {
		t.Fatal(err)
	}
	if params.Mode != "short_series_goods" || !strings.Contains(params.SignData, `"offerId":"123456"`) || params.PaySig == "" || params.Signature == "" {
		t.Fatalf("unexpected sign params: %#v", params)
	}
	if params.PaySig != hmacSHA256Hex("app-key", "requestVirtualPayment&"+params.SignData) {
		t.Fatal("paySig mismatch")
	}
	if params.Signature != hmacSHA256Hex("session", params.SignData) {
		t.Fatal("user signature mismatch")
	}
}

func TestParseDeliverNotifyAcceptsValidBody(t *testing.T) {
	pay, err := NewWeChatVirtualPay(VirtualPayConfig{
		AppID: "wxapp", AppSecret: "secret", OfferID: "123456", AppKey: "app-key",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"OutTradeNo":"abc","GoodsInfo":{"ProductId":"pack_3"},"WeChatPayInfo":{"TransactionId":"wx1"}}`)
	sig := hmacSHA256Hex("app-key", string(raw))
	notify, err := pay.ParseDeliverNotify(raw, sig)
	if err != nil || notify.OutTradeNo != "abc" || notify.ProductID != "pack_3" || notify.WxOrderID != "wx1" {
		t.Fatalf("notify=%#v err=%v", notify, err)
	}
	if _, err := pay.ParseDeliverNotify(raw, "deadbeef"); err == nil {
		t.Fatal("invalid signature should fail")
	}
	if _, err := pay.ParseDeliverNotify(raw, ""); err == nil {
		t.Fatal("empty signature should fail")
	}
	_ = hmac.Equal
}
