package today_test

import (
	"context"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/service/today"
)

// 编译期契约：Reader 的签名是本计划冻结的边界。
type todayReaderFake struct{}

func (todayReaderFake) ReadTodayGrounding(context.Context, string) (today.Grounding, error) {
	return today.Grounding{}, nil
}

var _ today.Reader = todayReaderFake{}

type writerFake struct {
	plan today.Plan
	err  error
}

func (f writerFake) CreateTodayPlan(_ context.Context, _ string, plan today.Plan) (today.Plan, error) {
	return plan, f.err
}

func (f writerFake) CurrentTodayPlan(context.Context, string) (today.Plan, error) {
	return f.plan, f.err
}

func (f writerFake) MarkTodayPlanActive(context.Context, string, string) (today.Plan, error) {
	return f.plan, f.err
}

func (f writerFake) RecordTodayPlanFeedback(context.Context, string, string, string) (today.Plan, error) {
	return f.plan, f.err
}

type signerFake struct {
	url       string
	expiresAt time.Time
	err       error
}

func (f signerFake) SignedURL(_ context.Context, objectKey string) (string, time.Time, error) {
	return f.url + objectKey, f.expiresAt, f.err
}

func mediaPlan() today.Plan {
	return today.Plan{
		ID: "today-1", Title: "今日利落通勤", Active: true, State: "ready",
		Media:          &domain.RenderMediaView{AssetID: "asset-1", SourceKind: "generated_preview", DisplayLabel: "风格参考"},
		MediaObjectKey: "users/u1/render-published/pub-1.jpg",
		MediaMIMEType:  "image/jpeg",
	}
}

// 读路径必须给发布媒体补签名 URL 与 MIMEType：客户端投影对空 url 一律
// 拒渲染（复访恢复照片不显示的根因）。
func TestCurrentSignsPublishedMedia(t *testing.T) {
	expires := time.Now().Add(time.Hour).UTC()
	svc := today.New(nil, writerFake{plan: mediaPlan()}, nil, nil, today.NewClock()).
		WithMediaSigner(signerFake{url: "https://signed.example/", expiresAt: expires})

	plan, err := svc.Current(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Media == nil {
		t.Fatal("media must be present")
	}
	if plan.Media.URL != "https://signed.example/users/u1/render-published/pub-1.jpg" {
		t.Fatalf("media url = %q", plan.Media.URL)
	}
	if plan.Media.MIMEType != "image/jpeg" {
		t.Fatalf("media mime = %q", plan.Media.MIMEType)
	}
	if !plan.Media.URLExpiresAt.Equal(expires) {
		t.Fatalf("media expires = %v", plan.Media.URLExpiresAt)
	}
	// 对象定位只用于签名，不外发（JSON 无此键由结构标签保证）。
	if plan.Media.AssetID != "asset-1" || plan.Media.SourceKind != "generated_preview" {
		t.Fatalf("media identity = %#v", plan.Media)
	}
}

// 未装配签名器（单测/降级组装）保持无 URL，不报错。
func TestCurrentWithoutSignerLeavesMediaUnsigned(t *testing.T) {
	svc := today.New(nil, writerFake{plan: mediaPlan()}, nil, nil, today.NewClock())
	plan, err := svc.Current(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Media == nil || plan.Media.URL != "" {
		t.Fatalf("unsigned media = %#v", plan.Media)
	}
}

// activate/feedback 同样走签名读路径。
func TestActivateAndFeedbackSignMedia(t *testing.T) {
	expires := time.Now().Add(time.Hour).UTC()
	svc := today.New(nil, writerFake{plan: mediaPlan()}, nil, nil, today.NewClock()).
		WithMediaSigner(signerFake{url: "https://signed.example/", expiresAt: expires})

	activated, err := svc.Activate(context.Background(), "user-1", "today-1")
	if err != nil {
		t.Fatal(err)
	}
	if activated.Media == nil || activated.Media.URL == "" {
		t.Fatalf("activated media = %#v", activated.Media)
	}
	feedback, err := svc.Feedback(context.Background(), "user-1", "today-1", "再轻松一点")
	if err != nil {
		t.Fatal(err)
	}
	if feedback.Media == nil || feedback.Media.URL == "" {
		t.Fatalf("feedback media = %#v", feedback.Media)
	}
}

// ---- 日程默认推导（旧线 buildTodayContext 语义；场景叫「日常」不叫「通勤」） ----

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

type plannerFake struct{ request today.TodayPlanRequest }

func (f *plannerFake) Generate(_ context.Context, request today.TodayPlanRequest) (today.TodayPlanOutput, error) {
	f.request = request
	return today.TodayPlanOutput{Title: "今日方案", Summary: "摘要", Steps: []today.TodayPlanStep{
		{Category: "hair", Label: "发型", Title: "t", Copy: "c"},
		{Category: "makeup", Label: "妆造", Title: "t", Copy: "c"},
		{Category: "outfit", Label: "穿搭", Title: "t", Copy: "c"},
	}}, nil
}

type readerFake struct{}

func (readerFake) ReadTodayGrounding(context.Context, string) (today.Grounding, error) {
	return today.Grounding{}, nil
}

// 2026-09-14 是周一，2026-09-13 是周日。
func TestGenerateDefaultsScheduleByDayType(t *testing.T) {
	cases := []struct {
		name string
		now  time.Time
		want string
	}{
		{"workday", time.Date(2026, 9, 14, 10, 0, 0, 0, time.Local), "日常"},
		{"weekend", time.Date(2026, 9, 13, 10, 0, 0, 0, time.Local), "休息"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			planner := &plannerFake{}
			writer := writerFake{}
			svc := today.New(readerFake{}, writer, planner, nil, fixedClock{now: tc.now})
			plan, err := svc.Generate(context.Background(), "user-1", today.CreateInput{City: "杭州"})
			if err != nil {
				t.Fatal(err)
			}
			// 默认日程进 AI prompt（planner 请求），也落进方案上下文。
			if planner.request.Schedule != tc.want {
				t.Fatalf("planner schedule = %q, want %q", planner.request.Schedule, tc.want)
			}
			if plan.Context.Schedule != tc.want {
				t.Fatalf("plan context schedule = %q, want %q", plan.Context.Schedule, tc.want)
			}
			for _, forbidden := range []string{"通勤"} {
				if planner.request.Schedule == forbidden || plan.Context.Schedule == forbidden {
					t.Fatalf("schedule must not be %q", forbidden)
				}
			}
		})
	}
}

func TestGenerateKeepsExplicitSchedule(t *testing.T) {
	planner := &plannerFake{}
	svc := today.New(readerFake{}, writerFake{}, planner, nil, fixedClock{now: time.Date(2026, 9, 14, 10, 0, 0, 0, time.Local)})
	plan, err := svc.Generate(context.Background(), "user-1", today.CreateInput{Schedule: "面试"})
	if err != nil {
		t.Fatal(err)
	}
	if planner.request.Schedule != "面试" || plan.Context.Schedule != "面试" {
		t.Fatalf("explicit schedule must pass through: %q/%q", planner.request.Schedule, plan.Context.Schedule)
	}
}

func TestContextDefaultsSchedule(t *testing.T) {
	svc := today.New(nil, writerFake{}, nil, nil, fixedClock{now: time.Date(2026, 9, 14, 10, 0, 0, 0, time.Local)})
	ctxOut := svc.Context(context.Background(), "杭州", "")
	if ctxOut.Schedule != "日常" || ctxOut.DayType != "工作日" {
		t.Fatalf("context = %#v", ctxOut)
	}
	weekendSvc := today.New(nil, writerFake{}, nil, nil, fixedClock{now: time.Date(2026, 9, 13, 10, 0, 0, 0, time.Local)})
	weekendCtx := weekendSvc.Context(context.Background(), "杭州", "")
	if weekendCtx.Schedule != "休息" {
		t.Fatalf("weekend schedule = %q, want 休息", weekendCtx.Schedule)
	}
}
