package rendering

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/zhanshimian/server/internal/domain"
	providerai "github.com/zhanshimian/server/internal/provider/ai"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/taskrunner"
)

// ErrQualityRejected marks a quality rejection that has been persisted and
// must not retry the same candidate.
var ErrQualityRejected = errors.New("render candidate rejected")

// ErrRenderSuperseded marks a run whose generation was replaced mid-flight.
var ErrRenderSuperseded = errors.New("render run superseded")

// Handler implements render_candidate_generate.
type Handler struct {
	service *Service
	// pending 把 Execute 的中间产物交给同 lease 的 Commit;task runner
	// 保证同进程顺序调用,崩溃即任务重排队,无需持久化。
	pending sync.Map
}

func (s *Service) Handler() *Handler {
	return &Handler{service: s}
}

func (h *Handler) Type() domain.TaskType { return RenderGenerateTaskType }

// candidateWork 是一次 Execute 的中间产物:候选已隔离入库、质量已评估,
// 等 Commit 决定发布 / 补第二候选 / 终态失败。
type candidateWork struct {
	run           domain.RenderRun
	candidate     domain.RenderCandidate
	ordinal       int
	quality       QualityResult
	published     *StoredObject
	publicationID string
	generation    int
}

func (h *Handler) stage(lease domain.TaskLease, work *candidateWork) {
	h.pending.Store(lease.LeaseToken, work)
}

func (h *Handler) consume(lease domain.TaskLease) *candidateWork {
	value, ok := h.pending.LoadAndDelete(lease.LeaseToken)
	if !ok {
		return nil
	}
	return value.(*candidateWork)
}

// Execute 生成一个候选:读 job → 校验 generation → 生成 → JPEG 归一化 →
// 隔离存储 → 记录候选(lease CAS)→ 质量门禁 → 按 gate 结果设置 disposition。
func (h *Handler) Execute(ctx context.Context, lease domain.TaskLease) (domain.TaskResult, error) {
	s := h.service
	var payload GenerateCandidatePayload
	if err := decodeTaskPayload(lease.Task.Payload, &payload); err != nil {
		return domain.TaskResult{}, &taskrunner.TaskError{Class: domain.ErrorPermanent, Code: "render_payload_invalid"}
	}

	job, err := s.repo.GetCandidateJob(ctx, lease.Task.UserID, payload.RenderRunID, payload.Ordinal)
	if err != nil {
		return domain.TaskResult{}, err
	}
	if int64(job.Run.Generation) != lease.Task.SubjectGeneration {
		// generation 已过期:superseded,不调用 Provider。
		return domain.TaskResult{Disposition: domain.TaskDomainFail, Failure: &domain.TaskFailure{
			Class: domain.ErrorSuperseded, Code: "render_generation_superseded",
		}}, ErrRenderSuperseded
	}
	if !domain.CanRequestCandidate(job.Run, payload.Ordinal, job.Previous) {
		return domain.TaskResult{Disposition: domain.TaskDomainFail, Failure: &domain.TaskFailure{
			Class: domain.ErrorPermanent, Code: "render_candidate_budget_exhausted",
		}}, fmt.Errorf("%w: ordinal %d not allowed", ErrQualityRejected, payload.Ordinal)
	}

	bodyInput := mediaToProviderImage(job.Body, "body")
	faceInput := mediaToProviderImage(job.Face, "face")
	generated, err := s.generator.Generate(ctx, providerai.GenerationRequest{
		RenderRunID:      job.Run.ID,
		Ordinal:          payload.Ordinal,
		Spec:             job.Spec.Spec,
		Body:             bodyInput,
		Face:             faceInput,
		RetryReasonCodes: previousReasonCodes(job.Previous),
		RouteState:       payload.RouteState,
	})
	if err != nil {
		return domain.TaskResult{}, classifyGenerationError(err)
	}

	normalized, err := s.normalizer.Normalize(generated.Data, generated.MIMEType)
	if err != nil {
		return domain.TaskResult{}, classifyGenerationError(&providerai.GenerationFailure{
			Class: domain.ErrorPermanent, Code: "render_output_invalid", Err: err,
		})
	}

	candidateID := s.config.NewIDs()
	stored, err := s.objects.PutCandidate(ctx, CandidateObjectInput{
		UserID: lease.Task.UserID, RunID: job.Run.ID, CandidateID: candidateID,
		Data: normalized.Data, SHA256: normalized.SHA256,
	})
	if err != nil {
		return domain.TaskResult{}, err
	}

	candidate, err := s.repo.RecordCandidate(ctx, RecordCandidateCommand{
		TaskID:            lease.Task.ID,
		LeaseToken:        lease.LeaseToken,
		UserID:            lease.Task.UserID,
		RenderRunID:       job.Run.ID,
		SubjectGeneration: int(lease.Task.SubjectGeneration),
		Ordinal:           payload.Ordinal,
		Asset: CandidateAsset{
			ObjectKey: stored.Key, SHA256: normalized.SHA256, MIMEType: normalized.MIMEType,
			ByteSize: normalized.ByteSize, Width: normalized.Width, Height: normalized.Height,
		},
		ProviderInvocationID: generated.Meta.InvocationID,
	})
	if err != nil {
		if errors.Is(err, repository.ErrLeaseLost) {
			return domain.TaskResult{}, &taskrunner.TaskError{Class: domain.ErrorSuperseded, Code: "render_lease_lost"}
		}
		if errors.Is(err, ErrRenderSuperseded) {
			return domain.TaskResult{Disposition: domain.TaskDomainFail, Failure: &domain.TaskFailure{
				Class: domain.ErrorSuperseded, Code: "render_generation_superseded",
			}}, ErrRenderSuperseded
		}
		return domain.TaskResult{}, err
	}

	// 质量门禁(隔离候选已入库后)。
	quality, err := s.gate.Evaluate(ctx, QualityInput{
		Run: job.Run, Candidate: candidate, Image: normalized,
		Body: bodyInput, Face: faceInput, Spec: job.Spec.Spec,
	})
	if err != nil {
		return domain.TaskResult{}, classifyGateError(err)
	}

	work := &candidateWork{
		run: job.Run, candidate: candidate, ordinal: payload.Ordinal,
		quality: quality, generation: job.Run.Generation,
	}

	switch quality.Decision {
	case string(domain.QualityDecisionPass):
		// 预创建 publication 并把对象提升到 published 前缀。
		publicationID := s.config.NewIDs()
		published, err := s.objects.Promote(ctx, PromoteObjectInput{
			UserID: lease.Task.UserID, PublicationID: publicationID,
			SourceKey: stored.Key, ExpectedSHA256: normalized.SHA256,
		})
		if err != nil {
			return domain.TaskResult{}, err
		}
		work.published = &published
		work.publicationID = publicationID
		h.stage(lease, work)
		return domain.TaskResult{Disposition: domain.TaskPublish, ResultType: "render_publication", ResultID: publicationID}, nil
	case string(domain.QualityDecisionRetry):
		h.stage(lease, work)
		return domain.TaskResult{Disposition: domain.TaskEnqueueNext, ResultType: "render_candidate", ResultID: candidate.ID}, nil
	default: // reject / error
		h.stage(lease, work)
		return domain.TaskResult{Disposition: domain.TaskDomainFail, Failure: &domain.TaskFailure{
			Class: domain.ErrorQualityRejected, Code: "render_quality_rejected",
		}}, nil
	}
}

// Commit 在双 CAS(lease + generation/version)事务中应用 disposition:
// pass → 发布;retry → 扩预算并入队 Candidate 2;reject/unavailable →
// run/operation 终态失败。staged work 只存在于 Execute 走到质量门禁之后;
// Execute 出错由 runner 直接以 TaskDomainFail 收尾时没有 staged work,
// 此时按 result.Failure 的错误码终态失败 run,不得误报 staged 缺失。
func (h *Handler) Commit(ctx context.Context, lease domain.TaskLease, result domain.TaskResult) (domain.CommitOutcome, error) {
	s := h.service

	if result.Disposition == domain.TaskDomainFail {
		if work := h.consume(lease); work != nil {
			return h.commitRejected(ctx, lease, work)
		}
		code := "render_failed"
		if result.Failure != nil && result.Failure.Code != "" {
			code = result.Failure.Code
		}
		if err := s.repo.FailRun(ctx, FailRunCommand{
			TaskID: lease.Task.ID, LeaseToken: lease.LeaseToken,
			UserID: lease.Task.UserID, RenderRunID: lease.Task.SubjectID,
			SubjectGeneration: int(lease.Task.SubjectGeneration),
			Outcome:           domain.RenderOutcomeFailed,
			ErrorCode:         code,
			Retryable:         false,
		}); err != nil {
			if errors.Is(err, repository.ErrLeaseLost) || errors.Is(err, ErrRenderSuperseded) {
				return domain.CommitSuperseded, nil
			}
			return domain.CommitSuperseded, err
		}
		return domain.CommitApplied, nil
	}

	work := h.consume(lease)
	if work == nil {
		return domain.CommitSuperseded, fmt.Errorf("staged candidate work missing for task %s", lease.ID)
	}

	switch result.Disposition {
	case domain.TaskPublish:
		outcome, err := s.repo.CommitEvaluation(ctx, CommitEvaluationCommand{
			TaskID: lease.Task.ID, LeaseToken: lease.LeaseToken,
			UserID: lease.Task.UserID, RenderRunID: work.run.ID,
			SubjectGeneration: int(lease.Task.SubjectGeneration),
			CandidateID:       work.candidate.ID,
			Evaluation:        evaluationOf(work.quality, s.config.QualityPolicyVersion),
			PublishedObject:   publishedObjectOf(work.published),
			PublicationID:     work.publicationID,
		})
		if err != nil {
			if errors.Is(err, repository.ErrLeaseLost) || errors.Is(err, ErrRenderSuperseded) {
				// CAS 失败:清理刚创建的 published 对象,隔离副本留给 GC。
				if work.published != nil {
					_ = s.objects.Delete(ctx, work.published.Key)
				}
				return domain.CommitSuperseded, nil
			}
			return domain.CommitSuperseded, err
		}
		_ = outcome
		return domain.CommitApplied, nil

	case domain.TaskEnqueueNext:
		enqueuedTaskID, err := s.repo.ExpandCandidateBudget(ctx, EnqueueNextCandidateCommand{
			TaskID: lease.Task.ID, LeaseToken: lease.LeaseToken,
			UserID: lease.Task.UserID, RenderRunID: work.run.ID,
			SubjectGeneration: int(lease.Task.SubjectGeneration),
			Quality:           evaluationOf(work.quality, s.config.QualityPolicyVersion),
		})
		if err != nil {
			if errors.Is(err, repository.ErrLeaseLost) || errors.Is(err, ErrRenderSuperseded) {
				return domain.CommitSuperseded, nil
			}
			return domain.CommitSuperseded, err
		}
		_ = enqueuedTaskID
		return domain.CommitApplied, nil

	default:
		return domain.CommitSuperseded, fmt.Errorf("unsupported render commit disposition %q", result.Disposition)
	}
}

// commitRejected 是候选走完质量门禁后的终态失败:先把质量评估留档,再失败
// run/operation(错误码固定为质量拒绝)。
func (h *Handler) commitRejected(ctx context.Context, lease domain.TaskLease, work *candidateWork) (domain.CommitOutcome, error) {
	s := h.service
	// 把质量评估留档后终态失败。
	if _, err := s.repo.CommitEvaluation(ctx, CommitEvaluationCommand{
		TaskID: lease.Task.ID, LeaseToken: lease.LeaseToken,
		UserID: lease.Task.UserID, RenderRunID: work.run.ID,
		SubjectGeneration: int(lease.Task.SubjectGeneration),
		CandidateID:       work.candidate.ID,
		Evaluation:        evaluationOf(work.quality, s.config.QualityPolicyVersion),
	}); err != nil && !errors.Is(err, repository.ErrLeaseLost) && !errors.Is(err, ErrRenderSuperseded) {
		return domain.CommitSuperseded, err
	}
	if err := s.repo.FailRun(ctx, FailRunCommand{
		TaskID: lease.Task.ID, LeaseToken: lease.LeaseToken,
		UserID: lease.Task.UserID, RenderRunID: work.run.ID,
		SubjectGeneration: int(lease.Task.SubjectGeneration),
		Outcome:           domain.RenderOutcomeFailed,
		ErrorCode:         "render_quality_rejected",
		Retryable:         false,
	}); err != nil {
		if errors.Is(err, repository.ErrLeaseLost) || errors.Is(err, ErrRenderSuperseded) {
			return domain.CommitSuperseded, nil
		}
		return domain.CommitSuperseded, err
	}
	return domain.CommitApplied, nil
}

// mediaToProviderImage 把 MediaAsset 映射为 provider 图片输入。
func mediaToProviderImage(asset domain.MediaAsset, role string) providerai.ImageInput {
	return providerai.ImageInput{AssetID: asset.ID, Role: role, MIMEType: asset.MIMEType}
}

// evaluationOf 把 gate 的结果映射到不可变质量评估行。
func evaluationOf(quality QualityResult, policyVersion string) domain.QualityEvaluation {
	return domain.QualityEvaluation{
		UserID:                "",
		SubjectType:           domain.QualitySubjectRenderCandidate,
		Policy:                domain.QualityPolicyRef{Version: policyVersion},
		Decision:              domain.QualityDecision(quality.Decision),
		ReasonCodes:           quality.ReasonCodes,
		InternalScores:        quality.InternalScores,
		EvaluatorInvocationID: quality.EvaluatorInvocationID,
	}
}

// publishedObjectOf 把存储结果映射为 domain 形状。
func publishedObjectOf(stored *StoredObject) *domain.RenderPublishedObject {
	if stored == nil {
		return nil
	}
	return &domain.RenderPublishedObject{Key: stored.Key, SHA256: stored.SHA256, ByteSize: stored.ByteSize}
}

// previousReasonCodes 提取上一候选的排序去重 reason codes 作为 Candidate 2
// 的修正输入。
func previousReasonCodes(previous *domain.QualityEvaluation) []string {
	if previous == nil {
		return nil
	}
	return append([]string(nil), previous.ReasonCodes...)
}

// classifyGenerationError 把 typed 失败交给 task runner 分类。
func classifyGenerationError(err error) error {
	var failure *providerai.GenerationFailure
	if errors.As(err, &failure) && failure.Code == "capability_unavailable" {
		// 无等价多图 route:domain fail → run unavailable,不重试。
		return &taskrunner.TaskError{Class: domain.ErrorPermanent, Code: failure.Code}
	}
	return err
}

func classifyGateError(err error) error {
	return err
}

func decodeTaskPayload(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		return errors.New("empty task payload")
	}
	return json.Unmarshal(raw, target)
}
