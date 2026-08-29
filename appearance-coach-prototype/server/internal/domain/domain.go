package domain

import (
	"encoding/json"
	"time"
)

const (
	AnalysisQueued     = "queued"
	AnalysisProcessing = "processing"
	AnalysisCompleted  = "completed"
	AnalysisFailed     = "failed"
)

type Session struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	User      User      `json:"user"`
}

type User struct {
	ID       string `json:"id"`
	Nickname string `json:"nickname"`
}

type MediaAsset struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	URL        string    `json:"url"`
	CreatedAt  time.Time `json:"created_at"`
	StorageKey string    `json:"-"`
	MIMEType   string    `json:"-"`
	ByteSize   int64     `json:"-"`
}

type CreateAnalysisInput struct {
	Scene    string   `json:"scene"`
	MediaIDs []string `json:"media_ids"`
	Profile  Profile  `json:"profile"`
}

type Profile struct {
	HeightCM int    `json:"height_cm"`
	Role     string `json:"role"`
	Budget   string `json:"budget"`
}

type Analysis struct {
	ID           string    `json:"id"`
	Status       string    `json:"status"`
	Progress     int       `json:"progress"`
	Stage        string    `json:"stage"`
	ReportID     string    `json:"report_id,omitempty"`
	ErrorMessage string    `json:"error_message,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Finding struct {
	ID       string  `json:"id"`
	Label    string  `json:"label"`
	Category string  `json:"category"`
	Severity string  `json:"severity"`
	AnchorX  float64 `json:"anchor_x"`
	AnchorY  float64 `json:"anchor_y"`
}

type Report struct {
	ID              string    `json:"id"`
	AnalysisID      string    `json:"analysis_id"`
	CurrentImageURL string    `json:"current_image_url"`
	ImpressionTags  []string  `json:"impression_tags"`
	PriorityTitle   string    `json:"priority_title"`
	PriorityCopy    string    `json:"priority_copy"`
	Findings        []Finding `json:"findings"`
	ProviderVersion string    `json:"provider_version"`
	GeneratedAt     time.Time `json:"generated_at"`
}

type PlanStep struct {
	ID       string          `json:"id"`
	Category string          `json:"category"`
	Title    string          `json:"title"`
	Summary  string          `json:"summary"`
	Details  json.RawMessage `json:"details"`
	Sort     int             `json:"sort"`
}

type Plan struct {
	ID             string     `json:"id"`
	ReportID       string     `json:"report_id"`
	Name           string     `json:"name"`
	Slug           string     `json:"slug"`
	ImageURL       string     `json:"image_url"`
	Recommended    bool       `json:"recommended"`
	Descriptor     string     `json:"descriptor"`
	Why            string     `json:"why"`
	OutcomeTags    []string   `json:"outcome_tags"`
	DifferenceTags []string   `json:"difference_tags"`
	Sort           int        `json:"sort"`
	Selected       bool       `json:"selected"`
	Steps          []PlanStep `json:"steps,omitempty"`
}

type ChecklistItem struct {
	ID          string `json:"id"`
	PlanID      string `json:"plan_id"`
	Category    string `json:"category"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Meta        string `json:"meta"`
	Completed   bool   `json:"completed"`
	Sort        int    `json:"sort"`
}

type FeedbackInput struct {
	PlanID  string   `json:"plan_id"`
	Tags    []string `json:"tags"`
	Comment string   `json:"comment"`
}

type ToolInput struct {
	Kind     string `json:"kind"`
	ReportID string `json:"report_id,omitempty"`
	MediaID  string `json:"media_id,omitempty"`
	Scene    string `json:"scene,omitempty"`
}

type ToolFinding struct {
	Label    string  `json:"label"`
	Category string  `json:"category"`
	Tone     string  `json:"tone"`
	AnchorX  float64 `json:"anchor_x,omitempty"`
	AnchorY  float64 `json:"anchor_y,omitempty"`
}

type ToolOption struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	ImageURL string   `json:"image_url"`
	Note     string   `json:"note"`
	Reason   string   `json:"reason"`
	Tags     []string `json:"tags"`
}

type ToolResult struct {
	ID              string        `json:"id"`
	Kind            string        `json:"kind"`
	Scene           string        `json:"scene"`
	Conclusion      string        `json:"conclusion"`
	PriorityTitle   string        `json:"priority_title"`
	PriorityCopy    string        `json:"priority_copy"`
	Tags            []string      `json:"tags"`
	Findings        []ToolFinding `json:"findings"`
	Options         []ToolOption  `json:"options,omitempty"`
	Saved           bool          `json:"saved"`
	ProviderVersion string        `json:"provider_version,omitempty"`
	CreatedAt       time.Time     `json:"created_at"`
}

type HairPreviewInput struct {
	MediaID  string `json:"media_id"`
	ReportID string `json:"report_id,omitempty"`
	StyleID  string `json:"style_id"`
	Scene    string `json:"scene,omitempty"`
}

type HairPreview struct {
	ID              string    `json:"id"`
	Status          string    `json:"status"`
	Progress        int       `json:"progress"`
	Stage           string    `json:"stage"`
	StyleID         string    `json:"style_id"`
	StyleName       string    `json:"style_name"`
	Scene           string    `json:"scene"`
	SourceImageURL  string    `json:"source_image_url"`
	ResultImageURL  string    `json:"result_image_url,omitempty"`
	ProviderVersion string    `json:"provider_version,omitempty"`
	Saved           bool      `json:"saved"`
	ErrorMessage    string    `json:"error_message,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type HairPreviewJob struct {
	PreviewID string
	UserID    string
	Attempt   int
	Input     HairPreviewInput
}

type AnalysisOutput struct {
	CurrentImageURL string
	ImpressionTags  []string
	PriorityTitle   string
	PriorityCopy    string
	Findings        []Finding
	Plans           []Plan
	ProviderVersion string
}

type AnalysisJob struct {
	ID         string
	AnalysisID string
	UserID     string
	Attempt    int
	Input      CreateAnalysisInput
}
