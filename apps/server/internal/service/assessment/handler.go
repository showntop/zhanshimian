package assessment

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/provider/ai"
	"github.com/zhanshimian/server/internal/service/taskrunner"
)

const (
	TaskTypeAssessment  = domain.TaskType("assessment")
	PayloadVersion      = 1
	ReportSchemaVersion = "report.v1"
)

type HandlerRepository interface {
	GetRunInput(ctx context.Context, userID, runID string) (domain.AssessmentRunInput, error)
	PrepareReport(ctx context.Context, params domain.PrepareReportParams) (string, error)
	CommitAssessment(ctx context.Context, lease domain.TaskLease, result domain.TaskResult) (domain.CommitOutcome, error)
}

type ImageLoader interface {
	Load(ctx context.Context, items []domain.PhotoSetItem) ([]ImageInput, error)
}

type OperationProgress interface {
	Set(ctx context.Context, operationID string, progressBPS int, stageCode string) error
}

type TechnicalChecker interface {
	Check(images []ImageInput) error
}

type PhotoContentChecker interface {
	Check(ctx context.Context, images []ai.ImageInput) (ai.PhotoQualityResult, error)
}

type IdentityChecker interface {
	Check(ctx context.Context, images []ai.ImageInput) (ai.IdentityResult, error)
}

type ReportAnalyzer interface {
	Analyze(ctx context.Context, input ai.ReportAnalysisInput) (ai.ReportAnalysisResult, error)
}

type EvidenceVerifier interface {
	Verify(ctx context.Context, images []ai.ImageInput, draft domain.ReportDraft) (ai.EvidenceResult, error)
}

type HandlerDeps struct {
	Repo         HandlerRepository
	Images       ImageLoader
	Progress     OperationProgress
	Technical    TechnicalChecker
	PhotoContent PhotoContentChecker
	Identity     IdentityChecker
	Analyzer     ReportAnalyzer
	Evidence     EvidenceVerifier
	Policy       Policy
}

type Handler struct {
	repo         HandlerRepository
	images       ImageLoader
	progress     OperationProgress
	technical    TechnicalChecker
	photoContent PhotoContentChecker
	identity     IdentityChecker
	analyzer     ReportAnalyzer
	evidence     EvidenceVerifier
	policy       Policy
}

func NewHandler(deps HandlerDeps) *Handler {
	return &Handler{
		repo:         deps.Repo,
		images:       deps.Images,
		progress:     deps.Progress,
		technical:    deps.Technical,
		photoContent: deps.PhotoContent,
		identity:     deps.Identity,
		analyzer:     deps.Analyzer,
		evidence:     deps.Evidence,
		policy:       deps.Policy,
	}
}

func (h *Handler) Type() domain.TaskType { return TaskTypeAssessment }

func (h *Handler) Execute(ctx context.Context, lease domain.TaskLease) (domain.TaskResult, error) {
	task := lease.Task
	input, err := h.repo.GetRunInput(ctx, task.UserID, task.SubjectID)
	if err != nil {
		return domain.TaskResult{}, err
	}
	images, err := h.images.Load(ctx, input.PhotoSet.Items)
	if err != nil {
		return domain.TaskResult{}, err
	}
	if err := h.progress.Set(ctx, task.OperationID, 1000, StagePhotoTechnicalCheck); err != nil {
		return domain.TaskResult{}, err
	}
	if err := h.technical.Check(images); err != nil {
		return h.stageRejection(err)
	}
	if err := h.progress.Set(ctx, task.OperationID, 2500, StagePhotoContentCheck); err != nil {
		return domain.TaskResult{}, err
	}
	aiImages := toAIImages(images)
	content, err := h.photoContent.Check(ctx, aiImages)
	if err != nil {
		return domain.TaskResult{}, err
	}
	if err := h.policy.AcceptContent(content); err != nil {
		return h.stageRejection(err)
	}
	if err := h.progress.Set(ctx, task.OperationID, 4000, StagePhotoIdentityCheck); err != nil {
		return domain.TaskResult{}, err
	}
	identity, err := h.identity.Check(ctx, aiImages)
	if err != nil {
		return domain.TaskResult{}, err
	}
	if err := h.policy.AcceptIdentity(identity, input.Run.QualityPolicyVersion); err != nil {
		return h.stageRejection(err)
	}
	for generation := 1; generation <= 2; generation++ {
		if err := h.progress.Set(ctx, task.OperationID, 5500, StageReportGenerating); err != nil {
			return domain.TaskResult{}, err
		}
		analysisResult, err := h.analyzer.Analyze(ctx, buildAnalysisInput(input, aiImages, generation))
		if err != nil {
			return domain.TaskResult{}, err
		}
		draft := analysisResult.Draft
		if err := ValidateReportDraft(draft); err != nil {
			return domain.TaskResult{}, err
		}
		if err := h.progress.Set(ctx, task.OperationID, 8000, StageEvidenceVerifying); err != nil {
			return domain.TaskResult{}, err
		}
		evidence, err := h.evidence.Verify(ctx, aiImages, draft)
		if err != nil {
			return domain.TaskResult{}, err
		}
		supported := h.policy.SupportedFindings(draft.Findings, evidence, input.Run.QualityPolicyVersion)
		if len(supported) < 3 {
			continue
		}
		if err := h.progress.Set(ctx, task.OperationID, 9500, StageReportPublishing); err != nil {
			return domain.TaskResult{}, err
		}
		report, quality := buildImmutablePublication(input, draft, evidence, supported, analysisResult.Meta.InvocationID, evidence.Meta.InvocationID)
		reportID, err := h.repo.PrepareReport(ctx, domain.PrepareReportParams{
			RunID:    input.Run.ID,
			UserID:   task.UserID,
			Report:   report,
			Quality:  quality,
			Findings: report.Findings,
		})
		if err != nil {
			return domain.TaskResult{}, err
		}
		return domain.TaskResult{Disposition: domain.TaskPublish, ResultType: "report", ResultID: reportID}, nil
	}
	return h.stageRejection(catalogFailure(codeReportEvidenceInsufficient))
}

func (h *Handler) Commit(ctx context.Context, lease domain.TaskLease, result domain.TaskResult) (domain.CommitOutcome, error) {
	return h.repo.CommitAssessment(ctx, lease, result)
}

func (h *Handler) stageRejection(err error) (domain.TaskResult, error) {
	failure := publicTaskFailure(err)
	return domain.TaskResult{Disposition: domain.TaskDomainFail, Failure: failure}, nil
}

func publicTaskFailure(err error) *domain.TaskFailure {
	var public *PublicFailure
	if errors.As(err, &public) && public != nil {
		return &domain.TaskFailure{Class: domain.ErrorQualityRejected, Code: public.Code}
	}
	var rejected *PhotoRejectedError
	if errors.As(err, &rejected) && rejected != nil {
		return &domain.TaskFailure{Class: domain.ErrorQualityRejected, Code: rejected.Code}
	}
	return &domain.TaskFailure{Class: domain.ErrorQualityRejected, Code: "assessment_rejected"}
}

func toAIImages(images []ImageInput) []ai.ImageInput {
	out := make([]ai.ImageInput, 0, len(images))
	for _, image := range images {
		out = append(out, ai.ImageInput{Role: image.Role, MIMEType: image.MIMEType, Data: image.Data})
	}
	return out
}

func buildAnalysisInput(input domain.AssessmentRunInput, images []ai.ImageInput, generation int) ai.ReportAnalysisInput {
	return ai.ReportAnalysisInput{
		Images:     images,
		Profile:    input.Run.ProfileSnapshot,
		Generation: generation,
	}
}

func buildImmutablePublication(
	input domain.AssessmentRunInput,
	draft domain.ReportDraft,
	evidence ai.EvidenceResult,
	supported []domain.DraftFinding,
	invocationID string,
	evaluatorInvocationID string,
) (domain.Report, domain.QualityEvaluation) {
	itemsByRole := make(map[domain.PhotoRole]domain.PhotoSetItem, len(input.PhotoSet.Items))
	for _, item := range input.PhotoSet.Items {
		itemsByRole[item.Role] = item
	}
	evidenceByKey := make(map[string]ai.EvidenceDecision, len(evidence.Findings))
	reasonCodes := make([]string, 0)
	for _, decision := range evidence.Findings {
		evidenceByKey[decision.Key] = decision
		if !decision.Supported && decision.ReasonCode != "" {
			reasonCodes = append(reasonCodes, decision.ReasonCode)
		}
	}
	publishedDraft := draft
	publishedDraft.Findings = supported
	findings := make([]domain.ReportFinding, 0, len(supported))
	for i, finding := range supported {
		item := itemsByRole[finding.SourceRole]
		findings = append(findings, domain.ReportFinding{
			UserID:             input.Run.UserID,
			Category:           finding.Category,
			Label:              finding.Label,
			VisibleObservation: finding.VisibleObservation,
			Recommendation:     finding.Recommendation,
			SourcePhotoItemID:  item.ID,
			Priority:           finding.Priority,
			Anchor:             finding.Anchor,
			Confidence:         evidenceByKey[finding.Key].Confidence,
			Position:           i + 1,
		})
	}
	hero := itemsByRole[domain.PhotoRoleBody]
	report := domain.Report{
		UserID:               input.Run.UserID,
		PhotoSetID:           input.PhotoSet.ID,
		HeroAssetID:          hero.MediaAssetID,
		SchemaVersion:        ReportSchemaVersion,
		ContentHash:          ReportContentHash(ReportSchemaVersion, publishedDraft),
		PriorityTitle:        draft.PriorityTitle,
		PriorityCopy:         draft.PriorityCopy,
		ImpressionTags:       append([]string(nil), draft.ImpressionTags...),
		ProfileSnapshot:      input.Run.ProfileSnapshot,
		ProviderInvocationID: invocationID,
		Findings:             findings,
	}
	scores, _ := json.Marshal(map[string]any{
		"supported_findings": len(supported),
		"draft_findings":     len(draft.Findings),
	})
	quality := domain.QualityEvaluation{
		UserID:                 input.Run.UserID,
		SubjectType:            domain.QualitySubjectReport,
		Policy:                 domain.QualityPolicyRef{Version: input.Run.QualityPolicyVersion},
		Decision:               domain.QualityDecisionPass,
		ReasonCodes:            reasonCodes,
		InternalScores:         scores,
		EvaluatorInvocationID:  evaluatorInvocationID,
	}
	return report, quality
}

var _ taskrunner.Handler = (*Handler)(nil)
