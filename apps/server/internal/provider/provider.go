package provider

import (
	"context"

	"github.com/zhanshimian/server/internal/domain"
)

type Analyzer interface {
	Analyze(context.Context, domain.CreateAnalysisInput) (domain.AnalysisOutput, error)
}

type progressReporterKey struct{}

// WithProgressReporter attaches a progress callback to the context.
// Callers can use ProgressReporter to retrieve it; if none is attached the
// returned function is nil. This lets analyzers report granular progress
// without changing the Analyzer interface.
func WithProgressReporter(ctx context.Context, reporter func(progress int, stage string)) context.Context {
	return context.WithValue(ctx, progressReporterKey{}, reporter)
}

// ProgressReporter returns the progress callback attached to ctx, or nil.
func ProgressReporter(ctx context.Context) func(progress int, stage string) {
	if r, ok := ctx.Value(progressReporterKey{}).(func(progress int, stage string)); ok {
		return r
	}
	return nil
}

type invocationSourceKey struct{}

// WithInvocationSource attaches a worker task identifier (e.g.
// "analysis:<id>") to the context so every AI invocation log line carries the
// job that issued it. Synchronous request-scoped callers may omit it.
func WithInvocationSource(ctx context.Context, source string) context.Context {
	return context.WithValue(ctx, invocationSourceKey{}, source)
}

// InvocationSource returns the task identifier attached to ctx, or "".
func InvocationSource(ctx context.Context) string {
	if value, ok := ctx.Value(invocationSourceKey{}).(string); ok {
		return value
	}
	return ""
}

type AnalysisImage struct {
	ID       string
	Kind     string
	MIMEType string
	URL      string
	Data     []byte
}

type MediaLoader interface {
	Load(context.Context, []string) ([]AnalysisImage, error)
}

type DemoAnalyzer struct{}

func NewDemoAnalyzer() *DemoAnalyzer { return &DemoAnalyzer{} }

func (d *DemoAnalyzer) Analyze(_ context.Context, input domain.CreateAnalysisInput) (domain.AnalysisOutput, error) {
	current := "/assets/looks/natural.png"
	if len(input.MediaIDs) == 0 {
		current = "/assets/looks/natural.png"
	}
	return domain.AnalysisOutput{
		CurrentImageURL: current,
		ImpressionTags:  []string{"自然亲和", "稳重克制", "线条柔和"},
		PriorityTitle:   "先提升头肩区域的利落感",
		PriorityCopy:    "抬高发型重心、露出肩颈、加强眉眼轮廓，整体会更精神。",
		ProviderVersion: "demo-v1.0.0",
		Findings: []domain.Finding{
			{Label: "肩颈线条可更利落", Category: "outfit", Severity: "medium", Photo: "body", Detail: "落肩版型让肩线下移、上半身影量变大；换成合肩剪裁能立即收紧轮廓。", AnchorX: .66, AnchorY: .49},
			{Label: "上身配色偏沉", Category: "color", Severity: "low", Photo: "body", Detail: "上身大面积深色压低明度，脸色被压住；内搭换成象牙白等浅色可提亮。", AnchorX: .31, AnchorY: .68},
			{Label: "发型重心偏低", Category: "hair", Severity: "medium", Photo: "face", Detail: "颅顶头发贴头皮，视觉重心压在颧骨附近，显得不够精神；抬高 2–3 cm 蓬松度会利落很多。", AnchorX: .60, AnchorY: .14},
			{Label: "眉眼对比度稍弱", Category: "makeup", Severity: "low", Photo: "face", Detail: "眉形偏淡、眼妆接近素颜，眼神聚焦力弱；清晰眉峰加贴根部内眼线即可改善。", AnchorX: .42, AnchorY: .28},
		},
	}, nil
}
