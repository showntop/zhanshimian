package bootstrap

import (
	"context"
	"io"
	"time"

	"github.com/zhanshimian/server/internal/config"
	"github.com/zhanshimian/server/internal/provider"
	providerai "github.com/zhanshimian/server/internal/provider/ai"
	"github.com/zhanshimian/server/internal/repository/postgres"
	"github.com/zhanshimian/server/internal/service/assessment"
	"github.com/zhanshimian/server/internal/service/body"
	"github.com/zhanshimian/server/internal/service/hair"
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
	presenter := newMediaPresenter(objects, cfg)
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

	// 渲染 Service 即 Planning 的渲染只读端口:方案读模型经它合并各 variant
	// 的当前渲染状态(含签名媒体 URL)。
	planningBundle, err := WirePlanning(cfg, store, aiRuntime, renderingBundle.Service, NewPlanningRenderStarter(renderingBundle.Service))
	if err != nil {
		return nil, err
	}

	// 3D 形象 Lite：计费在 API 侧（WithBilling）挂上，见 api.go。
	bodyBundle := WireBody(store, objects, bodySigner(objects, cfg), nil, ai.Orbit, 0, nil)

	hairHandler := wireHairHandler(store, objects, ai)

	registry, err := taskrunner.NewRegistry(
		[]taskrunner.Definition{assessmentBundle.Definition, planningBundle.Definition, renderingBundle.Definition, bodyBundle.Definition, HairDefinition()},
		[]taskrunner.Handler{assessmentBundle.Handler, planningBundle.Handler, renderingBundle.Handler, bodyBundle.Handler, hairHandler},
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

// HairDefinition 与渲染同一预算：图像生成慢，单次尝试 5 分钟，最多 3 次。
func HairDefinition() taskrunner.Definition {
	return taskrunner.Definition{
		Type:           hair.TaskTypeHairPreview,
		MaxAttempts:    hair.TaskMaxAttempts,
		Timeout:        300 * time.Second,
		LeaseDuration:  90 * time.Second,
		HeartbeatEvery: 30 * time.Second,
		Concurrency:    2,
		RetryBackoff:   taskrunner.ExponentialBackoff(5*time.Second, time.Minute),
	}
}

// wireHairHandler 组装发型预览的 worker handler。hair_edit 路由未配置时
// generator 为 nil：handler 落 hair_capability_unavailable 域失败
// （不 panic、不重试），与 body 能力未开放同一处理。
func wireHairHandler(store *postgres.Store, objects storage.ObjectStorage, ai AIBundle) *hair.Handler {
	var generator hair.Generator
	if ai.Runtime.HasRoute(provider.CapabilityHairEdit) {
		generator = hairEditGenerator{runtime: ai.Runtime}
	}
	return hair.NewHandler(store, objects, hairSourceLoader{objects: objects}, operationProgress{store: store},
		generator, hairNormalizer{decoder: rendering.NewJPEGNormalizerWithAspect(4, 3)})
}

// hairSourceLoader 读 hair 源图并按编辑预算约束（与渲染参考图同一做法：
// 数据万象下载时压缩 + 本地预算兜底）。
type hairSourceLoader struct{ objects storage.ObjectStorage }

func (l hairSourceLoader) Load(ctx context.Context, work hair.PreviewWork) (hair.SourceImage, error) {
	reader, err := storage.OpenProcessedOr(ctx, l.objects, work.SourceObjectKey, providerai.EditCOSProcess)
	if err != nil {
		return hair.SourceImage{}, err
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(reader)
	if err != nil {
		return hair.SourceImage{}, err
	}
	declared := providerai.SniffImageMIME(data, work.SourceMIMEType)
	data, mime := providerai.ConstrainEditImage(data, declared)
	return hair.SourceImage{MIMEType: mime, Data: data}, nil
}

// hairEditGenerator 把 legacy AIRuntime 的能力路由调用适配到 hair.Generator：
// primary + fallbacks + 成本上限由 runtime 的路由循环执行，业务侧不出现
// 厂商/模型名（红线：AI 只经能力路由）。
type hairEditGenerator struct{ runtime *provider.AIRuntime }

func (g hairEditGenerator) Generate(ctx context.Context, input hair.GenerateInput) (hair.GenerateOutput, error) {
	result, err := g.runtime.EditImage(ctx, provider.CapabilityHairEdit, provider.ImageEditRequest{
		Prompt: input.Prompt,
		Images: []provider.AnalysisImage{{Kind: "face", MIMEType: input.Face.MIMEType, Data: input.Face.Data}},
	})
	if err != nil {
		return hair.GenerateOutput{}, err
	}
	return hair.GenerateOutput{
		Data: result.Data, MIMEType: result.MIMEType, InvocationID: result.Meta.InvocationID,
	}, nil
}

// hairNormalizer 把渲染的 JPEG 归一化器适配到 hair.ImageNormalizer。
type hairNormalizer struct{ decoder *rendering.Decoder }

func (n hairNormalizer) Normalize(data []byte, declaredMIME string) (hair.NormalizedImage, error) {
	normalized, err := n.decoder.Normalize(data, declaredMIME)
	if err != nil {
		return hair.NormalizedImage{}, err
	}
	return hair.NormalizedImage{
		Data: normalized.Data, MIMEType: normalized.MIMEType, SHA256: normalized.SHA256,
		ByteSize: normalized.ByteSize, Width: normalized.Width, Height: normalized.Height,
	}, nil
}
