package bootstrap

import (
	"context"
	"strings"
	"testing"

	"github.com/zhanshimian/server/internal/config"
	"github.com/zhanshimian/server/internal/provider/ai"
	"github.com/zhanshimian/server/internal/service/rendering"
)

func TestRenderingDefinitionPinsBudgetAndLease(t *testing.T) {
	def := RenderingDefinition()
	if def.MaxAttempts != 3 || def.Timeout.Seconds() != 300 || def.LeaseDuration.Seconds() != 90 {
		t.Fatalf("definition drifted: %#v", def)
	}
	if def.HeartbeatEvery.Seconds() != 30 || def.Concurrency != 2 {
		t.Fatalf("definition drifted: %#v", def)
	}
}

func TestWireRenderingRegistersRenderCandidateGenerate(t *testing.T) {
	bundle, err := WireRendering(validRenderingConfig("test"), nil, nil, nil, &fakeStructuredRuntime{}, rendering.QualityPolicy{Version: "render-quality-v1"})
	if err != nil {
		t.Fatalf("WireRendering: %v", err)
	}
	if bundle.Service == nil || bundle.Handler == nil {
		t.Fatal("expected rendering service and handler")
	}
	types := bundle.Registry.Types()
	if len(types) != 1 || types[0] != "render_candidate_generate" {
		t.Fatalf("Registry.Types() = %v, want [render_candidate_generate]", types)
	}
	definition, ok := bundle.Registry.Definition("render_candidate_generate")
	if !ok {
		t.Fatal("registry missing render_candidate_generate")
	}
	if definition.MaxAttempts != 3 {
		t.Fatalf("MaxAttempts = %d, want 3", definition.MaxAttempts)
	}
	handler, ok := bundle.Registry.Handler("render_candidate_generate")
	if !ok || handler == nil || handler.Type() != "render_candidate_generate" {
		t.Fatalf("registry handler = %v ok=%v", handler, ok)
	}
}

func TestProductionRenderingRequiresEquivalentRoutes(t *testing.T) {
	cfg := validRenderingConfig("production")
	delete(cfg.AIRouting.Routes, "render_quality_evaluation")
	_, err := WireRendering(cfg, nil, nil, nil, &fakeStructuredRuntime{}, rendering.QualityPolicy{Version: "render-quality-v1"})
	if err == nil || !strings.Contains(err.Error(), "render_quality_evaluation") {
		t.Fatalf("expected missing capability error, got %v", err)
	}
}

func validRenderingConfig(environment string) config.Config {
	routes := map[string]config.AIRouteConfig{
		"plan_set_generation":         {Primary: "demo"},
		"plan_grounding_verification": {Primary: "demo"},
	}
	if environment == "production" {
		routes["full_look_generation"] = config.AIRouteConfig{
			Policy: "balanced", Primary: "prod/look",
			Fallbacks:   []string{"prod/look-b"},
			MaxSwitches: 1,
			Requirements: config.RouteRequirements{
				MinInputImages: 2, MultipleReferences: true, IdentityPreservation: true,
				OutputMIMETypes: []string{"image/jpeg"}, DataRetention: "zero",
			},
		}
		routes["render_quality_evaluation"] = config.AIRouteConfig{
			Policy: "balanced", Primary: "prod/quality",
			MaxSwitches: 1,
			Requirements: config.RouteRequirements{
				MinInputImages: 3, MultipleReferences: true, IdentityComparison: true,
				OutputMIMETypes: []string{"image/jpeg"}, DataRetention: "zero",
			},
		}
	}
	models := map[string]config.AIModelConfig{
		"demo": {
			Vendor: "demo", Protocol: "openai_chat_completions", Model: "demo",
			BaseURL: "https://example.com/v1", APIKeyEnv: "BOOTSTRAP_TEST_AI_KEY",
		},
	}
	if environment == "production" {
		models["prod/look"] = productionRenderingModel("look")
		models["prod/look-b"] = productionRenderingModel("look-b")
		models["prod/quality"] = productionRenderingModel("quality")
	}
	return config.Config{
		Environment: environment,
		AIRouting:   config.AIRoutingConfig{Models: models, Routes: routes},
	}
}

func productionRenderingModel(name string) config.AIModelConfig {
	return config.AIModelConfig{
		Vendor: "aliyun", Protocol: "dashscope_wan", Model: name,
		BaseURL: "https://example.com/v1", APIKeyEnv: "BOOTSTRAP_TEST_AI_KEY",
		MaxInputImages:                  4,
		SupportsMultipleReferenceImages: true,
		IdentityPreservation:            true,
		IdentityComparison:              true,
		OutputMIMETypes:                 []string{"image/jpeg"},
		DataRetention:                   "zero",
	}
}

// fakeStructuredRuntime 复用 planning_test 的形态;渲染装配测试只验证结构。
type fakeStructuredRuntime struct{}

func (fakeStructuredRuntime) Structured(context.Context, ai.StructuredRequest) (ai.StructuredResult, error) {
	return ai.StructuredResult{}, nil
}
