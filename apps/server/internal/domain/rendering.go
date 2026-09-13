package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// Rendering outcome/state vocabulary. Outcomes are the run's terminal facts;
// states are the public projection the API serves.
const (
	RenderOutcomePublished   = "published"
	RenderOutcomeUnavailable = "unavailable"
	RenderOutcomeFailed      = "failed"
	RenderOutcomeSuperseded  = "superseded"

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
	MIMEType     string    `json:"mime_type"`
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

// RenderRunIdempotencyKey derives the semantic dedupe key of a render run's
// first candidate task from the API idempotency key.
func RenderRunIdempotencyKey(userID, variantID, idempotencyKey string) string {
	sum := sha256.Sum256([]byte("render:" + userID + ":" + variantID + ":" + idempotencyKey))
	return "render:" + hex.EncodeToString(sum[:])
}

// RenderCandidateDedupeKey is the fixed dedupe key of one candidate task.
func RenderCandidateDedupeKey(runID string, ordinal int) string {
	return fmt.Sprintf("render:%s:candidate:%d", runID, ordinal)
}

// ---- cross-boundary rendering DTOs (shared with the postgres adapter) ----

type RenderCreateRunCommand struct {
	UserID               string
	PlanVariantID        string
	RenderSpecID         string
	IdempotencyKey       string
	RoutingPolicyVersion string
	QualityPolicyVersion string
}

type RenderCreateRunResult struct {
	Run       RenderRun
	Operation OperationRef
	Created   bool
}

type CurrentRender struct {
	Run         RenderRun
	Publication *RenderPublication
	Operation   Operation
}

// RenderRouteState carries the model-switch budget across task retries.
type RenderRouteState struct {
	AttemptedModelKeys []string `json:"attempted_model_keys"`
	SwitchesUsed       int      `json:"switches_used"`
}

// RenderGenerateCandidatePayload is the versioned render task payload; the
// RenderSpec itself is never copied into the task.
type RenderGenerateCandidatePayload struct {
	RenderRunID string           `json:"render_run_id"`
	Ordinal     int              `json:"ordinal"`
	RouteState  RenderRouteState `json:"route_state"`
}

// RenderCandidateJob 组装一次候选生成所需的一切。
type RenderCandidateJob struct {
	Run      RenderRun
	Spec     RenderSpec
	Body     MediaAsset
	Face     MediaAsset
	Previous *QualityEvaluation
}

// RenderCandidateAsset 是候选对象入库所需的元数据。
type RenderCandidateAsset struct {
	ObjectKey string
	SHA256    string
	MIMEType  string
	ByteSize  int64
	Width     int
	Height    int
}

// RenderRecordCandidateCommand 在 lease CAS 下记录一个隔离候选。
type RenderRecordCandidateCommand struct {
	TaskID            string
	LeaseToken        string
	UserID            string
	RenderRunID       string
	SubjectGeneration int
	Ordinal           int
	Asset             RenderCandidateAsset
	ProviderInvocationID string
}

// RenderEnqueueNextCandidateCommand 扩预算并入队 Candidate 2。
type RenderEnqueueNextCandidateCommand struct {
	TaskID            string
	LeaseToken        string
	UserID            string
	RenderRunID       string
	SubjectGeneration int
	Quality           QualityEvaluation
}

// RenderFailRunCommand 终态失败 run 和 operation。
type RenderFailRunCommand struct {
	TaskID            string
	LeaseToken        string
	UserID            string
	PlanVariantID     string
	RenderRunID       string
	SubjectGeneration int
	Outcome           string
	ErrorCode         string
	Retryable         bool
}

// RenderCommitEvaluationCommand 是 pass 分支的原子发布输入。
type RenderCommitEvaluationCommand struct {
	TaskID            string
	LeaseToken        string
	UserID            string
	RenderRunID       string
	SubjectGeneration int
	CandidateID       string
	Evaluation        QualityEvaluation
	PublishedObject *RenderPublishedObject
	PublicationID   string
}

// RenderPublishedObject 是提升后的发布对象。
type RenderPublishedObject struct {
	Key      string
	SHA256   string
	ByteSize int64
}

// RenderCommitEvaluationResult 是发布事务的结果。
type RenderCommitEvaluationResult struct {
	Outcome        string
	Publication    *RenderPublication
	EnqueuedTaskID string
}
