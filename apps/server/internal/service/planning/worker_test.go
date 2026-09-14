package planning

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/service/taskrunner"
)

func TestHandlerPreparesOnlyAfterAllGatesPass(t *testing.T) {
	deps := validHandlerDependencies()
	handler := NewHandlerForTest(deps)
	lease := validGenerateLease(1)
	result, err := handler.Execute(context.Background(), lease)
	if err != nil {
		t.Fatal(err)
	}
	if deps.store.prepareCalls != 1 || len(deps.store.command.RenderSpecs) != 3 {
		t.Fatalf("prepare command = %#v", deps.store.command)
	}
	if deps.enqueuer.calls != 0 {
		t.Fatal("passing candidate must not consume content retry")
	}
	if result.Disposition != domain.TaskPublish || result.ResultType != "plan_set" {
		t.Fatalf("result = %#v", result)
	}
	outcome, err := handler.Commit(context.Background(), lease, result)
	if err != nil || outcome != domain.CommitApplied {
		t.Fatalf("outcome=%s err=%v", outcome, err)
	}
	if deps.store.commitCalls != 1 {
		t.Fatalf("commit calls = %d", deps.store.commitCalls)
	}
}

func TestHandlerEnqueuesSecondContentAttemptAfterQualityReject(t *testing.T) {
	deps := validHandlerDependencies()
	deps.generator.output = invalidDifferenceCandidate()
	handler := NewHandlerForTest(deps)
	result, err := handler.Execute(context.Background(), validGenerateLease(1))
	if err != nil {
		t.Fatal(err)
	}
	if result.ResultType != "plan_set_attempt" {
		t.Fatalf("result = %#v", result)
	}
	if result.Disposition != domain.TaskEnqueueNext || deps.store.prepareCalls != 0 || deps.enqueuer.calls != 0 {
		t.Fatalf("prepare=%d enqueue=%d", deps.store.prepareCalls, deps.enqueuer.calls)
	}
	outcome, err := handler.Commit(context.Background(), validGenerateLease(1), result)
	if err != nil || outcome != domain.CommitApplied || deps.enqueuer.calls != 1 {
		t.Fatalf("commit outcome=%s enqueue=%d err=%v", outcome, deps.enqueuer.calls, err)
	}
	payload := deps.enqueuer.command.Task.Payload.(GenerateTaskPayload)
	if payload.ContentAttempt != 2 {
		t.Fatalf("retry payload = %#v", payload)
	}
	if len(payload.PriorReasonCodes) == 0 || payload.PriorReasonCodes[0] != ReasonDifferenceInsufficient {
		t.Fatalf("retry must carry prior reason codes: %#v", payload.PriorReasonCodes)
	}
	if got := deps.enqueuer.command.Task.DedupeKey; got == "" || !strings.Contains(got, ":content:2") {
		t.Fatalf("retry dedupe key = %q", got)
	}
	if deps.enqueuer.command.Quality.Decision != qualityDecisionRetry {
		t.Fatalf("quality record = %#v", deps.enqueuer.command.Quality)
	}
}

func TestHandlerFailsClosedAfterSecondRejectedCandidate(t *testing.T) {
	deps := validHandlerDependencies()
	deps.generator.output = invalidDifferenceCandidate()
	_, err := NewHandlerForTest(deps).Execute(context.Background(), validGenerateLease(2))
	if !errors.Is(err, ErrQualityRejected) {
		t.Fatalf("got %v", err)
	}
	if deps.store.prepareCalls != 0 || deps.operations.failCalls != 1 {
		t.Fatal("rejected candidate must never publish")
	}
	// The runner turns the classified error into a domain-fail commit.
	outcome, err := NewHandlerForTest(deps).Commit(context.Background(), validGenerateLease(2), domain.TaskResult{
		Disposition: domain.TaskDomainFail,
		Failure:     &domain.TaskFailure{Class: domain.ErrorQualityRejected, Code: codePlanQualityRejected},
	})
	if err != nil || outcome != domain.CommitApplied {
		t.Fatalf("outcome=%s err=%v", outcome, err)
	}
}

func TestHandlerVerifierRejectionAlsoConsumesContentRetry(t *testing.T) {
	deps := validHandlerDependencies()
	deps.verifier.result = VerificationResult{Decision: "reject", ReasonCodes: []string{"plan.report_contradiction"}}
	handler := NewHandlerForTest(deps)
	result, err := handler.Execute(context.Background(), validGenerateLease(1))
	if err != nil {
		t.Fatal(err)
	}
	if result.Disposition != domain.TaskEnqueueNext || deps.store.prepareCalls != 0 {
		t.Fatalf("verifier rejection must not prepare: %#v", result)
	}
	if _, err := handler.Commit(context.Background(), validGenerateLease(1), result); err != nil {
		t.Fatal(err)
	}
	payload := deps.enqueuer.command.Task.Payload.(GenerateTaskPayload)
	if payload.PriorReasonCodes[0] != "plan.report_contradiction" {
		t.Fatalf("verifier codes not carried: %#v", payload)
	}
}

// 生成器输出违约(模型给出不合契约的 JSON)同样是内容拒绝:消耗内容重试预算、
// 携带 reason code 再采一次,而不是把整个 operation 判失败——模型采样有方差,
// 第二次采样通常能给出合契约输出;两次都违约才 fail closed。
func TestHandlerGeneratorContractViolationConsumesContentRetry(t *testing.T) {
	deps := validHandlerDependencies()
	// 用 planning 侧哨兵包装,模拟 provider/ai 的真实错误链。
	deps.generator.err = fmt.Errorf("%w: makeup details carry outfit-only fields", ErrGeneratorContract)
	handler := NewHandlerForTest(deps)
	result, err := handler.Execute(context.Background(), validGenerateLease(1))
	if err != nil {
		t.Fatal(err)
	}
	if result.Disposition != domain.TaskEnqueueNext || result.ResultType != "plan_set_attempt" || deps.store.prepareCalls != 0 {
		t.Fatalf("contract violation must consume content retry: %#v", result)
	}
	if _, err := handler.Commit(context.Background(), validGenerateLease(1), result); err != nil {
		t.Fatal(err)
	}
	payload := deps.enqueuer.command.Task.Payload.(GenerateTaskPayload)
	if payload.ContentAttempt != 2 || len(payload.PriorReasonCodes) == 0 || payload.PriorReasonCodes[0] != ReasonGeneratorContract {
		t.Fatalf("retry payload = %#v", payload)
	}
	if deps.enqueuer.command.Quality.Decision != qualityDecisionRetry {
		t.Fatalf("quality record = %#v", deps.enqueuer.command.Quality)
	}
}

func TestHandlerGeneratorContractViolationFailsClosedOnSecondAttempt(t *testing.T) {
	deps := validHandlerDependencies()
	deps.generator.err = fmt.Errorf("%w: makeup details carry outfit-only fields", ErrGeneratorContract)
	_, err := NewHandlerForTest(deps).Execute(context.Background(), validGenerateLease(2))
	if !errors.Is(err, ErrQualityRejected) {
		t.Fatalf("got %v", err)
	}
	if deps.store.prepareCalls != 0 || deps.operations.failCalls != 1 {
		t.Fatal("second contract violation must fail closed without publish")
	}
}

func TestHandlerVerifierTransportErrorKeepsContentBudget(t *testing.T) {
	deps := validHandlerDependencies()
	deps.verifier.err = errors.New("verifier unavailable")
	_, err := NewHandlerForTest(deps).Execute(context.Background(), validGenerateLease(1))
	if err == nil {
		t.Fatal("verifier transport failure must surface for infra retry")
	}
	// 裸错误会被 taskrunner 归类为 permanent/unclassified,直接终杀操作;
	// 核验器故障是基础设施类问题,必须归类 transient 走任务重试。
	var taskErr *taskrunner.TaskError
	if !errors.As(err, &taskErr) || taskErr.Class != domain.ErrorTransient {
		t.Fatalf("verifier error must classify transient for infra retry, got %v", err)
	}
	if deps.store.prepareCalls != 0 || deps.enqueuer.calls != 0 {
		t.Fatal("transport failure must not prepare or consume the retry")
	}
}

// ---- fixtures ----

type fakeGenerator struct {
	output GeneratedPlanSet
	err    error
}

func (f *fakeGenerator) Generate(context.Context, GenerationInput) (GeneratedPlanSet, error) {
	return f.output, f.err
}

type fakeVerifier struct {
	result VerificationResult
	err    error
}

func (f *fakeVerifier) Verify(context.Context, VerificationInput) (VerificationResult, error) {
	if f.err != nil {
		return VerificationResult{}, f.err
	}
	return f.result, nil
}

type fakeOpWriter struct {
	markCalls int
	failCalls int
}

func (f *fakeOpWriter) MarkRunning(context.Context, domain.TaskLease, int, string, string) (bool, error) {
	f.markCalls++
	return true, nil
}

func (f *fakeOpWriter) Fail(context.Context, domain.TaskLease, PlanQualityRecord, string, string, bool) (bool, error) {
	f.failCalls++
	return true, nil
}

type fakeEnqueuer struct {
	calls   int
	command EnqueueRetryCommand
}

func (f *fakeEnqueuer) EnqueueRetry(_ context.Context, _ domain.TaskLease, command EnqueueRetryCommand) (domain.CommitOutcome, error) {
	f.calls++
	f.command = command
	return domain.CommitApplied, nil
}

type handlerDepsBundle struct {
	store      *fakeStore
	generator  *fakeGenerator
	verifier   *fakeVerifier
	operations *fakeOpWriter
	enqueuer   *fakeEnqueuer
}

func (b handlerDepsBundle) toDeps() HandlerDeps {
	return HandlerDeps{
		Reports:    fakeReports{report: validReport("20000000-0000-0000-0000-000000000001")},
		Generator:  b.generator,
		Verifier:   b.verifier,
		Operations: b.operations,
		Tasks:      b.enqueuer,
		Store:      b.store,
		Memories:   &fakeMemories{},
		NewIDs:     func() string { return "70000000-0000-0000-0000-000000000099" },
	}
}

func validHandlerDependencies() handlerDepsBundle {
	bundle := handlerDepsBundle{
		store:      &fakeStore{},
		generator:  &fakeGenerator{output: validValidationInput().Candidate},
		verifier:   &fakeVerifier{result: VerificationResult{Decision: qualityDecisionPass}},
		operations: &fakeOpWriter{},
		enqueuer:   &fakeEnqueuer{},
	}
	return bundle
}

func NewHandlerForTest(deps handlerDepsBundle) *Handler {
	return NewHandler(deps.toDeps())
}

func validGenerateLease(contentAttempt int) domain.TaskLease {
	brief := validBrief()
	payload := GenerateTaskPayload{
		PlanSetID:            "10000000-0000-0000-0000-000000000001",
		ReportID:             "20000000-0000-0000-0000-000000000001",
		Scene:                domain.SceneDaily,
		Brief:                brief,
		BriefHash:            BriefHash(brief),
		PlannerSchemaVersion: PlannerSchemaVersion,
		StyleRuleVersion:     StyleRuleVersion,
		ContentAttempt:       contentAttempt,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return domain.TaskLease{
		Task: domain.Task{
			ID:          "task-1",
			UserID:      "00000000-0000-0000-0000-000000000001",
			OperationID: "30000000-0000-0000-0000-000000000001",
			Type:        PlanSetGenerationTaskType,
			SubjectType: "plan_set",
			SubjectID:   payload.PlanSetID,
			Payload:     encoded,
		},
		LeaseToken: "lease-token-1",
		LeaseOwner: "worker-1",
	}
}

func invalidDifferenceCandidate() GeneratedPlanSet {
	candidate := validValidationInput().Candidate
	candidate.Variants[1].Steps = cloneSteps(candidate.Variants[0].Steps)
	return candidate
}
