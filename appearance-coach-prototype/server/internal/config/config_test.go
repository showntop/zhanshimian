package config

import "testing"

func TestLoadAIProviderDefaults(t *testing.T) {
	clearReleaseEnvironment(t)
	t.Setenv("AI_PROVIDER", "")
	t.Setenv("AI_FALLBACK_TO_DEMO", "")
	t.Setenv("AI_REQUEST_TIMEOUT_SECONDS", "")
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_VISION_MODEL", "")
	t.Setenv("HAIR_PREVIEW_PROVIDER", "")
	t.Setenv("HAIR_PREVIEW_FALLBACK_TO_DEMO", "")
	t.Setenv("OPENAI_IMAGE_MODEL", "")
	t.Setenv("OPENAI_IMAGE_QUALITY", "")
	t.Setenv("OUTFIT_DIAGNOSIS_PROVIDER", "")
	t.Setenv("OUTFIT_DIAGNOSIS_FALLBACK_TO_DEMO", "")
	t.Setenv("OPENAI_OUTFIT_MODEL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AIProvider != "demo" || !cfg.AIFallbackToDemo || cfg.OpenAIVisionModel != "gpt-5-mini" || cfg.HairPreviewProvider != "demo" || cfg.OpenAIImageModel != "gpt-image-2" || cfg.OutfitDiagnosisProvider != "demo" {
		t.Fatalf("unexpected AI defaults: %#v", cfg)
	}
}

func TestLoadRejectsInvalidImageQuality(t *testing.T) {
	clearReleaseEnvironment(t)
	t.Setenv("OPENAI_IMAGE_QUALITY", "ultra")
	if _, err := Load(); err == nil {
		t.Fatal("expected invalid image quality to be rejected")
	}
}

func TestLoadRejectsUnknownAIProvider(t *testing.T) {
	clearReleaseEnvironment(t)
	t.Setenv("AI_PROVIDER", "unknown")
	if _, err := Load(); err == nil {
		t.Fatal("expected unknown AI provider to be rejected")
	}
}

func TestLoadRejectsUnsafeProductionConfiguration(t *testing.T) {
	clearReleaseEnvironment(t)
	t.Setenv("APP_ENV", "production")
	if _, err := Load(); err == nil {
		t.Fatal("expected development login to be rejected in production")
	}
}

func TestLoadAcceptsProductionReleaseProviders(t *testing.T) {
	clearReleaseEnvironment(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("DEV_LOGIN_ENABLED", "false")
	t.Setenv("PUBLIC_BASE_URL", "https://api.jianwo.example")
	t.Setenv("WECHAT_APP_ID", "wx911e0fbcba0b24d0")
	t.Setenv("WECHAT_APP_SECRET", "test-secret")
	t.Setenv("STORAGE_PROVIDER", "cos")
	t.Setenv("ASSET_BUCKET", "jianwo-123")
	t.Setenv("ASSET_S3_ENDPOINT", "https://cos.ap-shanghai.myqcloud.com")
	t.Setenv("ASSET_REGION", "ap-shanghai")
	t.Setenv("COS_SECRET_ID", "test-id")
	t.Setenv("COS_SECRET_KEY", "test-key")
	t.Setenv("WEATHER_PROVIDER", "amap")
	t.Setenv("AMAP_WEB_SERVICE_KEY", "test-weather-key")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Environment != "production" || cfg.StorageProvider != "cos" || cfg.WeatherProvider != "amap" || cfg.COSBucketURL != "https://jianwo-123.cos.ap-shanghai.myqcloud.com" {
		t.Fatalf("unexpected release config: %#v", cfg)
	}
}

func TestLoadRejectsHTTPProductionBaseURL(t *testing.T) {
	clearReleaseEnvironment(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("DEV_LOGIN_ENABLED", "false")
	t.Setenv("WECHAT_APP_ID", "wx-test")
	t.Setenv("WECHAT_APP_SECRET", "secret")
	t.Setenv("PUBLIC_BASE_URL", "http://api.example.com")
	t.Setenv("STORAGE_PROVIDER", "cos")
	t.Setenv("COS_BUCKET_URL", "https://bucket.example.com")
	t.Setenv("COS_SECRET_ID", "id")
	t.Setenv("COS_SECRET_KEY", "key")
	t.Setenv("WEATHER_PROVIDER", "amap")
	t.Setenv("AMAP_WEB_SERVICE_KEY", "weather")
	if _, err := Load(); err == nil {
		t.Fatal("expected HTTP release URL to be rejected")
	}
}

func clearReleaseEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"APP_ENV", "DEV_LOGIN_ENABLED", "PUBLIC_BASE_URL", "WECHAT_APP_ID", "WECHAT_APP_SECRET",
		"STORAGE_PROVIDER", "COS_BUCKET_URL", "COS_SECRET_ID", "COS_SECRET_KEY",
		"ASSET_BUCKET", "ASSET_S3_ENDPOINT", "ASSET_REGION", "WEATHER_PROVIDER", "AMAP_WEB_SERVICE_KEY",
	} {
		t.Setenv(key, "")
	}
}
