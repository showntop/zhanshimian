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
