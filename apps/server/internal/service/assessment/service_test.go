package assessment

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/billing"
	"github.com/zhanshimian/server/internal/service/taskrunner"
)

var ctx = context.Background()

func TestCreateBuildsRoleAddressedPhotoSet(t *testing.T) {
	repo := newRepoFake()
	svc := newService(repo, validAssetReader())
	got, err := svc.Create(ctx, CreateCommand{
		UserID: "user-1",
		Slots:  domain.PhotoSlots{FaceAssetID: "face", SideAssetID: "side", BodyAssetID: "body"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if string(got.Operation.Kind) != "assessment" {
		t.Fatalf("operation kind = %q, want assessment", got.Operation.Kind)
	}
	if got.Task.Type != domain.TaskType("assessment") {
		t.Fatalf("task type = %q, want assessment", got.Task.Type)
	}
	if id := repo.last.Assets[domain.PhotoRoleFace].ID; id != "face" {
		t.Fatalf("face asset = %q, want face", id)
	}
}

func TestCreateRejectsMixedDemoAndUserPhotos(t *testing.T) {
	reader := validAssetReader()
	reader.assets["side"] = demoAsset("side")
	_, err := newService(newRepoFake(), reader).Create(ctx, validCreateCommand())
	assertValidationCode(t, err, "photo_origin_mixed")
}

func TestCreateSameInputsReusesRun(t *testing.T) {
	repo := newRepoFake()
	svc := newService(repo, validAssetReader())
	first, err := svc.Create(ctx, validCreateCommand())
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}
	second, err := svc.Create(ctx, validCreateCommand())
	if err != nil {
		t.Fatalf("second Create: %v", err)
	}
	if first.Run.ID != second.Run.ID {
		t.Fatalf("run IDs differ: %s vs %s", first.Run.ID, second.Run.ID)
	}
	if repo.createCount != 1 {
		t.Fatalf("createCount = %d, want 1", repo.createCount)
	}
}

func TestCreateRejectsBundledMixedWithUserPhotos(t *testing.T) {
	reader := validAssetReader()
	asset := reader.assets["body"]
	asset.Origin = domain.MediaOriginBundledReference
	reader.assets["body"] = asset
	_, err := newService(newRepoFake(), reader).Create(ctx, validCreateCommand())
	assertValidationCode(t, err, "photo_origin_mixed")
}

func TestCreateHidesForeignAndMissingAssets(t *testing.T) {
	missing := validAssetReader()
	delete(missing.assets, "side")
	_, err := newService(newRepoFake(), missing).Create(ctx, validCreateCommand())
	assertValidationCode(t, err, "photo_asset_not_found")

	foreign := validAssetReader()
	foreign.err = repository.ErrNotFound
	_, err = newService(newRepoFake(), foreign).Create(ctx, validCreateCommand())
	assertValidationCode(t, err, "photo_asset_not_found")
}

func TestCreateAcceptsAllDemoPhotos(t *testing.T) {
	reader := validAssetReader()
	reader.assets["face"] = demoAsset("face")
	reader.assets["side"] = demoAsset("side")
	reader.assets["body"] = demoAsset("body")
	got, err := newService(newRepoFake(), reader).Create(ctx, validCreateCommand())
	if err != nil {
		t.Fatalf("all-demo Create: %v", err)
	}
	if got.Run.ID == "" {
		t.Fatal("expected reused-capable run")
	}
}

func TestCreateHashesUseSchemaAndRoleSHA256(t *testing.T) {
	repo := newRepoFake()
	reader := validAssetReader()
	if _, err := newService(repo, reader).Create(ctx, validCreateCommand()); err != nil {
		t.Fatalf("Create: %v", err)
	}
	wantContent := PhotoSetContentHash(PhotoSetSchemaVersion, map[domain.PhotoRole]string{
		domain.PhotoRoleFace: reader.assets["face"].SHA256,
		domain.PhotoRoleSide: reader.assets["side"].SHA256,
		domain.PhotoRoleBody: reader.assets["body"].SHA256,
	})
	if repo.last.PhotoSetContentHash != wantContent {
		t.Fatalf("content hash = %q, want %q", repo.last.PhotoSetContentHash, wantContent)
	}
	wantInput := AnalysisInputHash(wantContent, json.RawMessage(`{"role":"designer","height_cm":172}`), AnalyzerSchemaVersion, QualityPolicyVersion)
	if repo.last.AnalysisInputHash != wantInput {
		t.Fatalf("input hash = %q, want %q", repo.last.AnalysisInputHash, wantInput)
	}
	if repo.last.PhotoSetSchemaVersion != PhotoSetSchemaVersion || repo.last.AnalyzerSchemaVersion != AnalyzerSchemaVersion || repo.last.QualityPolicyVersion != QualityPolicyVersion {
		t.Fatalf("schema versions = %+v", repo.last)
	}
}

func TestCreateCopiesMaxAttemptsFromDefinition(t *testing.T) {
	repo := newRepoFake()
	def := assessmentTaskDefinition()
	def.MaxAttempts = 5
	svc := NewService(repo, validAssetReader(), stubProfiles(), stubMedia(), def)
	if _, err := svc.Create(ctx, validCreateCommand()); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if repo.last.MaxTaskAttempts != 5 {
		t.Fatalf("MaxTaskAttempts = %d, want 5", repo.last.MaxTaskAttempts)
	}
}

func TestCreateReservesAssessmentOnce(t *testing.T) {
	repo := newRepoFake()
	billing := &billingFake{}
	svc := NewService(repo, validAssetReader(), stubProfiles(), stubMedia(), assessmentTaskDefinition()).WithBilling(billing)
	if _, err := svc.Create(ctx, validCreateCommand()); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(billing.calls) != 1 {
		t.Fatalf("reserve calls = %d, want 1", len(billing.calls))
	}
	got := billing.calls[0]
	if got.userID != "user-1" || got.operationID != "op-1" ||
		got.product != domain.ProductAssessment || got.units != 1 {
		t.Fatalf("unexpected reserve call: %#v", got)
	}
}

func TestGetReportPresentsMediaWithoutInternalFields(t *testing.T) {
	repo := newRepoFake()
	repo.report = publishedReport()
	svc := NewService(repo, validAssetReader(), stubProfiles(), stubMedia(), assessmentTaskDefinition())
	got, err := svc.GetReport(ctx, "user-1", "report-1")
	if err != nil {
		t.Fatalf("GetReport: %v", err)
	}
	assertPresentedReport(t, got)
}

func TestGetCurrentReportPresentsMedia(t *testing.T) {
	repo := newRepoFake()
	repo.report = publishedReport()
	svc := NewService(repo, validAssetReader(), stubProfiles(), stubMedia(), assessmentTaskDefinition())
	got, err := svc.GetCurrentReport(ctx, "user-1")
	if err != nil {
		t.Fatalf("GetCurrentReport: %v", err)
	}
	assertPresentedReport(t, got)
}

func TestPresentedTypesOmitInternalFields(t *testing.T) {
	assertNoField(t, ReportView{}, "ProviderInvocationID", "QualityEvaluationID", "ProfileSnapshot", "ObjectKey", "Confidence")
	assertNoField(t, FindingView{}, "Confidence", "UserID", "ReportID")
	assertNoField(t, PresentedMedia{}, "ObjectKey", "ProviderInvocationID", "Origin")
}

func assertPresentedReport(t *testing.T, got ReportView) {
	t.Helper()
	if got.ID != "report-1" {
		t.Fatalf("report id = %q, want report-1", got.ID)
	}
	if got.SourceMedia.Face.Media.URL != "https://signed/face.jpg" {
		t.Fatalf("face url = %q", got.SourceMedia.Face.Media.URL)
	}
	if got.SourceMedia.Face.Media.SourceKind != "user_original" || got.SourceMedia.Face.Media.DisplayLabel != "原本" {
		t.Fatalf("face media = %+v", got.SourceMedia.Face.Media)
	}
	if got.SourceMedia.Side.Media.SourceKind != "user_original" {
		t.Fatalf("side source_kind = %q", got.SourceMedia.Side.Media.SourceKind)
	}
	if got.SourceMedia.Body.ItemID != "item-body" {
		t.Fatalf("body item = %q", got.SourceMedia.Body.ItemID)
	}
	if got.SourceMedia.Face.Media.AssetID != "face" {
		t.Fatalf("face asset_id = %q", got.SourceMedia.Face.Media.AssetID)
	}
	if len(got.Findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(got.Findings))
	}
	if got.Findings[0].SourcePhoto.Role != domain.PhotoRoleFace {
		t.Fatalf("finding source role = %q", got.Findings[0].SourcePhoto.Role)
	}
}

func assertNoField(t *testing.T, value any, names ...string) {
	t.Helper()
	visible := map[string]struct{}{}
	typ := reflect.TypeOf(value)
	for i := 0; i < typ.NumField(); i++ {
		visible[typ.Field(i).Name] = struct{}{}
	}
	for _, name := range names {
		if _, ok := visible[name]; ok {
			t.Fatalf("%s must not expose %s", typ.Name(), name)
		}
	}
}

func assertValidationCode(t *testing.T, err error, code string) {
	t.Helper()
	var rejected *ValidationError
	if !errors.As(err, &rejected) || rejected.Code != code {
		t.Fatalf("got %v, want %s", err, code)
	}
}

func validCreateCommand() CreateCommand {
	return CreateCommand{
		UserID: "user-1",
		Slots:  domain.PhotoSlots{FaceAssetID: "face", SideAssetID: "side", BodyAssetID: "body"},
	}
}

func newService(repo Repository, assets AssetReader) *Service {
	return NewService(repo, assets, stubProfiles(), stubMedia(), assessmentTaskDefinition())
}

func assessmentTaskDefinition() taskrunner.Definition {
	return taskrunner.Definition{
		Type:           domain.TaskType("assessment"),
		MaxAttempts:    3,
		Timeout:        90 * time.Second,
		LeaseDuration:  30 * time.Second,
		HeartbeatEvery: 10 * time.Second,
		Concurrency:    2,
		RetryBackoff:   taskrunner.ExponentialBackoff(time.Second, time.Minute),
	}
}

type repoFake struct {
	last        domain.CreateAssessmentParams
	createCount int
	created     domain.CreatedAssessment
	report      domain.AssessmentReport
	reportErr   error
}

func newRepoFake() *repoFake {
	return &repoFake{}
}

func (r *repoFake) CreateOrReuseAssessment(_ context.Context, params domain.CreateAssessmentParams) (domain.CreatedAssessment, error) {
	if r.createCount > 0 && r.last.UserID == params.UserID && r.last.AnalysisInputHash == params.AnalysisInputHash {
		r.last = params
		return r.created, nil
	}
	r.createCount++
	r.last = params
	r.created = domain.CreatedAssessment{
		PhotoSet: domain.PhotoSet{
			ID: "photoset-1", UserID: params.UserID,
			ContentHash: params.PhotoSetContentHash, SchemaVersion: params.PhotoSetSchemaVersion,
		},
		Run:       domain.AnalysisRun{ID: "run-1", UserID: params.UserID, InputHash: params.AnalysisInputHash},
		Operation: domain.Operation{ID: "op-1", UserID: params.UserID, Kind: domain.OperationAssessment},
		Task:      domain.Task{ID: "task-1", UserID: params.UserID, Type: domain.TaskType("assessment"), MaxAttempts: params.MaxTaskAttempts},
	}
	return r.created, nil
}

func (r *repoFake) GetReport(_ context.Context, _, _ string) (domain.AssessmentReport, error) {
	if r.reportErr != nil {
		return domain.AssessmentReport{}, r.reportErr
	}
	return r.report, nil
}

func (r *repoFake) GetCurrentReport(_ context.Context, _ string) (domain.AssessmentReport, error) {
	if r.reportErr != nil {
		return domain.AssessmentReport{}, r.reportErr
	}
	return r.report, nil
}

type assetReaderFake struct {
	assets map[string]domain.MediaAsset
	err    error
}

func (r *assetReaderFake) GetReadyAssets(_ context.Context, _ string, ids []string) ([]domain.MediaAsset, error) {
	if r.err != nil {
		return nil, r.err
	}
	out := make([]domain.MediaAsset, 0, len(ids))
	for _, id := range ids {
		if asset, ok := r.assets[id]; ok {
			out = append(out, asset)
		}
	}
	return out, nil
}

func validAssetReader() *assetReaderFake {
	return &assetReaderFake{assets: map[string]domain.MediaAsset{
		"face": readyAsset("face", domain.MediaPurposeFace, domain.MediaOriginUserUpload),
		"side": readyAsset("side", domain.MediaPurposeSide, domain.MediaOriginUserUpload),
		"body": readyAsset("body", domain.MediaPurposeBody, domain.MediaOriginUserUpload),
	}}
}

func demoAsset(id string) domain.MediaAsset {
	return readyAsset(id, domain.MediaPurpose(id), domain.MediaOriginDemo)
}

func readyAsset(id string, purpose domain.MediaPurpose, origin domain.MediaOrigin) domain.MediaAsset {
	return domain.MediaAsset{
		ID: id, UserID: "user-1", Origin: origin, Purpose: purpose,
		ObjectKey: "objects/" + id, SHA256: id + "-sha", MIMEType: "image/jpeg",
		State: domain.MediaStateReady,
	}
}

type profileFake struct {
	snapshot json.RawMessage
}

func (p profileFake) Snapshot(context.Context, string) (json.RawMessage, error) {
	if len(p.snapshot) == 0 {
		return json.RawMessage(`{"role":"designer","height_cm":172}`), nil
	}
	return p.snapshot, nil
}

func stubProfiles() ProfileReader { return profileFake{} }

type mediaFake struct{}

func (mediaFake) Present(_ context.Context, asset domain.MediaAsset) (PresentedMedia, error) {
	kind, label := "user_original", "原本"
	if asset.Origin == domain.MediaOriginDemo {
		kind, label = "demo_example", "效果示例"
	}
	return PresentedMedia{
		URL:          "https://signed/" + asset.ID + ".jpg",
		URLExpiresAt: time.Unix(1_700_000_000, 0).UTC(),
		SourceKind:   kind,
		DisplayLabel: label,
	}, nil
}

func stubMedia() MediaPresenter { return mediaFake{} }

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

func publishedReport() domain.AssessmentReport {
	items := []domain.PhotoSetItem{
		{ID: "item-face", Role: domain.PhotoRoleFace, Asset: readyAsset("face", domain.MediaPurposeFace, domain.MediaOriginUserUpload)},
		{ID: "item-side", Role: domain.PhotoRoleSide, Asset: readyAsset("side", domain.MediaPurposeSide, domain.MediaOriginUserUpload)},
		{ID: "item-body", Role: domain.PhotoRoleBody, Asset: readyAsset("body", domain.MediaPurposeBody, domain.MediaOriginUserUpload)},
	}
	return domain.AssessmentReport{
		Report: domain.Report{
			ID: "report-1", UserID: "user-1", PhotoSetID: "photoset-1", HeroAssetID: "face",
			SchemaVersion: "report.v1", PriorityTitle: "先整理额前碎发", PriorityCopy: "额前碎发会挡住眉形。",
			ImpressionTags:       []string{"利落"},
			ProviderInvocationID: "inv-secret",
			QualityEvaluationID:  "quality-secret",
			Findings: []domain.ReportFinding{{
				ID: "finding-1", UserID: "user-1", ReportID: "report-1",
				Label: "额前碎发", VisibleObservation: "额前碎发落到眉毛上方", Recommendation: "向后梳理并固定",
				SourcePhotoItemID: "item-face", Category: "hair", Priority: 1, Position: 1,
				Anchor: domain.EvidenceAnchor{X: 0.2, Y: 0.1, W: 0.4, H: 0.2}, Confidence: 0.99,
			}},
		},
		PhotoSet: domain.PhotoSet{ID: "photoset-1", UserID: "user-1", Items: items},
	}
}

// ---- 用量日限（旧线 decideAnalysis：2 次/日，限额先于扣费） ----

type usageCounterFake struct {
	count int
	err   error
	since time.Time
}

func (u *usageCounterFake) CountOperationsCreatedSince(_ context.Context, _ string, kinds []domain.OperationKind, _ []string, since time.Time) (int, error) {
	if len(kinds) != 1 || kinds[0] != domain.OperationAssessment {
		return 0, errors.New("unexpected kinds filter")
	}
	u.since = since
	return u.count, u.err
}

func TestCreateRejectsWhenDailyAssessmentLimitReached(t *testing.T) {
	repo := newRepoFake()
	reserver := &billingFake{}
	svc := NewService(repo, validAssetReader(), stubProfiles(), stubMedia(), assessmentTaskDefinition()).
		WithBilling(reserver).
		WithUsageLimits(&usageCounterFake{count: limitAssessmentPerDay})
	_, err := svc.Create(ctx, validCreateCommand())
	if !errors.Is(err, billing.ErrRateLimited) {
		t.Fatalf("Create error = %v, want ErrRateLimited", err)
	}
	// 限额先于落库与扣费：超限不创建行、不 Reserve。
	if repo.createCount != 0 || len(reserver.calls) != 0 {
		t.Fatalf("limited create must not persist or reserve: creates=%d reserves=%d", repo.createCount, len(reserver.calls))
	}
}

func TestCreateWithinDailyLimitProceeds(t *testing.T) {
	repo := newRepoFake()
	reserver := &billingFake{}
	counter := &usageCounterFake{count: limitAssessmentPerDay - 1}
	svc := NewService(repo, validAssetReader(), stubProfiles(), stubMedia(), assessmentTaskDefinition()).
		WithBilling(reserver).
		WithUsageLimits(counter)
	if _, err := svc.Create(ctx, validCreateCommand()); err != nil {
		t.Fatal(err)
	}
	if repo.createCount != 1 || len(reserver.calls) != 1 {
		t.Fatalf("creates=%d reserves=%d, want 1/1", repo.createCount, len(reserver.calls))
	}
	// 日界按服务器本地自然日零点。
	if counter.since.Hour() != 0 || counter.since.Minute() != 0 || counter.since.Second() != 0 {
		t.Fatalf("day boundary = %v, want local midnight", counter.since)
	}
}
