package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// AllowedRetryReasons are the only stable reason codes a Candidate 2 prompt
// may carry. Provider raw error text never enters a prompt.
var AllowedRetryReasons = map[string]bool{
	"identity_drift":                 true,
	"anatomy_head":                   true,
	"anatomy_torso":                  true,
	"anatomy_arms":                   true,
	"anatomy_legs":                   true,
	"anatomy_hands":                  true,
	"composition_head_crop":          true,
	"composition_pose_changed":       true,
	"composition_background_changed": true,
	"semantic_hair_mismatch":         true,
	"semantic_makeup_mismatch":       true,
	"semantic_outfit_mismatch":       true,
	"unrequested_skin_tone_change":   true,
	"unrequested_age_change":         true,
	"unrequested_body_change":        true,
}

// ErrInvalidGenerationRequest marks a request that must not reach any model:
// wrong image count, identical references or missing spec fields.
var ErrInvalidGenerationRequest = errors.New("invalid generation request")

// ValidateGenerationRequest enforces exactly two distinct references —
// body first, face second — before any HTTP call happens.
func ValidateGenerationRequest(req GenerationRequest) error {
	if req.Body.Data == nil || req.Face.Data == nil {
		return fmt.Errorf("%w: body and face images are both required", ErrInvalidGenerationRequest)
	}
	if req.Body.AssetID == "" || req.Face.AssetID == "" {
		return fmt.Errorf("%w: body and face asset ids are required", ErrInvalidGenerationRequest)
	}
	if req.Body.AssetID == req.Face.AssetID {
		return fmt.Errorf("%w: body and face must be different assets", ErrInvalidGenerationRequest)
	}
	if req.Spec.Identity.BodyAssetID == "" || req.Spec.Identity.FaceAssetID == "" {
		return fmt.Errorf("%w: spec identity assets missing", ErrInvalidGenerationRequest)
	}
	for _, code := range req.RetryReasonCodes {
		if !AllowedRetryReasons[code] {
			return fmt.Errorf("%w: unknown retry reason %q", ErrInvalidGenerationRequest, code)
		}
	}
	return nil
}

// aspectRatioOf reduces the body width/height to a small aspect ratio such
// as "2:3"; output size always follows the body source, never a fixed box.
func aspectRatioOf(width, height int) string {
	if width <= 0 || height <= 0 {
		return "2:3"
	}
	a, b := width, height
	for b != 0 {
		a, b = b, a%b
	}
	gcd := a
	if gcd == 0 {
		return "2:3"
	}
	w, h := width/gcd, height/gcd
	for w > 16 || h > 16 {
		w = (w + 1) / 2
		h = (h + 1) / 2
	}
	return fmt.Sprintf("%d:%d", w, h)
}

// RenderSpecPrompt deterministically serializes the validated directive into
// the model instruction. It reads the spec only — never plan titles, page
// copy or marketing text — and appends candidate-2 corrections as stable
// reason codes.
func RenderSpecPrompt(req GenerationRequest) string {
	specJSON, err := json.Marshal(req.Spec)
	if err != nil {
		specJSON = []byte("{}")
	}
	var builder strings.Builder
	builder.WriteString("根据 body 图为构图与身体比例基准、face 图为身份参考，只按以下 RenderSpec 生成一张效果图。\n")
	builder.WriteString("保持人物身份、体型比例、肤色与年龄感；不得改变姿势、背景、光线与取景裁切；禁止 outpaint。\n")
	builder.WriteString("输出一张 JPEG 图片，宽高比跟随 body 原图。\n")
	builder.WriteString("RenderSpec: ")
	builder.Write(specJSON)
	if len(req.RetryReasonCodes) > 0 {
		corrections := append([]string(nil), req.RetryReasonCodes...)
		sort.Strings(corrections)
		encoded, _ := json.Marshal(map[string]any{"corrections": corrections})
		builder.WriteString("\nCorrections: ")
		builder.Write(encoded)
	}
	return builder.String()
}
