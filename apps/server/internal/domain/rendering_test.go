package domain

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestRenderRunViewUsesProductCopyAndHidesInternalScores(t *testing.T) {
	view := NewRenderRunView(RenderRun{
		ID: "run-1", PlanVariantID: "variant-1", Generation: 3,
		Outcome: RenderOutcomePublished,
	}, &RenderPublication{
		ID: "publication-1", AssetID: "asset-1",
		URL:          "https://signed.example/render.jpg",
		URLExpiresAt: time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC),
	}, Operation{ID: "operation-1", Kind: OperationRender, Status: OperationSucceeded})

	if view.Render.State != RenderStateReady {
		t.Fatalf("state = %q", view.Render.State)
	}
	if view.Render.Media.SourceKind != SourceKindGeneratedPreview {
		t.Fatalf("source_kind = %q", view.Render.Media.SourceKind)
	}
	if view.Render.Media.DisplayLabel != DisplayLabelStyleReference {
		t.Fatalf("display_label = %q", view.Render.Media.DisplayLabel)
	}
	encoded, _ := json.Marshal(view)
	for _, forbidden := range []string{"internal_scores", "provider", "model", "reason_codes"} {
		if bytes.Contains(encoded, []byte(forbidden)) {
			t.Fatalf("public view leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestRenderRunViewProjectsNonTerminalStages(t *testing.T) {
	cases := []struct {
		outcome   string
		stageCode string
		wantState string
		wantMedia bool
		wantRetry bool
	}{
		{"", "render.generating", RenderStateGenerating, false, false},
		{"", "render.checking", RenderStateChecking, false, true},
		{"", "", RenderStateQueued, false, false},
		{RenderOutcomeFailed, "render.generating", RenderStateFailed, false, false},
		{RenderOutcomeUnavailable, "", RenderStateUnavailable, false, false},
	}
	for _, tc := range cases {
		view := NewRenderRunView(
			RenderRun{ID: "run-1", Outcome: tc.outcome},
			nil,
			Operation{ID: "op-1", StageCode: tc.stageCode, Retryable: tc.wantRetry},
		)
		if view.Render.State != tc.wantState {
			t.Fatalf("outcome=%q stage=%q state = %q, want %q", tc.outcome, tc.stageCode, view.Render.State, tc.wantState)
		}
		if (view.Render.Media != nil) != tc.wantMedia {
			t.Fatalf("outcome=%q media presence mismatch: %#v", tc.outcome, view.Render.Media)
		}
		if view.Render.Retryable != tc.wantRetry {
			t.Fatalf("retryable = %v, want %v", view.Render.Retryable, tc.wantRetry)
		}
	}
}

func TestCanRequestCandidateOnlyExpandsAfterQualityRetry(t *testing.T) {
	run := RenderRun{CandidateLimit: 1}
	if !CanRequestCandidate(run, 1, nil) {
		t.Fatal("candidate 1 must be allowed")
	}
	retry := &QualityEvaluation{Decision: QualityDecisionRetry}
	if CanRequestCandidate(run, 2, retry) {
		t.Fatal("candidate 2 must wait for the persisted budget expansion")
	}
	run.CandidateLimit = 2
	if !CanRequestCandidate(run, 2, retry) {
		t.Fatal("quality retry must allow candidate 2")
	}
	reject := &QualityEvaluation{Decision: QualityDecisionReject}
	if CanRequestCandidate(run, 2, reject) || CanRequestCandidate(run, 3, retry) {
		t.Fatal("reject or ordinal 3 must not create another candidate")
	}
}

func TestDemoMediaViewUsesEffectExampleLabel(t *testing.T) {
	expires := time.Now().Add(15 * time.Minute)
	view := NewDemoMediaView("demo-asset", "https://signed.example/demo.jpg", expires)
	if view.SourceKind != SourceKindDemoExample || view.DisplayLabel != DisplayLabelEffectExample {
		t.Fatalf("demo projection drifted: %#v", view)
	}
}
