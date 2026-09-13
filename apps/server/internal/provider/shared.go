package provider

import (
	"context"
	"mime/multipart"
	"net/textproto"
)

// 本文件承接已删除的 provider.go/hair.go 中仍被 ai_runtime/openai 使用的
// 共享符号：ctx 携带的调用来源、图像预算、图像结构体与 multipart 写入。
// 语义不变，只做符号搬移（Task 9 Step 4）。

type invocationSourceKey struct{}

// WithInvocationSource attaches a worker task identifier (e.g.
// "task:<uuid>") to the context so provider invocations can be traced back
// to the queue item that caused them.
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

type imageBudgetKey struct{}

// WithImageBudget 记录本次加载的预算（媒体加载超限时拒绝而不是截断）。
func WithImageBudget(ctx context.Context, budget string) context.Context {
	return context.WithValue(ctx, imageBudgetKey{}, budget)
}

// ImageBudget returns the load budget attached to ctx, or "".
func ImageBudget(ctx context.Context) string {
	if value, ok := ctx.Value(imageBudgetKey{}).(string); ok {
		return value
	}
	return ""
}

type progressReporterKey struct{}

// WithProgressReporter attaches a progress callback to the context.
// Callers can use ProgressReporter to retrieve it; if none is attached the
// returned function is nil.
func WithProgressReporter(ctx context.Context, reporter func(progress int, stage string)) context.Context {
	return context.WithValue(ctx, progressReporterKey{}, reporter)
}

// ProgressReporter returns the progress callback attached by
// WithProgressReporter, or nil.
func ProgressReporter(ctx context.Context) func(progress int, stage string) {
	if reporter, ok := ctx.Value(progressReporterKey{}).(func(progress int, stage string)); ok {
		return reporter
	}
	return nil
}

// AnalysisImage 是传给图像能力的输入帧。
type AnalysisImage struct {
	ID       string
	Kind     string
	MIMEType string
	URL      string
	Data     []byte
}

// writeImagePart 把一帧图像写进 multipart 表单（文件名固定 source.jpg：
// 生成端按角色顺序读，不依赖文件名区分）。
func writeImagePart(writer *multipart.Writer, image AnalysisImage) error {
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="image"; filename="source.jpg"`)
	header.Set("Content-Type", image.MIMEType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return err
	}
	_, err = part.Write(image.Data)
	return err
}
