package rendering

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/zhanshimian/server/internal/domain"
)

// ErrValidation marks an invalid render request or spec: nothing was created.
var ErrValidation = errors.New("rendering validation error")

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
	created, err := s.repo.CreateRun(ctx, CreateRunCommand{
		UserID:               cmd.UserID,
		PlanVariantID:        cmd.PlanVariantID,
		RenderSpecID:         spec.ID,
		IdempotencyKey:       cmd.IdempotencyKey,
		RoutingPolicyVersion: s.config.RoutingPolicyVersion,
		QualityPolicyVersion: s.config.QualityPolicyVersion,
	})
	return StartRunResult{Run: created.Run, Operation: created.Operation}, err
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
