package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var ErrWeatherUnavailable = errors.New("weather provider unavailable")

// Weather is the provider-neutral snapshot consumed by product features.
// Keeping it small lets a real weather vendor replace the demo provider
// without leaking vendor response shapes into the service or database.
type Weather struct {
	City        string
	Condition   string
	Temperature int
}

type WeatherProvider interface {
	Current(context.Context, string) (Weather, error)
}

type DemoWeatherProvider struct{}

func NewDemoWeatherProvider() *DemoWeatherProvider { return &DemoWeatherProvider{} }

func (d *DemoWeatherProvider) Current(_ context.Context, city string) (Weather, error) {
	city = strings.TrimSpace(city)
	if city == "" {
		city = "上海"
	}
	return Weather{City: city, Condition: "多云", Temperature: 26}, nil
}

type AMapWeatherConfig struct {
	APIKey      string
	BaseURL     string
	DefaultCity string
	DefaultCode string
	Timeout     time.Duration
}

type AMapWeatherProvider struct {
	apiKey      string
	baseURL     string
	defaultCity string
	defaultCode string
	client      *http.Client
}

func NewAMapWeatherProvider(cfg AMapWeatherConfig, client *http.Client) (*AMapWeatherProvider, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("amap web service key is required")
	}
	if strings.TrimSpace(cfg.DefaultCity) == "" || strings.TrimSpace(cfg.DefaultCode) == "" {
		return nil, fmt.Errorf("amap default city and adcode are required")
	}
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = "https://restapi.amap.com/v3/weather/weatherInfo"
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid amap api url")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	if client == nil {
		client = &http.Client{Timeout: cfg.Timeout}
	}
	return &AMapWeatherProvider{
		apiKey: strings.TrimSpace(cfg.APIKey), baseURL: baseURL,
		defaultCity: strings.TrimSpace(cfg.DefaultCity), defaultCode: strings.TrimSpace(cfg.DefaultCode), client: client,
	}, nil
}

func (a *AMapWeatherProvider) Current(ctx context.Context, city string) (Weather, error) {
	requestedCity := strings.TrimSpace(city)
	cityCode := a.defaultCode
	if len(requestedCity) == 6 {
		if _, err := strconv.Atoi(requestedCity); err == nil {
			cityCode = requestedCity
		}
	}
	endpoint, err := url.Parse(a.baseURL)
	if err != nil {
		return Weather{}, ErrWeatherUnavailable
	}
	query := endpoint.Query()
	query.Set("key", a.apiKey)
	query.Set("city", cityCode)
	query.Set("extensions", "base")
	query.Set("output", "JSON")
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return Weather{}, ErrWeatherUnavailable
	}
	response, err := a.client.Do(req)
	if err != nil {
		// Do not wrap url.Error: the URL contains the AMap API key.
		return Weather{}, ErrWeatherUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
		return Weather{}, ErrWeatherUnavailable
	}
	var payload struct {
		Status   string `json:"status"`
		InfoCode string `json:"infocode"`
		Lives    []struct {
			City        string `json:"city"`
			Weather     string `json:"weather"`
			Temperature string `json:"temperature"`
		} `json:"lives"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 128<<10)).Decode(&payload); err != nil || payload.Status != "1" || payload.InfoCode != "10000" || len(payload.Lives) == 0 {
		return Weather{}, ErrWeatherUnavailable
	}
	temperature, err := strconv.ParseFloat(payload.Lives[0].Temperature, 64)
	if err != nil {
		return Weather{}, ErrWeatherUnavailable
	}
	resolvedCity := strings.TrimSpace(payload.Lives[0].City)
	if resolvedCity == "" {
		resolvedCity = a.defaultCity
	}
	return Weather{City: resolvedCity, Condition: payload.Lives[0].Weather, Temperature: int(temperature + 0.5)}, nil
}
