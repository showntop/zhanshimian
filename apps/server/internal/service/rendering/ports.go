package rendering

import (
	"context"
	"encoding/json"
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

// Repository is the rendering persistence surface.
type Repository interface {
	GetRenderSpecForVariant(ctx context.Context, userID, planVariantID string) (domain.RenderSpec, error)
	CreateRun(ctx context.Context, command CreateRunCommand) (CreateRunResult, error)
	GetRun(ctx context.Context, userID, runID string) (domain.RenderRun, *domain.RenderPublication, domain.Operation, error)
	ListCurrentByVariantIDs(ctx context.Context, userID string, variantIDs []string) (map[string]CurrentRender, error)
	GetCandidateJob(ctx context.Context, userID, runID string, ordinal int) (CandidateJob, error)
	RecordCandidate(ctx context.Context, command RecordCandidateCommand) (domain.RenderCandidate, error)
	ExpandCandidateBudget(ctx context.Context, command EnqueueNextCandidateCommand) (string, error)
	FailRun(ctx context.Context, command FailRunCommand) error
	CommitEvaluation(ctx context.Context, command CommitEvaluationCommand) (CommitEvaluationResult, error)
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

// CandidateAsset 是候选对象入库所需的元数据。
type CandidateAsset struct {
	ObjectKey string
	SHA256    string
	MIMEType  string
	ByteSize  int64
	Width     int
	Height    int
}

// QualityInput is everything the quality gate may see: normalized candidate
// bytes, the two references and the validated spec.
type QualityInput struct {
	Run       domain.RenderRun
	Candidate domain.RenderCandidate
	Image     NormalizedJPEG
	Body      providerai.ImageInput
	Face      providerai.ImageInput
	Spec      domain.RenderDirective
}

// QualityResult is one immutable quality decision with internal-only scores.
type QualityResult struct {
	Decision              string
	ReasonCodes           []string
	InternalScores        json.RawMessage
	EvaluatorInvocationID string
	CompletedStages       int
}

// CandidateJob carries everything one candidate generation needs.
type CandidateJob struct {
	Run        domain.RenderRun
	Spec       domain.RenderSpec
	Body       providerai.ImageInput
	Face       providerai.ImageInput
	Previous   *domain.QualityEvaluation
	RouteState providerai.RouteState
}

// RecordCandidateCommand atomically persists a quarantined candidate under
// lease + generation CAS.
type RecordCandidateCommand struct {
	TaskID               string
	LeaseToken           string
	UserID               string
	RenderRunID          string
	SubjectGeneration    int
	Ordinal              int
	Asset                CandidateAsset
	ProviderInvocationID string
}

// EnqueueNextCandidateCommand expands the budget to 2 and enqueues the
// second candidate.
type EnqueueNextCandidateCommand struct {
	TaskID            string
	LeaseToken        string
	UserID            string
	RenderRunID       string
	SubjectGeneration int
	Quality           domain.QualityEvaluation
}

// FailRunCommand terminally fails a run and its operation.
type FailRunCommand struct {
	TaskID            string
	LeaseToken        string
	UserID            string
	RenderRunID       string
	SubjectGeneration int
	Outcome           string // failed | unavailable
	ErrorCode         string
	Retryable         bool
}

// CommitEvaluationCommand publishes or rejects in one dual-CAS transaction.
type CommitEvaluationCommand struct {
	TaskID            string
	LeaseToken        string
	UserID            string
	RenderRunID       string
	SubjectGeneration int
	CandidateID       string
	Evaluation        QualityResult
	PublishedObject   *StoredObject
	PublicationID     string
}

type CommitEvaluationResult struct {
	Outcome        string
	Publication    *domain.RenderPublication
	EnqueuedTaskID string
}
