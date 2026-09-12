package bootstrap

import (
	"github.com/zhanshimian/server/internal/config"
	providerai "github.com/zhanshimian/server/internal/provider/ai"
	"github.com/zhanshimian/server/internal/repository/postgres"
	"github.com/zhanshimian/server/internal/service/assessment"
	"github.com/zhanshimian/server/internal/service/body"
	"github.com/zhanshimian/server/internal/service/planning"
	"github.com/zhanshimian/server/internal/service/rendering"
	"github.com/zhanshimian/server/internal/service/taskrunner"
	"github.com/zhanshimian/server/internal/storage"
)

// qualityCore 是 API 与 Worker 共用的质量核心组装：五个服务 + 合并后的 worker registry。
type qualityCore struct {
	Assessment *assessment.Service
	Planning   *planning.Service
	Rendering  *rendering.Service
	Body       *body.Service
	Registry   *taskrunner.Registry
}

// wireQualityCore 构造 assessment/planning/rendering 的 Service + Handler，
// 并把三个 handler 合并进同一个 worker registry。API 侧取 Service，
// Worker 侧取 Registry；两者共享同一批 store / 对象库 / AI runtime。
func wireQualityCore(cfg config.Config, store *postgres.Store, objects storage.ObjectStorage, ai AIBundle) (*qualityCore, error) {
	var signer storage.SignedURLStorage
	if s, ok := objects.(storage.SignedURLStorage); ok {
		signer = s
	}
	presenter := mediaPresenter{signer: signer, ttl: cfg.AssetURLTTL}
	aiRuntime := structuredRuntimeAdapter{runtime: ai.Runtime}
	providers := providerai.NewAssessmentProviders(aiRuntime)

	assessmentBundle, err := WireAssessment(cfg, AssessmentDeps{
		Repo: store, Assets: store, Profiles: store, Media: presenter,
		Handler: assessment.HandlerDeps{
			Repo:         store,
			Images:       imageLoader{objects: objects},
			Progress:     operationProgress{store: store},
			Technical:    assessment.TechnicalPhotoChecker{MaxBytes: 20 << 20, MaxDimension: 8192, MaxPixels: 40_000_000},
			PhotoContent: providers.PhotoContent,
			Identity:     providers.Identity,
			Analyzer:     providers.Analyzer,
			Evidence:     providers.Evidence,
			Policy:       assessment.Policy{},
		},
	})
	if err != nil {
		return nil, err
	}

	planningBundle, err := WirePlanning(cfg, store, aiRuntime)
	if err != nil {
		return nil, err
	}

	qualityPolicy := rendering.QualityPolicy{Version: "render-quality-v1"}
	if cfg.Environment == "production" {
		qualityPolicy, err = LoadRenderingQualityPolicy("config/render-quality-policy.v1.json")
		if err != nil {
			return nil, err
		}
	}
	renderingBundle, err := WireRendering(cfg, store, objects, runtimeImageCaller{ai.Runtime}, aiRuntime, qualityPolicy)
	if err != nil {
		return nil, err
	}

	// 3D 形象 Lite：计费在 API 侧（WithBilling）挂上，见 api.go。
	bodyBundle := WireBody(store, objects, bodySigner(objects, cfg), nil, ai.Orbit, 0, nil)

	registry, err := taskrunner.NewRegistry(
		[]taskrunner.Definition{assessmentBundle.Definition, planningBundle.Definition, renderingBundle.Definition, bodyBundle.Definition},
		[]taskrunner.Handler{assessmentBundle.Handler, planningBundle.Handler, renderingBundle.Handler, bodyBundle.Handler},
	)
	if err != nil {
		return nil, err
	}

	return &qualityCore{
		Assessment: assessmentBundle.Service,
		Planning:   planningBundle.Service,
		Rendering:  renderingBundle.Service,
		Body:       bodyBundle.Service,
		Registry:   registry,
	}, nil
}
