package bootstrap

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/zhanshimian/server/internal/config"
	"github.com/zhanshimian/server/internal/provider"
	providerai "github.com/zhanshimian/server/internal/provider/ai"
	"github.com/zhanshimian/server/internal/repository/postgres"
	"github.com/zhanshimian/server/internal/service/rendering"
	"github.com/zhanshimian/server/internal/service/taskrunner"
	"github.com/zhanshimian/server/internal/storage"
)

// aiRoutingVersion 用渲染 route 的 policy/version 占位:路由配置文件尚无
// 独立 version 字段,先以 quality route 的主模型键区分版本,Cutover 收敛。
func aiRoutingVersion(cfg config.Config) string {
	if route, ok := cfg.AIRouting.Routes[providerai.CapabilityFullLookGeneration]; ok {
		return "render-route:" + route.Primary
	}
	return "render-route:unset"
}

// LoadRenderingQualityPolicy 从磁盘加载渲染质量策略(生产装配使用)。
func LoadRenderingQualityPolicy(path string) (rendering.QualityPolicy, error) {
	return rendering.LoadQualityPolicy(path)
}

// runtimeImageCaller 把 legacy AIRuntime 的按模型直调适配为 Router 的窄端口。
type runtimeImageCaller struct {
	runtime *provider.AIRuntime
}

func (c runtimeImageCaller) GenerateOnModel(ctx context.Context, modelID, capability string, call providerai.RouterImageCall) (providerai.RouterImageResult, error) {
	images := make([]provider.AnalysisImage, 0, len(call.Images))
	for _, image := range call.Images {
		images = append(images, provider.AnalysisImage{Kind: image.Role, MIMEType: image.MIMEType, Data: image.Data})
	}
	result, err := c.runtime.EditImageOnModel(ctx, modelID, capability, provider.ImageEditRequest{
		Prompt: call.Prompt, Images: images, Quality: call.Quality,
	})
	if err != nil {
		return providerai.RouterImageResult{}, err
	}
	cost := result.Meta.EstimatedCostCNY
	return providerai.RouterImageResult{
		Data: result.Data, MIMEType: result.MIMEType,
		InvocationID: result.Meta.InvocationID, Protocol: result.Meta.Protocol,
		ProviderRequestID: result.Meta.RequestID, LatencyMS: result.Meta.LatencyMS,
		CostCNY: &cost,
	}, nil
}

// RenderingDefinition 固定 render_candidate_generate 的重试预算与租约。
func RenderingDefinition() taskrunner.Definition {
	return taskrunner.Definition{
		Type:           rendering.RenderGenerateTaskType,
		MaxAttempts:    3,
		Timeout:        300 * time.Second,
		LeaseDuration:  90 * time.Second,
		HeartbeatEvery: 30 * time.Second,
		Concurrency:    2,
		RetryBackoff:   taskrunner.ExponentialBackoff(5*time.Second, time.Minute),
	}
}

// RenderingBundle 承载 API 侧渲染 service 与 Worker 侧 handler/registry。
type RenderingBundle struct {
	Service    *rendering.Service
	Handler    *rendering.Handler
	Registry   *taskrunner.Registry
	Definition taskrunner.Definition
}

// renderingObjectAdapter 把 storage 渲染对象库适配到 rendering 端口类型。
type renderingObjectAdapter struct{ inner *storage.RenderObjectStore }

func (a renderingObjectAdapter) PutCandidate(ctx context.Context, input rendering.CandidateObjectInput) (rendering.StoredObject, error) {
	stored, err := a.inner.PutCandidate(ctx, storage.CandidateObjectInput{
		UserID: input.UserID, RunID: input.RunID, CandidateID: input.CandidateID,
		Data: input.Data, SHA256: input.SHA256,
	})
	return rendering.StoredObject(stored), err
}

func (a renderingObjectAdapter) Promote(ctx context.Context, input rendering.PromoteObjectInput) (rendering.StoredObject, error) {
	stored, err := a.inner.Promote(ctx, storage.PromoteObjectInput{
		UserID: input.UserID, PublicationID: input.PublicationID,
		SourceKey: input.SourceKey, ExpectedSHA256: input.ExpectedSHA256,
	})
	return rendering.StoredObject(stored), err
}

func (a renderingObjectAdapter) Delete(ctx context.Context, key string) error {
	return a.inner.Delete(ctx, key)
}

func (a renderingObjectAdapter) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	return a.inner.Open(ctx, key)
}

// structuredRuntimeAdapter 把 legacy AIRuntime 适配到 ai.StructuredRuntime。
type structuredRuntimeAdapter struct{ runtime *provider.AIRuntime }

func (a structuredRuntimeAdapter) Structured(ctx context.Context, request providerai.StructuredRequest) (providerai.StructuredResult, error) {
	legacy, err := a.runtime.StructuredCompatible(ctx, request)
	return legacy, err
}

// WireRendering 组装渲染 API/Worker 依赖;最终中央注册由 Cutover 完成。
func WireRendering(cfg config.Config, store *postgres.Store, objects storage.ObjectStorage,
	modelCaller providerai.ModelImageCaller, qualityRuntime providerai.StructuredRuntime,
	qualityPolicy rendering.QualityPolicy,
) (*RenderingBundle, error) {
	if err := validateRenderingRoutes(cfg); err != nil {
		return nil, err
	}
	// worker 事务方法是 *Store 的方法;预算由 definition 定义,bootstrap 校验一致。
	def := RenderingDefinition()
	router := providerai.NewRouter(
		providerai.CapabilityFullLookGeneration,
		renderRoutePrimary(cfg, providerai.CapabilityFullLookGeneration),
		renderRouteFallbacks(cfg, providerai.CapabilityFullLookGeneration),
		renderRouterModels(cfg),
		modelCaller,
	)
	renderObjects := renderingObjectAdapter{inner: storage.NewRenderObjectStore(objects)}

	svc := rendering.New(store, router, rendering.NewJPEGNormalizer(), renderObjects, rendering.Config{
		RoutingPolicyVersion: aiRoutingVersion(cfg),
		QualityPolicyVersion: qualityPolicy.Version,
	}).WithQualityGate(rendering.NewQualityGate(providerai.NewStructuredQualityEvaluator(qualityRuntime), qualityPolicy))

	handler := svc.Handler()
	registry, err := taskrunner.NewRegistry(
		[]taskrunner.Definition{def},
		[]taskrunner.Handler{handler},
	)
	if err != nil {
		return nil, err
	}
	return &RenderingBundle{
		Service: svc, Handler: handler, Registry: registry, Definition: def,
	}, nil
}

func validateRenderingRoutes(cfg config.Config) error {
	if cfg.Environment != "production" {
		return nil
	}
	for _, capability := range []string{
		providerai.CapabilityFullLookGeneration,
		providerai.CapabilityRenderQualityEvaluation,
	} {
		if _, ok := cfg.AIRouting.Routes[capability]; !ok {
			return fmt.Errorf("production AI routing requires %q", capability)
		}
	}
	return nil
}

func renderRoutePrimary(cfg config.Config, capability string) string {
	if route, ok := cfg.AIRouting.Routes[capability]; ok {
		return route.Primary
	}
	return ""
}

func renderRouteFallbacks(cfg config.Config, capability string) []string {
	if route, ok := cfg.AIRouting.Routes[capability]; ok {
		return route.Fallbacks
	}
	return nil
}

// renderRouterModels 把 config 模型元数据映射到 Router 的硬性条件模型。
func renderRouterModels(cfg config.Config) map[string]providerai.RouterModel {
	models := make(map[string]providerai.RouterModel, len(cfg.AIRouting.Models))
	for key, model := range cfg.AIRouting.Models {
		models[key] = providerai.RouterModel{
			Key:                             key,
			MaxInputImages:                  model.MaxInputImages,
			SupportsMultipleReferenceImages: model.SupportsMultipleReferenceImages,
			IdentityPreservation:            model.IdentityPreservation,
			IdentityComparison:              model.IdentityComparison,
			OutputMIMETypes:                 model.OutputMIMETypes,
			DataRetention:                   model.DataRetention,
		}
	}
	return models
}
