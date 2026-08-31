package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// PhotoRejection describes one uploaded photo that failed the content check.
type PhotoRejection struct {
	Kind   string
	Reason string
}

// PhotoRejectedError is returned when the submitted photos do not match the
// required kinds (front face, side profile, full body). It is permanent by
// nature: the job must not be retried, the user has to retake the photos.
type PhotoRejectedError struct {
	Rejections []PhotoRejection
}

func (e *PhotoRejectedError) Error() string {
	parts := make([]string, 0, len(e.Rejections))
	for _, item := range e.Rejections {
		parts = append(parts, photoKindName(item.Kind)+"："+item.Reason)
	}
	return "analysis photos rejected: " + strings.Join(parts, "；")
}

// UserMessage is the failure copy surfaced on the miniapp analysis page.
func (e *PhotoRejectedError) UserMessage() string {
	parts := make([]string, 0, len(e.Rejections))
	for _, item := range e.Rejections {
		parts = append(parts, photoKindName(item.Kind)+"照片："+item.Reason)
	}
	return strings.Join(parts, "；") + "。请按拍摄指引重新提交"
}

type photoCheckItem struct {
	Kind   string `json:"kind"`
	OK     bool   `json:"ok"`
	Reason string `json:"reason"`
}

type photoCheckPayload struct {
	Results []photoCheckItem `json:"results"`
}

const photoCheckPrompt = `请先只做照片合规检查，不要做形象分析。
三张照片依次声明为：正脸照、侧脸照、全身照。逐一判断照片内容是否与声明一致、主体清晰：
- 正脸照：真实人物正对镜头，面部完整清晰，无严重遮挡、模糊或过暗；
- 侧脸照：真实人物约 90° 侧脸，轮廓清晰；
- 全身照：真实人物正面全身，完整露出肩颈、腰线与鞋子。
风景、宠物、物品、截图、插画或无法辨认人物的照片一律判为不符合。
对每张照片输出 kind（face/side/body，与声明顺序对应）、ok（布尔）和 reason：ok 为 true 时 reason 用空字符串；否则用不超过 20 字的中文说明主要问题。顶层输出一个 JSON 对象，不要输出数组或额外解释。`

func photoCheckSchema() map[string]any {
	item := objectSchema(map[string]any{
		"kind":   map[string]any{"type": "string", "enum": []string{"face", "side", "body"}},
		"ok":     map[string]any{"type": "boolean"},
		"reason": map[string]any{"type": "string", "maxLength": 60},
	}, "kind", "ok", "reason")
	return objectSchema(map[string]any{
		"results": map[string]any{"type": "array", "items": item, "minItems": 3, "maxItems": 3},
	}, "results")
}

func validatePhotoCheckPayload(payload photoCheckPayload) error {
	if len(payload.Results) != 3 {
		return fmt.Errorf("photo check output has %d results, want 3", len(payload.Results))
	}
	seen := map[string]bool{}
	for _, item := range payload.Results {
		if !map[string]bool{"face": true, "side": true, "body": true}[item.Kind] || seen[item.Kind] {
			return fmt.Errorf("photo check output has unknown or duplicate kind %q", item.Kind)
		}
		seen[item.Kind] = true
		if item.OK && item.Reason != "" {
			return fmt.Errorf("photo check output for %q has a reason despite ok", item.Kind)
		}
		if !item.OK && strings.TrimSpace(item.Reason) == "" {
			return fmt.Errorf("photo check output for %q is missing a rejection reason", item.Kind)
		}
	}
	return nil
}

// checkPhotos gates the analysis on photo content. A definitive mismatch
// returns *PhotoRejectedError; a check that cannot complete (provider error,
// malformed output after fallbacks) is a transient error and the job may
// retry. Callers must only invoke it when a photo_check route exists.
func (a *RoutedAnalyzer) checkPhotos(ctx context.Context, images []AnalysisImage) error {
	result, err := a.runtime.Structured(ctx, CapabilityPhotoCheck, StructuredRequest{
		Instructions: "你是严格的拍摄引导检查员。只判断照片是否符合声明的拍摄类型与清晰度要求，不评价人物外貌。",
		Prompt:       photoCheckPrompt, Images: images,
		SchemaName: CapabilityPhotoCheck, Schema: photoCheckSchema(), MaxOutputTokens: 400,
		Validate: func(data []byte) error {
			var candidate photoCheckPayload
			if err := json.Unmarshal(data, &candidate); err != nil {
				return err
			}
			return validatePhotoCheckPayload(candidate)
		},
	})
	if err != nil {
		return err
	}
	var payload photoCheckPayload
	if err := json.Unmarshal(result.JSON, &payload); err != nil {
		return fmt.Errorf("decode photo check output: %w", err)
	}
	rejected := &PhotoRejectedError{}
	for _, item := range payload.Results {
		if !item.OK {
			rejected.Rejections = append(rejected.Rejections, PhotoRejection{Kind: item.Kind, Reason: strings.TrimSpace(item.Reason)})
		}
	}
	if len(rejected.Rejections) > 0 {
		return rejected
	}
	return nil
}
