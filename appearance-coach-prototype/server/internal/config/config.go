package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Environment                   string
	Addr                          string
	DatabaseURL                   string
	PublicBaseURL                 string
	UploadDir                     string
	AssetDir                      string
	StorageProvider               string
	AssetURLTTL                   time.Duration
	COSBucketURL                  string
	COSRegion                     string
	COSEndpoint                   string
	COSSecretID                   string
	COSSecretKey                  string
	COSKeyPrefix                  string
	DevLoginEnabled               bool
	WeChatAppID                   string
	WeChatAppSecret               string
	WeChatAPIBaseURL              string
	WeChatRequestTimeout          time.Duration
	WeatherProvider               string
	AMapWebServiceKey             string
	AMapAPIBaseURL                string
	AMapDefaultCity               string
	AMapDefaultAdcode             string
	WeatherRequestTimeout         time.Duration
	RunWorker                     bool
	SessionTTL                    time.Duration
	MaxUploadBytes                int64
	AnalysisPollTime              time.Duration
	AIProvider                    string
	AIFallbackToDemo              bool
	AIRequestTimeout              time.Duration
	OpenAIAPIKey                  string
	OpenAIBaseURL                 string
	OpenAIVisionModel             string
	HairPreviewProvider           string
	HairPreviewFallbackToDemo     bool
	HairPreviewTimeout            time.Duration
	OpenAIImageModel              string
	OpenAIImageQuality            string
	OutfitDiagnosisProvider       string
	OutfitDiagnosisFallbackToDemo bool
	OutfitDiagnosisTimeout        time.Duration
	OpenAIOutfitModel             string
}

func Load() (Config, error) {
	aiProvider := env("AI_PROVIDER", "demo")
	cfg := Config{
		Environment:                   env("APP_ENV", "development"),
		Addr:                          env("ADDR", ":58000"),
		DatabaseURL:                   env("DATABASE_URL", "postgres://jianwo:jianwo@localhost:55432/jianwo?sslmode=disable"),
		PublicBaseURL:                 env("PUBLIC_BASE_URL", "http://localhost:58000"),
		UploadDir:                     env("UPLOAD_DIR", "data/uploads"),
		AssetDir:                      env("ASSET_DIR", "assets"),
		StorageProvider:               env("STORAGE_PROVIDER", "local"),
		AssetURLTTL:                   time.Duration(envInt("ASSET_URL_TTL_SECONDS", 900)) * time.Second,
		COSBucketURL:                  resolvedCOSBucketURL(),
		COSRegion:                     env("ASSET_REGION", ""),
		COSEndpoint:                   env("ASSET_S3_ENDPOINT", ""),
		COSSecretID:                   os.Getenv("COS_SECRET_ID"),
		COSSecretKey:                  os.Getenv("COS_SECRET_KEY"),
		COSKeyPrefix:                  env("COS_KEY_PREFIX", "jianwo"),
		DevLoginEnabled:               envBool("DEV_LOGIN_ENABLED", true),
		WeChatAppID:                   os.Getenv("WECHAT_APP_ID"),
		WeChatAppSecret:               os.Getenv("WECHAT_APP_SECRET"),
		WeChatAPIBaseURL:              env("WECHAT_API_BASE_URL", "https://api.weixin.qq.com/sns/jscode2session"),
		WeChatRequestTimeout:          time.Duration(envInt("WECHAT_REQUEST_TIMEOUT_SECONDS", 5)) * time.Second,
		WeatherProvider:               env("WEATHER_PROVIDER", "demo"),
		AMapWebServiceKey:             os.Getenv("AMAP_WEB_SERVICE_KEY"),
		AMapAPIBaseURL:                env("AMAP_API_BASE_URL", "https://restapi.amap.com/v3/weather/weatherInfo"),
		AMapDefaultCity:               env("AMAP_DEFAULT_CITY", "上海"),
		AMapDefaultAdcode:             env("AMAP_DEFAULT_ADCODE", "310000"),
		WeatherRequestTimeout:         time.Duration(envInt("WEATHER_REQUEST_TIMEOUT_SECONDS", 5)) * time.Second,
		RunWorker:                     envBool("RUN_WORKER", true),
		SessionTTL:                    30 * 24 * time.Hour,
		MaxUploadBytes:                10 << 20,
		AnalysisPollTime:              700 * time.Millisecond,
		AIProvider:                    aiProvider,
		AIFallbackToDemo:              envBool("AI_FALLBACK_TO_DEMO", true),
		AIRequestTimeout:              time.Duration(envInt("AI_REQUEST_TIMEOUT_SECONDS", 90)) * time.Second,
		OpenAIAPIKey:                  os.Getenv("OPENAI_API_KEY"),
		OpenAIBaseURL:                 env("OPENAI_BASE_URL", "https://api.openai.com/v1"),
		OpenAIVisionModel:             env("OPENAI_VISION_MODEL", "gpt-5-mini"),
		HairPreviewProvider:           env("HAIR_PREVIEW_PROVIDER", aiProvider),
		HairPreviewFallbackToDemo:     envBool("HAIR_PREVIEW_FALLBACK_TO_DEMO", true),
		HairPreviewTimeout:            time.Duration(envInt("HAIR_PREVIEW_TIMEOUT_SECONDS", 150)) * time.Second,
		OpenAIImageModel:              env("OPENAI_IMAGE_MODEL", "gpt-image-2"),
		OpenAIImageQuality:            env("OPENAI_IMAGE_QUALITY", "medium"),
		OutfitDiagnosisProvider:       env("OUTFIT_DIAGNOSIS_PROVIDER", aiProvider),
		OutfitDiagnosisFallbackToDemo: envBool("OUTFIT_DIAGNOSIS_FALLBACK_TO_DEMO", true),
		OutfitDiagnosisTimeout:        time.Duration(envInt("OUTFIT_DIAGNOSIS_TIMEOUT_SECONDS", 60)) * time.Second,
		OpenAIOutfitModel:             env("OPENAI_OUTFIT_MODEL", "gpt-5-mini"),
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
	if cfg.AIProvider != "demo" && cfg.AIProvider != "openai" {
		return Config{}, fmt.Errorf("AI_PROVIDER must be demo or openai")
	}
	if cfg.HairPreviewProvider != "demo" && cfg.HairPreviewProvider != "openai" {
		return Config{}, fmt.Errorf("HAIR_PREVIEW_PROVIDER must be demo or openai")
	}
	if cfg.OutfitDiagnosisProvider != "demo" && cfg.OutfitDiagnosisProvider != "openai" {
		return Config{}, fmt.Errorf("OUTFIT_DIAGNOSIS_PROVIDER must be demo or openai")
	}
	if cfg.OpenAIImageQuality != "low" && cfg.OpenAIImageQuality != "medium" && cfg.OpenAIImageQuality != "high" {
		return Config{}, fmt.Errorf("OPENAI_IMAGE_QUALITY must be low, medium or high")
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
		if err := validateHTTPSURL(cfg.AMapAPIBaseURL); err != nil {
			return Config{}, fmt.Errorf("AMAP_API_BASE_URL: %w", err)
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
