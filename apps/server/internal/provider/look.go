package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/example/jianwo/server/internal/domain"
)

// LookInput describes one plan's target full-look direction plus the source
// photos the edit must preserve identity from.
type LookInput struct {
	Name     string
	Slug     string
	Why      string
	Steps    []domain.PlanStep
	MediaIDs []string
}

type LookOutput struct {
	ImageData       []byte
	MIMEType        string
	ProviderVersion string
}

// DemoLookGenerator keeps the complete async/storage/UI flow usable without
// spending image-model quota. It returns the user's own body photo unchanged,
// and is explicitly identified as a flow preview in API/UI metadata.
type DemoLookGenerator struct {
	loader MediaLoader
}

func NewDemoLookGenerator(loader MediaLoader) *DemoLookGenerator {
	return &DemoLookGenerator{loader: loader}
}

func (g *DemoLookGenerator) Generate(ctx context.Context, input LookInput) (LookOutput, error) {
	images, err := g.loader.Load(ctx, input.MediaIDs)
	if err != nil {
		return LookOutput{}, fmt.Errorf("load demo look source photos: %w", err)
	}
	for _, image := range images {
		if image.Kind == "body" {
			return LookOutput{ImageData: image.Data, MIMEType: image.MIMEType, ProviderVersion: "demo-look-pass-through-v1"}, nil
		}
	}
	if len(images) == 0 {
		return LookOutput{}, errors.New("demo look requires at least one source photo")
	}
	return LookOutput{ImageData: images[0].Data, MIMEType: images[0].MIMEType, ProviderVersion: "demo-look-pass-through-v1"}, nil
}

type LookGenerator interface {
	Generate(context.Context, LookInput) (LookOutput, error)
}

type RoutedLookGenerator struct {
	runtime *AIRuntime
	loader  MediaLoader
}

func NewRoutedLookGenerator(runtime *AIRuntime, loader MediaLoader) (*RoutedLookGenerator, error) {
	if runtime == nil || !runtime.HasRoute(CapabilityFullLookEdit) {
		return nil, errors.New("full look edit AI route is required")
	}
	if loader == nil {
		return nil, errors.New("media loader is required")
	}
	return &RoutedLookGenerator{runtime: runtime, loader: loader}, nil
}

func (g *RoutedLookGenerator) Generate(ctx context.Context, input LookInput) (LookOutput, error) {
	images, err := g.loader.Load(ctx, input.MediaIDs)
	if err != nil {
		return LookOutput{}, fmt.Errorf("load look source photos: %w", err)
	}
	// The body shot is the canvas (proportions, outfit); the face shot anchors
	// identity. Anything else is optional context for the editor.
	var canvas []AnalysisImage
	for _, kind := range []string{"body", "face"} {
		for _, image := range images {
			if image.Kind == kind {
				canvas = append(canvas, image)
			}
		}
	}
	if len(canvas) == 0 {
		canvas = images
	}
	result, err := g.runtime.EditImage(ctx, CapabilityFullLookEdit, ImageEditRequest{Prompt: lookPrompt(input), Images: canvas, Size: "1024*1536", Quality: "high"})
	if err != nil {
		return LookOutput{}, err
	}
	return LookOutput{ImageData: result.Data, MIMEType: result.MIMEType, ProviderVersion: result.Meta.ProviderVersion()}, nil
}

func lookPrompt(input LookInput) string {
	directions := map[string]string{}
	for _, step := range input.Steps {
		if directions[step.Category] == "" {
			directions[step.Category] = step.Title + "：" + step.Summary
		}
	}
	parts := make([]string, 0, 3)
	for _, category := range []string{"hair", "makeup", "outfit"} {
		names := map[string]string{"hair": "发型", "makeup": "妆容", "outfit": "穿搭"}
		if directions[category] != "" {
			parts = append(parts, names[category]+"——"+directions[category])
		}
	}
	var builder strings.Builder
	builder.WriteString("以第一张照片为基准，将人物的完整造型调整为「" + input.Name + "」方向")
	if len(parts) > 0 {
		builder.WriteString("：" + strings.Join(parts, "；"))
	}
	if input.Why != "" {
		builder.WriteString("。调整目标：" + input.Why)
	}
	builder.WriteString("。第二张照片是同一人物的面部参考。必须严格保留同一个人的身份、脸型、五官、表情和肤色，不要改变年龄感，不磨皮。输出必须是竖版真人整体造型照：头顶、完整脸部、发型、妆面、上衣和下装都要清楚可见，人物尽量从头到脚完整入镜；绝不能裁掉头部、遮住五官或只展示服装局部。可为满足完整构图适度调整取景，但保留原照片的姿势、背景与光线。不添加首饰、水印或文字。输出写实摄影效果，不是插画。")
	return builder.String()
}
