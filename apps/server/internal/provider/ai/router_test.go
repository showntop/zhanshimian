package ai

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/zhanshimian/server/internal/domain"
)

type routerCall struct {
	model  string
	images []RouterImage
}

type fakeModelCaller struct {
	mu          sync.Mutex
	calls       []routerCall
	failOn      map[string]int  // model -> 剩余失败次数
	permanentOn map[string]bool // model -> 永久失败
}

func (f *fakeModelCaller) GenerateOnModel(_ context.Context, modelID, _ string, call RouterImageCall) (RouterImageResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, routerCall{model: modelID, images: call.Images})
	if f.permanentOn[modelID] {
		return RouterImageResult{}, &CallError{Class: domain.ErrorPermanent, Code: "unsupported_parameter"}
	}
	if remaining, ok := f.failOn[modelID]; ok && remaining > 0 {
		f.failOn[modelID] = remaining - 1
		return RouterImageResult{}, &CallError{Class: domain.ErrorTransient, Code: "upstream_5xx"}
	}
	return RouterImageResult{Data: []byte("jpeg-bytes"), MIMEType: "image/jpeg", InvocationID: "inv-" + modelID, Protocol: "dashscope_wan"}, nil
}

func (f *fakeModelCaller) calledModels() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	models := make([]string, 0, len(f.calls))
	for _, call := range f.calls {
		models = append(models, call.model)
	}
	return models
}

func routerModels(keys ...string) map[string]RouterModel {
	models := make(map[string]RouterModel, len(keys))
	for _, key := range keys {
		models[key] = RouterModel{
			Key: key, MaxInputImages: 4, SupportsMultipleReferenceImages: true,
			IdentityPreservation: true, IdentityComparison: true,
			OutputMIMETypes: []string{"image/jpeg"}, DataRetention: "zero",
		}
	}
	return models
}

func routerRequest(state RouteState) GenerationRequest {
	return GenerationRequest{
		RenderRunID: "run-1", Ordinal: 1,
		Body:       ImageInput{AssetID: "asset-body", Role: "body", MIMEType: "image/jpeg", Width: 1200, Height: 1800, Data: []byte("body")},
		Face:       ImageInput{AssetID: "asset-face", Role: "face", MIMEType: "image/jpeg", Width: 1000, Height: 1000, Data: []byte("face")},
		RouteState: state,
	}
}

func failureState(t *testing.T, err error) RouteState {
	t.Helper()
	var failure *GenerationFailure
	if !errors.As(err, &failure) {
		t.Fatalf("expected GenerationFailure, got %v", err)
	}
	return failure.RouteState
}

func TestRouterNeverSwitchesMoreThanOnceAcrossRetries(t *testing.T) {
	caller := &fakeModelCaller{failOn: map[string]int{"primary": 1, "fallback": 1}}
	router := NewRouter(CapabilityFullLookGeneration, "primary", []string{"fallback", "third"}, routerModels("primary", "fallback", "third"), caller)
	_, firstErr := router.Generate(context.Background(), routerRequest(RouteState{}))
	first := failureState(t, firstErr)
	if first.SwitchesUsed != 1 ||
		!reflect.DeepEqual(first.AttemptedModelKeys, []string{"primary", "fallback"}) {
		t.Fatalf("first state = %#v", first)
	}

	caller.failOn["fallback"] = 0
	result, err := router.Generate(context.Background(), routerRequest(first))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(caller.calledModels(), []string{"primary", "fallback", "fallback"}) {
		t.Fatalf("retry switched again: %v", caller.calledModels())
	}
	if result.NextRouteState.SwitchesUsed != 1 {
		t.Fatalf("retry must keep the switch budget spent: %#v", result.NextRouteState)
	}
}

func TestRouterFiltersSingleImageFallbackBeforeAnyCall(t *testing.T) {
	caller := &fakeModelCaller{}
	models := routerModels("primary", "single-image")
	single := models["single-image"]
	single.MaxInputImages = 1
	models["single-image"] = single
	router := NewRouter(CapabilityFullLookGeneration, "primary", []string{"single-image"}, models, caller)
	if keys := router.EligibleModelKeys(); len(keys) != 1 || keys[0] != "primary" {
		t.Fatalf("eligible = %v", keys)
	}
	if _, err := router.Generate(context.Background(), routerRequest(RouteState{})); err != nil {
		t.Fatal(err)
	}
	for _, call := range caller.calls {
		if call.model == "single-image" {
			t.Fatal("filtered model was called")
		}
	}
}

func TestRouterReturnsCapabilityUnavailableWhenEverythingIsFiltered(t *testing.T) {
	caller := &fakeModelCaller{}
	router := NewRouter(CapabilityFullLookGeneration, "weak", nil, map[string]RouterModel{
		"weak": {Key: "weak", MaxInputImages: 1},
	}, caller)
	_, err := router.Generate(context.Background(), routerRequest(RouteState{}))
	var failure *GenerationFailure
	if !errors.As(err, &failure) || failure.Code != "capability_unavailable" {
		t.Fatalf("got %v, want capability_unavailable", err)
	}
	if len(caller.calls) != 0 {
		t.Fatalf("filtered models were called: %v", caller.calledModels())
	}
}

func TestRouterSendsBodyBeforeFace(t *testing.T) {
	caller := &fakeModelCaller{}
	router := NewRouter(CapabilityFullLookGeneration, "primary", nil, routerModels("primary"), caller)
	if _, err := router.Generate(context.Background(), routerRequest(RouteState{})); err != nil {
		t.Fatal(err)
	}
	if len(caller.calls) != 1 || len(caller.calls[0].images) != 2 {
		t.Fatalf("unexpected calls: %#v", caller.calls)
	}
	if caller.calls[0].images[0].Role != "body" || caller.calls[0].images[1].Role != "face" {
		t.Fatalf("image order = %#v", caller.calls[0].images)
	}
}

func TestRouterPermanentFailureDoesNotSwitch(t *testing.T) {
	caller := &fakeModelCaller{permanentOn: map[string]bool{"primary": true}}
	router := NewRouter(CapabilityFullLookGeneration, "primary", []string{"fallback"}, routerModels("primary", "fallback"), caller)
	_, err := router.Generate(context.Background(), routerRequest(RouteState{}))
	failure := asGenerationFailure(err)
	if failure.Class != domain.ErrorPermanent {
		t.Fatalf("class = %s", failure.Class)
	}
	if !reflect.DeepEqual(caller.calledModels(), []string{"primary"}) {
		t.Fatalf("permanent failure must not switch: %v", caller.calledModels())
	}
}
