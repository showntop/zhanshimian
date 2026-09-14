package assessment

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/provider/ai"
)

func TestHandlerRunsGatesInOrderAndPublishesSupportedFindings(t *testing.T) {
	spy := newHandlerFixture()
	spy.evidence.result = ai.EvidenceResult{Findings: []ai.EvidenceDecision{
		{Key: "f1", Supported: true, Confidence: .98},
		{Key: "f2", Supported: false, Confidence: .91, ReasonCode: "observation_not_visible"},
		{Key: "f3", Supported: true, Confidence: .97},
		{Key: "f4", Supported: true, Confidence: .96},
	}, Meta: ai.InvocationMeta{InvocationID: "inv-evidence"}}
	result, err := spy.handler.Execute(ctx, spy.lease)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	wantCalls := []string{"load", "technical", "content", "identity", "analyze", "evidence", "prepare"}
	if !reflect.DeepEqual(spy.calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", spy.calls, wantCalls)
	}
	if result.ResultType != "report" {
		t.Fatalf("ResultType = %q, want report", result.ResultType)
	}
	if result.Disposition != domain.TaskPublish {
		t.Fatalf("Disposition = %s, want %s", result.Disposition, domain.TaskPublish)
	}
	if spy.repo.prepared == nil || len(spy.repo.prepared.Findings) != 3 {
		t.Fatalf("prepared findings = %v, want 3", findingsLen(spy.repo.prepared))
	}
	if spy.repo.prepared.Report.ProviderInvocationID != "inv-analysis" {
		t.Fatalf("report must reference the analysis invocation, got %q", spy.repo.prepared.Report.ProviderInvocationID)
	}
	if spy.repo.prepared.Quality.EvaluatorInvocationID != "inv-evidence" {
		t.Fatalf("quality evaluation must reference the evidence invocation, got %q", spy.repo.prepared.Quality.EvaluatorInvocationID)
	}
	outcome, err := spy.handler.Commit(ctx, spy.lease, result)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if outcome != domain.CommitApplied {
		t.Fatalf("Commit outcome = %s, want %s", outcome, domain.CommitApplied)
	}
}

func TestHandlerRegeneratesOnceWhenEvidenceLeavesFewerThanThree(t *testing.T) {
	spy := newHandlerFixture()
	spy.evidence.results = []ai.EvidenceResult{onlyTwoSupported(), threeSupported()}
	if _, err := spy.handler.Execute(ctx, spy.lease); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if spy.analyzer.calls != 2 {
		t.Fatalf("analyzer.calls = %d, want 2", spy.analyzer.calls)
	}
	if spy.evidence.calls != 2 {
		t.Fatalf("evidence.calls = %d, want 2", spy.evidence.calls)
	}
}

func TestHandlerRejectsIdentityUncertaintyWithoutAnalysis(t *testing.T) {
	spy := newHandlerFixture()
	spy.identity.result = ai.IdentityResult{Decision: "uncertain", Confidence: .71}
	result, err := spy.handler.Execute(ctx, spy.lease)
	assertPublicFailure(t, result, err, "photo_identity_uncertain", "照片差异较大，请重新拍摄确认", false)
	if spy.analyzer.calls != 0 {
		t.Fatalf("analyzer.calls = %d, want 0", spy.analyzer.calls)
	}
	if spy.repo.failure != nil {
		t.Fatalf("Execute wrote repo.failure = %+v", spy.repo.failure)
	}
}

func TestHandlerUnknownPolicyVersionDoesNotPublish(t *testing.T) {
	spy := newHandlerFixture()
	spy.repo.input.Run.QualityPolicyVersion = "quality.unknown"
	result, err := spy.handler.Execute(ctx, spy.lease)
	assertPublicFailure(t, result, err, "quality_policy_unsupported", "这次未能形成可靠报告，请重新拍摄后再试", false)
	if spy.analyzer.calls != 0 {
		t.Fatalf("analyzer.calls = %d, want 0", spy.analyzer.calls)
	}
	if spy.repo.prepared != nil {
		t.Fatalf("PrepareReport ran for unknown policy: %+v", spy.repo.prepared)
	}
	if spy.repo.publishCount != 0 {
		t.Fatalf("publishCount = %d, want 0", spy.repo.publishCount)
	}
	if result.Failure == nil || result.Failure.Class != domain.ErrorQualityRejected {
		t.Fatalf("Failure.Class = %+v, want %s", result.Failure, domain.ErrorQualityRejected)
	}
}

func TestHandlerFailsClosedAfterSecondEvidenceFailure(t *testing.T) {
	spy := newHandlerFixture()
	spy.evidence.results = []ai.EvidenceResult{onlyTwoSupported(), onlyTwoSupported()}
	result, err := spy.handler.Execute(ctx, spy.lease)
	assertPublicFailure(t, result, err, "report_evidence_insufficient", "这次未能形成可靠报告，请重新拍摄后再试", true)
	if spy.repo.publishCount != 0 {
		t.Fatalf("publishCount = %d, want 0", spy.repo.publishCount)
	}
	if spy.repo.prepared != nil {
		t.Fatalf("PrepareReport ran on closed evidence gate: %+v", spy.repo.prepared)
	}
}

func assertPublicFailure(t *testing.T, result domain.TaskResult, err error, code, message string, retryable bool) {
	t.Helper()
	if err != nil {
		t.Fatalf("Execute returned error %v, want domain-fail result", err)
	}
	if result.Disposition != domain.TaskDomainFail {
		t.Fatalf("Disposition = %s, want %s", result.Disposition, domain.TaskDomainFail)
	}
	if result.Failure == nil || result.Failure.Code != code {
		t.Fatalf("Failure = %+v, want code %s", result.Failure, code)
	}
	got, ok := LookupPublicFailure(code)
	if !ok {
		t.Fatalf("missing PublicFailure catalog entry for %s", code)
	}
	if got.Message != message || got.Retryable != retryable {
		t.Fatalf("PublicFailure(%s) = %+v, want message %q retryable %v", code, got, message, retryable)
	}
}

type handlerFixture struct {
	handler   *Handler
	lease     domain.TaskLease
	calls     []string
	repo      *handlerRepoFake
	images    *imageLoaderFake
	progress  *progressFake
	technical *technicalFake
	content   *contentFake
	identity  *identityFake
	analyzer  *analyzerFake
	evidence  *evidenceFake
}

func newHandlerFixture() *handlerFixture {
	spy := &handlerFixture{
		lease: domain.TaskLease{Task: domain.Task{
			ID:             "task-1",
			UserID:         "user-1",
			OperationID:    "op-1",
			Type:           domain.TaskType("assessment"),
			SubjectID:      "run-1",
			PayloadVersion: 1,
		}},
		repo:      &handlerRepoFake{},
		images:    &imageLoaderFake{},
		progress:  &progressFake{},
		technical: &technicalFake{},
		content:   &contentFake{result: passingContent()},
		identity:  &identityFake{result: ai.IdentityResult{Decision: "pass", Confidence: 0.97}},
		analyzer:  &analyzerFake{draft: fixtureDraft()},
		evidence:  &evidenceFake{},
	}
	spy.repo.input = domain.AssessmentRunInput{
		Run: domain.AnalysisRun{
			ID:                    "run-1",
			UserID:                "user-1",
			PhotoSetID:            "photoset-1",
			OperationID:           "op-1",
			QualityPolicyVersion:  QualityPolicyVersion,
			AnalyzerSchemaVersion: AnalyzerSchemaVersion,
			ProfileSnapshot:       json.RawMessage(`{"role":"designer"}`),
		},
		PhotoSet: domain.PhotoSet{
			ID:     "photoset-1",
			UserID: "user-1",
			Items: []domain.PhotoSetItem{
				{ID: "item-face", Role: domain.PhotoRoleFace, MediaAssetID: "face", Asset: domain.MediaAsset{ID: "face", MIMEType: "image/jpeg"}},
				{ID: "item-side", Role: domain.PhotoRoleSide, MediaAssetID: "side", Asset: domain.MediaAsset{ID: "side", MIMEType: "image/jpeg"}},
				{ID: "item-body", Role: domain.PhotoRoleBody, MediaAssetID: "body", Asset: domain.MediaAsset{ID: "body", MIMEType: "image/jpeg"}},
			},
		},
	}
	spy.images.images = []ImageInput{
		{Role: "face", MIMEType: "image/jpeg", Data: []byte("face")},
		{Role: "side", MIMEType: "image/jpeg", Data: []byte("side")},
		{Role: "body", MIMEType: "image/jpeg", Data: []byte("body")},
	}
	spy.images.fixture = spy
	spy.technical.fixture = spy
	spy.content.fixture = spy
	spy.identity.fixture = spy
	spy.analyzer.fixture = spy
	spy.evidence.fixture = spy
	spy.repo.fixture = spy
	spy.handler = NewHandler(HandlerDeps{
		Repo:         spy.repo,
		Images:       spy.images,
		Progress:     spy.progress,
		Technical:    spy.technical,
		PhotoContent: spy.content,
		Identity:     spy.identity,
		Analyzer:     spy.analyzer,
		Evidence:     spy.evidence,
	})
	return spy
}

func (f *handlerFixture) record(name string) {
	f.calls = append(f.calls, name)
}

type handlerRepoFake struct {
	fixture      *handlerFixture
	input        domain.AssessmentRunInput
	prepared     *domain.PrepareReportParams
	publishCount int
	failure      *repoFailure
}

type repoFailure struct {
	Outcome domain.AnalysisOutcome
	Failure domain.TaskFailure
}

func (r *handlerRepoFake) GetRunInput(context.Context, string, string) (domain.AssessmentRunInput, error) {
	return r.input, nil
}

func (r *handlerRepoFake) PrepareReport(_ context.Context, params domain.PrepareReportParams) (string, error) {
	r.fixture.record("prepare")
	copied := params
	copied.Findings = append([]domain.ReportFinding(nil), params.Findings...)
	r.prepared = &copied
	return "report-1", nil
}

func (r *handlerRepoFake) CommitAssessment(_ context.Context, _ domain.TaskLease, result domain.TaskResult) (domain.CommitOutcome, error) {
	if result.Disposition == domain.TaskDomainFail {
		r.failure = &repoFailure{Failure: derefFailure(result.Failure)}
		return domain.CommitApplied, nil
	}
	if result.ResultType == "report" {
		r.publishCount++
	}
	return domain.CommitApplied, nil
}

type imageLoaderFake struct {
	fixture *handlerFixture
	images  []ImageInput
}

func (l *imageLoaderFake) Load(context.Context, []domain.PhotoSetItem) ([]ImageInput, error) {
	l.fixture.record("load")
	return l.images, nil
}

type progressFake struct{}

func (progressFake) Set(context.Context, string, int, string) error { return nil }

type technicalFake struct {
	fixture *handlerFixture
	err     error
}

func (c *technicalFake) Check([]ImageInput) error {
	c.fixture.record("technical")
	return c.err
}

type contentFake struct {
	fixture *handlerFixture
	result  ai.PhotoQualityResult
}

func (c *contentFake) Check(context.Context, []ai.ImageInput) (ai.PhotoQualityResult, error) {
	c.fixture.record("content")
	return c.result, nil
}

type identityFake struct {
	fixture *handlerFixture
	result  ai.IdentityResult
}

func (c *identityFake) Check(context.Context, []ai.ImageInput) (ai.IdentityResult, error) {
	c.fixture.record("identity")
	return c.result, nil
}

type analyzerFake struct {
	fixture *handlerFixture
	draft   domain.ReportDraft
	calls   int
}

func (a *analyzerFake) Analyze(context.Context, ai.ReportAnalysisInput) (ai.ReportAnalysisResult, error) {
	a.fixture.record("analyze")
	a.calls++
	return ai.ReportAnalysisResult{Draft: a.draft, Meta: ai.InvocationMeta{InvocationID: "inv-analysis"}}, nil
}

type evidenceFake struct {
	fixture *handlerFixture
	result  ai.EvidenceResult
	results []ai.EvidenceResult
	calls   int
}

func (e *evidenceFake) Verify(context.Context, []ai.ImageInput, domain.ReportDraft) (ai.EvidenceResult, error) {
	e.fixture.record("evidence")
	e.calls++
	if len(e.results) > 0 {
		return e.results[e.calls-1], nil
	}
	return e.result, nil
}

func onlyTwoSupported() ai.EvidenceResult {
	return ai.EvidenceResult{Findings: []ai.EvidenceDecision{
		{Key: "f1", Supported: true, Confidence: .98},
		{Key: "f2", Supported: false, Confidence: .91, ReasonCode: "observation_not_visible"},
		{Key: "f3", Supported: true, Confidence: .97},
		{Key: "f4", Supported: false, Confidence: .40, ReasonCode: "observation_not_visible"},
	}}
}

func threeSupported() ai.EvidenceResult {
	return ai.EvidenceResult{Findings: []ai.EvidenceDecision{
		{Key: "f1", Supported: true, Confidence: .98},
		{Key: "f2", Supported: true, Confidence: .96},
		{Key: "f3", Supported: true, Confidence: .97},
		{Key: "f4", Supported: false, Confidence: .40, ReasonCode: "observation_not_visible"},
	}}
}

func fixtureDraft() domain.ReportDraft {
	return domain.ReportDraft{
		ImpressionTags: []string{"利落", "干净"},
		PriorityTitle:  "先整理额前碎发",
		PriorityCopy:   "额前碎发会挡住眉形，先固定再看妆容层次。",
		Findings: []domain.DraftFinding{
			fixtureFinding("f1", "hair", domain.PhotoRoleFace, 1),
			fixtureFinding("f2", "makeup", domain.PhotoRoleFace, 2),
			fixtureFinding("f3", "outfit", domain.PhotoRoleBody, 3),
			fixtureFinding("f4", "color", domain.PhotoRoleBody, 4),
		},
	}
}

func fixtureFinding(key, category string, role domain.PhotoRole, position int) domain.DraftFinding {
	return domain.DraftFinding{
		Key:                key,
		Category:           category,
		Label:              key + " 可见细节",
		VisibleObservation: key + " 在照片中可见",
		Recommendation:     "调整 " + key,
		Priority:           1,
		Position:           position,
		SourceRole:         role,
		Anchor:             domain.EvidenceAnchor{X: 0.20, Y: 0.10, W: 0.40, H: 0.20},
	}
}

func passingContent() ai.PhotoQualityResult {
	return ai.PhotoQualityResult{Photos: []ai.PhotoQualityItem{
		{Role: domain.PhotoRoleFace, Decision: "pass"},
		{Role: domain.PhotoRoleSide, Decision: "pass"},
		{Role: domain.PhotoRoleBody, Decision: "pass"},
	}}
}

func findingsLen(params *domain.PrepareReportParams) int {
	if params == nil {
		return 0
	}
	return len(params.Findings)
}

func derefFailure(failure *domain.TaskFailure) domain.TaskFailure {
	if failure == nil {
		return domain.TaskFailure{}
	}
	return *failure
}
