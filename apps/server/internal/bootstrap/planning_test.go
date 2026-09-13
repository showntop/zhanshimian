package bootstrap

import (
	"context"
	"testing"

	"github.com/zhanshimian/server/internal/config"
	"github.com/zhanshimian/server/internal/provider/ai"
)

func TestPlanningDefinitionPinsBudgetAndLease(t *testing.T) {
	def := PlanningDefinition()
	if def.MaxAttempts != 3 || def.Timeout != 90_000_000_000 || def.LeaseDuration != 45_000_000_000 {
		t.Fatalf("definition drifted: %#v", def)
	}
	if def.HeartbeatEvery != 15_000_000_000 || def.Concurrency != 2 {
		t.Fatalf("definition drifted: %#v", def)
	}
}

func TestWirePlanningRegistersPlanSetGenerationHandler(t *testing.T) {
	bundle, err := WirePlanning(validPlanningConfig("test"), nil, &fakePlanningRuntime{})
	if err != nil {
		t.Fatalf("WirePlanning: %v", err)
	}
	if bundle.Service == nil || bundle.Handler == nil {
		t.Fatal("expected planning service and handler")
	}
	types := bundle.Registry.Types()
	if len(types) != 1 || types[0] != "plan_set.generate" {
		t.Fatalf("Registry.Types() = %v, want [plan_set.generate]", types)
	}
	definition, ok := bundle.Registry.Definition("plan_set.generate")
	if !ok {
		t.Fatal("registry missing plan_set.generate definition")
	}
	if definition.MaxAttempts != 3 {
		t.Fatalf("MaxAttempts = %d, want 3", definition.MaxAttempts)
	}
	handler, ok := bundle.Registry.Handler("plan_set.generate")
	if !ok || handler == nil || handler.Type() != "plan_set.generate" {
		t.Fatalf("registry handler = %v ok=%v", handler, ok)
	}
}

func TestProductionBootstrapRequiresPlanningCapabilities(t *testing.T) {
	cfg := validPlanningConfig("production")
	delete(cfg.AIRouting.Routes, "plan_grounding_verification")
	_, err := WirePlanning(cfg, nil, &fakePlanningRuntime{})
	if err == nil {
		t.Fatal("expected missing capability error")
	}
	cfg = validPlanningConfig("production")
	delete(cfg.AIRouting.Routes, "plan_set_generation")
	if _, err = WirePlanning(cfg, nil, &fakePlanningRuntime{}); err == nil {
		t.Fatal("expected missing plan_set_generation error")
	}
}

func validPlanningConfig(environment string) config.Config {
	return config.Config{
		Environment: environment,
		AIRouting: config.AIRoutingConfig{
			Models: map[string]config.AIModelConfig{
				"demo": {
					Vendor: "demo", Protocol: "openai_chat_completions", Model: "demo",
					BaseURL: "https://example.com/v1", APIKeyEnv: "BOOTSTRAP_TEST_AI_KEY",
				},
			},
			Routes: map[string]config.AIRouteConfig{
				"plan_set_generation":         {Primary: "demo"},
				"plan_grounding_verification": {Primary: "demo"},
			},
		},
	}
}

type fakePlanningRuntime struct{}

func (*fakePlanningRuntime) Structured(_ context.Context, _ ai.StructuredRequest) (ai.StructuredResult, error) {
	return ai.StructuredResult{}, nil
}
