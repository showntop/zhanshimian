package config

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestLoadRejectsMissingAIRouting(t *testing.T) {
	clearReleaseEnvironment(t)
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "AI_ROUTING") {
		t.Fatalf("expected missing AI routing to be rejected, got %v", err)
	}
}

func TestLoadParsesUnifiedAIRouting(t *testing.T) {
	clearReleaseEnvironment(t)
	t.Setenv("AI_ROUTING_JSON", `{
		"models":{"qwen":{"vendor":"aliyun","protocol":"openai_chat_completions","model":"qwen3.7-plus","base_url":"https://example.com/v1","api_key_env":"ALIYUN_API_KEY","structured_mode":"json_schema","parameters":{"enable_thinking":false}}},
		"routes":{"appearance_analysis":{"primary":"qwen","policy":"quality_first","max_cost_cny":0.08}}
	}`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	model := cfg.AIRouting.Models["qwen"]
	if cfg.AIRoutingSource != "AI_ROUTING_JSON" || model.Protocol != "openai_chat_completions" || model.Parameters["enable_thinking"] != false || cfg.AIRouting.Routes["appearance_analysis"].Primary != "qwen" {
		t.Fatalf("unexpected AI routing config: %#v", cfg.AIRouting)
	}
}

func TestLoadRejectsUnknownAIRoutingProtocol(t *testing.T) {
	clearReleaseEnvironment(t)
	t.Setenv("AI_ROUTING_JSON", `{"models":{"x":{"vendor":"x","protocol":"unknown","model":"x","base_url":"https://example.com","api_key_env":"X_KEY"}},"routes":{"appearance_analysis":{"primary":"x"}}}`)
	if _, err := Load(); err == nil {
		t.Fatal("expected unsupported AI protocol to be rejected")
	}
}

func TestExampleAIRoutingConfigIsValid(t *testing.T) {
	t.Setenv("BAILIAN_WORKSPACE_ID", "workspace-test")
	data, err := os.ReadFile("../../config/ai-routing.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var routing AIRoutingConfig
	routingData := expandAIRoutingEnv(data)
	if err := json.Unmarshal(routingData, &routing); err != nil {
		t.Fatal(err)
	}
	if err := validateAIRouting(routing); err != nil {
		t.Fatal(err)
	}
}

func TestLoadExpandsAIRoutingEnvironmentPlaceholders(t *testing.T) {
	clearReleaseEnvironment(t)
	t.Setenv("BAILIAN_WORKSPACE_ID", "workspace-test")
	t.Setenv("AI_ROUTING_JSON", `{"models":{"x":{"vendor":"aliyun","protocol":"openai_chat_completions","model":"qwen","base_url":"https://${BAILIAN_WORKSPACE_ID}.example.com/v1","api_key_env":"ALIYUN_API_KEY"}},"routes":{"advisor_chat":{"primary":"x"}}}`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.AIRouting.Models["x"].BaseURL; got != "https://workspace-test.example.com/v1" {
		t.Fatalf("placeholder was not expanded: %s", got)
	}
}

func TestLoadKeepsMissingAIRoutingPlaceholderVisible(t *testing.T) {
	clearReleaseEnvironment(t)
	t.Setenv("AI_ROUTING_JSON", `{"models":{"x":{"vendor":"aliyun","protocol":"openai_chat_completions","model":"qwen","base_url":"https://${BAILIAN_WORKSPACE_ID}.example.com/v1","api_key_env":"ALIYUN_API_KEY"}},"routes":{"advisor_chat":{"primary":"x"}}}`)
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "base_url") {
		t.Fatalf("expected missing placeholder to fail validation, got %v", err)
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
	t.Setenv("SMS_PROVIDER", "aliyun")
	t.Setenv("ALIYUN_SMS_ACCESS_KEY_ID", "test-sms-id")
	t.Setenv("ALIYUN_SMS_ACCESS_KEY_SECRET", "test-sms-secret")
	t.Setenv("ALIYUN_SMS_SIGN", "怎么打扮")
	t.Setenv("ALIYUN_SMS_TEMPLATE_CODE", "SMS-123456")
	t.Setenv("ALIYUN_API_KEY", "test-ai-key")
	t.Setenv("AI_ROUTING_JSON", `{
		"models":{
			"qwen":{"vendor":"aliyun","protocol":"openai_chat_completions","model":"qwen","base_url":"https://ai.example.com/v1","api_key_env":"ALIYUN_API_KEY"},
			"image":{"vendor":"aliyun","protocol":"dashscope_wan","model":"wan","base_url":"https://images.example.com/generate","api_key_env":"ALIYUN_API_KEY"}
		},
		"routes":{
			"appearance_analysis":{"primary":"qwen"},
			"outfit_diagnosis":{"primary":"qwen"},
			"purchase_diagnosis":{"primary":"qwen"},
			"advisor_chat":{"primary":"qwen"},
			"today_plan":{"primary":"qwen"},
			"hair_edit":{"primary":"image"}
		}
	}`)

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
		"AI_ROUTING_FILE", "AI_ROUTING_JSON", "ALIYUN_API_KEY", "VOLCENGINE_API_KEY",
		"BAILIAN_WORKSPACE_ID",
	} {
		t.Setenv(key, "")
	}
}
