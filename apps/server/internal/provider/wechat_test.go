package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWeChatCodeExchangerSendsOfficialParameters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("appid") != "wx-test" || query.Get("secret") != "secret-test" || query.Get("js_code") != "login-code" || query.Get("grant_type") != "authorization_code" {
			t.Fatalf("unexpected query: %v", query)
		}
		_, _ = w.Write([]byte(`{"openid":"openid-1","unionid":"unionid-1"}`))
	}))
	defer server.Close()

	exchanger, err := NewWeChatCodeExchanger(WeChatConfig{AppID: "wx-test", AppSecret: "secret-test", BaseURL: server.URL, Timeout: time.Second}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	identity, err := exchanger.ExchangeCode(context.Background(), "login-code")
	if err != nil {
		t.Fatal(err)
	}
	if identity.OpenID != "openid-1" || identity.UnionID != "unionid-1" {
		t.Fatalf("unexpected identity: %#v", identity)
	}
}

func TestWeChatCodeExchangerClassifiesVendorErrors(t *testing.T) {
	for _, test := range []struct {
		payload string
		want    error
	}{
		{`{"errcode":40029,"errmsg":"invalid code"}`, ErrWeChatCodeRejected},
		{`{"errcode":45011,"errmsg":"frequency limit"}`, ErrWeChatRateLimited},
		{`{"errcode":40125,"errmsg":"invalid secret"}`, ErrWeChatUnavailable},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(test.payload)) }))
		exchanger, err := NewWeChatCodeExchanger(WeChatConfig{AppID: "wx-test", AppSecret: "secret-test", BaseURL: server.URL}, server.Client())
		if err != nil {
			server.Close()
			t.Fatal(err)
		}
		_, got := exchanger.ExchangeCode(context.Background(), "code")
		server.Close()
		if got != test.want {
			t.Fatalf("payload %s: got %v, want %v", test.payload, got, test.want)
		}
	}
}

func TestWeChatCodeExchangerRejectsEmptyCodeWithoutRequest(t *testing.T) {
	exchanger, err := NewWeChatCodeExchanger(WeChatConfig{AppID: "wx-test", AppSecret: "secret-test"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exchanger.ExchangeCode(context.Background(), ""); err != ErrWeChatCodeRejected {
		t.Fatalf("got %v, want rejected", err)
	}
}
