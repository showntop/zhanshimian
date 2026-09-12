package domain

import (
	"encoding/json"
	"time"
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
	CategoryHair   StepCategory = "hair"
	CategoryMakeup StepCategory = "makeup"
	CategoryOutfit StepCategory = "outfit"

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
	PlannerSchemaVersion string
	StyleRuleVersion     string
	ProviderInvocationID string
	QualityEvaluationID  string
	CreatedAt            time.Time
	Variants             []PlanVariant
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
