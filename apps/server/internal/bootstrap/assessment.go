package bootstrap

import (
	"fmt"
	"time"

	"github.com/zhanshimian/server/internal/config"
	"github.com/zhanshimian/server/internal/service/assessment"
	"github.com/zhanshimian/server/internal/service/taskrunner"
)

var assessmentCapabilities = []string{
	"photo_quality_check",
	"photo_identity_consistency",
	"appearance_analysis",
	"report_evidence_verification",
}

type AssessmentDeps struct {
	Repo     assessment.Repository
	Assets   assessment.AssetReader
	Profiles assessment.ProfileReader
	Media    assessment.MediaPresenter
	Handler  assessment.HandlerDeps
}

type AssessmentBundle struct {
	Service    *assessment.Service
	Registry   *taskrunner.Registry
	Definition taskrunner.Definition
	Handler    *assessment.Handler
}

func AssessmentDefinition() taskrunner.Definition {
	return taskrunner.Definition{
		Type:           assessment.TaskTypeAssessment,
		MaxAttempts:    3,
		Timeout:        90 * time.Second,
		LeaseDuration:  30 * time.Second,
		HeartbeatEvery: 10 * time.Second,
		Concurrency:    2,
		RetryBackoff:   taskrunner.ExponentialBackoff(time.Second, time.Minute),
	}
}

func WireAssessment(cfg config.Config, deps AssessmentDeps) (*AssessmentBundle, error) {
	if err := validateAssessmentRoutes(cfg); err != nil {
		return nil, err
	}
	def := AssessmentDefinition()
	handler := assessment.NewHandler(deps.Handler)
	registry, err := taskrunner.NewRegistry(
		[]taskrunner.Definition{def},
		[]taskrunner.Handler{handler},
	)
	if err != nil {
		return nil, err
	}
	return &AssessmentBundle{
		Service:    assessment.NewService(deps.Repo, deps.Assets, deps.Profiles, deps.Media, def),
		Registry:   registry,
		Definition: def,
		Handler:    handler,
	}, nil
}

func validateAssessmentRoutes(cfg config.Config) error {
	if cfg.Environment != "production" {
		return nil
	}
	for _, capability := range assessmentCapabilities {
		if _, ok := cfg.AIRouting.Routes[capability]; !ok {
			return fmt.Errorf("production AI routing requires %q", capability)
		}
	}
	return nil
}
