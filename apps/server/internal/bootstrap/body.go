package bootstrap

import (
	"context"
	"log/slog"
	"time"

	"github.com/zhanshimian/server/internal/media"
	"github.com/zhanshimian/server/internal/provider"
	"github.com/zhanshimian/server/internal/repository/postgres"
	"github.com/zhanshimian/server/internal/service/body"
	"github.com/zhanshimian/server/internal/service/taskrunner"
	"github.com/zhanshimian/server/internal/storage"
)

// BodyBundle 是 3D 形象 Lite（body_orbit）的组装产物：API 侧取 Service，
// Worker 侧取 Definition + Handler。
type BodyBundle struct {
	Service    *body.Service
	Definition taskrunner.Definition
	Handler    *body.Handler
}

// BodyDefinition 与旧 taskMaxAttempts/taskTimeouts 一致：视频生成慢，
// 单次尝试给 5 分钟，最多 2 次。
func BodyDefinition() taskrunner.Definition {
	return taskrunner.Definition{
		Type:           body.TaskTypeBodyOrbit,
		MaxAttempts:    2,
		Timeout:        5 * time.Minute,
		LeaseDuration:  30 * time.Second,
		HeartbeatEvery: 10 * time.Second,
		Concurrency:    1,
		RetryBackoff:   taskrunner.ExponentialBackoff(time.Second, time.Minute),
	}
}

// WireBody 组装 body 服务与 worker handler。generator 为 nil 时能力视为未
// 开放：Service.Create 返回 ErrCapabilityUnavailable，Handler 落
// body_orbit_not_configured 域失败（不 panic、不重试）。
func WireBody(store *postgres.Store, objects storage.ObjectStorage, signer body.URLSigner, billing body.Billing, generator provider.OrbitGenerator, maxAttempts int, logger *slog.Logger) *BodyBundle {
	def := BodyDefinition()
	if maxAttempts > 0 {
		def.MaxAttempts = maxAttempts
	}
	adapted := bodyGenerator(generator)
	return &BodyBundle{
		Service:    body.New(store, store, signer, billing, adapted, def.MaxAttempts),
		Definition: def,
		Handler: body.NewHandler(store, objects, operationProgress{store: store},
			adapted, media.NewFFMPEGExtractor(), logger),
	}
}

// bodyGenerator 把 provider 层生成器适配到 body.Generator；未配置时返回
// nil，服务层据此按「能力未开放」处理（Available()=false）。
func bodyGenerator(g provider.OrbitGenerator) body.Generator {
	if g == nil {
		return nil
	}
	return orbitGeneratorAdapter{inner: g}
}

// orbitGeneratorAdapter 把 provider 层生成器适配到 body.Generator；inner 为
// nil 时适配结果也为 nil（服务层按能力未开放处理）。
type orbitGeneratorAdapter struct {
	inner provider.OrbitGenerator
}

func (a orbitGeneratorAdapter) Generate(ctx context.Context, input body.OrbitInput) (body.OrbitOutput, error) {
	out, err := a.inner.Generate(ctx, provider.OrbitInput{
		Body: input.Body, Face: input.Face, BodyMIME: input.BodyMIME, FaceMIME: input.FaceMIME,
	})
	if err != nil {
		return body.OrbitOutput{}, err
	}
	return body.OrbitOutput{
		VideoData: out.VideoData, MIMEType: out.MIMEType,
		Duration: out.Duration, ProviderVersion: out.ProviderVersion,
	}, nil
}

// bodyURLSigner 优先走 COS 签名；本地存储没有签名能力时退化为
// PUBLIC_BASE_URL + /uploads/ 的公开路径（开发环境）。
type bodyURLSigner struct {
	signer        storage.SignedURLStorage
	ttl           time.Duration
	publicBaseURL string
}

func (s bodyURLSigner) Sign(ctx context.Context, objectKey string) (string, error) {
	if s.signer != nil {
		return s.signer.SignedURL(ctx, objectKey, s.ttl)
	}
	return s.publicBaseURL + "/uploads/" + objectKey, nil
}
