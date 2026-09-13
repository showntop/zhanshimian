package rendering

import (
	"context"
	"io"
	"time"

	providerai "github.com/zhanshimian/server/internal/provider/ai"

	"github.com/zhanshimian/server/internal/domain"
)

// Task type and public stage codes for the rendering workflow.
const (
	RenderGenerateTaskType domain.TaskType = "render_candidate_generate"

	StageRenderGenerating = "render.generating"
	StageRenderChecking   = "render.checking"

	ProgressRenderGenerating = 2500
	ProgressRenderChecking   = 7000

	// AssetURLTTL is how long public render URLs stay signed.
	AssetURLTTL = 15 * time.Minute
)

// The cross-boundary DTOs live in domain (shared with the postgres adapter);
// rendering re-exposes them under local names.
type (
	CreateRunCommand         = domain.RenderCreateRunCommand
	CreateRunResult          = domain.RenderCreateRunResult
	CurrentRender            = domain.CurrentRender
	GenerateCandidatePayload = domain.RenderGenerateCandidatePayload
)

// Repository is the run-creation/read persistence surface. The worker-side
// candidate and publication methods join in handler.go / policy.go stages.
type Repository interface {
	GetRenderSpecForVariant(ctx context.Context, userID, planVariantID string) (domain.RenderSpec, error)
	CreateRun(ctx context.Context, command CreateRunCommand) (CreateRunResult, error)
	GetRun(ctx context.Context, userID, runID string) (domain.RenderRun, *domain.RenderPublication, domain.Operation, error)
	ListCurrentByVariantIDs(ctx context.Context, userID string, variantIDs []string) (map[string]CurrentRender, error)
}

// ImageGenerator produces candidate bytes from a validated spec.
type ImageGenerator interface {
	Generate(ctx context.Context, request providerai.GenerationRequest) (providerai.GenerationResult, error)
}

// RenderObjectStore isolates quarantined candidate bytes from published ones.
type RenderObjectStore interface {
	PutCandidate(ctx context.Context, input CandidateObjectInput) (StoredObject, error)
	Promote(ctx context.Context, input PromoteObjectInput) (StoredObject, error)
	Delete(ctx context.Context, key string) error
	Open(ctx context.Context, key string) (io.ReadCloser, error)
}

// JPEGNormalizer converts provider bytes into clean JPEG.
type JPEGNormalizer interface {
	Normalize(data []byte, declaredMIME string) (NormalizedJPEG, error)
}

// ---- commands and results ----

type StartRunCommand struct {
	UserID         string
	PlanVariantID  string
	IdempotencyKey string
}

type StartRunResult struct {
	Run       domain.RenderRun
	Operation domain.OperationRef
}

type Config struct {
	RoutingPolicyVersion string
	QualityPolicyVersion string
	AssetURLTTL          time.Duration
	Now                  func() time.Time
	NewIDs               func() string
}

// StoredObject is one immutable object write result.
type StoredObject struct {
	Key      string
	SHA256   string
	ByteSize int64
}

type CandidateObjectInput struct {
	UserID      string
	RunID       string
	CandidateID string
	Data        []byte
	SHA256      string
}

type PromoteObjectInput struct {
	UserID         string
	PublicationID  string
	SourceKey      string
	ExpectedSHA256 string
}

// NormalizedJPEG is clean, re-encoded JPEG without provider metadata.
type NormalizedJPEG struct {
	Data     []byte
	MIMEType string
	SHA256   string
	ByteSize int64
	Width    int
	Height   int
}
