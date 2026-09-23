package bootstrap

import (
	"strings"
	"testing"

	"github.com/zhanshimian/server/internal/config"
	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/service/assessment"
)

func TestWorkerRegistersAssessmentHandlerWithoutChangingRunnerLoop(t *testing.T) {
	bundle, err := WireAssessment(validAssessmentConfig("test"), AssessmentDeps{})
	if err != nil {
		t.Fatalf("WireAssessment: %v", err)
	}
	if bundle == nil || bundle.Service == nil {
		t.Fatal("expected Assessment Service")
	}
	if bundle.Registry == nil {
		t.Fatal("expected Registry")
	}
	if got := bundle.Registry.Types(); len(got) != 1 || got[0] != assessment.TaskTypeAssessment {
		t.Fatalf("Registry.Types() = %v, want [assessment]", got)
	}
	definition, ok := bundle.Registry.Definition(domain.TaskType("assessment"))
	if !ok {
		t.Fatal("Registry missing assessment definition")
	}
	if definition.MaxAttempts != 3 {
		t.Fatalf("MaxAttempts = %d, want 3", definition.MaxAttempts)
	}
	handler, ok := bundle.Registry.Handler(assessment.TaskTypeAssessment)
	if !ok || handler == nil || handler.Type() != assessment.TaskTypeAssessment {
		t.Fatalf("Registry handler = %v ok=%v", handler, ok)
	}
}

func TestProductionBootstrapRequiresAllAssessmentCapabilities(t *testing.T) {
	cfg := validAssessmentConfig("production")
	delete(cfg.AIRouting.Routes, "report_evidence_verification")
	_, err := WireAssessment(cfg, AssessmentDeps{})
	if err == nil || !strings.Contains(err.Error(), "report_evidence_verification") {
		t.Fatalf("expected report_evidence_verification in error, got %v", err)
	}
}

func validAssessmentConfig(environment string) config.Config {
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
				"photo_quality_check":          {Primary: "demo", MaxInputImages: 3},
				"photo_identity_consistency":   {Primary: "demo", MaxInputImages: 3},
				"appearance_analysis":          {Primary: "demo", MaxInputImages: 3},
				"report_evidence_verification": {Primary: "demo", MaxInputImages: 3},
			},
		},
	}
}
