package planning

import (
	"context"

	"github.com/zhanshimian/server/internal/domain"
)

// PlanSetGenerationTaskType is the only internal task type Planning registers.
const PlanSetGenerationTaskType domain.TaskType = "plan_set.generate"

// Operation progress stages for the public plan_set operation.
const (
	StagePlanReadingReport = "plan.reading_report"
	StagePlanChecking      = "plan.checking"
	StagePlanReady         = "plan.ready"

	ProgressPlanReading  = 1500
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

// CurrentRenderReader 只读各 variant 当前渲染投影(R9 接入 PlanSet 读模型)。
type CurrentRenderReader interface {
	ListCurrentByVariantIDs(ctx context.Context, userID string, variantIDs []string) (map[string]domain.RenderRunView, error)
}

// The cross-boundary DTOs below live in domain (frozen contract shared with
// the postgres adapter); planning re-exposes them under the frozen names.
type (
	ReportSnapshot        = domain.PlanningReportSnapshot
	FindingSnapshot       = domain.PlanningFindingSnapshot
	PlanSetKey            = domain.PlanningPlanSetKey
	StartOperationCommand = domain.PlanningStartOperationCommand
	EnqueueTask           = domain.PlanningEnqueueTask
	EnqueueRetryCommand   = domain.PlanningEnqueueRetryCommand
	PlanQualityRecord     = domain.PlanningPlanQualityRecord
	PrepareCommand        = domain.PlanningPrepareCommand
)

type CreateCommand struct {
	UserID         string
	ReportID       string
	Scene          domain.Scene
	Answers        map[string]string
	IdempotencyKey string
}

// GenerateTaskPayload is the versioned task payload of plan_set.generate.
type GenerateTaskPayload struct {
	PlanSetID            string            `json:"plan_set_id"`
	ReportID             string            `json:"report_id"`
	Scene                domain.Scene      `json:"scene"`
	Brief                domain.SceneBrief `json:"brief"`
	BriefHash            string            `json:"brief_hash"`
	PlannerSchemaVersion string            `json:"planner_schema_version"`
	StyleRuleVersion     string            `json:"style_rule_version"`
	ContentAttempt       int               `json:"content_attempt"`
	PriorReasonCodes     []string          `json:"prior_reason_codes"`
}

type GenerationInput struct {
	Report           ReportSnapshot
	Brief            domain.SceneBrief
	ContentAttempt   int
	PriorReasonCodes []string
}

type GeneratedPlanSet struct {
	InvocationID string                 `json:"-"`
	Variants     []GeneratedPlanVariant `json:"variants"`
}

type GeneratedPlanVariant struct {
	Slot           int                   `json:"slot"`
	Key            domain.PlanVariantKey `json:"key"`
	Name           string                `json:"name"`
	Descriptor     string                `json:"descriptor"`
	Rationale      string                `json:"rationale"`
	Recommended    bool                  `json:"recommended"`
	OutcomeTags    []string              `json:"outcome_tags"`
	DifferenceTags []string              `json:"difference_tags"`
	Steps          []GeneratedPlanStep   `json:"steps"`
}

type GeneratedPlanStep struct {
	Category   domain.StepCategory    `json:"category"`
	Action     domain.StepAction      `json:"action"`
	Title      string                 `json:"title"`
	Summary    string                 `json:"summary"`
	Details    domain.PlanStepDetails `json:"details"`
	Groundings []GeneratedGrounding   `json:"groundings"`
}

type GeneratedGrounding struct {
	SourceType domain.GroundingSourceType `json:"source_type"`
	SourceID   string                     `json:"source_id"`
	Reason     string                     `json:"reason"`
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

type CreateResult struct {
	PlanSetID string
	Accepted  bool
	Operation domain.OperationRef
	PlanSet   *domain.PlanSet
}
