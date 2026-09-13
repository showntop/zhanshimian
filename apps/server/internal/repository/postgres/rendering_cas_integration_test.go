package postgres

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

// renderingPublishFixture 复用 planning 发布链 + 已创建的 run。
type renderingPublishFixture struct {
	store     *Store
	userA     string
	userB     string
	variantID string
	run       domain.RenderRun
	task      domain.TaskLease
}

func newRenderingPublishFixture(t *testing.T) *renderingPublishFixture {
	t.Helper()
	f := newRenderingStore(t)
	ctx := context.Background()
	result, err := f.store.CreateRun(ctx, renderCreateCommand(f, "publish-key"))
	if err != nil || !result.Created {
		t.Fatalf("create run: created=%v err=%v", result.Created, err)
	}
	lease, ok, err := f.store.Claim(ctx, "worker-render", 30*1e9, []domain.TaskType{RenderCreateTaskType()})
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	return &renderingPublishFixture{
		store: f.store, userA: f.userA, userB: f.userB,
		variantID: f.variantID, run: result.Run, task: lease,
	}
}

// RenderCreateTaskType 由 postgres 包本地引用。
func RenderCreateTaskType() domain.TaskType { return renderCreateTaskType }

// PublishedKey/ReasonIdentityDrift 的本地引用(与 service/storage 端一致)。
func PublishedKey(userID, publicationID string) string {
	return "users/" + userID + "/render-published/" + publicationID + ".jpg"
}

const ReasonIdentityDrift = "identity_drift"

func passEvaluation(candidateID string) domain.QualityEvaluation {
	return domain.QualityEvaluation{
		UserID: "", SubjectType: domain.QualitySubjectRenderCandidate, SubjectID: candidateID,
		Policy:   domain.QualityPolicyRef{Version: "render-quality-v1"},
		Decision: domain.QualityDecisionPass, ReasonCodes: []string{},
		InternalScores: []byte(`{}`), EvaluatorInvocationID: "",
	}
}

func renderPublishCommand(f *renderingPublishFixture, candidateID string) domain.RenderCommitEvaluationCommand {
	publicationID := uuid.NewString()
	command := domain.RenderCommitEvaluationCommand{
		TaskID: f.task.ID, LeaseToken: f.task.LeaseToken,
		UserID: f.userA, RenderRunID: f.run.ID,
		SubjectGeneration: 1, CandidateID: candidateID,
		PublicationID: publicationID,
		Evaluation:    passEvaluation(candidateID),
		PublishedObject: &domain.RenderPublishedObject{
			Key: PublishedKey(f.userA, publicationID), SHA256: "abc123", ByteSize: 3,
		},
	}
	return command
}

func TestCommitEvaluationPassPublishesAtomically(t *testing.T) {
	f := newRenderingPublishFixture(t)
	ctx := context.Background()
	// 先隔离候选。
	candidate, err := f.store.RecordCandidate(ctx, domain.RenderRecordCandidateCommand{
		TaskID: f.task.ID, LeaseToken: f.task.LeaseToken,
		UserID: f.userA, RenderRunID: f.run.ID, SubjectGeneration: 1, Ordinal: 1,
		Asset: renderCandidateAsset(), ProviderInvocationID: seedRenderInvocation(t, f.store, f.userA, f.task.OperationID, f.task.ID),
	})
	if err != nil {
		t.Fatal(err)
	}
	command := renderPublishCommand(f, candidate.ID)
	got, err := f.store.CommitEvaluation(ctx, command)
	if err != nil || got.Outcome != domain.RenderOutcomePublished {
		t.Fatalf("outcome=%s err=%v", got.Outcome, err)
	}
	var publicationCount, headGeneration int
	var currentPublication *string
	if err = f.store.pool.QueryRow(ctx,
		`SELECT count(*) FROM render_publications WHERE user_id=$1::uuid`, f.userA).Scan(&publicationCount); err != nil {
		t.Fatal(err)
	}
	if publicationCount != 1 {
		t.Fatalf("publications = %d", publicationCount)
	}
	if err = f.store.pool.QueryRow(ctx,
		`SELECT generation, current_publication_id FROM render_heads WHERE user_id=$1::uuid AND plan_variant_id=$2::uuid`,
		f.userA, f.variantID).Scan(&headGeneration, &currentPublication); err != nil {
		t.Fatal(err)
	}
	if headGeneration != 1 || currentPublication == nil {
		t.Fatalf("head = gen %d publication %v", headGeneration, currentPublication)
	}
}

func TestCommitEvaluationExpiredLeaseTouchesNothing(t *testing.T) {
	f := newRenderingPublishFixture(t)
	ctx := context.Background()
	stale := f.task
	stale.LeaseToken = uuid.NewString()
	command := renderPublishCommand(&renderingPublishFixture{
		store: f.store, userA: f.userA, variantID: f.variantID, run: f.run, task: stale,
	}, uuid.NewString())
	_, err := f.store.CommitEvaluation(ctx, command)
	if err != repository.ErrLeaseLost {
		t.Fatalf("got %v, want lease lost", err)
	}
	var publicationCount int
	if err = f.store.pool.QueryRow(ctx,
		`SELECT count(*) FROM render_publications WHERE user_id=$1::uuid`, f.userA).Scan(&publicationCount); err != nil {
		t.Fatal(err)
	}
	if publicationCount != 0 {
		t.Fatalf("expired lease published %d rows", publicationCount)
	}
}

func TestExpandCandidateBudgetOnlyOnce(t *testing.T) {
	f := newRenderingPublishFixture(t)
	ctx := context.Background()
	// 先记录 candidate 1(评估行的 subject)。
	candidate, err := f.store.RecordCandidate(ctx, domain.RenderRecordCandidateCommand{
		TaskID: f.task.ID, LeaseToken: f.task.LeaseToken,
		UserID: f.userA, RenderRunID: f.run.ID, SubjectGeneration: 1, Ordinal: 1,
		Asset: renderCandidateAsset(), ProviderInvocationID: seedRenderInvocation(t, f.store, f.userA, f.task.OperationID, f.task.ID),
	})
	if err != nil {
		t.Fatal(err)
	}
	quality := domain.QualityEvaluation{
		ID: uuid.NewString(), UserID: f.userA, SubjectType: domain.QualitySubjectRenderCandidate, SubjectID: candidate.ID,
		Policy:   domain.QualityPolicyRef{Version: "render-quality-v1"},
		Decision: domain.QualityDecisionRetry, ReasonCodes: []string{ReasonIdentityDrift},
		InternalScores: []byte(`{}`),
	}
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			taskID, err := f.store.ExpandCandidateBudget(ctx, domain.RenderEnqueueNextCandidateCommand{
				TaskID: f.task.ID, LeaseToken: f.task.LeaseToken,
				UserID: f.userA, RenderRunID: f.run.ID, SubjectGeneration: 1,
				Quality: quality,
			})
			t.Logf("expand err=%v taskID=%s", err, taskID)
		}()
	}
	wg.Wait()
	var limit int
	if err = f.store.pool.QueryRow(ctx,
		`SELECT candidate_limit FROM render_runs WHERE id=$1::uuid AND user_id=$2::uuid`,
		f.run.ID, f.userA).Scan(&limit); err != nil {
		t.Fatal(err)
	}
	if limit != 2 {
		t.Fatalf("candidate_limit = %d", limit)
	}
	var candidate2Tasks int
	if err = f.store.pool.QueryRow(ctx, `
		SELECT count(*) FROM tasks
		WHERE user_id=$1::uuid AND dedupe_key=$2`,
		f.userA, domain.RenderCandidateDedupeKey(f.run.ID, 2)).Scan(&candidate2Tasks); err != nil {
		t.Fatal(err)
	}
	if candidate2Tasks != 1 {
		t.Fatalf("candidate 2 tasks = %d, want exactly 1", candidate2Tasks)
	}
}

func TestRenderCrossTenantReadIs404(t *testing.T) {
	f := newRenderingPublishFixture(t)
	_, _, _, err := f.store.GetRun(context.Background(), f.userB, f.run.ID)
	if err != repository.ErrNotFound {
		t.Fatalf("got %v", err)
	}
}

// ---- helpers ----

func renderCandidateAsset() domain.RenderCandidateAsset {
	return domain.RenderCandidateAsset{
		ObjectKey: "users/x/render-quarantine/run/candidate.jpg",
			SHA256:    schemaHex64(), MIMEType: "image/jpeg", ByteSize: 4096, Width: 1024, Height: 1536,
	}
}

func seedRenderInvocation(t *testing.T, store *Store, userID, operationID, taskID string) string {
	t.Helper()
	var id string
	err := store.pool.QueryRow(context.Background(), `
		INSERT INTO provider_invocations(
			user_id, operation_id, task_id, attempt_no, capability,
			routing_config_version, provider_key, model_key, protocol, request_hash, status
		) VALUES ($1::uuid,$2::uuid,$3::uuid,0,'full_look_generation','v1','p','m','http',$4,'succeeded')
		RETURNING id::text`,
		userID, operationID, taskID, schemaHex64()).Scan(&id)
	if err != nil {
		t.Fatalf("seed render invocation: %v", err)
	}
	return id
}
