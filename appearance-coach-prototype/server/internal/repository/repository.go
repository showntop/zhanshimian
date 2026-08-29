package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/example/jianwo/server/internal/domain"
)

var ErrNotFound = errNotFound("resource not found")

type errNotFound string

func (e errNotFound) Error() string { return string(e) }

type Repository interface {
	CreateSession(context.Context, string, string, []byte, time.Time) (domain.User, error)
	UserByTokenDigest(context.Context, []byte) (domain.User, error)
	CreateMedia(context.Context, string, string, string, string, int64) (domain.MediaAsset, error)
	GetMediaAssets(context.Context, []string) ([]domain.MediaAsset, error)
	GetMediaAssetsForUser(context.Context, string, []string) ([]domain.MediaAsset, error)
	CreateAnalysis(context.Context, string, domain.CreateAnalysisInput) (domain.Analysis, error)
	GetAnalysis(context.Context, string, string) (domain.Analysis, error)
	ClaimAnalysisJob(context.Context) (domain.AnalysisJob, bool, error)
	UpdateAnalysisProgress(context.Context, string, int, string) error
	CompleteAnalysis(context.Context, domain.AnalysisJob, domain.AnalysisOutput) (string, error)
	FailAnalysis(context.Context, domain.AnalysisJob, error) error
	GetReport(context.Context, string, string) (domain.Report, error)
	ListPlans(context.Context, string, string, string) ([]domain.Plan, error)
	CreateScenePlans(context.Context, string, string, domain.ScenePlanInput, []domain.Plan) ([]domain.Plan, error)
	GetPlan(context.Context, string, string) (domain.Plan, error)
	SelectPlan(context.Context, string, string) error
	GetChecklist(context.Context, string, string) ([]domain.ChecklistItem, error)
	SetChecklistItem(context.Context, string, string, bool) (domain.ChecklistItem, error)
	AddFeedback(context.Context, string, domain.FeedbackInput) error
	CreateToolResult(context.Context, string, domain.ToolInput, domain.ToolResult) (domain.ToolResult, error)
	SaveToolResult(context.Context, string, string) (domain.ToolResult, error)
	CreateHairPreview(context.Context, string, domain.HairPreviewInput, string) (domain.HairPreview, error)
	GetHairPreview(context.Context, string, string) (domain.HairPreview, error)
	ListSavedHairPreviews(context.Context, string) ([]domain.HairPreview, error)
	ClaimHairPreview(context.Context) (domain.HairPreviewJob, bool, error)
	CompleteHairPreview(context.Context, domain.HairPreviewJob, string, string, string) error
	FailHairPreview(context.Context, domain.HairPreviewJob, error) error
	SaveHairPreview(context.Context, string, string) (domain.HairPreview, error)
	GetTodayPlan(context.Context, string) (domain.TodayPlan, error)
	SaveTodayPlan(context.Context, string, domain.TodayPlan) (domain.TodayPlan, error)
	ActivateTodayPlan(context.Context, string, string) (domain.TodayPlan, error)
	FeedbackTodayPlan(context.Context, string, string, string) (domain.TodayPlan, error)
	CreateShareCard(context.Context, string, domain.ShareCardInput, json.RawMessage) (domain.ShareCard, error)
	GetShareCard(context.Context, string) (domain.ShareCard, error)
	RevokeShareCard(context.Context, string, string) error
	CreateWardrobeItem(context.Context, string, domain.WardrobeItemInput, string) (domain.WardrobeItem, error)
	ListWardrobeItems(context.Context, string) ([]domain.WardrobeItem, error)
	DeleteWardrobeItem(context.Context, string, string) error
	CreateWardrobeOutfit(context.Context, string, []domain.WardrobeItem, json.RawMessage) (domain.WardrobeOutfit, error)
	MarkWardrobeOutfitWorn(context.Context, string, string) (domain.WardrobeOutfit, error)
	CreateAdvisorConversation(context.Context, string, json.RawMessage) (domain.AdvisorConversation, error)
	AddAdvisorExchange(context.Context, string, string, string, string, []domain.AdvisorAction) (domain.AdvisorMessage, error)
	ListAdvisorMessages(context.Context, string, string) ([]domain.AdvisorMessage, error)
	ApplyAdvisorAction(context.Context, string, string) (domain.AdvisorAction, error)
	TrackProductEvent(context.Context, string, domain.ProductEventInput) error
	DeleteUserData(context.Context, string) ([]string, error)
}
