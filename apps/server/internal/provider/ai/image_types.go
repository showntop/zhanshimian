package ai

import (
	"context"

	"github.com/zhanshimian/server/internal/domain"
)

// Capability names of the rendering pipeline.
const (
	CapabilityFullLookGeneration      = "full_look_generation"
	CapabilityRenderQualityEvaluation = "render_quality_evaluation"
)

// RouteState carries the model-switch budget across task retries. It is part
// of the task payload, so a retried task resumes exactly where it failed and
// never regains a spent switch.
type RouteState = domain.RenderRouteState

// GenerationRequest is one candidate generation: validated spec plus exactly
// two references. Body is the composition/anatomy source and always comes
// first; face is the identity reference.
type GenerationRequest struct {
	RenderRunID      string
	Ordinal          int
	Spec             domain.RenderDirective
	Body             ImageInput
	Face             ImageInput
	RetryReasonCodes []string
	RouteState       RouteState
}

// GenerationResult carries raw provider bytes (any of JPEG/PNG/WebP) plus the
// routing state to persist for retries.
type GenerationResult struct {
	Data           []byte
	MIMEType       string
	Meta           InvocationMeta
	NextRouteState RouteState
}

// GenerationFailure is a typed generation error: class drives the task
// runner, route state must be persisted by the caller.
type GenerationFailure struct {
	Class      domain.ErrorClass
	Code       string
	RouteState RouteState
	Err        error
}

func (e *GenerationFailure) Error() string {
	if e == nil || e.Err == nil {
		return "generation failure"
	}
	return e.Err.Error()
}

func (e *GenerationFailure) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// ImageGenerator is the narrow generation surface the rendering service and
// the router expose.
type ImageGenerator interface {
	Generate(context.Context, GenerationRequest) (GenerationResult, error)
}
