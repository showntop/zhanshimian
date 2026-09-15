package rendering

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/limits"
	"github.com/zhanshimian/server/internal/service/billing"
)

// ErrValidation marks an invalid render request or spec: nothing was created.
var ErrValidation = errors.New("rendering validation error")

// 用量限额（与 legacy billing_rules decideLook 一致）：look 8 次/日 + 在途并发 2。
// USAGE_RENDER_RUNS_PER_DAY / USAGE_RENDER_CONCURRENCY 可覆盖，0 = 不限（内测用）。
var (
	limitRenderRunsPerDay = limits.FromEnv(limits.EnvRenderRunsPerDay, 8)
	limitLooksConcurrent  = limits.FromEnv(limits.EnvRenderConcurrency, 2)
)

// lookConcurrencySubjects 是在途并发计数口径：render_run（方案效果图）、
// hair_preview（发型预览）、body_presentation（3D 形象）对应旧线四类 look
// 任务中的三类；today_plan 渲染占位 operation 当前没有 worker 推进、永不
// 终态，计入会把并发槽永久占满，故排除（旧线 today_look 是真实会终态的任务）。
var lookConcurrencySubjects = []string{"render_run", "hair_preview", "body_presentation"}

// Service carries the rendering use cases: idempotent run creation, public
// run reads, and the worker-side candidate orchestration (handler.go).
type Service struct {
	repo       Repository
	generator  ImageGenerator
	normalizer JPEGNormalizer
	objects    RenderObjectStore
	gate       QualityGate
	config     Config
	signer     func(ctx context.Context, key string, ttl time.Duration) (string, error)
	billing    billing.Reserver
	usage      UsageCounter
}

func New(
	repo Repository,
	generator ImageGenerator,
	normalizer JPEGNormalizer,
	objects RenderObjectStore,
	config Config,
) *Service {
	if config.AssetURLTTL == 0 {
		config.AssetURLTTL = AssetURLTTL
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.NewIDs == nil {
		config.NewIDs = uuid.NewString
	}
	return &Service{repo: repo, generator: generator, normalizer: normalizer, objects: objects, config: config}
}

// WithQualityGate attaches the worker-side quality gate.
func (s *Service) WithQualityGate(gate QualityGate) *Service {
	s.gate = gate
	return s
}

// WithBilling attaches the reserve-only billing port used to charge a new
// render run at creation time. Nil is tolerated so the service still runs
// without billing wired.
func (s *Service) WithBilling(b billing.Reserver) *Service {
	s.billing = b
	return s
}

// WithUsageLimits 装配用量闸（与 WithBilling 同一链式做法）。限额先于扣费：
// 超限直接 429，不创建 run、不 Reserve。Nil 容忍（单测/降级组装）。
func (s *Service) WithUsageLimits(counter UsageCounter) *Service {
	s.usage = counter
	return s
}

// checkUsageLimits 恢复旧线 decideLook 的双闸：先日限（服务器本地自然日
// 计数今日已创建 render_run），后在途并发（非终态 look operation）。
func (s *Service) checkUsageLimits(ctx context.Context, userID string) error {
	if s.usage == nil {
		return nil
	}
	now := s.config.Now()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	created, err := s.usage.CountOperationsCreatedSince(ctx, userID,
		[]domain.OperationKind{domain.OperationRender}, []string{"render_run"}, dayStart)
	if err != nil {
		return err
	}
	if limitRenderRunsPerDay > 0 && created >= limitRenderRunsPerDay {
		return fmt.Errorf("%w: 今日形象方案制作次数已用完，明天再来", billing.ErrRateLimited)
	}
	active, err := s.usage.CountActiveOperations(ctx, userID, lookConcurrencySubjects)
	if err != nil {
		return err
	}
	if limitLooksConcurrent > 0 && active >= limitLooksConcurrent {
		return fmt.Errorf("%w: 请等待当前形象方案制作完成后再试", billing.ErrRateLimited)
	}
	return nil
}

// StartRun validates the variant's RenderSpec and idempotently creates the
// run plus its first candidate task. An invalid spec creates nothing.
func (s *Service) StartRun(ctx context.Context, cmd StartRunCommand) (StartRunResult, error) {
	if cmd.UserID == "" || cmd.PlanVariantID == "" || cmd.IdempotencyKey == "" {
		return StartRunResult{}, fmt.Errorf("%w: user, variant and idempotency key are required", ErrValidation)
	}
	spec, err := s.repo.GetRenderSpecForVariant(ctx, cmd.UserID, cmd.PlanVariantID)
	if err != nil {
		return StartRunResult{}, err
	}
	if err := validateStartSpec(spec); err != nil {
		return StartRunResult{}, err
	}
	// 限额先于落库与扣费：超限不创建 run、不 Reserve（旧线 authorize 次序）。
	if err := s.checkUsageLimits(ctx, cmd.UserID); err != nil {
		return StartRunResult{}, err
	}
	created, err := s.repo.CreateRun(ctx, CreateRunCommand{
		UserID:               cmd.UserID,
		PlanVariantID:        cmd.PlanVariantID,
		RenderSpecID:         spec.ID,
		IdempotencyKey:       cmd.IdempotencyKey,
		RoutingPolicyVersion: s.config.RoutingPolicyVersion,
		QualityPolicyVersion: s.config.QualityPolicyVersion,
	})
	if err != nil {
		return StartRunResult{}, err
	}
	if created.Created && s.billing != nil {
		if _, err := s.billing.Reserve(ctx, cmd.UserID, created.Operation.ID, domain.ProductRenderPublication, 1); err != nil {
			return StartRunResult{}, err
		}
	}
	return StartRunResult{Run: created.Run, Operation: created.Operation}, nil
}

// validateStartSpec enforces the fail-closed rendering contract before any
// operation exists.
func validateStartSpec(spec domain.RenderSpec) error {
	directive := spec.Spec
	identity := directive.Identity
	if identity.BodyAssetID == "" || identity.FaceAssetID == "" {
		return fmt.Errorf("%w: render spec misses identity assets", ErrValidation)
	}
	if identity.BodyAssetID == identity.FaceAssetID {
		return fmt.Errorf("%w: body and face assets must differ", ErrValidation)
	}
	if !identity.PreserveIdentity || !identity.PreserveBodyProportion || !identity.PreserveSkinTone || !identity.PreserveAgeImpression {
		return fmt.Errorf("%w: identity preservation must be fail-closed", ErrValidation)
	}
	if directive.Composition.AllowOutpaint {
		return fmt.Errorf("%w: outpainting is not allowed", ErrValidation)
	}
	if directive.Output.MIMEType != "image/jpeg" || directive.Output.AspectPolicy != "preserve_body_source" {
		return fmt.Errorf("%w: unsafe output policy", ErrValidation)
	}
	return nil
}

// GetRun projects the public run view and signs the published URL on ready.
func (s *Service) GetRun(ctx context.Context, userID, runID string) (domain.RenderRunView, error) {
	run, publication, operation, err := s.repo.GetRun(ctx, userID, runID)
	if err != nil {
		return domain.RenderRunView{}, err
	}
	if publication != nil {
		signed, expires, err := s.signPublication(ctx, publication)
		if err != nil {
			return domain.RenderRunView{}, err
		}
		publication.URL = signed
		publication.URLExpiresAt = expires
	}
	return domain.NewRenderRunView(run, publication, operation), nil
}

// ListCurrentByVariantIDs batches the current render projection for a plan
// set. Variants without a render head stay absent from the map.
func (s *Service) ListCurrentByVariantIDs(ctx context.Context, userID string, variantIDs []string) (map[string]domain.RenderRunView, error) {
	requested := make(map[string]bool, len(variantIDs))
	for _, id := range variantIDs {
		requested[id] = true
	}
	current, err := s.repo.ListCurrentByVariantIDs(ctx, userID, variantIDs)
	if err != nil {
		return nil, err
	}
	views := make(map[string]domain.RenderRunView, len(current))
	for variantID, item := range current {
		if !requested[variantID] {
			return nil, fmt.Errorf("render read returned unrequested variant %s", variantID)
		}
		publication := item.Publication
		if publication != nil {
			signed, expires, err := s.signPublication(ctx, publication)
			if err != nil {
				return nil, err
			}
			publication.URL = signed
			publication.URLExpiresAt = expires
		}
		views[variantID] = domain.NewRenderRunView(item.Run, publication, item.Operation)
	}
	return views, nil
}

func (s *Service) signPublication(ctx context.Context, publication *domain.RenderPublication) (string, time.Time, error) {
	if s.signer == nil {
		return publication.URL, publication.URLExpiresAt, nil
	}
	signed, err := s.signer(ctx, publication.ObjectKey, s.config.AssetURLTTL)
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, s.config.Now().Add(s.config.AssetURLTTL), nil
}

// RunSigner registers the URL signing hook used to sign published objects.
func (s *Service) RunSigner(signer func(ctx context.Context, key string, ttl time.Duration) (string, error)) {
	s.signer = signer
}

// ErrSuperseded marks a run whose generation was replaced while in flight.
var ErrSuperseded = errors.New("render run superseded")

// RunIdempotencyKey derives the semantic dedupe key of the first candidate
// task from the API idempotency key.
func RunIdempotencyKey(userID, variantID, idempotencyKey string) string {
	return domain.RenderRunIdempotencyKey(userID, variantID, idempotencyKey)
}
