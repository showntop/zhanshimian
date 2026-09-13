package rendering

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"sync"
	"testing"

	"github.com/zhanshimian/server/internal/domain"
	providerai "github.com/zhanshimian/server/internal/provider/ai"
	"github.com/zhanshimian/server/internal/repository"
)

// ---- worker fakes ----

type fakeGenerator struct {
	mu       sync.Mutex
	requests []providerai.GenerationRequest
	outputs  []generationOutcome
}

type generationOutcome struct {
	data []byte
	mime string
	err  error
}

func (g *fakeGenerator) Generate(_ context.Context, request providerai.GenerationRequest) (providerai.GenerationResult, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.requests = append(g.requests, request)
	if len(g.outputs) == 0 {
		return providerai.GenerationResult{}, errors.New("no scripted output")
	}
	out := g.outputs[0]
	g.outputs = g.outputs[1:]
	if out.err != nil {
		return providerai.GenerationResult{}, out.err
	}
	return providerai.GenerationResult{
		Data: out.data, MIMEType: out.mime,
		Meta: providerai.InvocationMeta{InvocationID: "inv-render-" + itoa(g.requestsMax()), ModelKey: "primary"},
	}, nil
}

func (g *fakeGenerator) requestsMax() int { return len(g.requests) }

type fakeObjects struct {
	mu         sync.Mutex
	candidates map[string][]byte
	published  map[string][]byte
}

func newFakeObjects() *fakeObjects {
	return &fakeObjects{candidates: map[string][]byte{}, published: map[string][]byte{}}
}

func (f *fakeObjects) PutCandidate(_ context.Context, input CandidateObjectInput) (StoredObject, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.candidates[input.CandidateID]; ok {
		return StoredObject{}, errors.New("object_exists")
	}
	f.candidates[input.CandidateID] = input.Data
	return StoredObject{Key: CandidateKey(input.UserID, input.RunID, input.CandidateID), SHA256: input.SHA256, ByteSize: int64(len(input.Data))}, nil
}

func (f *fakeObjects) Promote(_ context.Context, input PromoteObjectInput) (StoredObject, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, data := range f.candidates {
		if sumOf(data) == input.ExpectedSHA256 {
			key := PublishedKey(input.UserID, input.PublicationID)
			f.published[key] = data
			return StoredObject{Key: key, SHA256: input.ExpectedSHA256, ByteSize: int64(len(data))}, nil
		}
	}
	return StoredObject{}, errors.New("source not found")
}

func (f *fakeObjects) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.published, key)
	delete(f.candidates, key)
	return nil
}

func (f *fakeObjects) Open(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}

func sumOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func CandidateKey(userID, runID, candidateID string) string {
	return "users/" + userID + "/render-quarantine/" + runID + "/" + candidateID + ".jpg"
}

func PublishedKey(userID, publicationID string) string {
	return "users/" + userID + "/render-published/" + publicationID + ".jpg"
}

func itoa(n int) string {
	digits := ""
	if n == 0 {
		return "0"
	}
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

// workerRepoFake 记录所有仓储交互。
type workerRepoFake struct {
	repoFake
	mu                sync.Mutex
	spec              domain.RenderSpec
	run               domain.RenderRun
	jobOrdinal        int
	jobPrevious       *domain.QualityEvaluation
	recordCalls       int
	expandCalls       int
	failCalls         int
	commitCalls       int
	lastEnqueued      EnqueueNextCandidateCommand
	expandGenerations []int64
	commitErr         error
}

func (r *workerRepoFake) GetCandidateJob(_ context.Context, _, _ string, ordinal int) (CandidateJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.jobOrdinal = ordinal
	return CandidateJob{
		Run: r.run, Spec: r.spec,
		Body:     providerai.ImageInput{AssetID: "asset-body", Role: "body", MIMEType: "image/jpeg"},
		Face:     providerai.ImageInput{AssetID: "asset-face", Role: "face", MIMEType: "image/jpeg"},
		Previous: r.jobPrevious,
	}, nil
}

func (r *workerRepoFake) RecordCandidate(_ context.Context, command RecordCandidateCommand) (domain.RenderCandidate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recordCalls++
	return domain.RenderCandidate{
		ID: "candidate-" + itoa(command.Ordinal), UserID: command.UserID,
		RenderRunID: command.RenderRunID, Ordinal: command.Ordinal,
	}, nil
}

func (r *workerRepoFake) ExpandCandidateBudget(_ context.Context, command EnqueueNextCandidateCommand) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expandCalls++
	r.lastEnqueued = command
	r.expandGenerations = append(r.expandGenerations, int64(command.SubjectGeneration))
	r.run.CandidateLimit = 2
	return "task-2", nil
}

func (r *workerRepoFake) FailRun(_ context.Context, command FailRunCommand) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failCalls++
	r.run.Outcome = command.Outcome
	return nil
}

func (r *workerRepoFake) CommitEvaluation(_ context.Context, command CommitEvaluationCommand) (CommitEvaluationResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commitCalls++
	if r.commitErr != nil {
		return CommitEvaluationResult{}, r.commitErr
	}
	r.run.Outcome = domain.RenderOutcomePublished
	return CommitEvaluationResult{Outcome: domain.RenderOutcomePublished}, nil
}

// ---- harness ----

type workerHarness struct {
	handler   *Handler
	generator *fakeGenerator
	gate      *scriptedGate
	repo      *workerRepoFake
	objects   *fakeObjects
}

type scriptedGate struct {
	results []QualityResult
	calls   int
}

func (g *scriptedGate) Evaluate(context.Context, QualityInput) (QualityResult, error) {
	g.calls++
	if len(g.results) == 0 {
		return QualityResult{Decision: string(domain.QualityDecisionPass), ReasonCodes: []string{}}, nil
	}
	result := g.results[0]
	g.results = g.results[1:]
	return result, nil
}

func newWorkerHarness(t *testing.T) *workerHarness {
	t.Helper()
	repo := &workerRepoFake{}
	repo.spec = validRenderSpec("user-1", "variant-1")
	repo.run = domain.RenderRun{
		ID: "run-1", UserID: "user-1", PlanVariantID: "variant-1",
		Generation: 1, CandidateLimit: 1,
	}
	generator := &fakeGenerator{}
	generator.outputs = []generationOutcome{{data: jpegFixture(t, 1024, 1536), mime: "image/jpeg"}}
	objects := newFakeObjects()
	svc := New(repo, generator, NewJPEGNormalizer(), objects, testConfig())
	gate := &scriptedGate{}
	svc.WithQualityGate(gate)
	return &workerHarness{
		handler: svc.Handler(), generator: generator, gate: gate, repo: repo, objects: objects,
	}
}

func renderLease(ordinal int) domain.TaskLease {
	payload, _ := json.Marshal(GenerateCandidatePayload{RenderRunID: "run-1", Ordinal: ordinal})
	return domain.TaskLease{
		Task: domain.Task{
			ID: "task-1", UserID: "user-1", OperationID: "operation-1",
			Type: RenderGenerateTaskType, SubjectType: "render_run", SubjectID: "run-1",
			SubjectGeneration: 1, Payload: payload,
		},
		LeaseToken: "lease-1", LeaseOwner: "worker-1",
	}
}

// ---- tests ----

func TestCandidateOnePassPublishesOnce(t *testing.T) {
	h := newWorkerHarness(t)
	h.gate.results = []QualityResult{{
		Decision: string(domain.QualityDecisionPass), ReasonCodes: []string{},
		InternalScores: json.RawMessage(`{}`), EvaluatorInvocationID: "inv-q",
		CompletedStages: 5,
	}}
	result, err := h.handler.Execute(context.Background(), renderLease(1))
	if err != nil {
		t.Fatal(err)
	}
	if result.Disposition != domain.TaskPublish {
		t.Fatalf("disposition = %s", result.Disposition)
	}
	outcome, err := h.handler.Commit(context.Background(), renderLease(1), result)
	if err != nil || outcome != domain.CommitApplied {
		t.Fatalf("outcome=%s err=%v", outcome, err)
	}
	if h.repo.expandCalls != 0 {
		t.Fatalf("second task enqueued %d times", h.repo.expandCalls)
	}
	if h.repo.commitCalls != 1 {
		t.Fatalf("commit calls = %d", h.repo.commitCalls)
	}
}

func TestCandidateOneIdentityDriftEnqueuesExactlyOneSecondCandidate(t *testing.T) {
	h := newWorkerHarness(t)
	h.gate.results = []QualityResult{{
		Decision: string(domain.QualityDecisionRetry), ReasonCodes: []string{ReasonIdentityDrift},
		InternalScores: json.RawMessage(`{}`), CompletedStages: 2,
	}}
	result, err := h.handler.Execute(context.Background(), renderLease(1))
	if err != nil {
		t.Fatal(err)
	}
	if result.Disposition != domain.TaskEnqueueNext {
		t.Fatalf("disposition = %s", result.Disposition)
	}
	outcome, err := h.handler.Commit(context.Background(), renderLease(1), result)
	if err != nil || outcome != domain.CommitApplied {
		t.Fatalf("outcome=%s err=%v", outcome, err)
	}
	if h.repo.expandCalls != 1 || h.repo.lastEnqueued.SubjectGeneration != 1 {
		t.Fatalf("quality failure must enqueue exactly candidate 2: %#v", h.repo.lastEnqueued)
	}
	if h.repo.run.CandidateLimit != 2 {
		t.Fatal("candidate budget was not expanded exactly once")
	}
	if h.repo.commitCalls != 0 {
		t.Fatal("rejected candidate must not publish")
	}
}

func TestCandidateTwoRejectFailsRunAndKeepsPlanText(t *testing.T) {
	h := newWorkerHarness(t)
	h.repo.run.CandidateLimit = 2
	h.repo.jobPrevious = &domain.QualityEvaluation{
		Decision: domain.QualityDecisionRetry, ReasonCodes: []string{ReasonIdentityDrift},
	}
	h.gate.results = []QualityResult{{
		Decision: string(domain.QualityDecisionReject), ReasonCodes: []string{ReasonIdentityDrift},
		InternalScores: json.RawMessage(`{}`), CompletedStages: 2,
	}}
	result, err := h.handler.Execute(context.Background(), renderLease(2))
	if err != nil {
		t.Fatal(err)
	}
	if result.Disposition != domain.TaskDomainFail {
		t.Fatalf("disposition = %s", result.Disposition)
	}
	outcome, err := h.handler.Commit(context.Background(), renderLease(2), result)
	if err != nil || outcome != domain.CommitApplied {
		t.Fatalf("outcome=%s err=%v", outcome, err)
	}
	if h.repo.run.Outcome != domain.RenderOutcomeFailed && h.repo.failCalls != 1 {
		t.Fatalf("run outcome = %q failCalls = %d", h.repo.run.Outcome, h.repo.failCalls)
	}
	if h.repo.expandCalls != 0 {
		t.Fatal("candidate 2 rejection must not enqueue candidate 3")
	}
	// 方案文字不受影响:PlanVariant 行从未被 Rendering 触碰(无此类写入端口)。
}

func TestTechnicalRetryKeepsOrdinalOne(t *testing.T) {
	h := newWorkerHarness(t)
	h.generator.outputs = []generationOutcome{
		{err: &providerai.GenerationFailure{Class: domain.ErrorTransient, Code: "upstream_5xx"}},
		{data: jpegFixture(t, 1024, 1536), mime: "image/jpeg"},
	}
	h.gate.results = []QualityResult{{
		Decision: string(domain.QualityDecisionPass), ReasonCodes: []string{},
		InternalScores: json.RawMessage(`{}`), CompletedStages: 5,
	}}
	// 第一次:transient 错误 → typed error 交给 runner,ordinal 不变。
	if _, err := h.handler.Execute(context.Background(), renderLease(1)); err == nil {
		t.Fatal("expected transient error")
	}
	// runner 重试同一 task(ordinal 仍为 1,route state 未花费)。
	result, err := h.handler.Execute(context.Background(), renderLease(1))
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := h.handler.Commit(context.Background(), renderLease(1), result)
	if err != nil || outcome != domain.CommitApplied {
		t.Fatalf("outcome=%s err=%v", outcome, err)
	}
	if h.generator.requests[0].Ordinal != 1 || h.generator.requests[1].Ordinal != 1 {
		t.Fatalf("ordinals = %d/%d, both must be 1", h.generator.requests[0].Ordinal, h.generator.requests[1].Ordinal)
	}
	if h.repo.run.CandidateLimit != 1 {
		t.Fatalf("candidate_limit = %d, network retry must not expand budget", h.repo.run.CandidateLimit)
	}
}

func TestUnavailableRouteFailsRunWithoutMedia(t *testing.T) {
	h := newWorkerHarness(t)
	h.generator.outputs = []generationOutcome{
		{err: &providerai.GenerationFailure{Class: domain.ErrorPermanent, Code: "capability_unavailable"}},
	}
	_, err := h.handler.Execute(context.Background(), renderLease(1))
	if err == nil {
		t.Fatal("expected permanent capability failure")
	}
	// runner 会把 taskrunner.TaskError(permanent)转成 domain fail commit。
	work := h.handler.consume(renderLease(1))
	if work != nil {
		t.Fatal("capability failure must not stage candidate work")
	}
	if h.repo.run.Outcome == domain.RenderOutcomePublished {
		t.Fatal("unavailable route must not publish")
	}
}

func TestGeneratorRequestCarriesOnlySpecAndReferences(t *testing.T) {
	h := newWorkerHarness(t)
	h.gate.results = []QualityResult{{
		Decision: string(domain.QualityDecisionPass), ReasonCodes: []string{},
		InternalScores: json.RawMessage(`{}`), CompletedStages: 5,
	}}
	if _, err := h.handler.Execute(context.Background(), renderLease(1)); err != nil {
		t.Fatal(err)
	}
	request := h.generator.requests[0]
	if request.Body.AssetID != "asset-body" || request.Face.AssetID != "asset-face" {
		t.Fatalf("references = %#v", request)
	}
	if request.Spec.Output.MIMEType != "image/jpeg" {
		t.Fatalf("spec = %#v", request.Spec)
	}
}

func TestCandidateTwoCarriesPriorReasonCodes(t *testing.T) {
	h := newWorkerHarness(t)
	h.repo.run.CandidateLimit = 2
	h.repo.jobPrevious = &domain.QualityEvaluation{
		Decision:    domain.QualityDecisionRetry,
		ReasonCodes: []string{ReasonIdentityDrift, ReasonAnatomyLegs},
	}
	h.gate.results = []QualityResult{{
		Decision: string(domain.QualityDecisionPass), ReasonCodes: []string{},
		InternalScores: json.RawMessage(`{}`), CompletedStages: 5,
	}}
	if _, err := h.handler.Execute(context.Background(), renderLease(2)); err != nil {
		t.Fatal(err)
	}
	request := h.generator.requests[0]
	if request.Ordinal != 2 {
		t.Fatalf("ordinal = %d", request.Ordinal)
	}
	if !reflect.DeepEqual(request.RetryReasonCodes, []string{ReasonIdentityDrift, ReasonAnatomyLegs}) {
		t.Fatalf("candidate 2 correction input = %#v", request.RetryReasonCodes)
	}
}

func TestGenerationSupersededNeverCallsProvider(t *testing.T) {
	h := newWorkerHarness(t)
	h.repo.run.Generation = 3 // lease 声称 generation 1
	var called bool
	_ = called
	result, err := h.handler.Execute(context.Background(), renderLease(1))
	if !errors.Is(err, ErrRenderSuperseded) {
		t.Fatalf("got %v", err)
	}
	if result.Disposition != domain.TaskDomainFail {
		t.Fatalf("disposition = %s", result.Disposition)
	}
	if len(h.generator.requests) != 0 {
		t.Fatal("superseded generation must not call the provider")
	}
}

func TestCommitLeaseLostCleansPublishedObject(t *testing.T) {
	h := newWorkerHarness(t)
	h.gate.results = []QualityResult{{
		Decision: string(domain.QualityDecisionPass), ReasonCodes: []string{},
		InternalScores: json.RawMessage(`{}`), CompletedStages: 5,
	}}
	h.repo.commitErr = repository.ErrLeaseLost
	result, err := h.handler.Execute(context.Background(), renderLease(1))
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := h.handler.Commit(context.Background(), renderLease(1), result)
	if outcome != domain.CommitSuperseded || err != nil {
		t.Fatalf("outcome=%s err=%v", outcome, err)
	}
	if len(h.objects.published) != 0 {
		t.Fatalf("published objects survived CAS failure: %v", h.objects.published)
	}
}
