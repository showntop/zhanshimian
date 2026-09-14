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
