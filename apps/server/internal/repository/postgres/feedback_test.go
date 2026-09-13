package postgres

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

// generationFeedbackFixture 发布一条真实 publication,供反馈落库用例派生链路。
type generationFeedbackFixture struct {
	store       *Store
	userA       string
	userB       string
	publication *domain.RenderPublication
}

// newGenerationFeedbackFixture 走完整发布链:创建 run、记录候选、提交评估并发布。
func newGenerationFeedbackFixture(t *testing.T) *generationFeedbackFixture {
	t.Helper()
	f := newRenderingPublishFixture(t)
	candidate, err := f.store.RecordCandidate(ctx, domain.RenderRecordCandidateCommand{
		TaskID: f.task.ID, LeaseToken: f.task.LeaseToken,
		UserID: f.userA, RenderRunID: f.run.ID, SubjectGeneration: 1, Ordinal: 1,
		Asset: renderCandidateAsset(), ProviderInvocationID: seedRenderInvocation(t, f.store, f.userA, f.task.OperationID, f.task.ID),
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := f.store.CommitEvaluation(ctx, renderPublishCommand(f, candidate.ID))
	if err != nil || got.Outcome != domain.RenderOutcomePublished {
		t.Fatalf("publish: outcome=%s err=%v", got.Outcome, err)
	}
	// CommitEvaluation 不回填 Publication,这里直接以 render_publications 为
	// 事实源派生出包含 asset_id 的完整发布链路。
	var pub domain.RenderPublication
	if err = f.store.pool.QueryRow(ctx, `
		SELECT p.id::text, p.user_id::text, p.plan_variant_id::text, p.render_run_id::text,
		       p.candidate_id::text, p.quality_evaluation_id::text, c.asset_id::text, p.generation
		FROM render_publications p
		JOIN render_candidates c ON c.user_id = p.user_id AND c.id = p.candidate_id
		WHERE p.user_id=$1::uuid`, f.userA).Scan(
		&pub.ID, &pub.UserID, &pub.PlanVariantID, &pub.RenderRunID,
		&pub.CandidateID, &pub.QualityEvaluationID, &pub.AssetID, &pub.Generation); err != nil {
		t.Fatal(err)
	}
	return &generationFeedbackFixture{store: f.store, userA: f.userA, userB: f.userB, publication: &pub}
}

// insertFeedbackMedia 直插一条属于 userID 的 feedback 用途 ready 图片资产。
func insertFeedbackMedia(t *testing.T, store *Store, userID string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := store.pool.Exec(ctx, `
		INSERT INTO media_assets(id, user_id, origin, purpose, object_key, sha256, mime_type, byte_size, state, display_kind)
		VALUES ($1::uuid, $2::uuid, 'user_upload', 'feedback', $3, $4, 'image/jpeg', 1024, 'ready', 'original')`,
		id, userID, "users/"+userID+"/feedback/"+id+".jpg", schemaHex64())
	if err != nil {
		t.Fatalf("seed feedback media: %v", err)
	}
	return id
}

func generationFeedbackCommand(f *generationFeedbackFixture, key, hash string) domain.CreateGenerationFeedbackCommand {
	return domain.CreateGenerationFeedbackCommand{
		UserID:         f.userA,
		PublicationID:  f.publication.ID,
		Tags:           []domain.Tag{domain.GenerationHairMismatch},
		IdempotencyKey: key,
		RequestHash:    hash,
	}
}

func TestCreateGenerationFeedbackDerivesChain(t *testing.T) {
	f := newGenerationFeedbackFixture(t)
	got, created, err := f.store.CreateGenerationFeedback(ctx, generationFeedbackCommand(f, "chain-key", "hash-chain"))
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	if got.ID == "" || got.PublicationID != f.publication.ID {
		t.Fatalf("identity mismatch: %#v", got)
	}
	if got.RenderRunID != f.publication.RenderRunID || got.CandidateID != f.publication.CandidateID ||
		got.AssetID != f.publication.AssetID || got.Generation != f.publication.Generation {
		t.Fatalf("publication chain not frozen: %#v", got)
	}
	if len(got.Tags) != 1 || got.Tags[0] != domain.GenerationHairMismatch {
		t.Fatalf("tags not persisted: %#v", got.Tags)
	}
}

func TestCreateGenerationFeedbackPersistsOptionalPhoto(t *testing.T) {
	f := newGenerationFeedbackFixture(t)
	media := insertFeedbackMedia(t, f.store, f.userA)
	cmd := generationFeedbackCommand(f, "photo-key", "hash-photo")
	cmd.MediaAssetID = &media
	got, created, err := f.store.CreateGenerationFeedback(ctx, cmd)
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	if got.MediaAssetID == nil || *got.MediaAssetID != media {
		t.Fatalf("media asset id not persisted: %#v", got.MediaAssetID)
	}
}

func TestCreateGenerationFeedbackRejectsCrossUserPublication(t *testing.T) {
	f := newGenerationFeedbackFixture(t)
	cmd := generationFeedbackCommand(f, "cross-key", "hash-cross")
	cmd.UserID = f.userB
	_, _, err := f.store.CreateGenerationFeedback(ctx, cmd)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("err=%v, want ErrNotFound", err)
	}
}

func TestCreateGenerationFeedbackRejectsForeignMedia(t *testing.T) {
	f := newGenerationFeedbackFixture(t)
	foreign := insertFeedbackMedia(t, f.store, f.userB)
	cmd := generationFeedbackCommand(f, "foreign-key", "hash-foreign")
	cmd.MediaAssetID = &foreign
	_, _, err := f.store.CreateGenerationFeedback(ctx, cmd)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("err=%v, want ErrNotFound", err)
	}
}

func TestCreateGenerationFeedbackRejectsWrongPurposeMedia(t *testing.T) {
	f := newGenerationFeedbackFixture(t)
	// 属于 userA 但 purpose 非 feedback 的资产必须被拒。
	id := uuid.NewString()
	_, err := f.store.pool.Exec(ctx, `
		INSERT INTO media_assets(id, user_id, origin, purpose, object_key, sha256, mime_type, byte_size, state, display_kind)
		VALUES ($1::uuid, $2::uuid, 'user_upload', 'face', $3, $4, 'image/jpeg', 1024, 'ready', 'original')`,
		id, f.userA, "users/"+f.userA+"/face/"+id+".jpg", schemaHex64())
	if err != nil {
		t.Fatalf("seed face media: %v", err)
	}
	cmd := generationFeedbackCommand(f, "purpose-key", "hash-purpose")
	cmd.MediaAssetID = &id
	_, _, err = f.store.CreateGenerationFeedback(ctx, cmd)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("err=%v, want ErrNotFound", err)
	}
}

func TestCreateGenerationFeedbackReplaysSameKey(t *testing.T) {
	f := newGenerationFeedbackFixture(t)
	first, created, err := f.store.CreateGenerationFeedback(ctx, generationFeedbackCommand(f, "replay-key", "hash-replay"))
	if err != nil || !created {
		t.Fatalf("first: created=%v err=%v", created, err)
	}
	second, created, err := f.store.CreateGenerationFeedback(ctx, generationFeedbackCommand(f, "replay-key", "hash-replay"))
	if err != nil || created {
		t.Fatalf("second: created=%v err=%v", created, err)
	}
	if second.ID != first.ID {
		t.Fatalf("replay returned different id: %s vs %s", second.ID, first.ID)
	}
	if n := countRows(t, f.store.pool, "generation_feedback"); n != 1 {
		t.Fatalf("generation_feedback rows = %d, want 1", n)
	}
}

func TestCreateGenerationFeedbackSameKeyDifferentHashConflicts(t *testing.T) {
	f := newGenerationFeedbackFixture(t)
	if _, created, err := f.store.CreateGenerationFeedback(ctx, generationFeedbackCommand(f, "conflict-key", "hash-a")); err != nil || !created {
		t.Fatalf("first: created=%v err=%v", created, err)
	}
	_, _, err := f.store.CreateGenerationFeedback(ctx, generationFeedbackCommand(f, "conflict-key", "hash-b"))
	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("err=%v, want ErrIdempotencyConflict", err)
	}
}

func TestCreateGenerationFeedbackConcurrentSameKeyInsertsOnce(t *testing.T) {
	f := newGenerationFeedbackFixture(t)
	const workers = 8
	var wg sync.WaitGroup
	ids := make([]string, workers)
	createds := make([]bool, workers)
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got, created, err := f.store.CreateGenerationFeedback(ctx, generationFeedbackCommand(f, "concurrent-key", "hash-concurrent"))
			ids[i] = got.ID
			createds[i] = created
			errs[i] = err
		}(i)
	}
	wg.Wait()
	createdCount := 0
	firstID := ""
	for i := 0; i < workers; i++ {
		if errs[i] != nil {
			t.Fatalf("worker %d err=%v", i, errs[i])
		}
		if createds[i] {
			createdCount++
		}
		if ids[i] == "" {
			t.Fatalf("worker %d returned empty id", i)
		}
		if firstID == "" {
			firstID = ids[i]
		}
		if ids[i] != firstID {
			t.Fatalf("divergent ids: %s vs %s", ids[i], firstID)
		}
	}
	if createdCount != 1 {
		t.Fatalf("created count = %d, want 1", createdCount)
	}
	if n := countRows(t, f.store.pool, "generation_feedback"); n != 1 {
		t.Fatalf("generation_feedback rows = %d, want 1", n)
	}
}

// executionFeedbackFixture 在已发布 PlanSet 上创建一次 Selection 与 Execution,
// 捕获 hair/makeup/outfit 三个执行步骤,供完成或保持未完成两种门禁用例。
type executionFeedbackFixture struct {
	store       *Store
	userA       string
	userB       string
	variantA    string
	selectionID string
	planSetID   string
	executionID string
	hairStep    string
	makeupStep  string
	outfitStep  string
}

func newExecutionFeedbackFixture(t *testing.T) *executionFeedbackFixture {
	t.Helper()
	store, fx := newExecutionSnapshotFixture(t)
	exec, _, err := store.CreateExecutionFromSelection(ctx, domain.CreateExecutionCommand{
		UserID: fx.UserA, SelectionID: fx.SelectionA,
		IdempotencyKey: "execution-feedback", RequestHash: hashA,
	})
	if err != nil {
		t.Fatal(err)
	}
	f := &executionFeedbackFixture{
		store: store, userA: fx.UserA, userB: fx.UserB,
		variantA: fx.VariantA, selectionID: fx.SelectionA, planSetID: fx.PlanSetA, executionID: exec.ID,
	}
	f.captureSteps(t)
	return f
}

// captureSteps 捕获当前执行的 hair/makeup/outfit 三个执行步骤 id。
func (f *executionFeedbackFixture) captureSteps(t *testing.T) {
	t.Helper()
	for _, cat := range []struct {
		field    *string
		category string
	}{
		{&f.hairStep, "hair"}, {&f.makeupStep, "makeup"}, {&f.outfitStep, "outfit"},
	} {
		if err := f.store.pool.QueryRow(ctx, `
			SELECT id::text FROM execution_steps
			WHERE user_id=$1::uuid AND execution_id=$2::uuid AND category=$3`,
			f.userA, f.executionID, cat.category).Scan(cat.field); err != nil {
			t.Fatal(err)
		}
	}
}

// completeExecution 依次追加 step_completed×3 与 completed 事件,推进到 completed。
func (f *executionFeedbackFixture) completeExecution(t *testing.T) {
	t.Helper()
	version := 1
	for _, stepID := range []string{f.hairStep, f.makeupStep, f.outfitStep} {
		if _, err := f.store.AppendExecutionEvent(ctx, domain.AppendEventCommand{
			UserID: f.userA, ExecutionID: f.executionID,
			ClientEventID: uuid.NewString(), Type: domain.EventStepCompleted,
			StepID: &stepID, OccurredAt: time.Now(), ExpectedVersion: version,
		}); err != nil {
			t.Fatal(err)
		}
		version++
	}
	if _, err := f.store.AppendExecutionEvent(ctx, domain.AppendEventCommand{
		UserID: f.userA, ExecutionID: f.executionID,
		ClientEventID: uuid.NewString(), Type: domain.EventCompleted,
		OccurredAt: time.Now(), ExpectedVersion: version,
	}); err != nil {
		t.Fatal(err)
	}
}

// newSecondExecution 在同一 PlanSet 上用另一 variant 再建一次 Selection 与 Execution。
func (f *executionFeedbackFixture) newSecondExecution(t *testing.T) *executionFeedbackFixture {
	t.Helper()
	var variantB string
	if err := f.store.pool.QueryRow(ctx, `
		SELECT id::text FROM plan_variants
		WHERE user_id=$1::uuid AND plan_set_id=$2::uuid AND id <> $3::uuid
		LIMIT 1`, f.userA, f.planSetID, f.variantA).Scan(&variantB); err != nil {
		t.Fatal(err)
	}
	selection, _, err := f.store.CreateSelection(ctx, domain.CreateSelectionCommand{
		UserID: f.userA, PlanSetID: f.planSetID, PlanVariantID: variantB,
		IdempotencyKey: "execution-feedback-selection-b", RequestHash: hashA,
	})
	if err != nil {
		t.Fatal(err)
	}
	exec, _, err := f.store.CreateExecutionFromSelection(ctx, domain.CreateExecutionCommand{
		UserID: f.userA, SelectionID: selection.ID,
		IdempotencyKey: "execution-feedback-execution-b", RequestHash: hashA,
	})
	if err != nil {
		t.Fatal(err)
	}
	out := &executionFeedbackFixture{
		store: f.store, userA: f.userA, userB: f.userB,
		variantA: variantB, selectionID: selection.ID, planSetID: f.planSetID, executionID: exec.ID,
	}
	out.captureSteps(t)
	return out
}

func executionFeedbackCommand(f *executionFeedbackFixture, key string, preference domain.StructuredPreference, tags []domain.Tag) domain.CreateExecutionFeedbackCommand {
	return domain.CreateExecutionFeedbackCommand{
		UserID:         f.userA,
		ExecutionID:    f.executionID,
		Tags:           tags,
		Preference:     preference,
		IdempotencyKey: key,
		RequestHash:    hashA,
	}
}

func TestExecutionFeedbackPersistsAndFormsMemory(t *testing.T) {
	f := newExecutionFeedbackFixture(t)
	f.completeExecution(t)
	got, created, err := f.store.CreateExecutionFeedback(ctx, executionFeedbackCommand(f, "exec-feedback-memory",
		domain.StructuredPreference{Kind: domain.PreferenceLessFormal, Category: domain.CategoryOverall},
		[]domain.Tag{domain.ExecutionTooFormal}))
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	if got.SelectionID != f.selectionID || got.PlanSetID != f.planSetID {
		t.Fatalf("selection/plan_set chain not frozen: %#v", got)
	}
	if len(got.AppliedMemories) != 1 || got.AppliedMemories[0].Key != "formality" || got.AppliedMemories[0].Value != "less" {
		t.Fatalf("memory not formed: %#v", got.AppliedMemories)
	}
	if n := countRows(t, f.store.pool, "preference_memories"); n != 1 {
		t.Fatalf("preference_memories rows = %d, want 1", n)
	}

	var prefs []byte
	if err := f.store.pool.QueryRow(ctx, `SELECT preferences FROM user_profiles WHERE user_id=$1::uuid`, f.userA).Scan(&prefs); err != nil {
		t.Fatal(err)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(prefs, &root); err != nil {
		t.Fatal(err)
	}
	var fm map[string]any
	if err := json.Unmarshal(root["feedback_memory"], &fm); err != nil {
		t.Fatal(err)
	}
	if fm["formality"] != "less" {
		t.Fatalf("feedback_memory projection = %#v", fm)
	}
}

func TestExecutionFeedbackRejectsIncompleteExecution(t *testing.T) {
	f := newExecutionFeedbackFixture(t)
	_, _, err := f.store.CreateExecutionFeedback(ctx, executionFeedbackCommand(f, "exec-feedback-active",
		domain.StructuredPreference{}, []domain.Tag{domain.ExecutionEasy}))
	if !errors.Is(err, domain.ErrExecutionNotCompleted) {
		t.Fatalf("err=%v, want ErrExecutionNotCompleted", err)
	}
}

func TestExecutionFeedbackReplaysSameKey(t *testing.T) {
	f := newExecutionFeedbackFixture(t)
	f.completeExecution(t)
	cmd := executionFeedbackCommand(f, "exec-feedback-replay",
		domain.StructuredPreference{Kind: domain.PreferenceLessFormal, Category: domain.CategoryOverall},
		[]domain.Tag{domain.ExecutionTooFormal})
	first, created, err := f.store.CreateExecutionFeedback(ctx, cmd)
	if err != nil || !created {
		t.Fatalf("first: created=%v err=%v", created, err)
	}
	second, created, err := f.store.CreateExecutionFeedback(ctx, cmd)
	if err != nil || created {
		t.Fatalf("second: created=%v err=%v", created, err)
	}
	if second.ID != first.ID || len(second.AppliedMemories) != 1 {
		t.Fatalf("replay mismatch: %#v", second)
	}
	if n := countRows(t, f.store.pool, "execution_feedback"); n != 1 {
		t.Fatalf("execution_feedback rows = %d, want 1", n)
	}
}

func TestPreferenceMemoryListReturnsRecentFirst(t *testing.T) {
	f := newExecutionFeedbackFixture(t)
	f.completeExecution(t)
	if _, _, err := f.store.CreateExecutionFeedback(ctx, executionFeedbackCommand(f, "exec-feedback-avoid",
		domain.StructuredPreference{Kind: domain.PreferenceAvoid, Category: domain.CategoryColor, Value: "荧光绿"},
		[]domain.Tag{domain.ExecutionDislikeColor})); err != nil {
		t.Fatal(err)
	}

	second := f.newSecondExecution(t)
	second.completeExecution(t)
	if _, _, err := second.store.CreateExecutionFeedback(ctx, executionFeedbackCommand(second, "exec-feedback-formality",
		domain.StructuredPreference{Kind: domain.PreferenceLessFormal, Category: domain.CategoryOverall},
		[]domain.Tag{domain.ExecutionTooFormal})); err != nil {
		t.Fatal(err)
	}

	memories, err := f.store.ListPreferenceMemories(ctx, f.userA, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(memories) != 2 {
		t.Fatalf("list len = %d, want 2", len(memories))
	}
	if memories[0].Key != "formality" || memories[1].Key != "avoid_color" {
		t.Fatalf("order = %#v", memories)
	}
}
