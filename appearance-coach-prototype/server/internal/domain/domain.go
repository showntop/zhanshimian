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
	ID              string       `json:"id"`
	Status          string       `json:"status"`
	Progress        int          `json:"progress"`
	Stage           string       `json:"stage"`
	Scene           string       `json:"scene,omitempty"`
	PreviewImageURL string       `json:"preview_image_url,omitempty"`
	Media           []MediaAsset `json:"media,omitempty"`
	MediaIDs        []string     `json:"-"`
	ReportID        string       `json:"report_id,omitempty"`
	ErrorMessage    string       `json:"error_message,omitempty"`
	CreatedAt       time.Time    `json:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at"`
}

type Finding struct {
	ID       string  `json:"id"`
	Label    string  `json:"label"`
	Category string  `json:"category"`
	Severity string  `json:"severity"`
	Detail   string  `json:"detail"`
	// Photo marks which analysis photo the observation came from
	// (face/side/body); anchors are relative to that photo. Empty means a
	// legacy row that was only ever rendered on the body hero.
	Photo   string  `json:"photo,omitempty"`
	AnchorX float64 `json:"anchor_x"`
	AnchorY float64 `json:"anchor_y"`
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
	ID                string     `json:"id"`
	ReportID          string     `json:"report_id"`
	Scene             string     `json:"scene,omitempty"`
	Name              string     `json:"name"`
	Slug              string     `json:"slug"`
	ImageURL          string     `json:"image_url"`
	CurrentImageURL   string     `json:"current_image_url,omitempty"`
	GeneratedImageURL string     `json:"generated_image_url,omitempty"`
	GenerationStatus  string     `json:"generation_status,omitempty"`
	GenerationError   string     `json:"generation_error,omitempty"`
	LookProvider      string     `json:"look_provider,omitempty"`
	Recommended       bool       `json:"recommended"`
	Descriptor        string     `json:"descriptor"`
	Why               string     `json:"why"`
	OutcomeTags       []string   `json:"outcome_tags"`
	DifferenceTags    []string   `json:"difference_tags"`
	Sort              int        `json:"sort"`
	Selected          bool       `json:"selected"`
	Steps             []PlanStep `json:"steps,omitempty"`
}

// ScenePlanInput is the lightweight brief used to tailor a saved image
// profile to a specific upcoming occasion. It deliberately excludes photos
// and body measurements because an existing report is reused.
type ScenePlanInput struct {
	Scene   string            `json:"scene"`
	Answers map[string]string `json:"answers"`

	// Legacy fields keep clients from earlier development builds compatible.
	// New clients send Answers, whose keys vary by scene.
	Time       string `json:"time,omitempty"`
	Budget     string `json:"budget,omitempty"`
	Formality  string `json:"formality,omitempty"`
	Impression string `json:"impression,omitempty"`
}

type TodayContext struct {
	Date        string `json:"date"`
	City        string `json:"city"`
	Condition   string `json:"condition"`
	Temperature int    `json:"temperature"`
	DayType     string `json:"day_type"`
	Schedule    string `json:"schedule"`
}

type TodayPlanStep struct {
	Category string `json:"category"`
	Label    string `json:"label"`
	Title    string `json:"title"`
	Copy     string `json:"copy"`
}

type TodayPlan struct {
	ID              string          `json:"id"`
	ReportID        string          `json:"report_id,omitempty"`
	Context         TodayContext    `json:"context"`
	Title           string          `json:"title"`
	Summary         string          `json:"summary"`
	ImageURL        string          `json:"image_url"`
	Steps           []TodayPlanStep `json:"steps"`
	Active          bool            `json:"active"`
	Feedback        string          `json:"feedback,omitempty"`
	RegenerateCount int             `json:"regenerate_count"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type TodayPlanInput struct {
	ReportID string `json:"report_id,omitempty"`
	City     string `json:"city,omitempty"`
	Schedule string `json:"schedule,omitempty"`
	Refresh  bool   `json:"refresh,omitempty"`
}

type ShareCardInput struct {
	SourceType   string `json:"source_type"`
	SourceID     string `json:"source_id"`
	IncludePhoto bool   `json:"include_photo"`
}

type ShareCard struct {
	ID           string          `json:"id"`
	Token        string          `json:"token"`
	SourceType   string          `json:"source_type"`
	SourceID     string          `json:"source_id"`
	Snapshot     json.RawMessage `json:"snapshot"`
	IncludePhoto bool            `json:"include_photo"`
	Revoked      bool            `json:"revoked"`
	ExpiresAt    time.Time       `json:"expires_at"`
	CreatedAt    time.Time       `json:"created_at"`
}

type WardrobeItemInput struct {
	MediaID   string   `json:"media_id,omitempty"`
	Name      string   `json:"name"`
	Category  string   `json:"category"`
	Color     string   `json:"color"`
	Season    string   `json:"season,omitempty"`
	Formality string   `json:"formality,omitempty"`
	Scenes    []string `json:"scenes,omitempty"`
}

type WardrobeItem struct {
	ID        string    `json:"id"`
	MediaID   string    `json:"media_id,omitempty"`
	Name      string    `json:"name"`
	Category  string    `json:"category"`
	Color     string    `json:"color"`
	Season    string    `json:"season"`
	Formality string    `json:"formality"`
	Scenes    []string  `json:"scenes"`
	ImageURL  string    `json:"image_url"`
	Favorite  bool      `json:"favorite"`
	WearCount int       `json:"wear_count"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type WardrobeOutfit struct {
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	Note      string          `json:"note"`
	Context   json.RawMessage `json:"context"`
	ItemIDs   []string        `json:"item_ids"`
	Items     []WardrobeItem  `json:"items"`
	Worn      bool            `json:"worn"`
	CreatedAt time.Time       `json:"created_at"`
}

type AdvisorConversation struct {
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	Context   json.RawMessage `json:"context"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type AdvisorMessageInput struct {
	ConversationID string `json:"conversation_id,omitempty"`
	Content        string `json:"content"`
	ReportID       string `json:"report_id,omitempty"`
	TodayPlanID    string `json:"today_plan_id,omitempty"`
}

type AdvisorAction struct {
	ID      string          `json:"id"`
	Kind    string          `json:"kind"`
	Label   string          `json:"label"`
	Payload json.RawMessage `json:"payload"`
	Applied bool            `json:"applied"`
}

type AdvisorMessage struct {
	ID             string          `json:"id"`
	ConversationID string          `json:"conversation_id"`
	Role           string          `json:"role"`
	Content        string          `json:"content"`
	Actions        []AdvisorAction `json:"actions,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
}

type ProductEventInput struct {
	Name    string          `json:"name"`
	Payload json.RawMessage `json:"payload,omitempty"`
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
	MediaID string   `json:"media_id"`
}

type ToolInput struct {
	Kind     string       `json:"kind"`
	ReportID string       `json:"report_id,omitempty"`
	MediaID  string       `json:"media_id,omitempty"`
	Scene    string       `json:"scene,omitempty"`
	Context  *ToolContext `json:"-"`
}

type ToolContext struct {
	ImpressionTags []string
	PriorityTitle  string
	PriorityCopy   string
	Wardrobe       []ToolWardrobeItem
}

type ToolWardrobeItem struct {
	Name      string
	Category  string
	Color     string
	Season    string
	Formality string
	Scenes    []string
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

// PlanLookJob carries everything the worker needs to render one plan's
// full-look image: the plan's direction (name/descriptor/steps) and the
// source photos from the originating analysis.
type PlanLookJob struct {
	PlanID   string
	ReportID string
	UserID   string
	Name     string
	Slug     string
	Why      string
	Steps    []PlanStep
	MediaIDs []string
	Attempt  int
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
