package home

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

type readerSpy struct {
	snapshot      Snapshot
	latestPlanSet string
	mediaObject   MediaObject
	readHomeCalls int
	err           error
}

func (r *readerSpy) ReadHome(context.Context, string, time.Time) (Snapshot, error) {
	r.readHomeCalls++
	return r.snapshot, r.err
}

func (r *readerSpy) LatestPublishedPlanSetID(context.Context, string) (string, error) {
	if r.latestPlanSet == "" {
		return "", repository.ErrNotFound
	}
	return r.latestPlanSet, nil
}

func (r *readerSpy) MediaObjectInfo(context.Context, string, string) (MediaObject, error) {
	if r.mediaObject.ObjectKey == "" {
		return MediaObject{}, repository.ErrNotFound
	}
	return r.mediaObject, nil
}

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

type reportReaderFake struct {
	view ReportView
	err  error
}

func (f reportReaderFake) GetCurrentReport(context.Context, string) (ReportView, error) {
	return f.view, f.err
}

type planSetReaderFake struct {
	planSet domain.PlanSet
	err     error
}

func (f planSetReaderFake) GetPlanSet(context.Context, string, string) (domain.PlanSet, error) {
	return f.planSet, f.err
}

type todayReaderFake struct {
	plan TodayPlan
	err  error
}

func (f todayReaderFake) Current(context.Context, string) (TodayPlan, error) {
	return f.plan, f.err
}

type billingReaderFake struct {
	summary domain.BillingSummary
	err     error
}

func (f billingReaderFake) BillingSummary(context.Context, string) (domain.BillingSummary, error) {
	return f.summary, f.err
}

type signerFake struct {
	url       string
	expiresAt time.Time
	err       error
}

func (f signerFake) SignedURL(context.Context, string) (string, time.Time, error) {
	return f.url, f.expiresAt, f.err
}

func newTestService(reader Reader, deps Dependencies) *Service {
	return New(reader, fixedClock{now: time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)}, deps)
}

func TestBootstrapUsesSingleReadModelCall(t *testing.T) {
	reader := &readerSpy{snapshot: Snapshot{ActiveOperations: []domain.OperationRef{}}}
	svc := newTestService(reader, Dependencies{})

	got, err := svc.Bootstrap(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if reader.readHomeCalls != 1 {
		t.Fatalf("reader calls = %d, want 1", reader.readHomeCalls)
	}
	if got.ActiveOperations == nil {
		t.Fatal("ActiveOperations must never be nil")
	}
}

func TestBootstrapNormalizesNilOperations(t *testing.T) {
	reader := &readerSpy{snapshot: Snapshot{}}
	svc := newTestService(reader, Dependencies{})

	got, err := svc.Bootstrap(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ActiveOperations == nil {
		t.Fatal("nil ActiveOperations was not normalized to an empty slice")
	}
}

func TestBootstrapOmitsEmptyProfileRow(t *testing.T) {
	// 评估发布会回填仅含 current_report_id 的空档案行：不得当作已填档案下发。
	reader := &readerSpy{snapshot: Snapshot{Profile: &domain.ProfileSummary{}}}
	svc := newTestService(reader, Dependencies{})

	got, err := svc.Bootstrap(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ProfileSummary != nil {
		t.Fatalf("empty profile row must be omitted: %#v", got.ProfileSummary)
	}
}

func TestBootstrapOmitsContentKeysWhenNotFound(t *testing.T) {
	reader := &readerSpy{snapshot: Snapshot{}}
	svc := newTestService(reader, Dependencies{
		Reports:  reportReaderFake{err: repository.ErrNotFound},
		PlanSets: planSetReaderFake{},
		Today:    todayReaderFake{err: repository.ErrNotFound},
		Billing:  billingReaderFake{summary: domain.BillingSummary{}},
	})

	got, err := svc.Bootstrap(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Report != nil || got.PlanSet != nil || got.TodayPlan != nil {
		t.Fatalf("not-found keys must be omitted: %#v", got)
	}
	if got.Billing == nil {
		t.Fatal("billing summary must always be filled when the port is wired")
	}
	if got.Billing.SKUs == nil {
		t.Fatal("billing skus must never be nil")
	}
}

func TestBootstrapPropagatesReaderErrors(t *testing.T) {
	boom := errors.New("db down")
	reader := &readerSpy{err: boom}
	svc := newTestService(reader, Dependencies{})

	if _, err := svc.Bootstrap(context.Background(), "user-1"); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}

	readerOK := &readerSpy{snapshot: Snapshot{}}
	svc = newTestService(readerOK, Dependencies{Reports: reportReaderFake{err: boom}})
	if _, err := svc.Bootstrap(context.Background(), "user-1"); !errors.Is(err, boom) {
		t.Fatalf("report read err = %v, want %v", err, boom)
	}
}

func TestBootstrapAssemblesReportPlanSetToday(t *testing.T) {
	created := time.Date(2026, 9, 10, 8, 30, 0, 0, time.UTC)
	reader := &readerSpy{
		snapshot: Snapshot{
			Profile: &domain.ProfileSummary{HeightCM: 170, Role: "设计师", Budget: "1000-3000"},
		},
		latestPlanSet: "plan-set-1",
		mediaObject:   MediaObject{ObjectKey: "users/user-1/render-published/pub-1.jpg", MIMEType: "image/jpeg"},
	}
	expires := time.Date(2026, 9, 12, 13, 0, 0, 0, time.UTC)
	svc := newTestService(reader, Dependencies{
		Reports: reportReaderFake{view: ReportView{
			ID: "report-1", PhotoSetID: "photo-set-1", PriorityTitle: "先提亮气色",
			PriorityCopy: "从发色开始", SchemaVersion: "analyzer.v1",
			ImpressionTags: []string{"清爽"},
			SourceMedia: ReportSourceMedia{
				Face: ReportPhoto{ItemID: "item-face", Role: domain.PhotoRoleFace, Media: PresentedMedia{
					AssetID: "asset-face", URL: "https://signed.example/face", URLExpiresAt: expires,
					MIMEType: "image/jpeg", SourceKind: "user_original", DisplayLabel: "原本",
				}},
			},
			Findings: []FindingView{{
				ID: "finding-1", Category: "hair", Label: "发色", VisibleObservation: "发色偏深",
				Recommendation: "提亮一度", Priority: 1, Position: 1,
				SourcePhoto: SourcePhotoRef{ItemID: "item-face", Role: domain.PhotoRoleFace},
				Anchor:      domain.EvidenceAnchor{X: 0.1, Y: 0.1, W: 0.2, H: 0.2},
			}},
			CreatedAt: created,
		}},
		PlanSets: planSetReaderFake{planSet: domain.PlanSet{
			ID: "plan-set-1", ReportID: "report-1", Scene: domain.SceneDaily,
			SceneBrief:  domain.SceneBrief{Answers: map[string]string{"activity": "office"}},
			RenderState: "ready",
			Variants: []domain.PlanVariant{{
				ID: "variant-1", Slot: 1, Key: domain.VariantSharp, Name: "利落",
				Descriptor: "干练", Rationale: "贴合通勤", Recommended: true,
				OutcomeTags: []string{"精神"}, DifferenceTags: []string{"更利落"},
				Steps: []domain.PlanStep{{
					ID: "step-1", Category: domain.StepCategoryOutfit, Action: domain.ActionAdjust,
					Title: "上装", Summary: "浅色衬衫", Position: 1,
					Details:   domain.PlanStepDetails{Silhouette: "H", Palette: []string{"米白"}},
					CreatedAt: created,
				}},
				CreatedAt: created,
				Render: &domain.RenderStatusView{
					State: "ready", Retryable: false, OperationID: "op-1",
					Media: &domain.RenderMediaView{
						AssetID: "asset-render", URL: "https://signed.example/render", URLExpiresAt: expires,
						MIMEType: "image/jpeg", SourceKind: "generated_preview", DisplayLabel: "风格参考",
					},
				},
			}},
			CreatedAt: created,
		}},
		Today: todayReaderFake{plan: TodayPlan{
			ID: "today-1", Title: "今日利落通勤", Summary: "浅色提亮", Active: true, State: "ready",
			Steps: []domain.TodayPlanStep{{Category: "outfit", Label: "单品", Title: "衬衫", Copy: "浅色"}},
			Media: &domain.RenderMediaView{
				AssetID: "asset-today", SourceKind: "generated_preview", DisplayLabel: "风格参考",
			},
			CreatedAt: created, UpdatedAt: created,
		}},
		Billing: billingReaderFake{summary: domain.BillingSummary{Credits: 3, PaymentEnabled: false}},
		Media:   signerFake{url: "https://signed.example/today", expiresAt: expires},
	})

	got, err := svc.Bootstrap(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ProfileSummary == nil || got.ProfileSummary.Role != "设计师" {
		t.Fatalf("profile_summary = %#v", got.ProfileSummary)
	}
	if got.Report == nil || got.Report.ID != "report-1" {
		t.Fatalf("report = %#v", got.Report)
	}
	if got.Report.CreatedAt != "2026-09-10T08:30:00Z" {
		t.Fatalf("report created_at = %q", got.Report.CreatedAt)
	}
	if got.Report.SourceMedia.Face.Media.URL != "https://signed.example/face" {
		t.Fatalf("report face media = %#v", got.Report.SourceMedia.Face.Media)
	}
	if len(got.Report.Findings) != 1 || got.Report.Findings[0].SourcePhoto.ItemID != "item-face" {
		t.Fatalf("report findings = %#v", got.Report.Findings)
	}
	if got.PlanSet == nil || got.PlanSet.ID != "plan-set-1" || got.PlanSet.State != "ready" {
		t.Fatalf("plan_set = %#v", got.PlanSet)
	}
	if len(got.PlanSet.Variants) != 1 {
		t.Fatalf("plan_set variants = %#v", got.PlanSet.Variants)
	}
	variant := got.PlanSet.Variants[0]
	if variant.Render.State != "ready" || variant.Render.Media == nil || variant.Render.Media.AssetID != "asset-render" {
		t.Fatalf("variant render = %#v", variant.Render)
	}
	if variant.Render.OperationID == nil || *variant.Render.OperationID != "op-1" {
		t.Fatalf("variant render operation_id = %#v", variant.Render.OperationID)
	}
	outfit, ok := variant.Steps[0].(outfitPlanStepDTO)
	if !ok {
		t.Fatalf("outfit step payload type = %T", variant.Steps[0])
	}
	if outfit.Details.Silhouette != "H" || len(outfit.Details.Palette) != 1 {
		t.Fatalf("outfit details = %#v", outfit.Details)
	}
	if got.TodayPlan == nil || got.TodayPlan.ID != "today-1" {
		t.Fatalf("today_plan = %#v", got.TodayPlan)
	}
	if got.TodayPlan.Media == nil || got.TodayPlan.Media.URL != "https://signed.example/today" {
		t.Fatalf("today media = %#v", got.TodayPlan.Media)
	}
	if got.TodayPlan.Media.MIMEType != "image/jpeg" {
		t.Fatalf("today media mime = %q", got.TodayPlan.Media.MIMEType)
	}
	if got.TodayPlan.Media.URLExpiresAt != expires {
		t.Fatalf("today media expires = %v", got.TodayPlan.Media.URLExpiresAt)
	}
	if got.Billing == nil || got.Billing.Credits != 3 {
		t.Fatalf("billing = %#v", got.Billing)
	}
}

func TestBootstrapDropsTodayMediaWhenAssetDeleted(t *testing.T) {
	reader := &readerSpy{snapshot: Snapshot{}} // MediaObjectInfo → ErrNotFound
	svc := newTestService(reader, Dependencies{
		Today: todayReaderFake{plan: TodayPlan{
			ID: "today-1", State: "ready",
			Media: &domain.RenderMediaView{AssetID: "gone", SourceKind: "generated_preview", DisplayLabel: "风格参考"},
		}},
		Media: signerFake{url: "https://signed.example/today"},
	})

	got, err := svc.Bootstrap(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.TodayPlan == nil {
		t.Fatal("today_plan must still be present")
	}
	if got.TodayPlan.Media != nil {
		t.Fatalf("deleted asset must blank today media: %#v", got.TodayPlan.Media)
	}
}

func TestLatestPlanSetWithoutRenderHeadProjectsUnavailable(t *testing.T) {
	reader := &readerSpy{snapshot: Snapshot{}, latestPlanSet: "plan-set-1"}
	svc := newTestService(reader, Dependencies{
		PlanSets: planSetReaderFake{planSet: domain.PlanSet{
			ID: "plan-set-1", ReportID: "report-1", Scene: domain.SceneDaily,
			Variants: []domain.PlanVariant{{ID: "variant-1", Slot: 1, Key: domain.VariantSharp}},
		}},
	})

	got, err := svc.Bootstrap(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.PlanSet == nil {
		t.Fatal("plan_set must be present")
	}
	if got.PlanSet.State != "planning" {
		t.Fatalf("plan_set state = %q, want planning fallback", got.PlanSet.State)
	}
	if got.PlanSet.Variants[0].Render.State != "unavailable" {
		t.Fatalf("render state = %q, want unavailable", got.PlanSet.Variants[0].Render.State)
	}
}
