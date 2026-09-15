package planning

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/service/billing"
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
	Memories   PreferenceMemoryReader
	// Renders 是发布后的形象图自动触发端口；nil 时只产文字方案（降级/单测）。
	Renders    RenderStarter
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
	memories, err := h.listMemories(ctx, lease.Task.UserID)
	if err != nil {
		return domain.TaskResult{}, err
	}
	if memories != nil {
		report.ProfileSnapshot, err = embedFeedbackMemory(report.ProfileSnapshot, memories)
		if err != nil {
			return domain.TaskResult{}, err
		}
	}
	_, _ = h.deps.Operations.MarkRunning(ctx, lease, ProgressPlanReading, StagePlanReadingReport, "正在阅读你的形象报告")
	generated, err := h.deps.Generator.Generate(ctx, GenerationInput{
		Report:           report,
		Brief:            payload.Brief,
		ContentAttempt:   payload.ContentAttempt,
		PriorReasonCodes: payload.PriorReasonCodes,
	})
	if err != nil {
		// 生成器输出违约(模型给了不合契约的 JSON)按内容拒绝处理:消耗一次
		// 内容重试预算、携带 reason code 再采样,而不是直接判 operation 失败;
		// 两次都违约才 fail closed。模型采样有方差,基础设施重试无意义。
		if errors.Is(err, ErrGeneratorContract) {
			// reason code 带上违约细节:补生成的 prompt 据此能定位要修的内容,
			// 而不是只看到一个无法行动的 code。
			return h.executeRejection(ctx, lease, payload, []string{reasonWithDetail(ReasonGeneratorContract, generatorContractDetail(err))}, "")
		}
		// Transient provider errors bubble up for an infrastructure retry of the same task.
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
		// reason code 携带门禁细节(哪个变体/步骤、哪个词):补生成的 prompt
		// 只看得见 prior_reason_codes,没有细节模型无法定位要修的内容。
		reasonCodes = append(reasonCodes, reasonWithDetail(violation.Code, violation.Detail))
	}
	if len(reasonCodes) > 0 {
		return reasonCodes, "", nil
	}
	verification, err := h.deps.Verifier.Verify(ctx, VerificationInput{Report: report, Brief: payload.Brief, Candidate: generated})
	if err != nil {
		// 核验器调用失败是基础设施类故障(配额、传输、厂商输出违约),不是
		// 内容拒绝:归类 transient 让同一任务重试,不消耗内容重试预算。
		return nil, "", &taskrunner.TaskError{Class: domain.ErrorTransient, Code: "plan_verifier_unavailable"}
	}
	if verification.Decision != qualityDecisionPass {
		return verification.ReasonCodes, verification.InvocationID, nil
	}
	return nil, verification.InvocationID, nil
}

// generatorContractDetail extracts the innermost contract-violation message
// from a possibly router-combined error. The combined "all models failed"
// text embeds every candidate's failure (including fallback transport error
// payloads); only the precise violation belongs in the retry prompt. The
// innermost wrapper has the shortest message containing the sentinel text.
func generatorContractDetail(err error) string {
	sentinel := ErrGeneratorContract.Error()
	best := ""
	var walk func(error)
	walk = func(e error) {
		if e == nil {
			return
		}
		if msg := e.Error(); msg != sentinel && strings.Contains(msg, sentinel) && (best == "" || len(msg) < len(best)) {
			best = msg
		}
		switch u := e.(type) {
		case interface{ Unwrap() []error }:
			for _, inner := range u.Unwrap() {
				walk(inner)
			}
		case interface{ Unwrap() error }:
			walk(u.Unwrap())
		}
	}
	walk(err)
	detail := strings.TrimSpace(strings.TrimPrefix(best, sentinel))
	detail = strings.TrimSpace(strings.TrimPrefix(detail, ":"))
	const maxDetail = 300
	if len(detail) > maxDetail {
		detail = detail[:maxDetail]
	}
	return detail
}

// reasonWithDetail joins a stable reason code with its human-actionable
// detail. Reason codes are free-form text (DB text[] and prompt JSON), so
// carrying the detail is contract-compatible and lets the single content
// regeneration see exactly what to fix.
func reasonWithDetail(code, detail string) string {
	if detail == "" {
		return code
	}
	return code + ": " + detail
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
		outcome, err := h.deps.Store.CommitPrepared(ctx, lease, result)
		if err == nil && outcome == domain.CommitApplied && result.Disposition == domain.TaskPublish {
			h.startRenders(ctx, lease.Task.UserID, result.ResultID)
		}
		return outcome, err
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

// startRenders 在方案集发布后整批触发形象图渲染：文字方案已经可读，
// 任何一套触发失败只记日志——已发布的方案操作绝不被渲染拖回失败。
// 幂等键稳定（auto-render:{planSetID}:{variantID}）：任务崩溃重放由渲染侧
// 按键幂等吞掉，不重复扣费。遇日限 429 停止本批剩余，继续点只会拿到同一个拒绝。
func (h *Handler) startRenders(ctx context.Context, userID, planSetID string) {
	if h.deps.Renders == nil {
		return
	}
	planSet, err := h.deps.Store.Get(ctx, userID, planSetID)
	if err != nil {
		slog.Warn("plan renders auto-start skipped: plan set unreadable",
			"plan_set_id", planSetID, "error", err)
		return
	}
	for _, variant := range planSet.Variants {
		if err := h.deps.Renders.StartRun(ctx, userID, variant.ID,
			fmt.Sprintf("auto-render:%s:%s", planSetID, variant.ID)); err != nil {
			slog.Warn("plan render auto-start failed",
				"plan_set_id", planSetID, "variant_id", variant.ID, "error", err)
			if errors.Is(err, billing.ErrRateLimited) {
				return
			}
		}
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
	return fmt.Sprintf("plan-set:%s:%s:%s:content:2",
		payload.ReportID, payload.PlanningInputHash, payload.PlannerSchemaVersion)
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
		PlanningInputHash:    payload.PlanningInputHash,
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
	case domain.StepCategoryHair:
		return 1
	case domain.StepCategoryMakeup:
		return 2
	case domain.StepCategoryOutfit:
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

// listMemories reads the user's recent preference memories, tolerating a
// nil reader so callers that never wired memories still behave correctly.
func (h *Handler) listMemories(ctx context.Context, userID string) ([]domain.PreferenceMemory, error) {
	if h.deps.Memories == nil {
		return nil, nil
	}
	return h.deps.Memories.ListPreferenceMemories(ctx, userID, planningMemoryLimit)
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
