package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/zhanshimian/server/internal/domain"
)

var ErrNotFound = errNotFound("resource not found")

// ErrTaskRemoved marks a worker task whose target row was deleted while it was
// being processed (for example via DELETE /v1/me/data). It is not a failure
// worth retrying: the user discarded the data, so the result is moot.
var ErrTaskRemoved = errors.New("task target removed while processing")

type errNotFound string

func (e errNotFound) Error() string { return string(e) }

type Repository interface {
	// ---- 身份、会话与资料 ----
	// EnsureUserByIdentity finds (or creates) the user bound to one
	// (provider, identifier) pair, so the same person logging in from a
	// different device keeps a single account. nickname only seeds new users
	// and never overwrites an existing one.
	EnsureUserByIdentity(ctx context.Context, provider, identifier, nickname string) (domain.User, error)
	CreateDevUser(ctx context.Context, nickname string) (domain.User, error)
	ListIdentities(ctx context.Context, userID string) ([]domain.Identity, error)
	CreateSession(ctx context.Context, userID string, digest []byte, expiresAt time.Time) error
	// UserByTokenDigest resolves a session and slides its expiry forward
	// (30d) whenever less than 7d remain, in a single UPDATE.
	UserByTokenDigest(ctx context.Context, digest []byte) (domain.User, error)
	DeleteSessionByTokenDigest(ctx context.Context, digest []byte) error
	GetUserProfile(ctx context.Context, userID string) (domain.UserProfile, error)
	SaveUserProfile(ctx context.Context, userID string, profile domain.UserProfile) (domain.UserProfile, error)
	UpdateUserNickname(ctx context.Context, userID, nickname string) error
	UpdateUserAvatar(ctx context.Context, userID, mediaID string) error
	GetUserAvatar(ctx context.Context, userID string) (domain.MediaAsset, error)

	// ---- 短信验证码 ----
	CreateSmsCode(ctx context.Context, phone string, digest []byte, expiresAt time.Time) error
	LatestSmsCode(ctx context.Context, phone string) (domain.SmsCode, error)
	CountSmsCodesSince(ctx context.Context, phone string, since time.Time) (int64, error)
	ConsumeSmsCode(ctx context.Context, id string) error

	// ---- 媒体 ----
	CreateMedia(ctx context.Context, userID, kind, storageKey, mime string, size int64) (domain.MediaAsset, error)
	GetMediaAssets(ctx context.Context, ids []string) ([]domain.MediaAsset, error)
	GetMediaAssetsForUser(ctx context.Context, userID string, ids []string) ([]domain.MediaAsset, error)

	// ---- 统一任务队列 ----
	CreateTask(ctx context.Context, userID string, input domain.TaskInput) (domain.Task, error)
	GetTask(ctx context.Context, userID, taskID string) (domain.Task, error)
	GetTasksByIDs(ctx context.Context, userID string, ids []string) ([]domain.Task, error)
	ActiveTasks(ctx context.Context, userID string, limit int) ([]domain.Task, error)
	// LatestTasksByRef returns the newest task of taskType for every payload
	// reference in refs (e.g. plan ids for plan_look), keyed by ref.
	LatestTasksByRef(ctx context.Context, userID string, taskType domain.TaskType, refKey string, refs []string) (map[string]domain.Task, error)
	// ClaimTask atomically reclaims zombie rows (running with locked_at older
	// than 10 minutes) and locks one queued task of taskType via
	// FOR UPDATE SKIP LOCKED.
	ClaimTask(ctx context.Context, taskType domain.TaskType) (domain.Task, bool, error)
	UpdateTaskProgress(ctx context.Context, taskID string, progress int, stage string) error
	CompleteTask(ctx context.Context, taskID, resultRef string) error
	// FailTask records a failure; a zero retryAt marks the task permanently
	// failed, otherwise it is requeued to run at retryAt. code becomes the
	// API error.code (photo_rejected / timeout / task_failed).
	FailTask(ctx context.Context, taskID, code, message string, photoReasons []string, retryAt time.Time) error
	HealthJobs(ctx context.Context) (domain.JobsHealth, error)

	// ---- 分析与报告 ----
	// CreateAnalysis enqueues the analysis task in the same transaction and
	// is idempotent per user: an active analysis is returned together with
	// its live task instead of creating a second one.
	CreateAnalysis(ctx context.Context, userID string, input domain.CreateAnalysisInput) (domain.Analysis, *domain.Task, error)
	GetAnalysis(ctx context.Context, userID, analysisID string) (domain.Analysis, error)
	// GetAnalysisInput re-hydrates the worker input (media ids, scene,
	// profile) of a queued analysis.
	GetAnalysisInput(ctx context.Context, userID, analysisID string) (domain.CreateAnalysisInput, error)
	UpdateAnalysisProgress(ctx context.Context, analysisID string, progress int, stage string) error
	CompleteAnalysis(ctx context.Context, userID, analysisID string, output domain.AnalysisOutput) (string, error)
	// FailAnalysisPresentation writes the user-facing failure state onto the
	// analysis row itself (the queue state lives in tasks).
	FailAnalysisPresentation(ctx context.Context, analysisID, stage, message string) error
	GetReport(ctx context.Context, userID, reportID string) (domain.Report, error)
	// LatestReport returns the user's most recent report, so clients whose
	// local cache was wiped (e.g. reinstalled mini-program) can recover it.
	LatestReport(ctx context.Context, userID string) (domain.Report, error)

	// ---- 方案 ----
	ListPlans(ctx context.Context, userID, reportID, scene string) ([]domain.Plan, error)
	// UpsertScenePlans creates or refreshes the three-plan group of one
	// report+scene keyed by (report, scene, slug). When the stored brief is
	// unchanged the rows (and their checklists/selection) are left alone;
	// a changed brief rewrites text and clears the stale generated image.
	UpsertScenePlans(ctx context.Context, userID, reportID string, input domain.ScenePlanInput, plans []domain.Plan) ([]domain.Plan, error)
	GetPlan(ctx context.Context, userID, planID string) (domain.Plan, error)
	ApplyPlanLookResult(ctx context.Context, planID, resultURL, storageKey, providerVersion string) error
	// GetPlanLookJob re-hydrates one plan's render job (direction steps plus
	// the originating analysis photos).
	GetPlanLookJob(ctx context.Context, userID, planID string) (domain.PlanLookJob, error)
	SelectPlan(ctx context.Context, userID, planID string) error
	GetChecklist(ctx context.Context, userID, planID string) ([]domain.ChecklistItem, error)
	SetChecklistItem(ctx context.Context, userID, planID, itemID string, completed bool) (domain.ChecklistItem, error)
	AddFeedback(ctx context.Context, userID, planID string, input domain.FeedbackInput) error

	// ---- 诊断（同步，tool_results 表承载） ----
	CreateDiagnostic(ctx context.Context, userID string, input domain.DiagnosticInput, result domain.ToolResult) (domain.ToolResult, error)
	GetDiagnostic(ctx context.Context, userID, diagnosticID string) (domain.ToolResult, error)
	// LatestDiagnostic returns the user's newest result for kind (outfit/purchase).
	// Clients use it to resume after leaving the sync diagnose request.
	LatestDiagnostic(ctx context.Context, userID, kind string) (domain.ToolResult, error)
	SetDiagnosticSaved(ctx context.Context, userID, diagnosticID string, saved bool) (domain.ToolResult, error)

	// ---- 发型预览 ----
	// CreateHairPreview enqueues the hair_preview task atomically and is
	// idempotent per user: an active preview is returned with its live task.
	CreateHairPreview(ctx context.Context, userID string, input domain.HairPreviewInput, styleName string) (domain.HairPreview, *domain.Task, error)
	GetHairPreview(ctx context.Context, userID, previewID string) (domain.HairPreview, error)
	// GetActiveHairPreview returns the user's in-flight preview (its task is
	// queued/processing) so clients can resume discovery without a local ref.
	GetActiveHairPreview(ctx context.Context, userID string) (domain.HairPreview, error)
	GetHairPreviewInput(ctx context.Context, userID, previewID string) (domain.HairPreviewInput, error)
	ListSavedHairPreviews(ctx context.Context, userID string) ([]domain.HairPreview, error)
	ApplyHairPreviewResult(ctx context.Context, previewID, resultURL, storageKey, providerVersion string) error
	SaveHairPreview(ctx context.Context, userID, previewID string) (domain.HairPreview, error)

	// ---- 3D 形象（环绕预览） ----
	CreateBodyPresentation(ctx context.Context, userID string, input domain.BodyPresentationInput) (domain.BodyPresentation, *domain.Task, error)
	GetBodyPresentation(ctx context.Context, userID, id string) (domain.BodyPresentation, error)
	GetBodyOrbitWork(ctx context.Context, userID, id string) (domain.BodyPresentationInput, error)
	ListBodyPresentationStatus(ctx context.Context, userID string) (active, completed, failed *domain.BodyPresentation, err error)
	ApplyBodyOrbitResult(ctx context.Context, id, videoURL, videoKey string, durationMS int, frames []domain.OrbitFrame, frameKeys []string, providerVersion string) error

	// ---- 今日方案 ----
	GetTodayPlan(ctx context.Context, userID string) (domain.TodayPlan, error)
	SaveTodayPlan(ctx context.Context, userID string, input domain.TodayPlan) (domain.TodayPlan, error)
	GetTodayPlanLookJob(ctx context.Context, userID, planID string) (domain.TodayPlanLookJob, error)
	ApplyTodayLookResult(ctx context.Context, planID, resultURL, storageKey, providerVersion string) error
	ActivateTodayPlan(ctx context.Context, userID, planID string) (domain.TodayPlan, error)
	FeedbackTodayPlan(ctx context.Context, userID, planID, feedback string) (domain.TodayPlan, error)

	// ---- 分享 ----
	CreateShareCard(ctx context.Context, userID string, input domain.ShareCardInput, snapshot json.RawMessage) (domain.ShareCard, error)
	GetShareCard(ctx context.Context, token string) (domain.ShareCard, error)
	DeleteShareCard(ctx context.Context, userID, cardID string) error

	// ---- 衣橱 / 顾问 / 埋点 ----
	CreateWardrobeItem(ctx context.Context, userID string, input domain.WardrobeItemInput, imageURL string) (domain.WardrobeItem, error)
	ListWardrobeItems(ctx context.Context, userID string) ([]domain.WardrobeItem, error)
	DeleteWardrobeItem(ctx context.Context, userID, itemID string) error
	CreateWardrobeOutfit(ctx context.Context, userID string, input domain.WardrobeOutfitInput, contextData json.RawMessage, items []domain.WardrobeItem) (domain.WardrobeOutfit, error)
	MarkWardrobeOutfitWorn(ctx context.Context, userID, outfitID string) (domain.WardrobeOutfit, error)
	CreateAdvisorConversation(ctx context.Context, userID string, contextData json.RawMessage) (domain.AdvisorConversation, error)
	LatestAdvisorConversation(ctx context.Context, userID string) (domain.AdvisorConversation, error)
	AddAdvisorExchange(ctx context.Context, userID, conversationID, userContent, assistantContent string, actions []domain.AdvisorAction) (domain.AdvisorMessage, error)
	ListAdvisorMessages(ctx context.Context, userID, conversationID string) ([]domain.AdvisorMessage, error)
	ApplyAdvisorAction(ctx context.Context, userID, actionID string) (domain.AdvisorAction, error)
	TrackProductEvent(ctx context.Context, userID string, input domain.ProductEventInput) error

	// ---- 首页聚合 ----
	RecentPlan(ctx context.Context, userID string) (domain.Plan, error)

	DeleteUserData(ctx context.Context, userID string) ([]string, error)

	// ---- 计费 ----
	CountActiveTasksByTypes(ctx context.Context, userID string, types []string) (int, error)
	GetBillingWallet(ctx context.Context, userID string) (domain.BillingWallet, error)
	GetBillingUsage(ctx context.Context, userID string, day, hour, minute time.Time) (analysis, looks, diagnostics, advisor, advisorHour, orders int, err error)
	ApplyBilling(ctx context.Context, userID string, now time.Time, activeLooks int, decide func(domain.BillingSnapshot) (domain.BillingDecision, error)) error
	RefundBilling(ctx context.Context, refType, refID string) error
	RelinkBillingRefs(ctx context.Context, fromRefs, toRefs []string) error
	ListUnrefundedFailedTaskCharges(ctx context.Context) ([]domain.BillingLedgerEntry, error)
	CreateBillingOrder(ctx context.Context, row domain.BillingOrderRow) (domain.BillingOrderRow, error)
	GetBillingOrder(ctx context.Context, userID, orderID string) (domain.BillingOrderRow, error)
	GetBillingOrderByOutTradeNo(ctx context.Context, outTradeNo string) (domain.BillingOrderRow, error)
	FulfillBillingOrder(ctx context.Context, outTradeNo, wxOrderID string) (domain.BillingOrderRow, bool, error)
}
