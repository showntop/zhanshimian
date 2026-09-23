package ai

import (
	"context"
	"errors"

	"github.com/zhanshimian/server/internal/domain"
)

// RouterModel is the rendering-relevant metadata of one candidate model.
type RouterModel struct {
	Key                             string
	MaxInputImages                  int
	SupportsMultipleReferenceImages bool
	IdentityPreservation            bool
	IdentityComparison              bool
	OutputMIMETypes                 []string
	DataRetention                   string
}

// RouterImage is one reference image on the wire, role-tagged.
type RouterImage struct {
	Role     string
	MIMEType string
	Data     []byte
}

// RouterImageCall is one protocol-neutral generation call: body always first,
// face second, prompt derived only from the validated RenderSpec.
type RouterImageCall struct {
	Prompt  string
	Images  []RouterImage
	Quality string
}

// RouterImageResult is one model answer.
type RouterImageResult struct {
	Data              []byte
	MIMEType          string
	InvocationID      string
	Protocol          string
	ProviderRequestID string
	LatencyMS         int64
	CostCNY           *float64
}

// ModelImageCaller performs exactly one generation on one named model — no
// internal fallback loop; the router owns the switch budget.
type ModelImageCaller interface {
	GenerateOnModel(ctx context.Context, modelID, capability string, call RouterImageCall) (RouterImageResult, error)
}

// RouterRequirement is the per-capability hard requirement set.
type RouterRequirement struct {
	MinInputImages       int
	MultipleReferences   bool
	IdentityPreservation bool
	IdentityComparison   bool
	OutputMIMETypes      []string
	DataRetention        string
}

var routerRequirements = map[string]RouterRequirement{
	CapabilityFullLookGeneration: {
		MinInputImages: 2, MultipleReferences: true, IdentityPreservation: true,
		OutputMIMETypes: []string{"image/jpeg"}, DataRetention: "zero",
	},
	CapabilityRenderQualityEvaluation: {
		MinInputImages: 3, MultipleReferences: true, IdentityComparison: true,
		OutputMIMETypes: []string{"image/jpeg"}, DataRetention: "zero",
	},
}

// Router selects the model for one rendering call. Hard requirements filter
// the candidate list before the first call; a technical failure may switch
// once; the switch state lives in the task payload so a retried task never
// regains a spent switch.
type Router struct {
	capability string
	order      []string // eligible model keys, primary first
	models     map[string]RouterModel
	call       ModelImageCaller
}

// NewRouter builds the router for one capability.
func NewRouter(capability string, primary string, fallbacks []string, models map[string]RouterModel, call ModelImageCaller) *Router {
	requirement := routerRequirements[capability]
	order := make([]string, 0, 1+len(fallbacks))
	for _, key := range append([]string{primary}, fallbacks...) {
		if model, ok := models[key]; ok && ModelSatisfies(model, requirement) {
			order = append(order, key)
		}
	}
	return &Router{capability: capability, order: order, models: models, call: call}
}

// EligibleModelKeys exposes the filtered candidate order (tests and eval).
func (r *Router) EligibleModelKeys() []string {
	return append([]string(nil), r.order...)
}

// ModelSatisfies reports whether one model meets the hard requirements.
func ModelSatisfies(model RouterModel, requirement RouterRequirement) bool {
	if model.MaxInputImages < requirement.MinInputImages {
		return false
	}
	if requirement.MultipleReferences && !model.SupportsMultipleReferenceImages {
		return false
	}
	if requirement.IdentityPreservation && !model.IdentityPreservation {
		return false
	}
	if requirement.IdentityComparison && !model.IdentityComparison {
		return false
	}
	if requirement.DataRetention != "" && model.DataRetention != requirement.DataRetention {
		return false
	}
	for _, mime := range requirement.OutputMIMETypes {
		found := false
		for _, got := range model.OutputMIMETypes {
			if got == mime {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// Generate runs one generation honouring the persisted route state:
// a spent switch retries only the last attempted model; a fresh request
// tries the primary and may switch once on a transient/throttled failure.
func (r *Router) Generate(ctx context.Context, req GenerationRequest) (GenerationResult, error) {
	call := RouterImageCall{
		Prompt:  RenderSpecPrompt(req),
		Images:  orderedRenderImages(req),
		Quality: req.Spec.Output.Quality,
	}
	if len(r.order) == 0 {
		return GenerationResult{}, &GenerationFailure{
			Class: domain.ErrorPermanent, Code: "capability_unavailable",
			RouteState: req.RouteState, Err: errors.New("no eligible model for " + r.capability),
		}
	}
	state := req.RouteState
	if state.SwitchesUsed >= 1 {
		// 重试:只复跑最后一个已尝试模型,不再切换、不再回到 primary。
		last := state.AttemptedModelKeys[len(state.AttemptedModelKeys)-1]
		result, err := r.callModel(ctx, last, req, call)
		if err != nil {
			return GenerationResult{}, err
		}
		result.NextRouteState = state
		return result, nil
	}

	attempted := make([]string, 0, len(r.order))
	for index, key := range r.order {
		attempted = append(attempted, key)
		result, callErr := r.callModel(ctx, key, req, call)
		if callErr == nil {
			result.NextRouteState = RouteState{
				AttemptedModelKeys: append([]string(nil), attempted...),
				SwitchesUsed:       index,
			}
			return result, nil
		}
		failure := asGenerationFailure(callErr)
		failure.RouteState = RouteState{
			AttemptedModelKeys: append([]string(nil), attempted...),
			SwitchesUsed:       index,
		}
		// 只有 transient/throttled 才值得切换;permanent 换模型也不会好。
		if failure.Class != domain.ErrorTransient && failure.Class != domain.ErrorThrottled {
			return GenerationResult{}, failure
		}
		if index+1 >= len(r.order) {
			return GenerationResult{}, failure
		}
		// 切换预算只有一次:fallback(第二个候选)失败后不再尝试第三个。
		if index >= 1 {
			failure.RouteState.SwitchesUsed = 1
			failure.Code = "model_switch_exhausted"
			return GenerationResult{}, failure
		}
	}
	return GenerationResult{}, &GenerationFailure{
		Class:      domain.ErrorPermanent,
		Code:       "capability_unavailable",
		RouteState: RouteState{AttemptedModelKeys: append([]string(nil), attempted...), SwitchesUsed: 1},
		Err:        errors.New("every eligible model failed for " + r.capability),
	}
}

func (r *Router) callModel(ctx context.Context, key string, req GenerationRequest, call RouterImageCall) (GenerationResult, error) {
	result, err := r.call.GenerateOnModel(ctx, key, r.capability, call)
	if err != nil {
		var callErr *CallError
		class := domain.ErrorPermanent
		code := "model_call_failed"
		if errors.As(err, &callErr) {
			class = callErr.Class
			code = callErr.Code
		}
		return GenerationResult{}, &GenerationFailure{Class: class, Code: code, Err: err}
	}
	return GenerationResult{
		Data:     result.Data,
		MIMEType: result.MIMEType,
		Meta: InvocationMeta{
			InvocationID:      result.InvocationID,
			ModelKey:          key,
			Protocol:          result.Protocol,
			ProviderRequestID: result.ProviderRequestID,
			InputImages:       len(call.Images),
			OutputImages:      1,
			LatencyMS:         int(result.LatencyMS),
			EstimatedCostCNY:  result.CostCNY,
		},
	}, nil
}

func asGenerationFailure(err error) *GenerationFailure {
	var failure *GenerationFailure
	if errors.As(err, &failure) && failure != nil {
		return failure
	}
	var callErr *CallError
	if errors.As(err, &callErr) {
		return &GenerationFailure{Class: callErr.Class, Code: callErr.Code, Err: err}
	}
	return &GenerationFailure{Class: domain.ErrorTransient, Code: "model_call_failed", Err: err}
}

func orderedRenderImages(req GenerationRequest) []RouterImage {
	// body 固定是构图与身体比例基准,必须排在 face 之前。
	sources := make([]RouterImage, 0, 2)
	if req.Body.Data != nil {
		sources = append(sources, RouterImage{Role: "body", MIMEType: req.Body.MIMEType, Data: req.Body.Data})
	}
	if req.Face.Data != nil {
		sources = append(sources, RouterImage{Role: "face", MIMEType: req.Face.MIMEType, Data: req.Face.Data})
	}
	return sources
}
