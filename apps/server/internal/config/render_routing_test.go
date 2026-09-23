package config

import (
	"strings"
	"testing"
)

func TestFullLookRouteRejectsSingleImageOrWeakIdentityModel(t *testing.T) {
	cfg := validRenderingRouting()
	model := cfg.Models[cfg.Routes["full_look_generation"].Primary]
	model.MaxInputImages = 1
	cfg.Models[cfg.Routes["full_look_generation"].Primary] = model
	err := validateAIRouting(cfg)
	if err == nil || !strings.Contains(err.Error(), "requires at least 2 input images") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFullLookRouteRejectsMissingIdentityPreservation(t *testing.T) {
	cfg := validRenderingRouting()
	model := cfg.Models[cfg.Routes["full_look_generation"].Fallbacks[0]]
	model.IdentityPreservation = false
	cfg.Models[cfg.Routes["full_look_generation"].Fallbacks[0]] = model
	err := validateAIRouting(cfg)
	if err == nil || !strings.Contains(err.Error(), "must preserve identity") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRenderingRoutesRejectDemoModels(t *testing.T) {
	cfg := validRenderingRouting()
	demo := cfg.Models["aliyun/look-primary"]
	demo.Vendor = "demo"
	cfg.Models["demo/look"] = demo
	route := cfg.Routes["full_look_generation"]
	route.Primary = "demo/look"
	cfg.Routes["full_look_generation"] = route
	err := validateAIRouting(cfg)
	if err == nil || !strings.Contains(err.Error(), "demo model") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRenderingRouteRejectsNonZeroRetention(t *testing.T) {
	cfg := validRenderingRouting()
	model := cfg.Models[cfg.Routes["render_quality_evaluation"].Primary]
	model.DataRetention = "30d"
	cfg.Models[cfg.Routes["render_quality_evaluation"].Primary] = model
	err := validateAIRouting(cfg)
	if err == nil || !strings.Contains(err.Error(), "data_retention") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func eligibleRenderingModel(key string) AIModelConfig {
	return AIModelConfig{
		Vendor: "aliyun", Protocol: "dashscope_wan", Model: key,
		BaseURL: "https://example.com/v1", APIKeyEnv: "TEST_KEY",
		MaxInputImages:                  4,
		SupportsMultipleReferenceImages: true,
		IdentityPreservation:            true,
		IdentityComparison:              true,
		OutputMIMETypes:                 []string{"image/jpeg"},
		DataRetention:                   "zero",
	}
}

func validRenderingRouting() AIRoutingConfig {
	return AIRoutingConfig{
		Models: map[string]AIModelConfig{
			"aliyun/look-primary":  eligibleRenderingModel("look-primary"),
			"aliyun/look-fallback": eligibleRenderingModel("look-fallback"),
			"aliyun/quality":       eligibleRenderingModel("quality"),
		},
		Routes: map[string]AIRouteConfig{
			"full_look_generation": {
				Policy: "balanced", Primary: "aliyun/look-primary",
				Fallbacks:   []string{"aliyun/look-fallback"},
				MaxSwitches: 1,
				Requirements: RouteRequirements{
					MinInputImages: 2, MultipleReferences: true, IdentityPreservation: true,
					OutputMIMETypes: []string{"image/jpeg"}, DataRetention: "zero",
				},
			},
			"render_quality_evaluation": {
				Policy: "balanced", Primary: "aliyun/quality",
				MaxSwitches: 1,
				Requirements: RouteRequirements{
					MinInputImages: 3, MultipleReferences: true, IdentityComparison: true,
					OutputMIMETypes: []string{"image/jpeg"}, DataRetention: "zero",
				},
			},
		},
	}
}
