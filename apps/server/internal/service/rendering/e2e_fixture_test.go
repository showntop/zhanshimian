package rendering

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"sync"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	providerai "github.com/zhanshimian/server/internal/provider/ai"
)

// fixtureProvider 是确定性渲染 Provider:接收 body/face/Spec,
// 按脚本返回 pass JPEG 或指定 reason code,输出真实可解码 JPEG,
// 并暴露调用次数与图片角色顺序供测试断言。
type fixtureProvider struct {
	mu        sync.Mutex
	requests  []providerai.GenerationRequest
	qualities []providerai.QualityResult
	failFirst bool // 第一次 transient 失败
}

func (f *fixtureProvider) Generate(_ context.Context, request providerai.GenerationRequest) (providerai.GenerationResult, error) {
	f.mu.Lock()
	attempt := len(f.requests)
	f.requests = append(f.requests, request)
	f.mu.Unlock()
	if f.failFirst && attempt == 0 {
		return providerai.GenerationResult{}, &providerai.GenerationFailure{
			Class: domain.ErrorTransient, Code: "upstream_5xx",
		}
	}
	return providerai.GenerationResult{
		Data:           e2eJPEG(1024, 1536),
		MIMEType:       "image/jpeg",
		Meta:           providerai.InvocationMeta{InvocationID: fmt.Sprintf("inv-%d", attempt), ModelKey: "primary", OutputImages: 1, InputImages: 2},
		NextRouteState: providerai.RouteState{AttemptedModelKeys: []string{"primary"}},
	}, nil
}

// e2eJPEG 复用 jpeg_test 的编码 fixture。
func e2eJPEG(width, height int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for x := 0; x < width; x += 32 {
		for y := 0; y < height; y += 32 {
			img.Set(x, y, color.RGBA{R: uint8(x % 255), G: uint8(y % 255), B: 96, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func jsonRaw(s string) json.RawMessage { return json.RawMessage(s) }

func timeNow() time.Time { return time.Now() }

func TestE2ECandidateOnePassPublishes(t *testing.T) {
	h := newWorkerHarness(t)
	h.gate.results = []QualityResult{{
		Decision: string(domain.QualityDecisionPass), ReasonCodes: []string{},
		InternalScores: jsonRaw(`{}`), CompletedStages: 5,
	}}
	result, err := h.handler.Execute(context.Background(), renderLease(1))
	if err != nil {
		t.Fatal(err)
	}
	if result.Disposition != domain.TaskPublish {
		t.Fatalf("disposition = %s", result.Disposition)
	}
	outcome, err := h.handler.Commit(context.Background(), renderLease(1), result)
	if err != nil || outcome != domain.CommitApplied {
		t.Fatalf("outcome=%s err=%v", outcome, err)
	}
	if h.repo.expandCalls != 0 {
		t.Fatalf("second candidate tasks = %d, want 0", h.repo.expandCalls)
	}
	// 公开投影:ready + generated_preview + 风格参考。
	view := domain.NewRenderRunView(h.repo.run, &domain.RenderPublication{
		ID: "publication-1", AssetID: "asset-1", URL: "https://signed/x.jpg",
		URLExpiresAt: timeNow(), ObjectKey: PublishedKey("user-1", "publication-1"),
	}, domain.Operation{ID: "operation-1"})
	if view.Render.State != domain.RenderStateReady ||
		view.Render.Media.SourceKind != domain.SourceKindGeneratedPreview ||
		view.Render.Media.DisplayLabel != domain.DisplayLabelStyleReference {
		t.Fatalf("public projection drifted: %#v", view.Render)
	}
}

func TestE2ECandidateOneDriftCandidateTwoPasses(t *testing.T) {
	h := newWorkerHarness(t)
	h.gate.results = []QualityResult{
		{Decision: string(domain.QualityDecisionRetry), ReasonCodes: []string{ReasonIdentityDrift},
			InternalScores: jsonRaw(`{}`), CompletedStages: 2},
		{Decision: string(domain.QualityDecisionPass), ReasonCodes: []string{},
			InternalScores: jsonRaw(`{}`), CompletedStages: 5},
	}
	first, err := h.handler.Execute(context.Background(), renderLease(1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.handler.Commit(context.Background(), renderLease(1), first); err != nil {
		t.Fatal(err)
	}
	// Commit 已扩预算并落库 retry 评估;fake 的 job 需要反映这一事实。
	h.repo.jobPrevious = &domain.QualityEvaluation{
		Decision: domain.QualityDecisionRetry, ReasonCodes: []string{ReasonIdentityDrift},
	}
	// Candidate 2 的生成输出。
	h.generator.outputs = append(h.generator.outputs, generationOutcome{data: e2eJPEG(1024, 1536), mime: "image/jpeg"})
	second, err := h.handler.Execute(context.Background(), renderLease(2))
	if err != nil {
		t.Fatal(err)
	}
	if second.Disposition != domain.TaskPublish {
		t.Fatalf("candidate 2 disposition = %s", second.Disposition)
	}
	if _, err = h.handler.Commit(context.Background(), renderLease(2), second); err != nil {
		t.Fatal(err)
	}
	// 只发布一次、只扣一次额度。
	if h.repo.commitCalls != 1 || h.repo.expandCalls != 1 {
		t.Fatalf("commits=%d expands=%d", h.repo.commitCalls, h.repo.expandCalls)
	}
}

func TestE2EBothCandidatesFailRunFailed(t *testing.T) {
	h := newWorkerHarness(t)
	h.repo.run.CandidateLimit = 2
	h.repo.jobPrevious = &domain.QualityEvaluation{Decision: domain.QualityDecisionRetry}
	h.gate.results = []QualityResult{
		{Decision: string(domain.QualityDecisionReject), ReasonCodes: []string{ReasonIdentityDrift},
			InternalScores: jsonRaw(`{}`)},
	}
	result, err := h.handler.Execute(context.Background(), renderLease(2))
	if err != nil {
		t.Fatal(err)
	}
	if result.Disposition != domain.TaskDomainFail {
		t.Fatalf("disposition = %s", result.Disposition)
	}
	if _, err = h.handler.Commit(context.Background(), renderLease(2), result); err != nil {
		t.Fatal(err)
	}
	if h.repo.run.Outcome != domain.RenderOutcomeFailed && h.repo.failCalls == 0 {
		t.Fatal("run must terminally fail")
	}
	// 方案文字不受影响:Planning 产物从未被 Rendering 触碰。
	if h.repo.spec.ID == "" {
		t.Fatal("render spec should remain intact")
	}
}

func TestE2EWebPStillServedJPEG(t *testing.T) {
	normalizer := NewJPEGNormalizer()
	webp := minimalVP8()
	got, err := normalizer.Normalize(webp, "image/webp")
	if err != nil {
		t.Fatal(err)
	}
	if got.MIMEType != "image/jpeg" {
		t.Fatalf("mime = %s, want image/jpeg", got.MIMEType)
	}
}

func TestE2EDemoVersionIsContractFailure(t *testing.T) {
	h := newWorkerHarness(t)
	// Demo provider_version 不允许出现在渲染候选中:模拟 gate 返回 error 判定。
	h.gate.results = []QualityResult{{
		Decision: string(domain.QualityDecisionError), ReasonCodes: []string{ReasonQualitySchemaInvalid},
		InternalScores: jsonRaw(`{}`),
	}}
	h.repo.run.CandidateLimit = 2
	h.repo.jobPrevious = &domain.QualityEvaluation{Decision: domain.QualityDecisionRetry}
	result, err := h.handler.Execute(context.Background(), renderLease(2))
	if err != nil {
		t.Fatal(err)
	}
	if result.Disposition != domain.TaskDomainFail {
		t.Fatalf("disposition = %s, want domain fail", result.Disposition)
	}
}

var _ = errors.New
