package domain

import (
	"encoding/json"
	"time"
)

// Rendering outcome/state vocabulary. Outcomes are the run's terminal facts;
// states are the public projection the API serves.
const (
	RenderOutcomePublished  = "published"
	RenderOutcomeUnavailable = "unavailable"
	RenderOutcomeFailed     = "failed"
	RenderOutcomeSuperseded = "superseded"

	RenderStateQueued      = "queued"
	RenderStateGenerating  = "generating"
	RenderStateChecking    = "checking"
	RenderStateReady       = "ready"
	RenderStateFailed      = "failed"
	RenderStateUnavailable = "unavailable"

	SourceKindGeneratedPreview = "generated_preview"
	SourceKindDemoExample      = "demo_example"
	DisplayLabelStyleReference = "风格参考"
	DisplayLabelEffectExample  = "效果示例"
)

// RenderHead is the tiny mutable pointer per plan variant: the generation
// guard plus the current publication. Everything else in rendering is
// immutable.
type RenderHead struct {
	UserID               string
	PlanVariantID        string
	Generation           int
	CurrentPublicationID string
	Version              int64
}

type RenderRun struct {
	ID                   string
	UserID               string
	PlanVariantID        string
	RenderSpecID         string
	Generation           int
	OperationID          string
	CandidateLimit       int
	RoutingPolicyVersion string
	QualityPolicyVersion string
	Outcome              string
	CreatedAt            time.Time
	FinishedAt           *time.Time
}

type RenderCandidate struct {
	ID                   string
	UserID               string
	RenderRunID          string
	Ordinal              int
	AssetID              string
	ProviderInvocationID string
	CreatedAt            time.Time
}

type RenderPublication struct {
	ID                  string
	UserID              string
	PlanVariantID       string
	RenderRunID         string
	CandidateID         string
	QualityEvaluationID string
	AssetID             string
	Generation          int
	ObjectKey           string
	URL                 string
	URLExpiresAt        time.Time
	CreatedAt           time.Time
}

type RenderMediaView struct {
	AssetID      string    `json:"asset_id"`
	URL          string    `json:"url"`
	URLExpiresAt time.Time `json:"url_expires_at"`
	SourceKind   string    `json:"source_kind"`
	DisplayLabel string    `json:"display_label"`
}

type RenderStatusView struct {
	State         string           `json:"state"`
	Retryable     bool             `json:"retryable"`
	OperationID   string           `json:"operation_id"`
	RenderRunID   *string          `json:"render_run_id"`
	PublicationID *string          `json:"publication_id"`
	Media         *RenderMediaView `json:"media"`
}

type RenderRunView struct {
	ID            string           `json:"id"`
	PlanVariantID string           `json:"plan_variant_id"`
	Generation    int              `json:"generation"`
	Render        RenderStatusView `json:"render"`
}

// CanRequestCandidate is the deterministic candidate budget: candidate 1 is
// always the initial budget; candidate 2 only exists after a persisted
// quality retry expanded the limit. There is no ordinal 3.
func CanRequestCandidate(run RenderRun, ordinal int, previous *QualityEvaluation) bool {
	if ordinal == 1 {
		return run.CandidateLimit == 1
	}
	return ordinal == 2 && run.CandidateLimit == 2 &&
		previous != nil && previous.Decision == QualityDecisionRetry
}

// NewRenderRunView projects the public view from immutable facts only. It
// never carries internal scores, provider identity or reason codes.
func NewRenderRunView(run RenderRun, publication *RenderPublication, operation Operation) RenderRunView {
	view := RenderStatusView{
		State:       renderStateOf(run, operation),
		Retryable:   operation.Retryable,
		OperationID: operation.ID,
		Media:       nil,
	}
	runID := run.ID
	view.RenderRunID = &runID
	if publication != nil {
		publicationID := publication.ID
		view.PublicationID = &publicationID
		view.State = RenderStateReady
		view.Retryable = false
		view.Media = &RenderMediaView{
			AssetID:      publication.AssetID,
			URL:          publication.URL,
			URLExpiresAt: publication.URLExpiresAt,
			SourceKind:   SourceKindGeneratedPreview,
			DisplayLabel: DisplayLabelStyleReference,
		}
	}
	return RenderRunView{
		ID:            run.ID,
		PlanVariantID: run.PlanVariantID,
		Generation:    run.Generation,
		Render:        view,
	}
}

func renderStateOf(run RenderRun, operation Operation) string {
	switch run.Outcome {
	case RenderOutcomePublished:
		return RenderStateReady
	case RenderOutcomeUnavailable:
		return RenderStateUnavailable
	case RenderOutcomeFailed:
		return RenderStateFailed
	case RenderOutcomeSuperseded:
		return RenderStateUnavailable
	}
	switch operation.StageCode {
	case "render.generating":
		return RenderStateGenerating
	case "render.checking":
		return RenderStateChecking
	default:
		return RenderStateQueued
	}
}

// NewDemoMediaView is the demo-only projection: demo_example + 效果示例. It
// deliberately does not reuse the generated-preview projection.
func NewDemoMediaView(assetID, signedURL string, expiresAt time.Time) RenderMediaView {
	return RenderMediaView{
		AssetID:      assetID,
		URL:          signedURL,
		URLExpiresAt: expiresAt,
		SourceKind:   SourceKindDemoExample,
		DisplayLabel: DisplayLabelEffectExample,
	}
}

// RenderEvaluationScores is the closed shape of quality internal scores —
// confidences and booleans only, never embeddings, URLs or image bytes.
type RenderEvaluationScores struct {
	Technical   float64 `json:"technical"`
	Identity    float64 `json:"identity"`
	Anatomy     float64 `json:"anatomy"`
	Composition float64 `json:"composition"`
	Semantic    float64 `json:"semantic"`
}

func (s RenderEvaluationScores) RawMessage() json.RawMessage {
	encoded, err := json.Marshal(s)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return encoded
}
