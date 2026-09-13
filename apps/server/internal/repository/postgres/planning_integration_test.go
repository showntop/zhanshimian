package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

func TestPrepareAndCommitPlanSetIsAtomicImmutableAndTenantScoped(t *testing.T) {
	store, users := newPlanningStore(t)
	lease := validDatabaseLease(t, store, users)
	command := validPrepareCommand(users)
	got, err := store.Prepare(context.Background(), lease, command)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Get(context.Background(), users.A, got.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("prepared result became visible before commit: %v", err)
	}
	outcome, err := store.CommitPrepared(context.Background(), lease, domain.TaskResult{
		Disposition: domain.TaskPublish, ResultType: "plan_set", ResultID: got.ID,
	})
	if err != nil || outcome != domain.CommitApplied {
		t.Fatalf("outcome=%s err=%v", outcome, err)
	}
	if len(got.Variants) != 3 || totalPlanSteps(got) != 9 || totalPlanGroundings(got) < 9 {
		t.Fatalf("incomplete graph: %d variants, %d steps, %d groundings",
			len(got.Variants), totalPlanSteps(got), totalPlanGroundings(got))
	}
	published, err := store.Get(context.Background(), users.A, got.ID)
	if err != nil {
		t.Fatalf("published graph is not readable: %v", err)
	}
	if published.SceneBrief.Scene != domain.SceneDaily || published.BriefHash == "" {
		t.Fatalf("brief not persisted: %#v", published.SceneBrief)
	}
	if _, err = store.Get(context.Background(), users.B, got.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("cross-tenant read = %v", err)
	}
	if _, err = store.pool.Exec(context.Background(),
		`UPDATE plan_sets SET scene='date' WHERE id=$1::uuid AND user_id=$2::uuid`, got.ID, users.A); err == nil {
		t.Fatal("immutable plan set accepted an UPDATE")
	} else if pgCode(err) != "55000" {
		t.Fatalf("immutable update code = %q, err=%v", pgCode(err), err)
	}
}

func TestPreparePlanSetRejectsIncompleteGraph(t *testing.T) {
	store, users := newPlanningStore(t)
	lease := validDatabaseLease(t, store, users)
	command := validPrepareCommand(users)
	command.PlanSet.Variants = command.PlanSet.Variants[:2]
	if _, err := store.Prepare(context.Background(), lease, command); err == nil {
		t.Fatal("two variants committed")
	}
	if n := countRows(t, store.pool, "plan_sets"); n != 0 {
		t.Fatalf("plan_sets rows = %d, want 0", n)
	}
	var status string
	if err := store.pool.QueryRow(context.Background(),
		`SELECT status FROM operations WHERE id=$1::uuid`, lease.OperationID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status == string(domain.OperationSucceeded) {
		t.Fatal("operation succeeded on a rejected prepare")
	}
}

func TestPlanningStaleLeaseCannotCommit(t *testing.T) {
	store, users := newPlanningStore(t)
	lease := validDatabaseLease(t, store, users)
	command := validPrepareCommand(users)
	got, err := store.Prepare(context.Background(), lease, command)
	if err != nil {
		t.Fatal(err)
	}
	stale := lease
	stale.LeaseToken = uuid.NewString()
	outcome, err := store.CommitPrepared(context.Background(), stale, domain.TaskResult{
		Disposition: domain.TaskPublish, ResultType: "plan_set", ResultID: got.ID,
	})
	if outcome != domain.CommitSuperseded || err != nil {
		t.Fatalf("outcome=%s err=%v", outcome, err)
	}
}

func TestRenderSpecReaderIsTenantScopedAndTyped(t *testing.T) {
	store, users := newPlanningStore(t)
	lease := validDatabaseLease(t, store, users)
	command := validPrepareCommand(users)
	got, err := store.Prepare(context.Background(), lease, command)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.GetRenderSpecForVariant(context.Background(), users.A, got.Variants[0].ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("uncommitted spec visible: %v", err)
	}
	if _, err = store.CommitPrepared(context.Background(), lease, domain.TaskResult{
		Disposition: domain.TaskPublish, ResultType: "plan_set", ResultID: got.ID,
	}); err != nil {
		t.Fatal(err)
	}
	spec, err := store.GetRenderSpecForVariant(context.Background(), users.A, got.Variants[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if spec.PlanVariantID != got.Variants[0].ID || spec.SourcePhotoSetID != users.photoSetID {
		t.Fatalf("spec linkage mismatch: %#v", spec)
	}
	if err := spec.Validate(); err != nil {
		t.Fatalf("stored spec must validate: %v", err)
	}
	if _, err = store.GetRenderSpecForVariant(context.Background(), users.B, got.Variants[0].ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("cross-tenant spec read = %v", err)
	}
}

// ---- fixtures ----

type planningUsers struct {
	A, B        string
	planSetID   string
	photoSetID  string
	reportID    string
	faceAssetID string
	bodyAssetID string
	invocation  string
}

// newPlanningStore reuses the assessment fixture: a published report with a
// face/side/body photo set is exactly what planning builds upon.
func newPlanningStore(t *testing.T) (*Store, *planningUsers) {
	t.Helper()
	store, fixture := publishedAssessmentStore(t)
	users := &planningUsers{
		A:           fixture.userID,
		B:           fixture.otherUserID,
		photoSetID:  "50000000-0000-0000-0000-000000000001",
		reportID:    fixture.reportID,
		faceAssetID: fixture.slots.FaceAssetID,
		bodyAssetID: fixture.slots.BodyAssetID,
	}
	var photoSetID string
	if err := store.pool.QueryRow(context.Background(),
		`SELECT id::text FROM photo_sets WHERE user_id=$1::uuid LIMIT 1`, users.A).Scan(&photoSetID); err != nil {
		t.Fatal(err)
	}
	users.photoSetID = photoSetID
	return store, users
}

// validDatabaseLease starts a real planning operation+task and claims it.
func validDatabaseLease(t *testing.T, store *Store, users *planningUsers) domain.TaskLease {
	t.Helper()
	ops := NewPlanningOperations(store, 3)
	users.planSetID = uuid.NewString()
	planSetID := users.planSetID
	dedupe := "plan-set:integration:" + uuid.NewString()
	ref, created, err := ops.StartWithTask(context.Background(), domain.PlanningStartOperationCommand{
		OperationID: uuid.NewString(),
		UserID:      users.A,
		Kind:        domain.OperationPlanSet,
		SubjectType: domain.PlanningSubjectType,
		SubjectID:   planSetID,
		DedupeKey:   dedupe,
		Task: domain.PlanningEnqueueTask{
			Type:              domain.TaskType("plan_set.generate"),
			SubjectType:       domain.PlanningSubjectType,
			SubjectID:         planSetID,
			SubjectGeneration: 1,
			PayloadVersion:    1,
			Payload:           map[string]string{"plan_set_id": planSetID},
			DedupeKey:         dedupe,
		},
	})
	if err != nil || !created {
		t.Fatalf("start: created=%v err=%v", created, err)
	}
	lease, ok, err := store.Claim(context.Background(), "worker-planning", 30*time.Second, []domain.TaskType{"plan_set.generate"})
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	users.invocation = insertPlanningInvocation(t, store, users.A, ref.ID, lease.ID)
	return lease
}

func insertPlanningInvocation(t *testing.T, store *Store, userID, operationID, taskID string) string {
	t.Helper()
	var id string
	err := store.pool.QueryRow(context.Background(), `
		INSERT INTO provider_invocations(
			user_id, operation_id, task_id, attempt_no, capability,
			routing_config_version, provider_key, model_key, protocol, request_hash, status
		) VALUES ($1::uuid,$2::uuid,$3::uuid,0,'plan_set_generation','v1','p','m','http',$4,'succeeded')
		RETURNING id::text`,
		userID, operationID, taskID, schemaHex64()).Scan(&id)
	if err != nil {
		t.Fatalf("insert planning invocation: %v", err)
	}
	return id
}

// validPrepareCommand builds a complete graph: 3 variants × 3 steps × 1+
// groundings + 3 render specs, all with fixed row ids.
func validPrepareCommand(users *planningUsers) domain.PlanningPrepareCommand {
	brief := domain.SceneBrief{
		SchemaVersion: "brief.v1",
		Scene:         domain.SceneDaily,
		Answers: map[string]string{
			"activity": "office", "weather": "air_conditioned",
			"preparation": "closet", "impression": "natural",
		},
	}
	planSetID := users.planSetID
	command := domain.PlanningPrepareCommand{
		UserID: users.A,
		PlanSet: domain.PlanSet{
			ID:                   planSetID,
			UserID:               users.A,
			ReportID:             users.reportID,
			ProfileSnapshot:      []byte(`{"role":"designer"}`),
			Scene:                domain.SceneDaily,
			SceneBrief:           brief,
			BriefHash:            "ab7d1c0b3e2f4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8091",
			PlanningInputHash:    "cd7d1c0b3e2f4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8091",
			PlannerSchemaVersion: "plan-set.v1",
			StyleRuleVersion:     "style-rules.v1",
			ProviderInvocationID: users.invocation,
			QualityEvaluationID:  uuid.NewString(),
		},
		Quality: domain.PlanningPlanQualityRecord{
			ID:                    uuid.NewString(),
			UserID:                users.A,
			SubjectID:             planSetID,
			PolicyVersion:         "plan-set.v1",
			Decision:              "pass",
			ReasonCodes:           []string{},
			InternalScores:        []byte(`{}`),
			EvaluatorInvocationID: users.invocation,
		},
	}
	keys := []domain.PlanVariantKey{domain.VariantSharp, domain.VariantWarm, domain.VariantNatural}
	for slot, key := range keys {
		variantID := uuid.NewString()
		variant := domain.PlanVariant{
			ID: variantID, UserID: users.A, PlanSetID: planSetID,
			Slot: slot + 1, Key: key,
			Name: "方案" + string(key), Descriptor: "有精神且自然", Rationale: "落实报告优先建议",
			Recommended: slot == 0,
			OutcomeTags: []string{"易执行"}, DifferenceTags: []string{"发型线条"},
		}
		categories := []domain.StepCategory{domain.StepCategoryHair, domain.StepCategoryMakeup, domain.StepCategoryOutfit}
		for position, category := range categories {
			stepID := uuid.NewString()
			step := domain.PlanStep{
				ID: stepID, UserID: users.A, PlanVariantID: variantID,
				Category: category,
				Action:   domain.ActionAdjust,
				Title:    "调整" + string(category), Summary: "按报告依据微调。",
				Position: position + 1,
				Details: domain.PlanStepDetails{
					Target: "目标" + string(category), Intensity: "low",
					Silhouette: "合肩直线版型", Palette: []string{"象牙白", "深灰"},
					Layers: []string{"浅色内搭"}, Avoid: []string{"夸张图案"},
					Formality: "smart_casual",
				},
				Groundings: []domain.PlanStepGrounding{{
					UserID: users.A, PlanStepID: stepID,
					SourceType: domain.SourceReportFinding, SourceID: "21000000-0000-0000-0000-000000000001",
					Reason: "落实最高优先 finding",
				}},
			}
			variant.Steps = append(variant.Steps, step)
		}
		command.PlanSet.Variants = append(command.PlanSet.Variants, variant)
		command.RenderSpecs = append(command.RenderSpecs, domain.RenderSpec{
			ID:               uuid.NewString(),
			UserID:           users.A,
			PlanVariantID:    variantID,
			SourcePhotoSetID: users.photoSetID,
			SchemaVersion:    "render_spec.v1",
			Spec: domain.RenderDirective{
				Identity: domain.RenderIdentity{
					BodyAssetID: users.bodyAssetID, FaceAssetID: users.faceAssetID,
					PreserveIdentity: true, PreserveBodyProportion: true,
					PreserveSkinTone: true, PreserveAgeImpression: true,
				},
				Composition: domain.RenderComposition{
					PreservePose: true, PreserveBackground: true,
					PreserveLighting: true, PreserveSourceCrop: true, AllowOutpaint: false,
				},
				Hair:   domain.RenderHair{Action: "adjust", Target: "抬高发型重心", Intensity: "low"},
				Makeup: domain.RenderMakeup{Action: "keep", Target: "保持干净眉形", Intensity: "low"},
				Outfit: domain.RenderOutfit{
					Silhouette: "合肩直线版型", Palette: []string{"象牙白"},
					Layers: []string{"浅色内搭"}, Avoid: []string{"夸张图案"},
				},
				Output: domain.RenderOutput{MIMEType: "image/jpeg", AspectPolicy: "preserve_body_source", Quality: "high"},
			},
			ContentHash: "cd7d1c0b3e2f4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8092",
		})
	}
	return command
}

func totalPlanSteps(planSet domain.PlanSet) int {
	total := 0
	for _, variant := range planSet.Variants {
		total += len(variant.Steps)
	}
	return total
}

func totalPlanGroundings(planSet domain.PlanSet) int {
	total := 0
	for _, variant := range planSet.Variants {
		for _, step := range variant.Steps {
			total += len(step.Groundings)
		}
	}
	return total
}

func pgCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}
