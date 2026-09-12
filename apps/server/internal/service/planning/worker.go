package planning

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/google/uuid"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/service/taskrunner"
)

// ErrQualityRejected marks the final content rejection: both the first
// candidate and the single content regeneration failed the quality gates.
var ErrQualityRejected = errors.New("plan quality rejected")

const (
	qualityDecisionPass   = "pass"
	qualityDecisionRetry  = "retry"
	qualityDecisionReject = "reject"

	codePlanQualityRejected = "plan_quality_rejected"
	retryPublicMessage      = "第一次方案未通过质量检查，正在再补一次"
)

// Handler implements the leased plan_set.generate task. Execute never ends
// the task or fails the operation on content rejections; it returns a
// disposition that Commit turns into the atomic store transition.
type Handler struct {
	deps HandlerDeps

	// pending carries the Execute-computed quality decision to Commit. The
	// runner invokes Commit synchronously after Execute for the same lease,
	// so an in-memory handoff is sufficient; a crash in between re-queues the
	// task and Execute runs again.
	pending sync.Map // lease token -> pendingDecision
}

type HandlerDeps struct {
	Reports    ReportReader
	Generator  PlanSetGenerator
	Verifier   PlanSetVerifier
	Operations OperationWriter
	Tasks      TaskEnqueuer
	Store      PlanSetStore
	NewIDs     func() string
}

func NewHandler(deps HandlerDeps) *Handler {
	return &Handler{deps: deps}
}

func (h *Handler) Type() domain.TaskType { return PlanSetGenerationTaskType }

type pendingDecision struct {
	quality PlanQualityRecord
	next    EnqueueTask
}

func (h *Handler) Execute(ctx context.Context, lease domain.TaskLease) (domain.TaskResult, error) {
	payload, err := decodeGeneratePayload(lease.Task.Payload)
	if err != nil {
		return domain.TaskResult{}, &taskrunner.TaskError{Class: domain.ErrorPermanent, Code: "plan_payload_invalid"}
	}
	report, err := h.deps.Reports.GetPlanningReport(ctx, lease.Task.UserID, payload.ReportID)
	if err != nil {
		return domain.TaskResult{}, err
	}
	_, _ = h.deps.Operations.MarkRunning(ctx, lease, ProgressPlanReading, StagePlanReadingReport, "正在阅读你的形象报告")
	generated, err := h.deps.Generator.Generate(ctx, GenerationInput{
		Report:           report,
		Brief:            payload.Brief,
		ContentAttempt:   payload.ContentAttempt,
		PriorReasonCodes: payload.PriorReasonCodes,
	})
	if err != nil {
		// Contract/schema errors are permanent; transient provider errors
		// bubble up for an infrastructure retry of the same task.
		return domain.TaskResult{}, err
	}
	_, _ = h.deps.Operations.MarkRunning(ctx, lease, ProgressPlanChecking, StagePlanChecking, "正在检查三套方案")

	reasonCodes, evaluatorID, err := h.evaluateCandidate(ctx, report, payload, generated)
	if err != nil {
		return domain.TaskResult{}, err
	}
	if len(reasonCodes) > 0 {
		return h.executeRejection(ctx, lease, payload, reasonCodes, evaluatorID)
	}

	quality := PlanQualityRecord{
		ID: h.newID(), UserID: lease.Task.UserID, SubjectID: payload.PlanSetID,
		PolicyVersion: PlannerSchemaVersion, Decision: qualityDecisionPass,
		ReasonCodes: []string{}, InternalScores: json.RawMessage(`{}`),
		EvaluatorInvocationID: evaluatorID,
	}
	planSet := h.materializePlanSet(report, payload, generated, quality)
	specs, err := CompileRenderSpecs(planSet, report)
	if err != nil {
		return domain.TaskResult{}, &taskrunner.TaskError{Class: domain.ErrorPermanent, Code: "plan_render_spec_invalid"}
	}
	prepared, err := h.deps.Store.Prepare(ctx, lease, PrepareCommand{
		UserID: lease.Task.UserID, PlanSet: planSet, Quality: quality, RenderSpecs: specs,
	})
	if err != nil {
		return domain.TaskResult{}, err
	}
	return domain.TaskResult{Disposition: domain.TaskPublish, ResultType: "plan_set", ResultID: prepared.ID}, nil
}

// evaluateCandidate runs the deterministic gate and — only when it is clean —
// the semantic verifier. It returns sorted reason codes and the evaluator
// invocation ID for the quality record. A verifier transport error is
// returned so the same task retries without consuming the content budget.
func (h *Handler) evaluateCandidate(ctx context.Context, report ReportSnapshot, payload GenerateTaskPayload, generated GeneratedPlanSet) ([]string, string, error) {
	var reasonCodes []string
	for _, violation := range ValidateCandidate(ValidationInput{Report: report, Brief: payload.Brief, Candidate: generated}) {
		reasonCodes = append(reasonCodes, violation.Code)
	}
	if len(reasonCodes) > 0 {
		return reasonCodes, "", nil
	}
	verification, err := h.deps.Verifier.Verify(ctx, VerificationInput{Report: report, Brief: payload.Brief, Candidate: generated})
	if err != nil {
		return nil, "", err
	}
	if verification.Decision != qualityDecisionPass {
		return verification.ReasonCodes, verification.InvocationID, nil
	}
	return nil, verification.InvocationID, nil
}

func (h *Handler) executeRejection(ctx context.Context, lease domain.TaskLease, payload GenerateTaskPayload, reasonCodes []string, evaluatorID string) (domain.TaskResult, error) {
	quality := PlanQualityRecord{
		ID: h.newID(), UserID: lease.Task.UserID, SubjectID: payload.PlanSetID,
		PolicyVersion: PlannerSchemaVersion, Decision: qualityDecisionRetry,
		ReasonCodes: reasonCodes, InternalScores: json.RawMessage(`{}`),
		EvaluatorInvocationID: evaluatorID,
	}
	if payload.ContentAttempt <= 1 {
		h.stage(lease, pendingDecision{quality: quality, next: contentRetryTask(payload, reasonCodes)})
		return domain.TaskResult{Disposition: domain.TaskEnqueueNext, ResultType: "plan_set_attempt", ResultID: quality.ID}, nil
	}
	if _, err := h.deps.Operations.Fail(ctx, lease, quality, codePlanQualityRejected, "两次方案都未能通过质量检查，请稍后重试", false); err != nil {
		return domain.TaskResult{}, err
	}
	h.stage(lease, pendingDecision{quality: quality})
	return domain.TaskResult{}, ErrQualityRejected
}

// Commit applies the disposition in one atomic store transition: publish the
// prepared graph, enqueue the single content retry, or fail the operation.
func (h *Handler) Commit(ctx context.Context, lease domain.TaskLease, result domain.TaskResult) (domain.CommitOutcome, error) {
	switch result.Disposition {
	case domain.TaskPublish, domain.TaskDomainFail:
		return h.deps.Store.CommitPrepared(ctx, lease, result)
	case domain.TaskEnqueueNext:
		staged := h.consume(lease)
		if staged == nil {
			return domain.CommitSuperseded, fmt.Errorf("staged quality decision missing for task %s", lease.ID)
		}
		return h.deps.Tasks.EnqueueRetry(ctx, lease, EnqueueRetryCommand{
			UserID:        lease.Task.UserID,
			OperationID:   lease.Task.OperationID,
			ProgressBPS:   ProgressPlanChecking,
			StageCode:     StagePlanChecking,
			PublicMessage: retryPublicMessage,
			Quality:       staged.quality,
			Task:          staged.next,
		})
	default:
		return domain.CommitSuperseded, fmt.Errorf("unsupported disposition %q", result.Disposition)
	}
}

// contentRetryTask builds the second (and last) content attempt payload with
// a dedicated dedupe key.
func contentRetryTask(payload GenerateTaskPayload, reasonCodes []string) EnqueueTask {
	next := payload
	next.ContentAttempt = 2
	next.PriorReasonCodes = reasonCodes
	return EnqueueTask{
		Type:              PlanSetGenerationTaskType,
		SubjectType:       "plan_set",
		SubjectID:         payload.PlanSetID,
		SubjectGeneration: 1,
		PayloadVersion:    1,
		Payload:           next,
		DedupeKey:         contentRetryDedupeKey(payload),
	}
}

func contentRetryDedupeKey(payload GenerateTaskPayload) string {
	return fmt.Sprintf("plan-set:%s:%s:%s:%s:content:2",
		payload.ReportID, payload.Scene, payload.BriefHash, payload.PlannerSchemaVersion)
}

// materializePlanSet assigns final row IDs to the accepted candidate.
func (h *Handler) materializePlanSet(report ReportSnapshot, payload GenerateTaskPayload, generated GeneratedPlanSet, quality PlanQualityRecord) domain.PlanSet {
	planSet := domain.PlanSet{
		ID:                   payload.PlanSetID,
		UserID:               report.UserID,
		ReportID:             payload.ReportID,
		ProfileSnapshot:      report.ProfileSnapshot,
		Scene:                payload.Scene,
		SceneBrief:           payload.Brief,
		BriefHash:            payload.BriefHash,
		PlannerSchemaVersion: payload.PlannerSchemaVersion,
		StyleRuleVersion:     payload.StyleRuleVersion,
		ProviderInvocationID: generated.InvocationID,
		QualityEvaluationID:  quality.ID,
	}
	for _, variant := range generated.Variants {
		domainVariant := domain.PlanVariant{
			ID:             h.newID(),
			UserID:         planSet.UserID,
			PlanSetID:      planSet.ID,
			Slot:           variant.Slot,
			Key:            variant.Key,
			Name:           variant.Name,
			Descriptor:     variant.Descriptor,
			Rationale:      variant.Rationale,
			Recommended:    variant.Recommended,
			OutcomeTags:    variant.OutcomeTags,
			DifferenceTags: variant.DifferenceTags,
		}
		for _, step := range variant.Steps {
			domainStep := domain.PlanStep{
				ID:            h.newID(),
				UserID:        planSet.UserID,
				PlanVariantID: domainVariant.ID,
				Category:      step.Category,
				Action:        step.Action,
				Title:         step.Title,
				Summary:       step.Summary,
				Details:       step.Details,
				Position:      stepPositionOf(step.Category),
			}
			for _, grounding := range step.Groundings {
				domainStep.Groundings = append(domainStep.Groundings, domain.PlanStepGrounding{
					UserID:     planSet.UserID,
					PlanStepID: domainStep.ID,
					SourceType: grounding.SourceType,
					SourceID:   grounding.SourceID,
					Reason:     grounding.Reason,
				})
			}
			domainVariant.Steps = append(domainVariant.Steps, domainStep)
		}
		planSet.Variants = append(planSet.Variants, domainVariant)
	}
	return planSet
}

func stepPositionOf(category domain.StepCategory) int {
	switch category {
	case domain.CategoryHair:
		return 1
	case domain.CategoryMakeup:
		return 2
	case domain.CategoryOutfit:
		return 3
	}
	return 3
}

func decodeGeneratePayload(raw json.RawMessage) (GenerateTaskPayload, error) {
	var payload GenerateTaskPayload
	if len(raw) == 0 {
		return payload, errors.New("empty plan_set.generate payload")
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return payload, fmt.Errorf("decode plan_set.generate payload: %w", err)
	}
	if payload.PlanSetID == "" || payload.ReportID == "" || payload.ContentAttempt < 1 {
		return payload, errors.New("plan_set.generate payload is incomplete")
	}
	return payload, nil
}

func (h *Handler) newID() string {
	if h.deps.NewIDs != nil {
		return h.deps.NewIDs()
	}
	return uuid.NewString()
}

func (h *Handler) stage(lease domain.TaskLease, decision pendingDecision) {
	h.pending.Store(lease.LeaseToken, decision)
}

func (h *Handler) consume(lease domain.TaskLease) *pendingDecision {
	value, ok := h.pending.LoadAndDelete(lease.LeaseToken)
	if !ok {
		return nil
	}
	decision := value.(pendingDecision)
	return &decision
}
