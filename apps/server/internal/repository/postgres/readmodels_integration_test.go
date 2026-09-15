package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/share"
	"github.com/zhanshimian/server/internal/service/today"
)

// readmodelFixture 复用完整发布链（report → plan set → render → publication）
// 并落一条 Selection，是七个外围 Reader 集成测试的共同底座。
type readmodelFixture struct {
	store         *Store
	userA, userB  string
	variantID     string
	planSetID     string
	publicationID string
	reportID      string
}

func newReadModelFixture(t *testing.T) *readmodelFixture {
	t.Helper()
	rf := newRenderingPublishFixture(t)
	ctx := context.Background()

	candidate, err := rf.store.RecordCandidate(ctx, domain.RenderRecordCandidateCommand{
		TaskID: rf.task.ID, LeaseToken: rf.task.LeaseToken,
		UserID: rf.userA, RenderRunID: rf.run.ID, SubjectGeneration: 1, Ordinal: 1,
		Asset: renderCandidateAsset(), ProviderInvocationID: seedRenderInvocation(t, rf.store, rf.userA, rf.task.OperationID, rf.task.ID),
	})
	if err != nil {
		t.Fatal(err)
	}

	publicationID := uuid.NewString()
	outcome, err := rf.store.CommitEvaluation(ctx, domain.RenderCommitEvaluationCommand{
		TaskID: rf.task.ID, LeaseToken: rf.task.LeaseToken,
		UserID: rf.userA, RenderRunID: rf.run.ID,
		SubjectGeneration: 1, CandidateID: candidate.ID,
		PublicationID: publicationID,
		Evaluation:    passEvaluation(candidate.ID),
		PublishedObject: &domain.RenderPublishedObject{
			Key: PublishedKey(rf.userA, publicationID), SHA256: "abc123", ByteSize: 3,
		},
	})
	if err != nil || outcome.Outcome != domain.RenderOutcomePublished {
		t.Fatalf("publish: outcome=%v err=%v", outcome, err)
	}

	var planSetID, reportID string
	if err := rf.store.pool.QueryRow(ctx,
		`SELECT plan_set_id::text FROM plan_variants WHERE user_id=$1::uuid AND id=$2::uuid`,
		rf.userA, rf.variantID).Scan(&planSetID); err != nil {
		t.Fatal(err)
	}
	if err := rf.store.pool.QueryRow(ctx,
		`SELECT report_id::text FROM plan_sets WHERE user_id=$1::uuid AND id=$2::uuid`,
		rf.userA, planSetID).Scan(&reportID); err != nil {
		t.Fatal(err)
	}

	_, _, err = rf.store.CreateSelection(ctx, domain.CreateSelectionCommand{
		UserID:              rf.userA,
		PlanSetID:           planSetID,
		PlanVariantID:       rf.variantID,
		RenderPublicationID: &publicationID,
		IdempotencyKey:      "readmodel-sel:" + uuid.NewString(),
		RequestHash:         "readmodel-sel",
	})
	if err != nil {
		t.Fatal(err)
	}

	return &readmodelFixture{
		store: rf.store, userA: rf.userA, userB: rf.userB,
		variantID: rf.variantID, planSetID: planSetID,
		publicationID: publicationID, reportID: reportID,
	}
}

func (f *readmodelFixture) seedWardrobeItem(t *testing.T, userID string) {
	t.Helper()
	_, err := f.store.pool.Exec(context.Background(),
		`INSERT INTO wardrobe_items(user_id, name, category, color, formality)
		 VALUES ($1::uuid, '米白针织衫', 'top', '米白', 'casual')`, userID)
	if err != nil {
		t.Fatal(err)
	}
}

func (f *readmodelFixture) seedInFlightOperation(t *testing.T, userID string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := f.store.pool.Exec(context.Background(),
		`INSERT INTO operations(id, user_id, kind, subject_type, subject_id, status, progress_bps, stage_code, public_message)
		 VALUES ($1::uuid, $2::uuid, 'render', 'plan_variant', $3::uuid, 'running', 4200, 'generating', '正在生成')`,
		id, userID, f.variantID)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestReadShareSourceRequiresOwnedPublishedAsset(t *testing.T) {
	f := newReadModelFixture(t)
	ctx := context.Background()

	source, err := f.store.ReadShareSource(ctx, f.userA, "plan_variant", f.variantID)
	if err != nil {
		t.Fatal(err)
	}
	if source.SourceType != "plan_variant" || source.SourceID != f.variantID {
		t.Fatalf("source identity = %s/%s", source.SourceType, source.SourceID)
	}
	if source.AssetID == "" || source.ObjectKey == "" {
		t.Fatalf("source lacks asset identity: %#v", source)
	}
	if source.SourceKind != domain.MediaSourceGeneratedPreview {
		t.Fatalf("source_kind = %q, want generated_preview", source.SourceKind)
	}
	if source.DisplayLabel != "风格参考" {
		t.Fatalf("display_label = %q", source.DisplayLabel)
	}
	if source.MIMEType != "image/jpeg" {
		t.Fatalf("mime = %q, want image/jpeg（创建响应的媒体投影强制校验）", source.MIMEType)
	}
	if source.ObjectKey != "" && (source.ObjectKey[:4] == "http" || source.ObjectKey[:3] == "://") {
		t.Fatalf("object key leaked a URL: %q", source.ObjectKey)
	}

	// 越权：userB 读 userA 的 variant 一律 NotFound。
	if _, err := f.store.ReadShareSource(ctx, f.userB, "plan_variant", f.variantID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("cross-tenant share = %v, want ErrNotFound", err)
	}
	// 未发布的来源不产生分享。
	if _, err := f.store.ReadShareSource(ctx, f.userA, "plan_variant", uuid.NewString()); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("unknown variant share = %v, want ErrNotFound", err)
	}
	if _, err := f.store.ReadShareSource(ctx, f.userA, "bogus", f.variantID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("unknown source type = %v, want ErrNotFound", err)
	}
}

// 今日方案的 render_publication_id 当前没有任何写路径回填（恒 NULL）：
// 分享源读取必须退化为纯文本快照（不 404），回填后同一查询自动带媒体。
func TestReadShareTodayPlanToleratesMissingRenderPublication(t *testing.T) {
	f := newReadModelFixture(t)
	ctx := context.Background()

	plan, err := f.store.CreateTodayPlan(ctx, f.userA, today.Plan{
		Title: "今日利落通勤", Summary: "浅色提亮",
		Steps: []domain.TodayPlanStep{}, Active: true, State: "planning",
	})
	if err != nil {
		t.Fatalf("create today plan: %v", err)
	}

	source, err := f.store.ReadShareSource(ctx, f.userA, "today_plan", plan.ID)
	if err != nil {
		t.Fatalf("today share source must not 404 without publication: %v", err)
	}
	if source.Title != "今日利落通勤" || source.Summary != "浅色提亮" {
		t.Fatalf("snapshot copy = %#v", source)
	}
	if source.AssetID != "" || source.ObjectKey != "" {
		t.Fatalf("text-only source must not carry asset identity: %#v", source)
	}

	// 回填发布（渲染链路接回后的状态）：同一读取必须带上资产身份与 mime。
	if _, err := f.store.pool.Exec(ctx, `
		UPDATE today_plans SET render_publication_id=$3::uuid
		WHERE user_id=$1::uuid AND id=$2::uuid`, f.userA, plan.ID, f.publicationID); err != nil {
		t.Fatal(err)
	}
	withMedia, err := f.store.ReadShareSource(ctx, f.userA, "today_plan", plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if withMedia.AssetID == "" || withMedia.ObjectKey == "" {
		t.Fatalf("published source lacks asset identity: %#v", withMedia)
	}
	if withMedia.MIMEType != "image/jpeg" || withMedia.SourceKind != domain.MediaSourceGeneratedPreview {
		t.Fatalf("published source media = %q/%q", withMedia.MIMEType, withMedia.SourceKind)
	}

	// 越权：userB 读 userA 的今日方案一律 NotFound。
	if _, err := f.store.ReadShareSource(ctx, f.userB, "today_plan", plan.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("cross-tenant today share = %v, want ErrNotFound", err)
	}
}

// 公开读取分享必须把对象定位（object key + mime）带给签名层；
// 创建行本身不存 URL（快照只存资产身份）。
func TestShareRoundTripCarriesMediaLocation(t *testing.T) {
	f := newReadModelFixture(t)
	ctx := context.Background()

	source, err := f.store.ReadShareSource(ctx, f.userA, "plan_variant", f.variantID)
	if err != nil {
		t.Fatal(err)
	}
	card, err := f.store.InsertShare(ctx, f.userA, source, share.Snapshot{
		Title: source.Title, Summary: source.Summary,
		AssetID: source.AssetID, SourceKind: string(source.SourceKind), DisplayLabel: source.DisplayLabel,
	}, false)
	if err != nil {
		t.Fatalf("insert share: %v", err)
	}

	public, err := f.store.GetPublicShareByToken(ctx, card.Token)
	if err != nil {
		t.Fatalf("public read: %v", err)
	}
	if public.Snapshot.AssetID != source.AssetID {
		t.Fatalf("snapshot asset = %q, want %q", public.Snapshot.AssetID, source.AssetID)
	}
	if public.ObjectKey == "" {
		t.Fatal("public read must carry the current object key for signing")
	}
	if public.MIMEType != "image/jpeg" {
		t.Fatalf("public read mime = %q（接收方投影强制 image/jpeg）", public.MIMEType)
	}
}

// active_operations 的语义是「需要用户关注的操作」：failed 必须下发
// （我的页任务中心「未完成，点击查看」）；终态 succeeded 不下发。
func TestReadHomeIncludesFailedOperations(t *testing.T) {
	f := newReadModelFixture(t)
	ctx := context.Background()

	failedID, succeededID := uuid.NewString(), uuid.NewString()
	// 库约束：failed 必须带 trace_id（operations_check）。
	if _, err := f.store.pool.Exec(ctx, `
		INSERT INTO operations(id, user_id, kind, subject_type, subject_id, status, trace_id)
		VALUES ($1::uuid, $2::uuid, 'assessment', 'photo_set', $3::uuid, 'failed', 'trace-seed')`,
		failedID, f.userA, f.variantID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.pool.Exec(ctx, `
		INSERT INTO operations(id, user_id, kind, subject_type, subject_id, status)
		VALUES ($1::uuid, $2::uuid, 'assessment', 'photo_set', $3::uuid, 'succeeded')`,
		succeededID, f.userA, f.variantID); err != nil {
		t.Fatal(err)
	}

	snap, err := f.store.ReadHome(ctx, f.userA, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	statuses := map[string]domain.OperationStatus{}
	for _, ref := range snap.ActiveOperations {
		statuses[ref.ID] = ref.Status
	}
	if statuses[failedID] != domain.OperationFailed {
		t.Fatalf("failed operation missing from active_operations: %#v", snap.ActiveOperations)
	}
	if _, found := statuses[succeededID]; found {
		t.Fatalf("succeeded operation must not be active: %#v", snap.ActiveOperations)
	}
}

func TestReadTodayGroundingUsesCurrentReportAndPublishedSelection(t *testing.T) {
	f := newReadModelFixture(t)
	ctx := context.Background()

	g, err := f.store.ReadTodayGrounding(ctx, f.userA)
	if err != nil {
		t.Fatal(err)
	}
	if g.ReportID == "" {
		t.Fatalf("today grounding missing report")
	}
	if len(g.Findings) == 0 {
		t.Fatalf("today grounding missing findings")
	}
	if g.SelectedPlan == nil || g.SelectedPlan.ID != f.variantID {
		t.Fatalf("selected plan = %#v", g.SelectedPlan)
	}
	if g.Publication == nil || g.Publication.AssetID == "" {
		t.Fatalf("today grounding missing published media")
	}
}

func TestReadAdvisorGroundingNeverCrossesUserBoundary(t *testing.T) {
	f := newReadModelFixture(t)
	f.seedWardrobeItem(t, f.userA)
	ctx := context.Background()

	mine, err := f.store.ReadAdvisorGrounding(ctx, f.userA)
	if err != nil {
		t.Fatal(err)
	}
	if len(mine.Wardrobe) == 0 {
		t.Fatalf("userA should see own wardrobe")
	}
	if mine.Report == nil || len(mine.Report.Findings) == 0 {
		t.Fatalf("userA grounding missing report")
	}

	theirs, err := f.store.ReadAdvisorGrounding(ctx, f.userB)
	if err != nil {
		t.Fatal(err)
	}
	if len(theirs.Wardrobe) != 0 {
		t.Fatalf("userB saw userA wardrobe: %#v", theirs.Wardrobe)
	}
	if theirs.Report != nil {
		t.Fatalf("userB saw userA report")
	}
}

func TestReadHomeReturnsOperationsNotTasks(t *testing.T) {
	f := newReadModelFixture(t)
	opID := f.seedInFlightOperation(t, f.userA)
	ctx := context.Background()

	snap, err := f.store.ReadHome(ctx, f.userA, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if snap.Profile == nil {
		t.Fatalf("home missing profile summary: %#v", snap.Profile)
	}
	if snap.ActiveOperations == nil {
		t.Fatalf("active_operations must never be nil")
	}
	found := false
	for _, ref := range snap.ActiveOperations {
		if ref.ID == opID {
			found = true
			if ref.Kind != domain.OperationRender || ref.Status != domain.OperationRunning {
				t.Fatalf("operation ref = %#v", ref)
			}
		}
	}
	if !found {
		t.Fatalf("in-flight operation missing from home: %#v", snap.ActiveOperations)
	}
}

func TestLatestPublishedPlanSetIDStaysInTenant(t *testing.T) {
	f := newReadModelFixture(t)
	ctx := context.Background()

	id, err := f.store.LatestPublishedPlanSetID(ctx, f.userA)
	if err != nil {
		t.Fatal(err)
	}
	if id != f.planSetID {
		t.Fatalf("latest plan set = %q, want %q", id, f.planSetID)
	}
	if _, err := f.store.LatestPublishedPlanSetID(ctx, f.userB); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("cross-tenant latest plan set = %v, want ErrNotFound", err)
	}
}

func TestMediaObjectInfoRequiresOwnedLiveAsset(t *testing.T) {
	f := newReadModelFixture(t)
	ctx := context.Background()

	var assetID, wantKey string
	if err := f.store.pool.QueryRow(ctx, `
		SELECT ma.id::text, ma.object_key
		FROM render_publications rp
		JOIN render_candidates rc ON rc.user_id=rp.user_id AND rc.id=rp.candidate_id
		JOIN media_assets ma ON ma.user_id=rc.user_id AND ma.id=rc.asset_id
		WHERE rp.user_id=$1::uuid AND rp.id=$2::uuid`, f.userA, f.publicationID).
		Scan(&assetID, &wantKey); err != nil {
		t.Fatal(err)
	}

	object, err := f.store.MediaObjectInfo(ctx, f.userA, assetID)
	if err != nil {
		t.Fatal(err)
	}
	if object.ObjectKey != wantKey || object.MIMEType == "" {
		t.Fatalf("media object = %#v", object)
	}
	if _, err := f.store.MediaObjectInfo(ctx, f.userB, assetID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("cross-tenant media object = %v, want ErrNotFound", err)
	}
	if _, err := f.store.MediaObjectInfo(ctx, f.userA, uuid.NewString()); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("unknown media object = %v, want ErrNotFound", err)
	}

	if _, err := f.store.pool.Exec(ctx, `
		UPDATE media_assets SET state='deleted', deleted_at=now()
		WHERE user_id=$1::uuid AND id=$2::uuid`, f.userA, assetID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.MediaObjectInfo(ctx, f.userA, assetID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("deleted media object = %v, want ErrNotFound", err)
	}
}

func TestReadDiagnosticGroundingOmitsWardrobeForOutfit(t *testing.T) {
	f := newReadModelFixture(t)
	f.seedWardrobeItem(t, f.userA)
	ctx := context.Background()

	outfit, err := f.store.ReadDiagnosticGrounding(ctx, f.userA, f.reportID, "outfit")
	if err != nil {
		t.Fatal(err)
	}
	if len(outfit.Wardrobe) != 0 {
		t.Fatalf("outfit grounding leaked wardrobe: %#v", outfit.Wardrobe)
	}
	if outfit.Report == nil || len(outfit.Report.Findings) == 0 {
		t.Fatalf("outfit grounding missing report")
	}

	purchase, err := f.store.ReadDiagnosticGrounding(ctx, f.userA, f.reportID, "purchase")
	if err != nil {
		t.Fatal(err)
	}
	if len(purchase.Wardrobe) == 0 {
		t.Fatalf("purchase grounding missing wardrobe")
	}
}
