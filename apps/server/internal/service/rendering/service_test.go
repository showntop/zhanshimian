package rendering

import (
	"context"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

func TestStartRunUsesValidatedRenderSpecAndOneInitialCandidate(t *testing.T) {
	repo := newRepoFake()
	repo.spec = validRenderSpec("user-1", "variant-1")
	svc := New(repo, nil, nil, nil, Config{
		RoutingPolicyVersion: "render-route-v1",
		QualityPolicyVersion: "render-quality-v1",
	})

	got, err := svc.StartRun(context.Background(), StartRunCommand{
		UserID: "user-1", PlanVariantID: "variant-1", IdempotencyKey: "idem-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Run.CandidateLimit != 1 || got.Run.Generation != 1 {
		t.Fatalf("unexpected run: %#v", got.Run)
	}
	if repo.enqueued.Ordinal != 1 || repo.enqueued.SubjectGeneration != 1 {
		t.Fatalf("unexpected initial task: %#v", repo.enqueued)
	}
	if repo.command.RoutingPolicyVersion != "render-route-v1" || repo.command.QualityPolicyVersion != "render-quality-v1" {
		t.Fatalf("policy versions not propagated: %#v", repo.command)
	}
}

func TestStartRunReusesSameIdempotencyKey(t *testing.T) {
	repo := newRepoFake()
	repo.spec = validRenderSpec("user-1", "variant-1")
	svc := New(repo, nil, nil, nil, testConfig())
	first, err := svc.StartRun(context.Background(), StartRunCommand{
		UserID: "user-1", PlanVariantID: "variant-1", IdempotencyKey: "same",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.StartRun(context.Background(), StartRunCommand{
		UserID: "user-1", PlanVariantID: "variant-1", IdempotencyKey: "same",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Run.ID != second.Run.ID || repo.createCalls != 1 {
		t.Fatalf("idempotency failed: first=%s second=%s calls=%d",
			first.Run.ID, second.Run.ID, repo.createCalls)
	}
}

func TestStartRunReservesOnceAndNotOnReuse(t *testing.T) {
	repo := newRepoFake()
	repo.spec = validRenderSpec("user-1", "variant-1")
	billing := &billingFake{}
	svc := New(repo, nil, nil, nil, testConfig()).WithBilling(billing)
	cmd := StartRunCommand{UserID: "user-1", PlanVariantID: "variant-1", IdempotencyKey: "idem-1"}
	if _, err := svc.StartRun(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StartRun(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	if len(billing.calls) != 1 {
		t.Fatalf("reserve calls = %d, want 1", len(billing.calls))
	}
	got := billing.calls[0]
	if got.userID != "user-1" || got.operationID != "operation-1" ||
		got.product != domain.ProductRenderPublication || got.units != 1 {
		t.Fatalf("unexpected reserve call: %#v", got)
	}
}

func TestStartRunRejectsInvalidSpecWithoutCreatingAnything(t *testing.T) {
	repo := newRepoFake()
	repo.spec = validRenderSpec("user-1", "variant-1")
	repo.spec.Spec.Composition.AllowOutpaint = true
	svc := New(repo, nil, nil, nil, testConfig())
	if _, err := svc.StartRun(context.Background(), StartRunCommand{
		UserID: "user-1", PlanVariantID: "variant-1", IdempotencyKey: "idem-1",
	}); err == nil {
		t.Fatal("outpainting spec must be rejected")
	}
	if repo.createCalls != 0 {
		t.Fatalf("invalid spec created %d runs", repo.createCalls)
	}
}

func TestStartRunRejectsIdenticalBodyAndFaceAssets(t *testing.T) {
	repo := newRepoFake()
	repo.spec = validRenderSpec("user-1", "variant-1")
	repo.spec.Spec.Identity.FaceAssetID = repo.spec.Spec.Identity.BodyAssetID
	svc := New(repo, nil, nil, nil, testConfig())
	if _, err := svc.StartRun(context.Background(), StartRunCommand{
		UserID: "user-1", PlanVariantID: "variant-1", IdempotencyKey: "idem-1",
	}); err == nil {
		t.Fatal("identical body/face assets must be rejected")
	}
}

func TestGetRunSignsPublishedURLAndHidesInternals(t *testing.T) {
	repo := newRepoFake()
	repo.spec = validRenderSpec("user-1", "variant-1")
	repo.run = domain.RenderRun{ID: "run-1", UserID: "user-1", Outcome: domain.RenderOutcomePublished}
	repo.publication = &domain.RenderPublication{
		ID: "pub-1", AssetID: "asset-1", ObjectKey: "users/user-1/render-published/pub-1.jpg",
		URL: "internal://pub-1",
	}
	svc := New(repo, nil, nil, nil, testConfig())
	svc.RunSigner(func(context.Context, string, time.Duration) (string, error) {
		return "https://signed.example/pub-1.jpg", nil
	})
	view, err := svc.GetRun(context.Background(), "user-1", "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if view.Render.State != domain.RenderStateReady || view.Render.Media == nil {
		t.Fatalf("unexpected view: %#v", view.Render)
	}
	if view.Render.Media.URL != "https://signed.example/pub-1.jpg" {
		t.Fatalf("url not signed: %q", view.Render.Media.URL)
	}
	if view.Render.Media.DisplayLabel != domain.DisplayLabelStyleReference {
		t.Fatalf("label drifted: %q", view.Render.Media.DisplayLabel)
	}
}

func TestGetRunCrossTenantIsNotFound(t *testing.T) {
	repo := newRepoFake()
	repo.getErr = repository.ErrNotFound
	svc := New(repo, nil, nil, nil, testConfig())
	if _, err := svc.GetRun(context.Background(), "user-b", "run-1"); err != repository.ErrNotFound {
		t.Fatalf("got %v", err)
	}
}

// ---- fakes ----

func newRepoFake() *repoFake { return &repoFake{} }

type repoFake struct {
	spec        domain.RenderSpec
	run         domain.RenderRun
	publication *domain.RenderPublication
	operation   domain.Operation
	getErr      error
	createCalls int
	command     CreateRunCommand
	enqueued    struct {
		Ordinal           int
		SubjectGeneration int
	}
}

func (r *repoFake) GetRenderSpecForVariant(context.Context, string, string) (domain.RenderSpec, error) {
	return r.spec, nil
}

func (r *repoFake) CreateRun(_ context.Context, command CreateRunCommand) (CreateRunResult, error) {
	// 模拟仓储幂等:相同语义输入直接返回既有结果,不算一次创建。
	if r.command == command && r.createCalls > 0 {
		return CreateRunResult{
			Run: r.run,
			Operation: domain.OperationRef{
				ID: "operation-1", Kind: domain.OperationRender, Status: domain.OperationAccepted,
			},
			Created: false,
		}, nil
	}
	r.createCalls++
	r.command = command
	r.run = domain.RenderRun{
		ID: "run-1", UserID: command.UserID, PlanVariantID: command.PlanVariantID,
		RenderSpecID: command.RenderSpecID, Generation: 1, CandidateLimit: 1,
		RoutingPolicyVersion: command.RoutingPolicyVersion,
		QualityPolicyVersion: command.QualityPolicyVersion,
	}
	r.enqueued.Ordinal = 1
	r.enqueued.SubjectGeneration = 1
	return CreateRunResult{
		Run: r.run,
		Operation: domain.OperationRef{
			ID: "operation-1", Kind: domain.OperationRender, Status: domain.OperationAccepted,
		},
		Created: true,
	}, nil
}

func (r *repoFake) GetRun(context.Context, string, string) (domain.RenderRun, *domain.RenderPublication, domain.Operation, error) {
	if r.getErr != nil {
		return domain.RenderRun{}, nil, domain.Operation{}, r.getErr
	}
	return r.run, r.publication, r.operation, nil
}

func (r *repoFake) ListCurrentByVariantIDs(context.Context, string, []string) (map[string]CurrentRender, error) {
	return nil, nil
}

func validRenderSpec(userID, variantID string) domain.RenderSpec {
	return domain.RenderSpec{
		ID: "spec-1", UserID: userID, PlanVariantID: variantID,
		SchemaVersion: "render_spec.v1",
		Spec: domain.RenderDirective{
			Identity: domain.RenderIdentity{
				BodyAssetID: "asset-body", FaceAssetID: "asset-face",
				PreserveIdentity: true, PreserveBodyProportion: true,
				PreserveSkinTone: true, PreserveAgeImpression: true,
			},
			Composition: domain.RenderComposition{
				PreservePose: true, PreserveBackground: true,
				PreserveLighting: true, PreserveSourceCrop: true, AllowOutpaint: false,
			},
			Output: domain.RenderOutput{
				MIMEType: "image/jpeg", AspectPolicy: "preserve_body_source", Quality: "high",
			},
		},
	}
}

func testConfig() Config {
	return Config{RoutingPolicyVersion: "render-route-v1", QualityPolicyVersion: "render-quality-v1"}
}

func (r *repoFake) GetCandidateJob(context.Context, string, string, int) (CandidateJob, error) {
	return CandidateJob{}, nil
}

func (r *repoFake) RecordCandidate(context.Context, RecordCandidateCommand) (domain.RenderCandidate, error) {
	return domain.RenderCandidate{}, nil
}

func (r *repoFake) ExpandCandidateBudget(context.Context, EnqueueNextCandidateCommand) (string, error) {
	return "", nil
}

func (r *repoFake) FailRun(context.Context, FailRunCommand) error { return nil }

func (r *repoFake) CommitEvaluation(context.Context, CommitEvaluationCommand) (CommitEvaluationResult, error) {
	return CommitEvaluationResult{}, nil
}

type billingFake struct {
	calls []reserveCall
}

type reserveCall struct {
	userID, operationID string
	product             domain.Product
	units               int
}

func (b *billingFake) Reserve(_ context.Context, userID, operationID string, product domain.Product, units int) (domain.Reservation, error) {
	b.calls = append(b.calls, reserveCall{userID: userID, operationID: operationID, product: product, units: units})
	return domain.Reservation{ID: "reservation-1"}, nil
}
