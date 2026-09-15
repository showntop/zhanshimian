package bootstrap

import (
	"context"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/config"
	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/assessment"
	"github.com/zhanshimian/server/internal/service/home"
	"github.com/zhanshimian/server/internal/service/taskrunner"
	"github.com/zhanshimian/server/internal/service/today"
	"github.com/zhanshimian/server/internal/storage"
)

// 本地存储没有签名能力：读路径必须回退 PUBLIC_BASE_URL + /uploads/ 公开路径，
// 否则报告源图/demo 媒体在开发环境拿不到 URL。
func TestMediaPresenterLocalFallsBackToPublicUploads(t *testing.T) {
	objects, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	presenter := newMediaPresenter(objects, config.Config{
		PublicBaseURL: "http://localhost:58000/", AssetURLTTL: 15 * time.Minute,
	})

	presented, err := presenter.Present(context.Background(), domain.MediaAsset{
		ObjectKey: "users/user-1/uploads/intent-1", MIMEType: "image/jpeg",
		Origin: domain.MediaOriginUserUpload, DisplayKind: domain.DisplayKindOriginal,
	})
	if err != nil {
		t.Fatal(err)
	}
	if presented.URL != "http://localhost:58000/uploads/users/user-1/uploads/intent-1" {
		t.Fatalf("url = %q", presented.URL)
	}
	if presented.URLExpiresAt.IsZero() {
		t.Fatal("url_expires_at must be set even on the public fallback")
	}
	if presented.SourceKind != "user_original" || presented.DisplayLabel != "原本" {
		t.Fatalf("source mapping = %q/%q", presented.SourceKind, presented.DisplayLabel)
	}
}

func TestHomeMediaSignerLocalFallsBackToPublicUploads(t *testing.T) {
	objects, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	signer := newMediaURLSigner(objects, "http://localhost:58000", 15*time.Minute)

	url, expiresAt, err := signer.SignedURL(context.Background(), "users/user-1/render-published/pub-1.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if url != "http://localhost:58000/uploads/users/user-1/render-published/pub-1.jpg" {
		t.Fatalf("url = %q", url)
	}
	if expiresAt.IsZero() {
		t.Fatal("expiresAt must be set even on the public fallback")
	}
}

func TestDemoBundledAssetMatchesContractKinds(t *testing.T) {
	for _, kind := range []string{"face", "side", "body", "outfit"} {
		file, ext, mime := demoBundledAsset(kind)
		if file == "" || ext != "jpg" || mime != "image/jpeg" {
			t.Fatalf("%s bundled = %q/%q/%q", kind, file, ext, mime)
		}
	}
}

// ---- 适配器转换：home 端口 ← 具体服务的公开读方法 ----

type fakeAssessmentRepo struct {
	report domain.AssessmentReport
	err    error
}

func (f fakeAssessmentRepo) CreateOrReuseAssessment(context.Context, domain.CreateAssessmentParams) (domain.CreatedAssessment, error) {
	return domain.CreatedAssessment{}, repository.ErrNotFound
}

func (f fakeAssessmentRepo) GetReport(context.Context, string, string) (domain.AssessmentReport, error) {
	return f.report, f.err
}

func (f fakeAssessmentRepo) GetCurrentReport(context.Context, string) (domain.AssessmentReport, error) {
	return f.report, f.err
}

// homeReportReader 必须复用 assessment 读路径的呈现（含 source_media 签名/
// 回退 URL），只做类型转换，不丢字段。
func TestHomeReportReaderConvertsAssessmentView(t *testing.T) {
	created := time.Date(2026, 9, 10, 8, 30, 0, 0, time.UTC)
	asset := domain.MediaAsset{
		ID: "asset-face", ObjectKey: "users/user-1/uploads/face", MIMEType: "image/jpeg",
		Origin: domain.MediaOriginUserUpload, DisplayKind: domain.DisplayKindOriginal, State: domain.MediaStateReady,
	}
	repo := fakeAssessmentRepo{report: domain.AssessmentReport{
		Report: domain.Report{
			ID: "report-1", PhotoSetID: "photo-set-1", PriorityTitle: "先提亮气色",
			PriorityCopy: "从发色开始", SchemaVersion: "analyzer.v1",
			ImpressionTags: []string{"清爽"},
			Findings: []domain.ReportFinding{{
				ID: "finding-1", Category: "hair", Label: "发色", VisibleObservation: "发色偏深",
				Recommendation: "提亮一度", Priority: 1, Position: 1,
				SourcePhotoItemID: "item-face",
				Anchor:            domain.EvidenceAnchor{X: 0.1, Y: 0.2, W: 0.3, H: 0.4},
			}},
			CreatedAt: created,
		},
		PhotoSet: domain.PhotoSet{
			ID: "photo-set-1",
			Items: []domain.PhotoSetItem{
				{ID: "item-face", Role: domain.PhotoRoleFace, Asset: asset},
				{ID: "item-side", Role: domain.PhotoRoleSide, Asset: asset},
				{ID: "item-body", Role: domain.PhotoRoleBody, Asset: asset},
			},
		},
	}}
	objects, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	presenter := newMediaPresenter(objects, config.Config{PublicBaseURL: "http://localhost:58000", AssetURLTTL: 15 * time.Minute})
	svc := assessment.NewService(repo, nil, nil, presenter, taskrunner.Definition{})
	reader := homeReportReader{inner: svc}

	var port home.ReportReader = reader // 编译期契约：适配器满足 home 端口。
	view, err := port.GetCurrentReport(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if view.ID != "report-1" || view.PhotoSetID != "photo-set-1" || view.SchemaVersion != "analyzer.v1" {
		t.Fatalf("view identity = %#v", view)
	}
	face := view.SourceMedia.Face
	if face.ItemID != "item-face" || face.Role != domain.PhotoRoleFace {
		t.Fatalf("face photo = %#v", face)
	}
	if face.Media.URL != "http://localhost:58000/uploads/users/user-1/uploads/face" {
		t.Fatalf("face url = %q", face.Media.URL)
	}
	if face.Media.AssetID != "asset-face" || face.Media.SourceKind != "user_original" || face.Media.DisplayLabel != "原本" {
		t.Fatalf("face media = %#v", face.Media)
	}
	if len(view.Findings) != 1 || view.Findings[0].SourcePhoto.ItemID != "item-face" ||
		view.Findings[0].SourcePhoto.Role != domain.PhotoRoleFace ||
		view.Findings[0].Anchor.W != 0.3 {
		t.Fatalf("findings = %#v", view.Findings)
	}

	notFound := homeReportReader{inner: assessment.NewService(
		fakeAssessmentRepo{err: repository.ErrNotFound}, nil, nil, presenter, taskrunner.Definition{})}
	if _, err := notFound.GetCurrentReport(context.Background(), "user-1"); err != repository.ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound passthrough", err)
	}
}

type fakeTodayWriter struct {
	plan today.Plan
	err  error
}

func (f fakeTodayWriter) CreateTodayPlan(_ context.Context, _ string, plan today.Plan) (today.Plan, error) {
	return plan, nil
}

func (f fakeTodayWriter) CurrentTodayPlan(context.Context, string) (today.Plan, error) {
	return f.plan, f.err
}

func (f fakeTodayWriter) MarkTodayPlanActive(_ context.Context, _ string, id string) (today.Plan, error) {
	return f.plan, f.err
}

func (f fakeTodayWriter) RecordTodayPlanFeedback(_ context.Context, _ string, _ string, _ string) (today.Plan, error) {
	return f.plan, f.err
}

func TestHomeTodayReaderConvertsPlan(t *testing.T) {
	created := time.Date(2026, 9, 12, 9, 0, 0, 0, time.UTC)
	feedback := "再轻松一点"
	writer := fakeTodayWriter{plan: today.Plan{
		ID: "today-1", ReportID: "report-1", Title: "今日利落通勤", Summary: "浅色提亮",
		Context:   domain.TodayContext{City: "杭州", DayType: "工作日"},
		Steps:     []domain.TodayPlanStep{{Category: "outfit", Label: "单品", Title: "衬衫", Copy: "浅色"}},
		Active:    true,
		State:     "ready",
		Operation: domain.OperationRef{ID: "op-1", Kind: domain.OperationRender, Status: domain.OperationRunning},
		Media: &domain.RenderMediaView{
			AssetID: "asset-today", SourceKind: "generated_preview", DisplayLabel: "风格参考",
		},
		Feedback:  &feedback,
		CreatedAt: created, UpdatedAt: created,
	}}
	svc := today.New(nil, writer, nil, nil, today.NewClock())
	reader := homeTodayReader{inner: svc}

	var port home.TodayReader = reader // 编译期契约：适配器满足 home 端口。
	plan, err := port.Current(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if plan.ID != "today-1" || plan.Title != "今日利落通勤" || !plan.Active || plan.State != "ready" {
		t.Fatalf("plan = %#v", plan)
	}
	if plan.Operation.ID != "op-1" || plan.Media == nil || plan.Media.AssetID != "asset-today" {
		t.Fatalf("plan operation/media = %#v %#v", plan.Operation, plan.Media)
	}
	if plan.Feedback == nil || *plan.Feedback != feedback {
		t.Fatalf("plan feedback = %#v", plan.Feedback)
	}
	if len(plan.Steps) != 1 || plan.Context.City != "杭州" {
		t.Fatalf("plan steps/context = %#v %#v", plan.Steps, plan.Context)
	}

	empty := homeTodayReader{inner: today.New(nil, fakeTodayWriter{err: repository.ErrNotFound}, nil, nil, today.NewClock())}
	if _, err := empty.Current(context.Background(), "user-1"); err != repository.ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound passthrough", err)
	}
}
