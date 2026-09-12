package planning

import (
	"context"
	"encoding/json"

	"github.com/zhanshimian/server/internal/domain"
)

// PlanSetGenerationTaskType is the only internal task type Planning registers.
const PlanSetGenerationTaskType domain.TaskType = "plan_set.generate"

// Operation progress stages for the public plan_set operation.
const (
	StagePlanReadingReport = "plan.reading_report"
	StagePlanChecking      = "plan.checking"
	StagePlanReady         = "plan.ready"

	ProgressPlanReading = 1500
	ProgressPlanChecking = 6500
	ProgressPlanReady    = 10000
)

// ReportReader reads the published, immutable report a plan set is built on.
type ReportReader interface {
	GetPlanningReport(ctx context.Context, userID, reportID string) (ReportSnapshot, error)
}

// OperationStarter inserts the public operation and its initial content task
// in one transaction, idempotent on (user_id, kind, dedupe_key). The bool
// reports whether this call created the pair.
type OperationStarter interface {
	StartWithTask(ctx context.Context, command StartOperationCommand) (domain.OperationRef, bool, error)
}

// TaskEnqueuer records the first rejected quality attempt, closes the first
// content task, flips the operation to retrying and inserts the second
// content task — all inside one lease-CAS transaction.
type TaskEnqueuer interface {
	EnqueueRetry(ctx context.Context, lease domain.TaskLease, command EnqueueRetryCommand) (domain.CommitOutcome, error)
}

// OperationWriter advances or fails the public operation under lease CAS.
// Both methods report whether the lease CAS took effect.
type OperationWriter interface {
	MarkRunning(ctx context.Context, lease domain.TaskLease, progressBPS int, stageCode, publicMessage string) (bool, error)
	Fail(ctx context.Context, lease domain.TaskLease, quality PlanQualityRecord, code, publicMessage string, retryable bool) (bool, error)
}

// PlanSetStore owns the immutable plan set graph and its publication state.
type PlanSetStore interface {
	FindPublished(ctx context.Context, key PlanSetKey) (domain.PlanSet, bool, error)
	Get(ctx context.Context, userID, planSetID string) (domain.PlanSet, error)
	List(ctx context.Context, userID, reportID string, scene *domain.Scene) ([]domain.PlanSet, error)
	Prepare(ctx context.Context, lease domain.TaskLease, command PrepareCommand) (domain.PlanSet, error)
	CommitPrepared(ctx context.Context, lease domain.TaskLease, result domain.TaskResult) (domain.CommitOutcome, error)
}

// PlanSetGenerator calls the plan_set_generation capability.
type PlanSetGenerator interface {
	Generate(ctx context.Context, input GenerationInput) (GeneratedPlanSet, error)
}

// PlanSetVerifier calls the plan_grounding_verification capability.
type PlanSetVerifier interface {
	Verify(ctx context.Context, input VerificationInput) (VerificationResult, error)
}

// RenderSpecReader is the frozen hand-off to the Rendering plan.
type RenderSpecReader interface {
	GetRenderSpecForVariant(ctx context.Context, userID, planVariantID string) (domain.RenderSpec, error)
}

// ReportSnapshot is the immutable report view Planning plans against. The
// profile snapshot is the one captured at report publication — Planning never
// re-reads the mutable profile table.
type ReportSnapshot struct {
	ID                string
	UserID            string
	PhotoSetID        string
	FaceAssetID       string
	BodyAssetID       string
	ProfileSnapshot   json.RawMessage
	ImpressionTags    []string
	PriorityTitle     string
	PriorityCopy      string
	PriorityFindingID string
	Findings          []FindingSnapshot
}

type FindingSnapshot struct {
	ID                 string
	Category           string
	Priority           int
	Label              string
	VisibleObservation string
	Recommendation     string
}

type StartOperationCommand struct {
	OperationID    string
	UserID         string
	Kind           domain.OperationKind
	SubjectType    string
	SubjectID      string
	IdempotencyKey string
	DedupeKey      string
	Task           EnqueueTask
}

type EnqueueTask struct {
	Type              domain.TaskType
	SubjectType       string
	SubjectID         string
	SubjectGeneration int
	PayloadVersion    int
	Payload           any
	DedupeKey         string
}

// PlanSetKey is the semantic identity of one planning request. Equal keys
// must reuse the same published result without calling AI again.
type PlanSetKey struct {
	UserID               string
	ReportID             string
	Scene                domain.Scene
	BriefHash            string
	PlannerSchemaVersion string
}

type CreateCommand struct {
	UserID         string
	ReportID       string
	Scene          domain.Scene
	Answers        map[string]string
	IdempotencyKey string
}

// GenerateTaskPayload is the versioned task payload of plan_set.generate.
type GenerateTaskPayload struct {
	PlanSetID            string             `json:"plan_set_id"`
	ReportID             string             `json:"report_id"`
	Scene                domain.Scene       `json:"scene"`
	Brief                domain.SceneBrief  `json:"brief"`
	BriefHash            string             `json:"brief_hash"`
	PlannerSchemaVersion string             `json:"planner_schema_version"`
	StyleRuleVersion     string             `json:"style_rule_version"`
	ContentAttempt       int                `json:"content_attempt"`
	PriorReasonCodes     []string           `json:"prior_reason_codes"`
}

type EnqueueRetryCommand struct {
	UserID        string
	OperationID   string
	ProgressBPS   int
	StageCode     string
	PublicMessage string
	Quality       PlanQualityRecord
	Task          EnqueueTask
}

type GenerationInput struct {
	Report           ReportSnapshot
	Brief            domain.SceneBrief
	ContentAttempt   int
	PriorReasonCodes []string
}

type GeneratedPlanSet struct {
	InvocationID string
	Variants     []GeneratedPlanVariant
}

type GeneratedPlanVariant struct {
	Slot           int
	Key            domain.PlanVariantKey
	Name           string
	Descriptor     string
	Rationale      string
	Recommended    bool
	OutcomeTags    []string
	DifferenceTags []string
	Steps          []GeneratedPlanStep
}

type GeneratedPlanStep struct {
	Category   domain.StepCategory
	Action     domain.StepAction
	Title      string
	Summary    string
	Details    domain.PlanStepDetails
	Groundings []GeneratedGrounding
}

type GeneratedGrounding struct {
	SourceType domain.GroundingSourceType
	SourceID   string
	Reason     string
}

type VerificationInput struct {
	Report    ReportSnapshot
	Brief     domain.SceneBrief
	Candidate GeneratedPlanSet
}

type VerificationResult struct {
	InvocationID string
	Decision     string
	ReasonCodes  []string
	Violations   []string
}

// PlanQualityRecord is the gate decision persisted with the plan set.
type PlanQualityRecord struct {
	ID                    string
	UserID                string
	SubjectID             string
	PolicyVersion         string
	Decision              string
	ReasonCodes           []string
	InternalScores        json.RawMessage
	EvaluatorInvocationID string
}

type PrepareCommand struct {
	UserID      string
	PlanSet     domain.PlanSet
	Quality     PlanQualityRecord
	RenderSpecs []domain.RenderSpec
}

type CreateResult struct {
	PlanSetID string
	Accepted  bool
	Operation domain.OperationRef
	PlanSet   *domain.PlanSet
}
