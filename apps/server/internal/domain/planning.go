package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrRenderSpecInvalid marks a render spec that violates render_spec.v1.
var ErrRenderSpecInvalid = errors.New("render spec violates render_spec.v1")

// PlanningOperationKind and shared planning subject type labels.
const (
	PlanningSubjectType = "plan_set"
)

// Scene is one of the six supported scenes. Scene names are stable product
// vocabulary; the display copy lives in the client packages.
type Scene string

const (
	SceneGeneral   Scene = "general"
	SceneInterview Scene = "interview"
	SceneWedding   Scene = "wedding"
	SceneDate      Scene = "date"
	SceneDaily     Scene = "daily"
	SceneGathering Scene = "gathering"
)

// SceneBrief is the canonical, user-answered brief of one plan set request.
type SceneBrief struct {
	SchemaVersion string            `json:"schema_version"`
	Scene         Scene             `json:"scene"`
	Answers       map[string]string `json:"answers"`
}

type StepCategory string
type StepAction string
type GroundingSourceType string
type PlanVariantKey string

const (
	StepCategoryHair   StepCategory = "hair"
	StepCategoryMakeup StepCategory = "makeup"
	StepCategoryOutfit StepCategory = "outfit"

	ActionKeep   StepAction = "keep"
	ActionAdjust StepAction = "adjust"

	SourceReportFinding     GroundingSourceType = "report_finding"
	SourceSceneAnswer       GroundingSourceType = "scene_answer"
	SourceProfilePreference GroundingSourceType = "profile_preference"
	SourceStyleRule         GroundingSourceType = "style_rule"
	SourceFeedbackMemory    GroundingSourceType = "feedback_memory"

	VariantSharp   PlanVariantKey = "sharp"
	VariantWarm    PlanVariantKey = "warm"
	VariantNatural PlanVariantKey = "natural"
)

// PlanSet is the immutable published planning artifact: exactly three
// variants, never updated after publication.
type PlanSet struct {
	ID                   string
	UserID               string
	ReportID             string
	ProfileSnapshot      json.RawMessage
	Scene                Scene
	SceneBrief           SceneBrief
	BriefHash            string
	PlanningInputHash    string
	PlannerSchemaVersion string
	StyleRuleVersion     string
	ProviderInvocationID string
	QualityEvaluationID  string
	CreatedAt            time.Time
	Variants             []PlanVariant

	// 读投影字段:聚合后的整体渲染状态(ready/ready_partial/failed/rendering)。
	RenderState string
}

type PlanVariant struct {
	ID             string
	UserID         string
	PlanSetID      string
	Slot           int
	Key            PlanVariantKey
	Name           string
	Descriptor     string
	Rationale      string
	Recommended    bool
	OutcomeTags    []string
	DifferenceTags []string
	Steps          []PlanStep
	CreatedAt      time.Time

	// 读投影字段:由 Planning read model 合并的当前渲染视图,不落库。
	// nil 表示该 variant 尚无渲染头(从未触发渲染或读模型未装配)。
	Render *RenderStatusView
}

type PlanStep struct {
	ID            string
	UserID        string
	PlanVariantID string
	Category      StepCategory
	Action        StepAction
	Title         string
	Summary       string
	Details       PlanStepDetails
	Position      int
	Groundings    []PlanStepGrounding
	CreatedAt     time.Time
}

// PlanStepDetails is a closed, typed field set — no free-form maps.
type PlanStepDetails struct {
	Target     string   `json:"target"`
	Intensity  string   `json:"intensity"`
	Silhouette string   `json:"silhouette"`
	Palette    []string `json:"palette"`
	Layers     []string `json:"layers"`
	Avoid      []string `json:"avoid"`
	Formality  string   `json:"formality"`
}

type PlanStepGrounding struct {
	UserID     string
	PlanStepID string
	SourceType GroundingSourceType
	SourceID   string
	Reason     string
}

// RenderSpec is the frozen, deterministic hand-off to the rendering pipeline:
// a machine-executable directive compiled from one plan variant. Identity and
// composition preservation is fail-closed — only plan content varies.
type RenderSpec struct {
	ID               string
	UserID           string
	PlanVariantID    string
	SourcePhotoSetID string
	SchemaVersion    string
	Spec             RenderDirective
	ContentHash      string
	CreatedAt        time.Time
}

type RenderDirective struct {
	Identity    RenderIdentity    `json:"identity"`
	Composition RenderComposition `json:"composition"`
	Hair        RenderHair        `json:"hair"`
	Makeup      RenderMakeup      `json:"makeup"`
	Outfit      RenderOutfit      `json:"outfit"`
	Output      RenderOutput      `json:"output"`
}

type RenderIdentity struct {
	BodyAssetID            string `json:"body_asset_id"`
	FaceAssetID            string `json:"face_asset_id"`
	PreserveIdentity       bool   `json:"preserve_identity"`
	PreserveBodyProportion bool   `json:"preserve_body_proportion"`
	PreserveSkinTone       bool   `json:"preserve_skin_tone"`
	PreserveAgeImpression  bool   `json:"preserve_age_impression"`
}

type RenderComposition struct {
	PreservePose       bool `json:"preserve_pose"`
	PreserveBackground bool `json:"preserve_background"`
	PreserveLighting   bool `json:"preserve_lighting"`
	PreserveSourceCrop bool `json:"preserve_source_crop"`
	AllowOutpaint      bool `json:"allow_outpaint"`
}

type RenderHair struct {
	Action    string `json:"action"`
	Target    string `json:"target"`
	Intensity string `json:"intensity"`
}

type RenderMakeup struct {
	Action    string `json:"action"`
	Target    string `json:"target"`
	Intensity string `json:"intensity"`
}

type RenderOutfit struct {
	Silhouette string   `json:"silhouette"`
	Palette    []string `json:"palette"`
	Layers     []string `json:"layers"`
	Avoid      []string `json:"avoid"`
}

type RenderOutput struct {
	MIMEType     string `json:"mime_type"`
	AspectPolicy string `json:"aspect_policy"`
	Quality      string `json:"quality"`
}

// Validate enforces the fail-closed render_spec.v1 rules on a stored spec.
func (s RenderSpec) Validate() error {
	if s.SchemaVersion != "render_spec.v1" {
		return fmt.Errorf("%w: schema version %q", ErrRenderSpecInvalid, s.SchemaVersion)
	}
	return s.Spec.Validate()
}

// Validate enforces the fail-closed preservation and output rules on the
// directive itself.
func (d RenderDirective) Validate() error {
	identity := d.Identity
	if identity.BodyAssetID == "" || identity.FaceAssetID == "" {
		return fmt.Errorf("%w: identity assets missing", ErrRenderSpecInvalid)
	}
	if !identity.PreserveIdentity || !identity.PreserveBodyProportion || !identity.PreserveSkinTone || !identity.PreserveAgeImpression {
		return fmt.Errorf("%w: identity preservation must be fail-closed", ErrRenderSpecInvalid)
	}
	composition := d.Composition
	if !composition.PreservePose || !composition.PreserveBackground || !composition.PreserveLighting || !composition.PreserveSourceCrop || composition.AllowOutpaint {
		return fmt.Errorf("%w: composition preservation must be fail-closed", ErrRenderSpecInvalid)
	}
	if err := validateRenderAppearanceStep("hair", d.Hair.Action, d.Hair.Target, d.Hair.Intensity); err != nil {
		return err
	}
	if err := validateRenderAppearanceStep("makeup", d.Makeup.Action, d.Makeup.Target, d.Makeup.Intensity); err != nil {
		return err
	}
	outfit := d.Outfit
	if len(outfit.Palette) < 1 || len(outfit.Palette) > 8 {
		return fmt.Errorf("%w: outfit palette wants 1-8 colors, got %d", ErrRenderSpecInvalid, len(outfit.Palette))
	}
	if len(outfit.Layers) > 8 || len(outfit.Avoid) > 8 {
		return fmt.Errorf("%w: outfit layers/avoid exceed 8 items", ErrRenderSpecInvalid)
	}
	if outfit.Silhouette == "" {
		return fmt.Errorf("%w: outfit silhouette required", ErrRenderSpecInvalid)
	}
	output := d.Output
	if output.MIMEType != "image/jpeg" || output.AspectPolicy != "preserve_body_source" || output.Quality != "high" {
		return fmt.Errorf("%w: unsafe output policy %#v", ErrRenderSpecInvalid, output)
	}
	return nil
}

func validateRenderAppearanceStep(name, action, target, intensity string) error {
	if action != string(ActionKeep) && action != string(ActionAdjust) {
		return fmt.Errorf("%w: %s action %q", ErrRenderSpecInvalid, name, action)
	}
	if target == "" {
		return fmt.Errorf("%w: %s target required", ErrRenderSpecInvalid, name)
	}
	if intensity != "low" && intensity != "medium" {
		return fmt.Errorf("%w: %s intensity must be low|medium, got %q", ErrRenderSpecInvalid, name, intensity)
	}
	return nil
}

// ---- cross-boundary planning DTOs (frozen contract; shared by the
// planning service and the postgres adapter) ----

// PlanningReportSnapshot is the immutable report view Planning plans against.
// The profile snapshot is the one captured at report publication.
type PlanningReportSnapshot struct {
	ID                string
	UserID            string
	PhotoSetID        string
	FaceAssetID       string
	BodyAssetID       string
	ProfileSnapshot   json.RawMessage
	ImpressionTags    []string
	PriorityTitle     string
	PriorityCopy      string
	PriorityFindingID string
	Findings          []PlanningFindingSnapshot
}

type PlanningFindingSnapshot struct {
	ID                 string
	Category           string
	Priority           int
	Label              string
	VisibleObservation string
	Recommendation     string
}

// PlanningPlanSetKey is the semantic identity of one planning request. Equal
// keys must reuse the same published result without calling AI again.
type PlanningPlanSetKey struct {
	UserID               string
	ReportID             string
	Scene                Scene
	BriefHash            string
	PlanningInputHash    string
	PlannerSchemaVersion string
}

// FeedbackMemoryItem is one preference memory carried into the next planning
// run; it is the source_id a feedback_memory grounding points at.
type FeedbackMemoryItem struct {
	ID       string `json:"id"`
	Key      string `json:"key"`
	Category string `json:"category"`
	Value    string `json:"value"`
}

// FeedbackMemorySnapshot is the deterministic structure embedded under
// profile_snapshot.feedback_memory when Planning consumes preference memories.
type FeedbackMemorySnapshot struct {
	Items []FeedbackMemoryItem `json:"items"`
}

type PlanningStartOperationCommand struct {
	OperationID    string
	UserID         string
	Kind           OperationKind
	SubjectType    string
	SubjectID      string
	IdempotencyKey string
	DedupeKey      string
	Task           PlanningEnqueueTask
}

type PlanningEnqueueTask struct {
	Type              TaskType
	SubjectType       string
	SubjectID         string
	SubjectGeneration int
	PayloadVersion    int
	Payload           any
	DedupeKey         string
}

// PlanningPlanQualityRecord is the gate decision persisted with the plan set.
type PlanningPlanQualityRecord struct {
	ID                    string
	UserID                string
	SubjectID             string
	PolicyVersion         string
	Decision              string
	ReasonCodes           []string
	InternalScores        json.RawMessage
	EvaluatorInvocationID string
}

type PlanningEnqueueRetryCommand struct {
	UserID        string
	OperationID   string
	ProgressBPS   int
	StageCode     string
	PublicMessage string
	Quality       PlanningPlanQualityRecord
	Task          PlanningEnqueueTask
}

type PlanningPrepareCommand struct {
	UserID      string
	PlanSet     PlanSet
	Quality     PlanningPlanQualityRecord
	RenderSpecs []RenderSpec
}
