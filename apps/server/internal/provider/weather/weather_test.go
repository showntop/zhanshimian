package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDemoWeatherProviderUsesDefaultAndRequestedCity(t *testing.T) {
	provider := NewDemoWeatherProvider()
	defaultWeather, err := provider.Current(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if defaultWeather.City != "上海" || defaultWeather.Condition == "" {
		t.Fatalf("unexpected default weather: %#v", defaultWeather)
	}
	requested, err := provider.Current(context.Background(), "杭州")
	if err != nil {
		t.Fatal(err)
	}
	if requested.City != "杭州" {
		t.Fatalf("requested city lost: %#v", requested)
	}
}

func TestAMapWeatherProviderReturnsCurrentWeather(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("key") != "weather-key" || query.Get("city") != "杭州" || query.Get("extensions") != "base" {
			t.Fatalf("unexpected query: %v", query)
		}
		_, _ = w.Write([]byte(`{"status":"1","infocode":"10000","lives":[{"city":"杭州市","weather":"多云","temperature":"25.6"}]}`))
	}))
	defer server.Close()
	provider, err := NewAMapWeatherProvider(AMapWeatherConfig{APIKey: "weather-key", BaseURL: server.URL, DefaultCity: "上海", DefaultCode: "310000"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	weather, err := provider.Current(context.Background(), "杭州")
	if err != nil {
		t.Fatal(err)
	}
	if weather.City != "杭州市" || weather.Condition != "多云" || weather.Temperature != 26 {
		t.Fatalf("unexpected weather: %#v", weather)
	}
}

func TestAMapWeatherProviderFallsBackToDefaultCity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("city"); got != "310000" {
			t.Fatalf("unexpected city query: %v", got)
		}
		_, _ = w.Write([]byte(`{"status":"1","infocode":"10000","lives":[{"city":"上海市","weather":"晴","temperature":"30"}]}`))
	}))
	defer server.Close()
	provider, err := NewAMapWeatherProvider(AMapWeatherConfig{APIKey: "weather-key", BaseURL: server.URL, DefaultCity: "上海", DefaultCode: "310000"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{"", "这是一个超出长度限制因而不会被透传的城市名称输入"} {
		if _, err := provider.Current(context.Background(), input); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAMapWeatherProviderRejectsVendorFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"0","infocode":"10001","info":"INVALID_USER_KEY"}`))
	}))
	defer server.Close()
	provider, err := NewAMapWeatherProvider(AMapWeatherConfig{APIKey: "weather-key", BaseURL: server.URL, DefaultCity: "上海", DefaultCode: "310000"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Current(context.Background(), ""); err != ErrWeatherUnavailable {
		t.Fatalf("got %v, want unavailable", err)
	}
}
