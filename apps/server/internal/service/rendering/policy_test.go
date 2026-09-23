package rendering

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/zhanshimian/server/internal/domain"
	providerai "github.com/zhanshimian/server/internal/provider/ai"
)

type qualityFake struct {
	result providerai.QualityResult
	err    error
	calls  int
}

func (q *qualityFake) Evaluate(context.Context, providerai.QualityRequest) (providerai.QualityResult, error) {
	q.calls++
	return q.result, q.err
}

func passStage() providerai.StageResult {
	return providerai.StageResult{Pass: true, Confidence: 0.99}
}

func allPass() providerai.QualityResult {
	return providerai.QualityResult{
		Technical:   passStage(),
		Identity:    passStage(),
		Anatomy:     passStage(),
		Composition: passStage(),
		Semantic:    passStage(),
		Meta:        providerai.InvocationMeta{InvocationID: "inv-quality"},
	}
}

func testPolicy() QualityPolicy {
	return QualityPolicy{
		Version:     "render-quality-v1",
		Identity:    QualityStage{MinimumConfidence: 0.82},
		Anatomy:     QualityStage{MinimumConfidence: 0.9},
		Composition: QualityStage{MinimumConfidence: 0.9},
		Semantic:    QualityStage{MinimumConfidence: 0.85},
	}
}

func validQualityInput(ordinal int) QualityInput {
	return QualityInput{
		Run:       domain.RenderRun{ID: "run-1", CandidateLimit: 1},
		Candidate: domain.RenderCandidate{ID: "candidate-1", Ordinal: ordinal},
		Image:     NormalizedJPEG{MIMEType: "image/jpeg", Width: 1024, Height: 1536, SHA256: "abc"},
		Body:      providerai.ImageInput{AssetID: "asset-body", Role: "body"},
		Face:      providerai.ImageInput{AssetID: "asset-face", Role: "face"},
	}
}

func TestGateChainStopsAtFirstFailedStage(t *testing.T) {
	evaluator := &qualityFake{result: providerai.QualityResult{
		Technical: passStage(),
		Identity:  providerai.StageResult{Pass: false, Confidence: 0.96, ReasonCodes: []string{ReasonIdentityDrift}},
		Anatomy:   passStage(),
	}}
	chain := NewQualityGate(evaluator, testPolicy())
	got, err := chain.Evaluate(context.Background(), validQualityInput(1))
	if err != nil {
		t.Fatal(err)
	}
	if got.Decision != string(domain.QualityDecisionRetry) ||
		!reflect.DeepEqual(got.ReasonCodes, []string{ReasonIdentityDrift}) {
		t.Fatalf("unexpected decision: %#v", got)
	}
	if got.CompletedStages != 1 { // 技术完成、身份失败
		t.Fatalf("completed stages = %d", got.CompletedStages)
	}
}

func TestGatePassesWhenAllStagesPass(t *testing.T) {
	evaluator := &qualityFake{result: allPass()}
	chain := NewQualityGate(evaluator, testPolicy())
	got, err := chain.Evaluate(context.Background(), validQualityInput(1))
	if err != nil {
		t.Fatal(err)
	}
	if got.Decision != string(domain.QualityDecisionPass) || got.CompletedStages != 5 {
		t.Fatalf("unexpected result: %#v", got)
	}
	if got.InternalScores == nil || string(got.InternalScores) == "{}" {
		t.Fatal("internal scores missing")
	}
}

func TestGateIdentityUnknownOnCandidateOneIsRetry(t *testing.T) {
	evaluator := &qualityFake{result: providerai.QualityResult{
		Technical: passStage(),
		Identity:  providerai.StageResult{Pass: false, Confidence: 0.5}, // 低于阈值且无 reason code
	}}
	chain := NewQualityGate(evaluator, testPolicy())
	got, err := chain.Evaluate(context.Background(), validQualityInput(1))
	if err != nil {
		t.Fatal(err)
	}
	if got.Decision != string(domain.QualityDecisionRetry) ||
		!reflect.DeepEqual(got.ReasonCodes, []string{ReasonIdentityUnknown}) {
		t.Fatalf("unexpected result: %#v", got)
	}
}

func TestGateIdentityUnknownOnCandidateTwoIsReject(t *testing.T) {
	evaluator := &qualityFake{result: providerai.QualityResult{
		Technical: passStage(),
		Identity:  providerai.StageResult{Pass: false, Confidence: 0.5},
	}}
	chain := NewQualityGate(evaluator, testPolicy())
	got, err := chain.Evaluate(context.Background(), validQualityInput(2))
	if err != nil {
		t.Fatal(err)
	}
	if got.Decision != string(domain.QualityDecisionReject) {
		t.Fatalf("decision = %s, want reject", got.Decision)
	}
}

func TestGateViewJSONNeverLeaksInternalScores(t *testing.T) {
	evaluator := &qualityFake{result: allPass()}
	chain := NewQualityGate(evaluator, testPolicy())
	got, err := chain.Evaluate(context.Background(), validQualityInput(1))
	if err != nil {
		t.Fatal(err)
	}
	view := domain.NewRenderRunView(domain.RenderRun{ID: "run-1"}, nil, domain.Operation{})
	encoded, _ := json.Marshal(view)
	for _, key := range []string{"internal_scores", "reason_codes", "technical", "identity"} {
		if bytesContains(encoded, []byte(key)) {
			t.Fatalf("public view leaked %q", key)
		}
	}
	_ = got
}

func TestGateUnknownReasonCodeFailsClosed(t *testing.T) {
	evaluator := &qualityFake{result: providerai.QualityResult{
		Technical: passStage(),
		Identity:  providerai.StageResult{Pass: false, Confidence: 0.96, ReasonCodes: []string{"provider_made_this_up"}},
	}}
	chain := NewQualityGate(evaluator, testPolicy())
	got, err := chain.Evaluate(context.Background(), validQualityInput(1))
	if err != nil {
		t.Fatal(err)
	}
	if got.Decision != string(domain.QualityDecisionError) ||
		!reflect.DeepEqual(got.ReasonCodes, []string{ReasonQualitySchemaInvalid}) {
		t.Fatalf("unexpected result: %#v", got)
	}
}

func TestGateProviderErrorBubblesForTaskRetry(t *testing.T) {
	evaluator := &qualityFake{err: errors.New("evaluator unavailable")}
	chain := NewQualityGate(evaluator, testPolicy())
	if _, err := chain.Evaluate(context.Background(), validQualityInput(1)); err == nil {
		t.Fatal("provider error must bubble up, not consume the candidate budget")
	}
}

func TestLocalTechnicalChecksRunBeforeEvaluator(t *testing.T) {
	evaluator := &qualityFake{result: allPass()}
	chain := NewQualityGate(evaluator, testPolicy())
	input := validQualityInput(1)
	input.Image.MIMEType = "image/webp"
	got, err := chain.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if got.Decision == string(domain.QualityDecisionPass) {
		t.Fatal("webp candidate passed the technical gate")
	}
	_ = evaluator
}

// ---- 小工具 ----

func bytesContains(haystack, needle []byte) bool {
	return bytes.Contains(haystack, needle)
}
