package config

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestAIRoutingRequiresAssessmentMaxInputImages(t *testing.T) {
	routing := minimalStructuredRouting("appearance_analysis")
	if err := validateAIRouting(routing); err == nil {
		t.Fatal("expected appearance_analysis without max_input_images to fail")
	}
	routing.Routes["appearance_analysis"] = AIRouteConfig{Primary: "qwen", MaxInputImages: 2}
	if err := validateAIRouting(routing); err == nil {
		t.Fatal("expected max_input_images < 3 to fail")
	}
	routing.Routes["appearance_analysis"] = AIRouteConfig{Primary: "qwen", MaxInputImages: 3}
	if err := validateAIRouting(routing); err != nil {
		t.Fatal(err)
	}
}

func TestAIRoutingAcceptsAssessmentCapabilities(t *testing.T) {
	routing := AIRoutingConfig{
		Models: map[string]AIModelConfig{
			"qwen": {
				Vendor: "aliyun", Protocol: "openai_chat_completions", Model: "qwen",
				BaseURL: "https://example.com/v1", APIKeyEnv: "ALIYUN_API_KEY",
			},
		},
		Routes: map[string]AIRouteConfig{
			"photo_quality_check":          {Primary: "qwen", MaxInputImages: 3},
			"photo_identity_consistency":   {Primary: "qwen", MaxInputImages: 3},
			"appearance_analysis":          {Primary: "qwen", MaxInputImages: 3},
			"report_evidence_verification": {Primary: "qwen", MaxInputImages: 3},
		},
	}
	if err := validateAIRouting(routing); err != nil {
		t.Fatal(err)
	}
}

func TestAIRoutingKeepsLeftoverPhotoCheck(t *testing.T) {
	routing := minimalStructuredRouting("photo_check")
	if err := validateAIRouting(routing); err != nil {
		t.Fatalf("leftover photo_check must remain a valid structured route: %v", err)
	}
}

func TestAIRoutingShippedConfigsGiveAssessmentThreeImages(t *testing.T) {
	t.Setenv("BAILIAN_WORKSPACE_ID", "workspace-test")
	required := []string{"photo_quality_check", "photo_identity_consistency", "appearance_analysis", "report_evidence_verification"}
	for _, name := range []string{"ai-routing.example.json", "ai-routing.production.json"} {
		data, err := os.ReadFile("../../config/" + name)
		if err != nil {
			t.Fatal(err)
		}
		var routing AIRoutingConfig
		if err := json.Unmarshal(expandAIRoutingEnv(data), &routing); err != nil {
			t.Fatalf("decode %s: %v", name, err)
		}
		if err := validateAIRouting(routing); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, capability := range required {
			route, ok := routing.Routes[capability]
			if !ok {
				t.Fatalf("%s missing %q", name, capability)
			}
			if route.MaxInputImages < 3 {
				t.Fatalf("%s %s max_input_images = %d, want >= 3", name, capability, route.MaxInputImages)
			}
		}
	}
}

func TestLoadRejectsProductionMissingAssessmentCapability(t *testing.T) {
	clearReleaseEnvironment(t)
	setProductionReleaseEnv(t)
	t.Setenv("AI_ROUTING_JSON", `{
		"models":{
			"qwen":{"vendor":"aliyun","protocol":"openai_chat_completions","model":"qwen","base_url":"https://ai.example.com/v1","api_key_env":"ALIYUN_API_KEY"},
			"image":{"vendor":"aliyun","protocol":"dashscope_wan","model":"wan","base_url":"https://images.example.com/generate","api_key_env":"ALIYUN_API_KEY"}
		},
		"routes":{
			"appearance_analysis":{"primary":"qwen","max_input_images":3},
			"outfit_diagnosis":{"primary":"qwen"},
			"purchase_diagnosis":{"primary":"qwen"},
			"advisor_chat":{"primary":"qwen"},
			"today_plan":{"primary":"qwen"},
			"hair_edit":{"primary":"image"},
			"photo_quality_check":{"primary":"qwen","max_input_images":3},
			"photo_identity_consistency":{"primary":"qwen","max_input_images":3}
		}
	}`)
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "report_evidence_verification") {
		t.Fatalf("expected production to require report_evidence_verification, got %v", err)
	}
}

func minimalStructuredRouting(capability string) AIRoutingConfig {
	return AIRoutingConfig{
		Models: map[string]AIModelConfig{
			"qwen": {
				Vendor: "aliyun", Protocol: "openai_chat_completions", Model: "qwen",
				BaseURL: "https://example.com/v1", APIKeyEnv: "ALIYUN_API_KEY",
			},
		},
		Routes: map[string]AIRouteConfig{
			capability: {Primary: "qwen"},
		},
	}
}

func setProductionReleaseEnv(t *testing.T) {
	t.Helper()
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
	t.Setenv("ALIYUN_SMS_SIGN", "uplook")
	t.Setenv("ALIYUN_SMS_TEMPLATE_CODE", "SMS-123456")
	t.Setenv("ALIYUN_API_KEY", "test-ai-key")
}
