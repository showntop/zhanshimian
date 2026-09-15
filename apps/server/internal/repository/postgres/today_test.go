package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/today"
	"github.com/zhanshimian/server/internal/testutil"
)

// 无生效方案时 CurrentTodayPlan 必须给出 repository.ErrNotFound —— handler 据此
// 回 200 data:null(契约:无生效方案时 data 为 null)。裸 pgx.ErrNoRows 会被
// writeServiceError 当成 500(第 9 轮 E2E 实测)。
func TestCurrentTodayPlanMapsNoRowsToNotFound(t *testing.T) {
	store := New(testutil.NewPostgres(t))
	ctx := context.Background()
	var userID string
	if err := store.pool.QueryRow(ctx, `INSERT INTO users(nickname) VALUES('today-empty') RETURNING id::text`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CurrentTodayPlan(ctx, userID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("empty current must map to ErrNotFound, got %v", err)
	}
}

// 无当前报告的用户也要能生成今日方案(报告列可空)。线上实测 NULLIF($2::uuid,'')
// 把 '' 字面量先转成 uuid,任何输入都 22P02 —— 空 report id 是常态路径,必须钉住。
func TestCreateTodayPlanWithoutReportPersistsNullReportID(t *testing.T) {
	store := New(testutil.NewPostgres(t))
	ctx := context.Background()
	var userID string
	if err := store.pool.QueryRow(ctx, `INSERT INTO users(nickname) VALUES('today-test') RETURNING id::text`).Scan(&userID); err != nil {
		t.Fatal(err)
	}

	plan, err := store.CreateTodayPlan(ctx, userID, today.Plan{
		ReportID: "",
		Context:  domain.TodayContext{City: "杭州", Condition: "cloudy", Temperature: 26, DayType: "weekday", Schedule: "通勤"},
		Title:    "今日利落通勤",
		Summary:  "保持肩线利落,一层内搭应对空调。",
		Steps: []domain.TodayPlanStep{
			{Category: "outfit", Label: "上装", Title: "合肩衬衫", Copy: "选合肩剪裁。"},
		},
		Active: true,
		State:  "planning",
	})
	if err != nil {
		t.Fatalf("create without report: %v", err)
	}
	if plan.ID == "" {
		t.Fatal("returned plan must carry the inserted id")
	}

	current, err := store.CurrentTodayPlan(ctx, userID)
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if current.ReportID != "" || current.Title != "今日利落通勤" || len(current.Steps) != 1 || current.Steps[0].Category != "outfit" {
		t.Fatalf("readback = %#v", current)
	}
}

// 有当前报告的用户:report_id 必须按复合外键落库并可读回。
func TestCreateTodayPlanWithReportPersists(t *testing.T) {
	f := newReadModelFixture(t)
	ctx := context.Background()

	plan, err := f.store.CreateTodayPlan(ctx, f.userA, today.Plan{
		ReportID: f.reportID,
		Context:  domain.TodayContext{City: "杭州", DayType: "weekday", Schedule: "通勤"},
		Title:    "报告驱动方案",
		Steps: []domain.TodayPlanStep{
			{Category: "hair", Label: "发型", Title: "抬高颅顶", Copy: "只整理颅顶线条。"},
		},
		Active: true,
		State:  "planning",
	})
	if err != nil {
		t.Fatalf("create with report: %v", err)
	}
	if plan.ID == "" {
		t.Fatal("returned plan must carry the inserted id")
	}

	current, err := f.store.CurrentTodayPlan(ctx, f.userA)
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if current.ReportID != f.reportID {
		t.Fatalf("report id = %q, want %q", current.ReportID, f.reportID)
	}
}

// 创建今日方案必须同事务创建并回填其搭配图渲染的公开 Operation（契约
// TodayPlanAccepted 要求响应带 operation，客户端凭它轮询）。
func TestCreateTodayPlanStartsRenderOperation(t *testing.T) {
	store := New(testutil.NewPostgres(t))
	ctx := context.Background()
	var userID string
	if err := store.pool.QueryRow(ctx, `INSERT INTO users(nickname) VALUES('today-op') RETURNING id::text`).Scan(&userID); err != nil {
		t.Fatal(err)
	}

	plan, err := store.CreateTodayPlan(ctx, userID, today.Plan{
		Context: domain.TodayContext{City: "杭州"},
		Title:   "今日利落通勤",
		Steps:   []domain.TodayPlanStep{},
		Active:  true,
		State:   "planning",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if plan.Operation.ID == "" {
		t.Fatal("create must return the render operation ref")
	}
	if plan.Operation.Kind != domain.OperationRender || plan.Operation.Status != domain.OperationAccepted {
		t.Fatalf("operation = %#v", plan.Operation)
	}

	var subjectType string
	if err := store.pool.QueryRow(ctx, `
		SELECT subject_type FROM operations WHERE user_id=$1::uuid AND id=$2::uuid`,
		userID, plan.Operation.ID).Scan(&subjectType); err != nil {
		t.Fatalf("operation row: %v", err)
	}
	if subjectType != "today_plan" {
		t.Fatalf("subject_type = %q", subjectType)
	}

	current, err := store.CurrentTodayPlan(ctx, userID)
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if current.Operation.ID != plan.Operation.ID || current.Operation.Status != domain.OperationAccepted {
		t.Fatalf("readback operation = %#v", current.Operation)
	}
}

// 读模型必须把发布媒体的对象定位（object key + mime）带给呈现层：
// 签名在服务层即时发生，URL 不落库（敏感信息红线）。
func TestCurrentTodayPlanCarriesPublishedMediaLocation(t *testing.T) {
	f := newReadModelFixture(t)
	ctx := context.Background()

	plan, err := f.store.CreateTodayPlan(ctx, f.userA, today.Plan{
		Context: domain.TodayContext{City: "杭州"},
		Title:   "今日利落通勤",
		Steps:   []domain.TodayPlanStep{},
		Active:  true,
		State:   "ready",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := f.store.pool.Exec(ctx, `
		UPDATE today_plans SET render_publication_id=$3::uuid
		WHERE user_id=$1::uuid AND id=$2::uuid`, f.userA, plan.ID, f.publicationID); err != nil {
		t.Fatal(err)
	}

	current, err := f.store.CurrentTodayPlan(ctx, f.userA)
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if current.Media == nil {
		t.Fatal("published plan must carry media")
	}
	if current.Media.AssetID == "" || current.Media.SourceKind != "generated_preview" || current.Media.DisplayLabel != "风格参考" {
		t.Fatalf("media = %#v", current.Media)
	}
	if current.MediaObjectKey == "" {
		t.Fatal("media object key must reach the presenter")
	}
	if current.MediaMIMEType != "image/jpeg" || current.Media.MIMEType != "image/jpeg" {
		t.Fatalf("media mime = %q/%q", current.MediaMIMEType, current.Media.MIMEType)
	}
	if current.Media.URL != "" {
		t.Fatalf("repository must never persist/sign URLs: %q", current.Media.URL)
	}
}
