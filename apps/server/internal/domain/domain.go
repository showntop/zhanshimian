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

// ---- 统一任务系统 ----

type TaskType string

const (
	TaskTypeAnalysis    TaskType = "analysis"
	TaskTypeHairPreview TaskType = "hair_preview"
	TaskTypePlanGroup   TaskType = "plan_group"
	TaskTypePlanLook    TaskType = "plan_look"
	TaskTypeTodayLook   TaskType = "today_look"
)

// Task statuses follow the contract enum queued/processing/completed/failed,
// shared with the analyses table.
const (
	TaskQueued     = "queued"
	TaskProcessing = "processing"
	TaskCompleted  = "completed"
	TaskFailed     = "failed"
)

// Task is one row of the unified queue. Payload carries the domain reference
// (analysis_id / preview_id / plan_id); handlers re-hydrate business data by
// that reference so the queue never duplicates domain state.
type Task struct {
	ID        string
	UserID    string
	Type      string
	Payload   json.RawMessage
	Status    string
	Progress  int
	Stage     string
	Attempts  int
	LastError string
	ResultRef string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TaskInput is what service layers hand to the queue when enqueueing.
type TaskInput struct {
	Type     TaskType
	Payload  any
	Progress int
	Stage    string
}

// TaskView is the API shape of a task, embedded in analyses/plans/previews and
// served directly by GET /v1/tasks/{id}.
type TaskView struct {
	ID        string     `json:"id"`
	Type      string     `json:"type"`
	Status    string     `json:"status"`
	Progress  int        `json:"progress"`
	Stage     string     `json:"stage,omitempty"`
	ResultRef string     `json:"result_ref,omitempty"`
	Error     *TaskError `json:"error,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// TaskError carries the user-facing failure. photo_reasons is populated when
// the analysis photos failed the content check (one Chinese reason per photo).
type TaskError struct {
	Code         string   `json:"code"`
	Message      string   `json:"message"`
	PhotoReasons []string `json:"photo_reasons,omitempty"`
}

// TaskPayloadOf extracts the domain reference from a task payload.
type AnalysisTaskPayload struct {
	AnalysisID string `json:"analysis_id"`
}

type HairPreviewTaskPayload struct {
	PreviewID string `json:"preview_id"`
}

type PlanLookTaskPayload struct {
	PlanID string `json:"plan_id"`
}

// PlanGroupTaskPayload references the report whose general plan group should
// be (re)generated. Analysis no longer authors plans; this task owns general
// group creation from the stored report content.
type PlanGroupTaskPayload struct {
	ReportID string `json:"report_id"`
}

type TodayLookTaskPayload struct {
	PlanID string `json:"plan_id"`
}

// JobsHealth feeds /healthz observability for the unified queue.
type JobsHealth struct {
	OldestQueuedSeconds int64 `json:"oldest_queued_seconds"`
	FailedLastHour      int64 `json:"failed_last_hour"`
}

// ---- 身份与会话 ----

type Session struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	User      User      `json:"user"`
}

type User struct {
	ID       string `json:"id"`
	Nickname string `json:"nickname"`
}

// IdentityProvider values are fixed by the user_identities CHECK constraint.
const (
	ProviderWeChatMiniApp = "wechat_miniapp"
	ProviderWeChatApp     = "wechat_app"
	ProviderApple         = "apple"
	ProviderPhone         = "phone"
)

type Identity struct {
	ID         string    `json:"id"`
	Provider   string    `json:"provider"`
	Identifier string    `json:"identifier"`
	CreatedAt  time.Time `json:"created_at"`
}

// UserProfile is the persisted 补充资料: height/role/budget are required on
// PUT, the measurements are optional. A missing row simply means the user has
// not filled the profile yet (GET returns null).
type UserProfile struct {
	HeightCM  int       `json:"height_cm"`
	Role      string    `json:"role"`
	Budget    string    `json:"budget"`
	WeightKG  *float64  `json:"weight_kg,omitempty"`
	BustCM    *float64  `json:"bust_cm,omitempty"`
	WaistCM   *float64  `json:"waist_cm,omitempty"`
	HipCM     *float64  `json:"hip_cm,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// MeAccount is the GET /v1/me payload: the account plus every bound identity.
type MeAccount struct {
	ID         string     `json:"id"`
	Nickname   string     `json:"nickname"`
	Identities []Identity `json:"identities"`
}

type SmsCode struct {
	ID        string
	Phone     string
	Digest    []byte
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}

// ---- 媒体与分析 ----

type MediaAsset struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	URL        string    `json:"url"`
	Demo       bool      `json:"demo,omitempty"`
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
	TaskID          string       `json:"task_id,omitempty"`
	ErrorMessage    string       `json:"error_message,omitempty"`
	CreatedAt       time.Time    `json:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at"`
}

type Finding struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Category string `json:"category"`
	Severity string `json:"severity"`
	Detail   string `json:"detail"`
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

// ---- 方案 ----

type PlanStep struct {
	ID       string          `json:"id"`
	Category string          `json:"category"`
	Title    string          `json:"title"`
	Summary  string          `json:"summary"`
	Details  json.RawMessage `json:"details"`
	Sort     int             `json:"sort"`
}

type Plan struct {
	ID                string `json:"id"`
	ReportID          string `json:"report_id"`
	Scene             string `json:"scene,omitempty"`
	Name              string `json:"name"`
	Slug              string `json:"slug"`
	ImageURL          string `json:"image_url"`
	CurrentImageURL   string `json:"current_image_url,omitempty"`
	GeneratedImageURL string `json:"generated_image_url,omitempty"`
	// GenerationStatus/GenerationError are API-facing projections of the
	// plan_look task (the queue state itself lives in tasks).
	GenerationStatus string     `json:"generation_status,omitempty"`
	GenerationError  string     `json:"generation_error,omitempty"`
	LookProvider     string     `json:"look_provider,omitempty"`
	Recommended      bool       `json:"recommended"`
	Descriptor       string     `json:"descriptor"`
	Why              string     `json:"why"`
	OutcomeTags      []string   `json:"outcome_tags"`
	DifferenceTags   []string   `json:"difference_tags"`
	Sort             int        `json:"sort"`
	Selected         bool       `json:"selected"`
	Steps            []PlanStep `json:"steps,omitempty"`
	// LookTask embeds the latest plan_look task so plan lists render image
	// generation state without a second round of polling endpoints.
	LookTask *TaskView `json:"look_task,omitempty"`
}

// ScenePlanInput is the normalized brief used to tailor a saved image profile
// to a specific upcoming occasion. It deliberately excludes photos and body
// measurements because an existing report is reused.
type ScenePlanInput struct {
	Scene   string
	Answers map[string]string
}

// PlansUpsertInput is the request body of PUT /v1/reports/{id}/plans:
// the target scene plus the flat scene questionnaire answers.
type PlansUpsertInput struct {
	Scene   string            `json:"scene"`
	Answers map[string]string `json:"answers"`
}

type FeedbackInput struct {
	Tags    []string `json:"tags"`
	Comment string   `json:"comment"`
	MediaID string   `json:"media_id"`
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

// TodayPlanLookJob mirrors PlanLookJob for the daily plan's own try-on.
type TodayPlanLookJob struct {
	PlanID   string
	ReportID string
	UserID   string
	Title    string
	Summary  string
	Steps    []TodayPlanStep
	MediaIDs []string
	Attempt  int
}

// ---- 今日 ----

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
	// GeneratedImageURL is the user's own try-on rendered asynchronously from
	// the report photos; ImageURL stays the bundled 风格参考 shown meanwhile.
	GeneratedImageURL string    `json:"generated_image_url,omitempty"`
	GenerationStatus  string    `json:"generation_status"`
	GenerationError   string    `json:"generation_error,omitempty"`
	LookProvider      string    `json:"look_provider,omitempty"`
	LookTask          *TaskView `json:"look_task,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type TodayPlanInput struct {
	ReportID string `json:"report_id,omitempty"`
	City     string `json:"city,omitempty"`
	Schedule string `json:"schedule,omitempty"`
	Refresh  bool   `json:"refresh,omitempty"`
}

// ---- 分享 / 衣橱 / 顾问 / 埋点 ----

type ShareCardInput struct {
	SourceType   string `json:"source_type"`
	SourceID     string `json:"source_id"`
	IncludePhoto bool   `json:"include_photo"`
}

// ShareView is the public projection of a share card (GET /v1/shares/{token}):
// management fields like id/token never leave.
type ShareView struct {
	SourceType   string          `json:"source_type"`
	Snapshot     json.RawMessage `json:"snapshot"`
	IncludePhoto bool            `json:"include_photo"`
	ExpiresAt    time.Time       `json:"expires_at"`
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

// WardrobeOutfitInput is the request body of POST /v1/wardrobe/outfits: the
// client composes item_ids explicitly and freezes the current context.
type WardrobeOutfitInput struct {
	Title   string        `json:"title"`
	Note    string        `json:"note,omitempty"`
	Context *TodayContext `json:"context,omitempty"`
	ItemIDs []string      `json:"item_ids"`
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

// ---- 诊断与发型（原 tools 重组） ----

// DiagnosticInput is the request body of POST /v1/diagnostics.
type DiagnosticInput struct {
	Kind     string       `json:"kind"`
	MediaID  string       `json:"media_id"`
	Scene    string       `json:"scene,omitempty"`
	ReportID string       `json:"report_id,omitempty"`
	Context  *ToolContext `json:"-"`
}

type ToolContext struct {
	ImpressionTags []string
	PriorityTitle  string
	PriorityCopy   string
	Wardrobe       []ToolWardrobeItem
	Profile        *UserProfile
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
	MediaID         string        `json:"media_id,omitempty"`
	ImageURL        string        `json:"image_url,omitempty"`
	CreatedAt       time.Time     `json:"created_at"`
}

type HairPreviewInput struct {
	MediaID  string `json:"media_id"`
	ReportID string `json:"report_id,omitempty"`
	StyleID  string `json:"style_id"`
	Scene    string `json:"scene,omitempty"`
}

type HairPreview struct {
	ID string `json:"id"`
	// Status/Progress/Stage/ErrorMessage project the hair_preview task state.
	Status          string    `json:"status"`
	Progress        int       `json:"progress"`
	Stage           string    `json:"stage"`
	ErrorMessage    string    `json:"error_message,omitempty"`
	StyleID         string    `json:"style_id"`
	StyleName       string    `json:"style_name"`
	Scene           string    `json:"scene"`
	SourceImageURL  string    `json:"source_image_url"`
	ResultImageURL  string    `json:"result_image_url,omitempty"`
	ProviderVersion string    `json:"provider_version,omitempty"`
	Saved           bool      `json:"saved"`
	Task            *TaskView `json:"task,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
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

// ---- 首页聚合 ----

type HomeBootstrap struct {
	// ProfileSummary is the persisted profile when present (null otherwise).
	ProfileSummary *UserProfile `json:"profile_summary"`
	Report         *Report      `json:"report"`
	TodayPlan      *TodayPlan   `json:"today_plan"`
	ActiveTasks    []TaskView   `json:"active_tasks"`
	RecentPlan     *Plan        `json:"recent_plan"`
}
