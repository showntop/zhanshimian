package assessment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/limits"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/billing"
	"github.com/zhanshimian/server/internal/service/taskrunner"
)

const (
	PhotoSetSchemaVersion = "photo-set.v1"
	AnalyzerSchemaVersion = "analyzer.v2"
	QualityPolicyVersion  = "quality.v1"
)

type Service struct {
	repo           Repository
	assets         AssetReader
	profiles       ProfileReader
	media          MediaPresenter
	taskDefinition taskrunner.Definition
	billing        billing.Reserver
	usage          UsageCounter
	now            func() time.Time
}

type CreateCommand struct {
	UserID string
	Slots  domain.PhotoSlots
}

type CreateResult struct {
	Run       domain.AnalysisRun
	PhotoSet  domain.PhotoSet
	Operation domain.Operation
	Task      domain.Task
}

type PresentedMedia struct {
	AssetID      string
	URL          string
	URLExpiresAt time.Time
	MIMEType     string
	SourceKind   string
	DisplayLabel string
}

type ReportPhoto struct {
	ItemID string
	Role   domain.PhotoRole
	Media  PresentedMedia
}

type ReportSourceMedia struct {
	Face ReportPhoto
	Side ReportPhoto
	Body ReportPhoto
}

type SourcePhotoRef struct {
	ItemID string
	Role   domain.PhotoRole
}

type FindingView struct {
	ID, Category, Label, VisibleObservation, Recommendation string
	Priority, Position                                      int
	SourcePhoto                                             SourcePhotoRef
	Anchor                                                  domain.EvidenceAnchor
}

type ReportView struct {
	ID, PhotoSetID, HeroAssetID, PriorityTitle, PriorityCopy, SchemaVersion string
	ImpressionTags                                                          []string
	SourceMedia                                                             ReportSourceMedia
	Findings                                                                []FindingView
	CreatedAt                                                               time.Time
}

func NewService(repo Repository, assets AssetReader, profiles ProfileReader, media MediaPresenter, taskDefinition taskrunner.Definition) *Service {
	return &Service{
		repo:           repo,
		assets:         assets,
		profiles:       profiles,
		media:          media,
		taskDefinition: taskDefinition,
	}
}

// WithBilling attaches the reserve-only billing port used to charge a new
// assessment at creation time. Nil is tolerated so the service still runs
// without billing wired (the welcome/free path and tests).
func (s *Service) WithBilling(b billing.Reserver) *Service {
	s.billing = b
	return s
}

// WithUsageLimits 装配用量日限闸（与 WithBilling 同一链式做法）。限额先于
// 扣费：超限直接 429，不发生 Reserve。Nil 容忍（单测/降级组装）。
func (s *Service) WithUsageLimits(counter UsageCounter) *Service {
	s.usage = counter
	return s
}

// limitAssessmentPerDay 与 legacy billing_rules limitAnalysisPerDay 一致；
// 环境变量 USAGE_ANALYSIS_PER_DAY 可覆盖，0 = 不限（内测用）。
var limitAssessmentPerDay = limits.FromEnv(limits.EnvAnalysisPerDay, 2)

// checkDailyLimit 按服务器本地自然日计数本用户已创建的 assessment
// operation：达到上限即拒绝（旧线 decideAnalysis 的日限语义）。
func (s *Service) checkDailyLimit(ctx context.Context, userID string) error {
	if s.usage == nil {
		return nil
	}
	now := time.Now()
	if s.now != nil {
		now = s.now()
	}
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	count, err := s.usage.CountOperationsCreatedSince(ctx, userID, []domain.OperationKind{domain.OperationAssessment}, nil, dayStart)
	if err != nil {
		return err
	}
	if limitAssessmentPerDay > 0 && count >= limitAssessmentPerDay {
		return fmt.Errorf("%w: 今日形象分析次数已用完，明天再来", billing.ErrRateLimited)
	}
	return nil
}

func (s *Service) Create(ctx context.Context, cmd CreateCommand) (CreateResult, error) {
	ids := []string{cmd.Slots.FaceAssetID, cmd.Slots.SideAssetID, cmd.Slots.BodyAssetID}
	assets, err := s.assets.GetReadyAssets(ctx, cmd.UserID, ids)
	if err != nil {
		return CreateResult{}, hideForeignAsset(err)
	}
	byID := indexAssets(assets)
	if err := ValidateSlots(cmd.UserID, cmd.Slots, byID); err != nil {
		return CreateResult{}, err
	}
	if err := validateOrigins(byID); err != nil {
		return CreateResult{}, err
	}
	profile, err := s.profiles.Snapshot(ctx, cmd.UserID)
	if err != nil {
		return CreateResult{}, err
	}
	contentHash := PhotoSetContentHash(PhotoSetSchemaVersion, roleHashes(cmd.Slots, byID))
	inputHash := AnalysisInputHash(contentHash, profile, AnalyzerSchemaVersion, QualityPolicyVersion)
	params := buildCreateParams(cmd, byID, profile, contentHash, inputHash)
	params.MaxTaskAttempts = s.taskDefinition.MaxAttempts
	// 限额先于落库与扣费：超限不创建行、不 Reserve（与旧线 authorize 次序一致）。
	if err := s.checkDailyLimit(ctx, cmd.UserID); err != nil {
		return CreateResult{}, err
	}
	created, err := s.repo.CreateOrReuseAssessment(ctx, params)
	if err != nil {
		return CreateResult{}, err
	}
	if !created.Reused && s.billing != nil {
		if _, err := s.billing.Reserve(ctx, cmd.UserID, created.Operation.ID, domain.ProductAssessment, 1); err != nil {
			return CreateResult{}, err
		}
	}
	return CreateResult{
		Run:       created.Run,
		PhotoSet:  created.PhotoSet,
		Operation: created.Operation,
		Task:      created.Task,
	}, nil
}

func (s *Service) GetReport(ctx context.Context, userID, reportID string) (ReportView, error) {
	report, err := s.repo.GetReport(ctx, userID, reportID)
	if err != nil {
		return ReportView{}, err
	}
	return s.presentReport(ctx, report)
}

func (s *Service) GetCurrentReport(ctx context.Context, userID string) (ReportView, error) {
	report, err := s.repo.GetCurrentReport(ctx, userID)
	if err != nil {
		return ReportView{}, err
	}
	return s.presentReport(ctx, report)
}

func (s *Service) presentReport(ctx context.Context, report domain.AssessmentReport) (ReportView, error) {
	source := ReportSourceMedia{}
	byItem := make(map[string]domain.PhotoSetItem, len(report.PhotoSet.Items))
	for _, item := range report.PhotoSet.Items {
		media, err := s.presentAsset(ctx, item.Asset)
		if err != nil {
			return ReportView{}, err
		}
		photo := ReportPhoto{ItemID: item.ID, Role: item.Role, Media: media}
		switch item.Role {
		case domain.PhotoRoleFace:
			source.Face = photo
		case domain.PhotoRoleSide:
			source.Side = photo
		case domain.PhotoRoleBody:
			source.Body = photo
		}
		byItem[item.ID] = item
	}
	findings := make([]FindingView, 0, len(report.Findings))
	for _, finding := range report.Findings {
		findings = append(findings, FindingView{
			ID:                 finding.ID,
			Category:           finding.Category,
			Label:              finding.Label,
			VisibleObservation: finding.VisibleObservation,
			Recommendation:     finding.Recommendation,
			Priority:           finding.Priority,
			Position:           finding.Position,
			SourcePhoto:        SourcePhotoRef{ItemID: finding.SourcePhotoItemID, Role: byItem[finding.SourcePhotoItemID].Role},
			Anchor:             finding.Anchor,
		})
	}
	return ReportView{
		ID:             report.ID,
		PhotoSetID:     report.PhotoSetID,
		HeroAssetID:    report.HeroAssetID,
		PriorityTitle:  report.PriorityTitle,
		PriorityCopy:   report.PriorityCopy,
		SchemaVersion:  report.SchemaVersion,
		ImpressionTags: append([]string(nil), report.ImpressionTags...),
		SourceMedia:    source,
		Findings:       findings,
		CreatedAt:      report.CreatedAt,
	}, nil
}

func (s *Service) presentAsset(ctx context.Context, asset domain.MediaAsset) (PresentedMedia, error) {
	media, err := s.media.Present(ctx, asset)
	if err != nil {
		return PresentedMedia{}, err
	}
	media.AssetID = asset.ID
	if media.MIMEType == "" {
		media.MIMEType = asset.MIMEType
	}
	return media, nil
}

func hideForeignAsset(err error) error {
	if errors.Is(err, repository.ErrNotFound) {
		return &ValidationError{Code: "photo_asset_not_found"}
	}
	return err
}

func validateOrigins(byID map[string]domain.MediaAsset) error {
	var family string
	for _, asset := range byID {
		next := originFamily(asset.Origin)
		if next == "" {
			return &ValidationError{Code: "photo_origin_mixed"}
		}
		if family == "" {
			family = next
			continue
		}
		if next != family {
			return &ValidationError{Code: "photo_origin_mixed"}
		}
	}
	return nil
}

func originFamily(origin domain.MediaOrigin) string {
	switch origin {
	case domain.MediaOriginUserUpload:
		return "user"
	case domain.MediaOriginDemo:
		return "demo"
	default:
		return ""
	}
}

func indexAssets(assets []domain.MediaAsset) map[string]domain.MediaAsset {
	byID := make(map[string]domain.MediaAsset, len(assets))
	for _, asset := range assets {
		byID[asset.ID] = asset
	}
	return byID
}

func roleHashes(slots domain.PhotoSlots, byID map[string]domain.MediaAsset) map[domain.PhotoRole]string {
	return map[domain.PhotoRole]string{
		domain.PhotoRoleFace: byID[slots.FaceAssetID].SHA256,
		domain.PhotoRoleSide: byID[slots.SideAssetID].SHA256,
		domain.PhotoRoleBody: byID[slots.BodyAssetID].SHA256,
	}
}

func buildCreateParams(cmd CreateCommand, byID map[string]domain.MediaAsset, profile json.RawMessage, contentHash, inputHash string) domain.CreateAssessmentParams {
	return domain.CreateAssessmentParams{
		UserID:                cmd.UserID,
		PhotoSetSchemaVersion: PhotoSetSchemaVersion,
		PhotoSetContentHash:   contentHash,
		AnalysisInputHash:     inputHash,
		ProfileSnapshot:       profile,
		Slots:                 cmd.Slots,
		Assets: map[domain.PhotoRole]domain.MediaAsset{
			domain.PhotoRoleFace: byID[cmd.Slots.FaceAssetID],
			domain.PhotoRoleSide: byID[cmd.Slots.SideAssetID],
			domain.PhotoRoleBody: byID[cmd.Slots.BodyAssetID],
		},
		AnalyzerSchemaVersion: AnalyzerSchemaVersion,
		QualityPolicyVersion:  QualityPolicyVersion,
	}
}
