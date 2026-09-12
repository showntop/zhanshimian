package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Environment           string
	Addr                  string
	DatabaseURL           string
	PublicBaseURL         string
	UploadDir             string
	AssetDir              string
	StorageProvider       string
	AssetURLTTL           time.Duration
	COSBucketURL          string
	COSRegion             string
	COSEndpoint           string
	COSSecretID           string
	COSSecretKey          string
	COSKeyPrefix          string
	DevLoginEnabled       bool
	WeChatAppID           string
	WeChatAppSecret       string
	WeChatAPIBaseURL      string
	WeChatRequestTimeout  time.Duration
	WeatherProvider       string
	AMapWebServiceKey     string
	AMapAPIBaseURL        string
	AMapDefaultCity       string
	AMapDefaultAdcode     string
	WeatherRequestTimeout time.Duration
	// 二期：多端身份与登录
	SmsProvider              string
	SmsRatePerPhonePerHour   int64
	AliyunSmsAccessKeyID     string
	AliyunSmsAccessKeySecret string
	AliyunSmsSign            string
	AliyunSmsTemplateCode    string
	AppleBundleID            string
	WeChatOpenAppID          string
	WeChatOpenAppSecret      string
	RunWorker                bool
	SessionTTL               time.Duration
	MaxUploadBytes           int64
	AnalysisPollTime         time.Duration
	AIRouting                AIRoutingConfig
	AIRoutingSource          string
	BillingPaymentEnabled    bool
	BillingSKUs              []BillingSKU
	VirtualPayOfferID        string
	VirtualPayAppKey         string
	VirtualPayEnv            int
	VirtualPayAPIBase        string
}

func Load() (Config, error) {
	aiRouting, aiRoutingSource, err := loadAIRouting()
	if err != nil {
		return Config{}, err
	}
	if aiRoutingSource == "" {
		return Config{}, fmt.Errorf("AI_ROUTING_FILE or AI_ROUTING_JSON is required")
	}
	cfg := Config{
		Environment:              env("APP_ENV", "development"),
		Addr:                     env("ADDR", ":58000"),
		DatabaseURL:              env("DATABASE_URL", "postgres://jianwo:jianwo@localhost:55432/jianwo?sslmode=disable"),
		PublicBaseURL:            env("PUBLIC_BASE_URL", "http://localhost:58000"),
		UploadDir:                env("UPLOAD_DIR", "data/uploads"),
		AssetDir:                 env("ASSET_DIR", "assets"),
		StorageProvider:          env("STORAGE_PROVIDER", "local"),
		AssetURLTTL:              time.Duration(envInt("ASSET_URL_TTL_SECONDS", 900)) * time.Second,
		COSBucketURL:             resolvedCOSBucketURL(),
		COSRegion:                env("ASSET_REGION", ""),
		COSEndpoint:              env("ASSET_S3_ENDPOINT", ""),
		COSSecretID:              os.Getenv("COS_SECRET_ID"),
		COSSecretKey:             os.Getenv("COS_SECRET_KEY"),
		COSKeyPrefix:             env("COS_KEY_PREFIX", "jianwo"),
		DevLoginEnabled:          envBool("DEV_LOGIN_ENABLED", true),
		WeChatAppID:              os.Getenv("WECHAT_APP_ID"),
		WeChatAppSecret:          os.Getenv("WECHAT_APP_SECRET"),
		WeChatAPIBaseURL:         env("WECHAT_API_BASE_URL", "https://api.weixin.qq.com/sns/jscode2session"),
		WeChatRequestTimeout:     time.Duration(envInt("WECHAT_REQUEST_TIMEOUT_SECONDS", 5)) * time.Second,
		WeatherProvider:          env("WEATHER_PROVIDER", "demo"),
		AMapWebServiceKey:        os.Getenv("AMAP_WEB_SERVICE_KEY"),
		AMapAPIBaseURL:           env("AMAP_API_BASE_URL", "https://restapi.amap.com/v3/weather/weatherInfo"),
		AMapDefaultCity:          env("AMAP_DEFAULT_CITY", "上海"),
		AMapDefaultAdcode:        env("AMAP_DEFAULT_ADCODE", "310000"),
		WeatherRequestTimeout:    time.Duration(envInt("WEATHER_REQUEST_TIMEOUT_SECONDS", 5)) * time.Second,
		SmsProvider:              env("SMS_PROVIDER", "console"),
		SmsRatePerPhonePerHour:   int64(envInt("SMS_RATE_PER_PHONE_PER_HOUR", 5)),
		AliyunSmsAccessKeyID:     os.Getenv("ALIYUN_SMS_ACCESS_KEY_ID"),
		AliyunSmsAccessKeySecret: os.Getenv("ALIYUN_SMS_ACCESS_KEY_SECRET"),
		AliyunSmsSign:            os.Getenv("ALIYUN_SMS_SIGN"),
		AliyunSmsTemplateCode:    os.Getenv("ALIYUN_SMS_TEMPLATE_CODE"),
		AppleBundleID:            os.Getenv("APPLE_BUNDLE_ID"),
		WeChatOpenAppID:          os.Getenv("WECHAT_OPEN_APP_ID"),
		WeChatOpenAppSecret:      os.Getenv("WECHAT_OPEN_APP_SECRET"),
		RunWorker:                envBool("RUN_WORKER", true),
		SessionTTL:               30 * 24 * time.Hour,
		MaxUploadBytes:           10 << 20,
		AnalysisPollTime:         time.Duration(envInt("ANALYSIS_POLL_MS", 700)) * time.Millisecond,
		AIRouting:                aiRouting,
		AIRoutingSource:          aiRoutingSource,
	}
	if skus, err := loadBillingSKUs(); err != nil {
		return Config{}, err
	} else {
		cfg.BillingSKUs = skus
	}
	cfg.BillingPaymentEnabled = envBool("BILLING_PAYMENT_ENABLED", false)
	cfg.VirtualPayOfferID = strings.TrimSpace(os.Getenv("WECHAT_VIRTUAL_PAY_OFFER_ID"))
	cfg.VirtualPayAppKey = strings.TrimSpace(os.Getenv("WECHAT_VIRTUAL_PAY_APP_KEY"))
	cfg.VirtualPayEnv = 1
	if cfg.Environment == "production" {
		cfg.VirtualPayEnv = 0
	}
	if raw := strings.TrimSpace(os.Getenv("WECHAT_VIRTUAL_PAY_ENV")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && (parsed == 0 || parsed == 1) {
			cfg.VirtualPayEnv = parsed
		}
	}
	cfg.VirtualPayAPIBase = env("WECHAT_VIRTUAL_PAY_API_BASE", "https://api.weixin.qq.com")
	if cfg.BillingPaymentEnabled && (cfg.VirtualPayOfferID == "" || cfg.VirtualPayAppKey == "") {
		return Config{}, fmt.Errorf("WECHAT_VIRTUAL_PAY_OFFER_ID and WECHAT_VIRTUAL_PAY_APP_KEY are required when billing payment is enabled")
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.Environment != "development" && cfg.Environment != "test" && cfg.Environment != "staging" && cfg.Environment != "production" {
		return Config{}, fmt.Errorf("APP_ENV must be development, test, staging or production")
	}
	if cfg.StorageProvider != "local" && cfg.StorageProvider != "cos" {
		return Config{}, fmt.Errorf("STORAGE_PROVIDER must be local or cos")
	}
	if cfg.StorageProvider == "cos" && (cfg.COSBucketURL == "" || cfg.COSSecretID == "" || cfg.COSSecretKey == "") {
		return Config{}, fmt.Errorf("COS_BUCKET_URL, COS_SECRET_ID and COS_SECRET_KEY are required for COS storage")
	}
	if cfg.WeatherProvider != "demo" && cfg.WeatherProvider != "amap" {
		return Config{}, fmt.Errorf("WEATHER_PROVIDER must be demo or amap")
	}
	if cfg.WeatherProvider == "amap" && cfg.AMapWebServiceKey == "" {
		return Config{}, fmt.Errorf("AMAP_WEB_SERVICE_KEY is required for AMap weather")
	}
	if cfg.SmsProvider != "console" && cfg.SmsProvider != "aliyun" {
		return Config{}, fmt.Errorf("SMS_PROVIDER must be console or aliyun")
	}
	if cfg.SmsProvider == "aliyun" && (cfg.AliyunSmsSign == "" || cfg.AliyunSmsTemplateCode == "") {
		return Config{}, fmt.Errorf("ALIYUN_SMS_SIGN and ALIYUN_SMS_TEMPLATE_CODE are required for aliyun SMS")
	}
	if cfg.SmsProvider == "aliyun" && (cfg.AliyunSmsAccessKeyID == "" || cfg.AliyunSmsAccessKeySecret == "") {
		return Config{}, fmt.Errorf("ALIYUN_SMS_ACCESS_KEY_ID and ALIYUN_SMS_ACCESS_KEY_SECRET are required for aliyun SMS")
	}
	if cfg.Environment == "production" {
		if cfg.DevLoginEnabled {
			return Config{}, fmt.Errorf("DEV_LOGIN_ENABLED must be false in production")
		}
		if cfg.WeChatAppID == "" || cfg.WeChatAppSecret == "" {
			return Config{}, fmt.Errorf("WECHAT_APP_ID and WECHAT_APP_SECRET are required in production")
		}
		if err := validateHTTPSURL(cfg.PublicBaseURL); err != nil {
			return Config{}, fmt.Errorf("PUBLIC_BASE_URL: %w", err)
		}
		if err := validateHTTPSURL(cfg.WeChatAPIBaseURL); err != nil {
			return Config{}, fmt.Errorf("WECHAT_API_BASE_URL: %w", err)
		}
		if cfg.StorageProvider != "cos" {
			return Config{}, fmt.Errorf("STORAGE_PROVIDER must be cos in production")
		}
		if err := validateHTTPSURL(cfg.COSBucketURL); err != nil {
			return Config{}, fmt.Errorf("COS_BUCKET_URL: %w", err)
		}
		if cfg.WeatherProvider != "amap" {
			return Config{}, fmt.Errorf("WEATHER_PROVIDER must be amap in production")
		}
		// ConsoleSms 是固定验证码的开发实现，生产禁用（否则任何人可登录任意手机号）。
		if cfg.SmsProvider != "aliyun" {
			return Config{}, fmt.Errorf("SMS_PROVIDER must be aliyun in production")
		}
		if err := validateHTTPSURL(cfg.AMapAPIBaseURL); err != nil {
			return Config{}, fmt.Errorf("AMAP_API_BASE_URL: %w", err)
		}
		if cfg.AIRoutingSource == "" {
			return Config{}, fmt.Errorf("AI_ROUTING_FILE or AI_ROUTING_JSON is required in production")
		} else {
			for _, capability := range []string{"appearance_analysis", "outfit_diagnosis", "purchase_diagnosis", "advisor_chat", "hair_edit", "today_plan"} {
				if _, ok := cfg.AIRouting.Routes[capability]; !ok {
					return Config{}, fmt.Errorf("production AI routing requires %q", capability)
				}
			}
			for id, model := range cfg.AIRouting.Models {
				// Lab-only fixture: DemoOrbitGenerator ignores URL/key and results
				// are forced to the 「效果示例」 badge. Other capabilities stay Demo-free.
				if model.Protocol == "demo_orbit" {
					continue
				}
				if strings.EqualFold(model.Vendor, "demo") {
					return Config{}, fmt.Errorf("production AI model %q must not use demo vendor", id)
				}
				if strings.TrimSpace(os.Getenv(model.APIKeyEnv)) == "" {
					return Config{}, fmt.Errorf("%s is required by AI model %q", model.APIKeyEnv, id)
				}
				if strings.Contains(strings.ToUpper(model.BaseURL), "YOUR_") || strings.Contains(model.BaseURL, "<") {
					return Config{}, fmt.Errorf("AI model %q base_url still contains a placeholder", id)
				}
				if err := validateHTTPSURL(model.BaseURL); err != nil {
					return Config{}, fmt.Errorf("AI model %q base_url: %w", id, err)
				}
			}
		}
	}
	return cfg, nil
}

func validateHTTPSURL(value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return fmt.Errorf("must be an absolute HTTPS URL")
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return fmt.Errorf("must not use a loopback host")
	}
	return nil
}

// resolvedCOSBucketURL accepts both the native COS bucket URL and the
// bucket/endpoint pair used by S3-compatible deployment panels.
func resolvedCOSBucketURL() string {
	if value := strings.TrimSpace(os.Getenv("COS_BUCKET_URL")); value != "" {
		return value
	}
	bucket := strings.Trim(strings.TrimSpace(os.Getenv("ASSET_BUCKET")), "/")
	endpoint := strings.TrimRight(strings.TrimSpace(os.Getenv("ASSET_S3_ENDPOINT")), "/")
	if bucket == "" || endpoint == "" {
		return ""
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + bucket + "." + parsed.Host
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

type BillingSKU struct {
	ID               string `json:"id"`
	Title            string `json:"title"`
	Credits          int    `json:"credits"`
	PriceFen         int    `json:"price_fen"`
	OriginalPriceFen int    `json:"original_price_fen"`
	Badge            string `json:"badge"`
	ProductID        string `json:"product_id"`
}

func loadBillingSKUs() ([]BillingSKU, error) {
	defaults := []BillingSKU{
		{ID: "pack_3", Title: "体验次数 ×3", Credits: 3, PriceFen: 690, OriginalPriceFen: 990, ProductID: "pack_3"},
		{ID: "pack_10", Title: "常用次数 ×10", Credits: 10, PriceFen: 1690, OriginalPriceFen: 2990, Badge: "featured", ProductID: "pack_10"},
		{ID: "pack_30", Title: "超值次数 ×30", Credits: 30, PriceFen: 4990, OriginalPriceFen: 9990, Badge: "value", ProductID: "pack_30"},
	}
	raw := strings.TrimSpace(os.Getenv("BILLING_SKUS_JSON"))
	if raw == "" {
		return defaults, nil
	}
	var skus []BillingSKU
	if err := json.Unmarshal([]byte(raw), &skus); err != nil {
		return nil, fmt.Errorf("BILLING_SKUS_JSON: %w", err)
	}
	if len(skus) == 0 {
		return nil, fmt.Errorf("BILLING_SKUS_JSON must contain at least one sku")
	}
	for _, sku := range skus {
		if sku.ID == "" || sku.Credits <= 0 || sku.PriceFen <= 0 || sku.ProductID == "" {
			return nil, fmt.Errorf("BILLING_SKUS_JSON entries need id, credits, price_fen and product_id")
		}
		if sku.OriginalPriceFen < 0 || (sku.OriginalPriceFen > 0 && sku.OriginalPriceFen <= sku.PriceFen) {
			return nil, fmt.Errorf("BILLING_SKUS_JSON original_price_fen must be greater than price_fen")
		}
	}
	return skus, nil
}

func envInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
