package bootstrap

import (
	"fmt"
	"time"

	"github.com/zhanshimian/server/internal/config"
	"github.com/zhanshimian/server/internal/provider/ai"
	"github.com/zhanshimian/server/internal/repository/postgres"
	"github.com/zhanshimian/server/internal/service/planning"
	"github.com/zhanshimian/server/internal/service/taskrunner"
)

// PlanningDefinition pins the plan_set.generate retry budget and lease
// timings; the postgres adapters read MaxAttempts from it so commands never
// carry retry budgets.
func PlanningDefinition() taskrunner.Definition {
	return taskrunner.Definition{
		Type:           planning.PlanSetGenerationTaskType,
		MaxAttempts:    3,
		Timeout:        90 * time.Second,
		LeaseDuration:  45 * time.Second,
		HeartbeatEvery: 15 * time.Second,
		Concurrency:    2,
		RetryBackoff:   taskrunner.ExponentialBackoff(5*time.Second, time.Minute),
	}
}

// PlanningBundle carries the API-side planning service and the worker-side
// registry. Final composition into the central API/Worker bootstrap stays
// with Peripherals/Cutover.
type PlanningBundle struct {
	Service    *planning.Service
	Registry   *taskrunner.Registry
	Definition taskrunner.Definition
	Handler    *planning.Handler
}

// WirePlanning assembles the planning service, handler and registry from the
// postgres adapter and the capability-routed AI runtime. renders 是渲染读模型
// 的只读端口(渲染 Service 即满足该接口);为 nil 时方案读模型不合并渲染状态。
// autoRenders 是发布后整批触发形象图渲染的端口;为 nil 时只产文字方案。
func WirePlanning(cfg config.Config, store *postgres.Store, runtime ai.StructuredRuntime, renders planning.CurrentRenderReader, autoRenders planning.RenderStarter) (*PlanningBundle, error) {
	if err := validatePlanningRoutes(cfg); err != nil {
		return nil, err
	}
	def := PlanningDefinition()
	operations := postgres.NewPlanningOperations(store, def.MaxAttempts)
	handler := planning.NewHandler(planning.HandlerDeps{
		Reports:    store,
		Generator:  ai.NewPlanSetGenerator(runtime),
		Verifier:   ai.NewPlanSetVerifier(runtime),
		Operations: operations,
		Tasks:      operations,
		Store:      store,
		Memories:   store,
		Decisions:  store,
		Renders:    autoRenders,
	})
	registry, err := taskrunner.NewRegistry(
		[]taskrunner.Definition{def},
		[]taskrunner.Handler{handler},
	)
	if err != nil {
		return nil, err
	}
	return &PlanningBundle{
		Service: planning.NewService(planning.Dependencies{
			Reports:    store,
			Operations: operations,
			Store:      store,
			Renders:    renders,
			Memories:   store,
			Decisions:  store,
		}),
		Registry:   registry,
		Definition: def,
		Handler:    handler,
	}, nil
}

func validatePlanningRoutes(cfg config.Config) error {
	if cfg.Environment != "production" {
		return nil
	}
	for _, capability := range []string{
		ai.CapabilityPlanSetGeneration,
		ai.CapabilityPlanGroundingVerification,
	} {
		if _, ok := cfg.AIRouting.Routes[capability]; !ok {
			return fmt.Errorf("production AI routing requires %q", capability)
		}
	}
	return nil
}
