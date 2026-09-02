package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/example/jianwo/server/internal/domain"
	"github.com/example/jianwo/server/internal/provider"
	"github.com/example/jianwo/server/internal/repository"
	"github.com/example/jianwo/server/internal/storage"
	"github.com/google/uuid"
)

var ErrValidation = errors.New("validation error")
var ErrForbidden = errors.New("forbidden")

type Service struct {
	repo            repository.Repository
	storage         storage.ObjectStorage
	analyzer        provider.Analyzer
	hairGenerator   provider.HairPreviewGenerator
	lookGenerator   provider.LookGenerator
	outfitAdvisor   provider.OutfitAdvisor
	purchaseAdvisor provider.OutfitAdvisor
	advisorChat     provider.AdvisorChat
	weather         provider.WeatherProvider
	wechat          provider.WeChatAuthenticator
	publicBaseURL   string
	assetURLTTL     time.Duration
	sessionTTL      time.Duration
	maxUpload       int64
	logger          *slog.Logger
}

type ProviderOptions struct {
	Hair        provider.HairPreviewGenerator
	Look        provider.LookGenerator
	Outfit      provider.OutfitAdvisor
	Purchase    provider.OutfitAdvisor
	Advisor     provider.AdvisorChat
	Weather     provider.WeatherProvider
	WeChat      provider.WeChatAuthenticator
	AssetURLTTL time.Duration
}

func New(repo repository.Repository, storage storage.ObjectStorage, analyzer provider.Analyzer, publicBaseURL string, sessionTTL time.Duration, maxUpload int64, logger *slog.Logger, options ...ProviderOptions) *Service {
	hairGenerator := provider.HairPreviewGenerator(provider.NewDemoHairGenerator())
	outfitAdvisor := provider.OutfitAdvisor(provider.NewDemoOutfitAdvisor())
	var purchaseAdvisor provider.OutfitAdvisor
	var advisorChat provider.AdvisorChat
	weather := provider.WeatherProvider(provider.NewDemoWeatherProvider())
	var wechat provider.WeChatAuthenticator
	var lookGenerator provider.LookGenerator
	assetURLTTL := 15 * time.Minute
	if len(options) > 0 {
		if options[0].Hair != nil {
			hairGenerator = options[0].Hair
		}
		if options[0].Outfit != nil {
			outfitAdvisor = options[0].Outfit
		}
		purchaseAdvisor = options[0].Purchase
		advisorChat = options[0].Advisor
		if options[0].Weather != nil {
			weather = options[0].Weather
		}
		wechat = options[0].WeChat
		lookGenerator = options[0].Look
		if options[0].AssetURLTTL > 0 {
			assetURLTTL = options[0].AssetURLTTL
		}
	}
	return &Service{repo: repo, storage: storage, analyzer: analyzer, hairGenerator: hairGenerator, lookGenerator: lookGenerator, outfitAdvisor: outfitAdvisor, purchaseAdvisor: purchaseAdvisor, advisorChat: advisorChat, weather: weather, wechat: wechat, publicBaseURL: strings.TrimSuffix(publicBaseURL, "/"), assetURLTTL: assetURLTTL, sessionTTL: sessionTTL, maxUpload: maxUpload, logger: logger}
}

func (s *Service) DevLogin(ctx context.Context, nickname string) (domain.Session, error) {
	return s.createSession(ctx, "dev:"+uuid.NewString(), nickname)
}

func (s *Service) WeChatLogin(ctx context.Context, code, nickname string) (domain.Session, error) {
	if s.wechat == nil {
		return domain.Session{}, provider.ErrWeChatUnavailable
	}
	identity, err := s.wechat.ExchangeCode(ctx, code)
	if err != nil {
		return domain.Session{}, err
	}
	return s.createSession(ctx, identity.OpenID, nickname)
}

func (s *Service) createSession(ctx context.Context, openID, nickname string) (domain.Session, error) {
	if strings.TrimSpace(nickname) == "" {
		nickname = "怎么打扮用户"
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return domain.Session{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	digest := sha256.Sum256([]byte(token))
	expiresAt := time.Now().Add(s.sessionTTL)
	user, err := s.repo.CreateSession(ctx, openID, nickname, digest[:], expiresAt)
	if err != nil {
		return domain.Session{}, err
	}
	return domain.Session{Token: token, ExpiresAt: expiresAt, User: user}, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (domain.User, error) {
	if token == "" {
		return domain.User{}, ErrForbidden
	}
	digest := sha256.Sum256([]byte(token))
	user, err := s.repo.UserByTokenDigest(ctx, digest[:])
	if errors.Is(err, repository.ErrNotFound) {
		return domain.User{}, ErrForbidden
	}
	return user, err
}

func (s *Service) UploadMedia(ctx context.Context, userID, kind, filename, mimeType string, size int64, reader io.Reader) (domain.MediaAsset, error) {
	_ = filename
	_ = mimeType
	validKind := map[string]bool{"face": true, "side": true, "body": true, "feedback": true, "outfit": true, "product": true, "wardrobe": true}
	if !validKind[kind] {
		return domain.MediaAsset{}, fmt.Errorf("%w: unsupported photo kind", ErrValidation)
	}
	if size <= 0 || size > s.maxUpload {
		return domain.MediaAsset{}, fmt.Errorf("%w: photo must be between 1 byte and 10 MB", ErrValidation)
	}
	header := make([]byte, 512)
	read, readErr := io.ReadFull(reader, header)
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return domain.MediaAsset{}, fmt.Errorf("inspect photo: %w", readErr)
	}
	header = header[:read]
	detectedMIME, ext := detectImageType(header)
	if ext == "" {
		return domain.MediaAsset{}, fmt.Errorf("%w: only jpeg, png and webp are supported", ErrValidation)
	}
	key := fmt.Sprintf("%s/%s%s", userID, uuid.NewString(), ext)
	content := io.MultiReader(bytes.NewReader(header), reader)
	storedKey, err := s.storage.Save(ctx, key, io.LimitReader(content, s.maxUpload+1))
	if err != nil {
		return domain.MediaAsset{}, err
	}
	asset, err := s.repo.CreateMedia(ctx, userID, kind, storedKey, detectedMIME, size)
	if err != nil {
		_ = s.storage.Delete(ctx, storedKey)
		return domain.MediaAsset{}, err
	}
	asset.URL = s.absoluteURL("/uploads/" + storedKey)
	return asset, nil
}

func detectImageType(header []byte) (string, string) {
	mimeType := http.DetectContentType(header)
	extensions := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp"}
	return mimeType, extensions[mimeType]
}

func (s *Service) CreateDemoMedia(ctx context.Context, userID, kind string) (domain.MediaAsset, error) {
	validKind := map[string]bool{"face": true, "side": true, "body": true, "outfit": true, "product": true, "wardrobe": true}
	if !validKind[kind] {
		return domain.MediaAsset{}, fmt.Errorf("%w: unsupported photo kind", ErrValidation)
	}
	asset, err := s.repo.CreateMedia(ctx, userID, kind, "demo/"+kind+".png", "image/png", 1)
	if err != nil {
		return domain.MediaAsset{}, err
	}
	asset.URL = s.demoMediaURL(kind)
	return asset, nil
}

func (s *Service) CreateAnalysis(ctx context.Context, userID string, input domain.CreateAnalysisInput) (domain.Analysis, error) {
	if len(input.MediaIDs) != 3 {
		return domain.Analysis{}, fmt.Errorf("%w: 请上传正脸、侧脸和全身三张照片", ErrValidation)
	}
	seen := map[string]bool{}
	for _, id := range input.MediaIDs {
		if _, err := uuid.Parse(id); err != nil || seen[id] {
			return domain.Analysis{}, fmt.Errorf("%w: invalid media ids", ErrValidation)
		}
		seen[id] = true
	}
	if input.Scene == "" {
		input.Scene = "general"
	}
	analysis, err := s.repo.CreateAnalysis(ctx, userID, input)
	if err != nil {
		return domain.Analysis{}, err
	}
	analysis.MediaIDs = input.MediaIDs
	return s.hydrateAnalysisMedia(ctx, userID, analysis)
}

func (s *Service) GetAnalysis(ctx context.Context, userID, id string) (domain.Analysis, error) {
	analysis, err := s.repo.GetAnalysis(ctx, userID, id)
	if err != nil {
		return domain.Analysis{}, err
	}
	return s.hydrateAnalysisMedia(ctx, userID, analysis)
}

// hydrateAnalysisMedia makes the exact user-owned photos available to every
// analysis state. Keeping the lookup server-side avoids trusting temporary
// mini-program paths and keeps a resumed analysis visually consistent.
func (s *Service) hydrateAnalysisMedia(ctx context.Context, userID string, analysis domain.Analysis) (domain.Analysis, error) {
	assets, err := s.repo.GetMediaAssetsForUser(ctx, userID, analysis.MediaIDs)
	if err != nil {
		return domain.Analysis{}, err
	}
	for index := range assets {
		assets[index].URL = s.mediaAssetURL(assets[index])
	}
	if analysis.PreviewImageURL == "" {
		analysis.PreviewImageURL = s.pickReportPreviewImage(assets)
	}
	analysis.Media = assets
	return analysis, nil
}

// pickReportPreviewImage chooses the full-body photo as the report hero image
// when available, falling back to face and then to any uploaded photo.
func (s *Service) pickReportPreviewImage(assets []domain.MediaAsset) string {
	for _, kind := range []string{"body", "face"} {
		for _, asset := range assets {
			if asset.Kind == kind {
				return asset.URL
			}
		}
	}
	if len(assets) > 0 {
		return assets[0].URL
	}
	return ""
}

func (s *Service) analysisPreviewURL(ctx context.Context, userID string, mediaIDs []string) (string, error) {
	assets, err := s.repo.GetMediaAssetsForUser(ctx, userID, mediaIDs)
	if err != nil {
		return "", err
	}
	for index := range assets {
		// This value is persisted on the report row, so it must stay in
		// canonical relative form: GetReport re-expands and re-signs it on
		// every read, and a stored signed URL would expire with its TTL.
		assets[index].URL = relativeAssetURL(assets[index])
	}
	if url := s.pickReportPreviewImage(assets); url != "" {
		return url, nil
	}
	return "", repository.ErrNotFound
}

func (s *Service) mediaAssetURL(asset domain.MediaAsset) string {
	if strings.HasPrefix(asset.StorageKey, "demo/") {
		return s.demoMediaURL(asset.Kind)
	}
	return s.absoluteURL("/uploads/" + strings.TrimPrefix(asset.StorageKey, "/"))
}

// relativeAssetURL is the storable form of an asset reference: a bundled
// /assets path or an /uploads path without host or signature. Read paths
// expand it through absoluteURL, which signs a fresh URL on private object
// stores, so persisted URLs never expire with their signature.
func relativeAssetURL(asset domain.MediaAsset) string {
	if strings.HasPrefix(asset.StorageKey, "demo/") {
		return demoMediaAssetPath(asset.Kind)
	}
	return "/uploads/" + strings.TrimPrefix(asset.StorageKey, "/")
}

func (s *Service) demoMediaURL(kind string) string {
	return s.absoluteURL(demoMediaAssetPath(kind))
}

func demoMediaAssetPath(kind string) string {
	assetName := "natural.png"
	if kind == "product" {
		assetName = "warm.png"
	}
	return "/assets/looks/" + assetName
}
func (s *Service) GetReport(ctx context.Context, userID, id string) (domain.Report, error) {
	report, err := s.repo.GetReport(ctx, userID, id)
	if err == nil {
		report.CurrentImageURL = s.resolveAssetURL(report.CurrentImageURL)
	}
	return report, err
}
func (s *Service) ListPlans(ctx context.Context, userID, reportID, scene string) ([]domain.Plan, error) {
	plans, err := s.repo.ListPlans(ctx, userID, reportID, scene)
	for i := range plans {
		plans[i].ImageURL = s.resolveAssetURL(plans[i].ImageURL)
		plans[i].CurrentImageURL = s.resolveAssetURL(plans[i].CurrentImageURL)
		if plans[i].GeneratedImageURL != "" {
			plans[i].GeneratedImageURL = s.resolveAssetURL(plans[i].GeneratedImageURL)
		}
	}
	return plans, err
}

// GeneratePlanLooks queues full-look image generation for every plan of the
// report+scene and returns the plans with their current generation state.
func (s *Service) GeneratePlanLooks(ctx context.Context, userID, reportID, scene string, refresh bool) ([]domain.Plan, error) {
	if s.lookGenerator == nil {
		return nil, fmt.Errorf("%w: 当前环境暂未开启本人方案生成", ErrValidation)
	}
	if _, err := uuid.Parse(reportID); err != nil {
		return nil, fmt.Errorf("%w: 无效的形象报告", ErrValidation)
	}
	if scene == "" {
		scene = "general"
	}
	if !map[string]bool{"general": true, "daily": true, "interview": true, "wedding": true, "date": true}[scene] {
		return nil, fmt.Errorf("%w: 不支持的使用场景", ErrValidation)
	}
	if err := s.repo.QueuePlanLooks(ctx, userID, reportID, scene, refresh); err != nil {
		return nil, err
	}
	plans, err := s.ListPlans(ctx, userID, reportID, scene)
	if err == nil && len(plans) == 0 {
		return nil, repository.ErrNotFound
	}
	return plans, err
}
func (s *Service) GetPlan(ctx context.Context, userID, planID string) (domain.Plan, error) {
	plan, err := s.repo.GetPlan(ctx, userID, planID)
	if err == nil {
		plan.ImageURL = s.resolveAssetURL(plan.ImageURL)
		plan.CurrentImageURL = s.resolveAssetURL(plan.CurrentImageURL)
		if plan.GeneratedImageURL != "" {
			plan.GeneratedImageURL = s.resolveAssetURL(plan.GeneratedImageURL)
		}
	}
	return plan, err
}
func (s *Service) SelectPlan(ctx context.Context, userID, planID string) error {
	return s.repo.SelectPlan(ctx, userID, planID)
}
func (s *Service) GetChecklist(ctx context.Context, userID, planID string) ([]domain.ChecklistItem, error) {
	return s.repo.GetChecklist(ctx, userID, planID)
}
func (s *Service) SetChecklistItem(ctx context.Context, userID, itemID string, completed bool) (domain.ChecklistItem, error) {
	return s.repo.SetChecklistItem(ctx, userID, itemID, completed)
}
func (s *Service) AddFeedback(ctx context.Context, userID string, input domain.FeedbackInput) error {
	if input.PlanID == "" || len(input.Tags) == 0 {
		return fmt.Errorf("%w: feedback plan and tags are required", ErrValidation)
	}
	if len(input.Comment) > 500 {
		return fmt.Errorf("%w: comment is too long", ErrValidation)
	}
	return s.repo.AddFeedback(ctx, userID, input)
}

func (s *Service) RunTool(ctx context.Context, userID string, input domain.ToolInput) (domain.ToolResult, error) {
	validKinds := map[string]bool{"hair": true, "outfit": true, "purchase": true}
	if !validKinds[input.Kind] {
		return domain.ToolResult{}, fmt.Errorf("%w: 不支持的顾问工具", ErrValidation)
	}
	if input.Scene == "" {
		input.Scene = "daily"
	}
	validScenes := map[string]bool{"general": true, "daily": true, "interview": true, "wedding": true, "date": true}
	if !validScenes[input.Scene] {
		return domain.ToolResult{}, fmt.Errorf("%w: 不支持的使用场景", ErrValidation)
	}
	toolContext := &domain.ToolContext{}
	if input.ReportID != "" {
		if _, err := uuid.Parse(input.ReportID); err != nil {
			return domain.ToolResult{}, fmt.Errorf("%w: 无效的形象档案", ErrValidation)
		}
		report, err := s.repo.GetReport(ctx, userID, input.ReportID)
		if err != nil {
			return domain.ToolResult{}, err
		}
		toolContext.ImpressionTags, toolContext.PriorityTitle, toolContext.PriorityCopy = report.ImpressionTags, report.PriorityTitle, report.PriorityCopy
	}
	if input.Kind == "purchase" {
		items, err := s.repo.ListWardrobeItems(ctx, userID)
		if err != nil {
			return domain.ToolResult{}, err
		}
		for index, item := range items {
			if index == 20 {
				break
			}
			toolContext.Wardrobe = append(toolContext.Wardrobe, domain.ToolWardrobeItem{Name: item.Name, Category: item.Category, Color: item.Color, Season: item.Season, Formality: item.Formality, Scenes: item.Scenes})
		}
	}
	if input.ReportID != "" || len(toolContext.Wardrobe) > 0 {
		input.Context = toolContext
	}
	if input.Kind != "hair" && input.MediaID == "" {
		return domain.ToolResult{}, fmt.Errorf("%w: 请先上传需要判断的照片", ErrValidation)
	}
	if input.MediaID != "" {
		if _, err := uuid.Parse(input.MediaID); err != nil {
			return domain.ToolResult{}, fmt.Errorf("%w: 无效的照片", ErrValidation)
		}
		assets, err := s.repo.GetMediaAssetsForUser(ctx, userID, []string{input.MediaID})
		if err != nil {
			return domain.ToolResult{}, err
		}
		expectedKind := map[string]string{"outfit": "outfit", "purchase": "product"}[input.Kind]
		if expectedKind != "" && (len(assets) != 1 || assets[0].Kind != expectedKind) {
			return domain.ToolResult{}, fmt.Errorf("%w: 照片类型与诊断工具不匹配", ErrValidation)
		}
	}
	result := buildToolResult(input.Kind, input.Scene)
	var err error
	if input.Kind == "outfit" {
		result, err = s.outfitAdvisor.Diagnose(ctx, input)
		if err != nil {
			return domain.ToolResult{}, err
		}
	} else if input.Kind == "purchase" && s.purchaseAdvisor != nil {
		result, err = s.purchaseAdvisor.Diagnose(ctx, input)
		if err != nil {
			return domain.ToolResult{}, err
		}
	}
	result, err = s.repo.CreateToolResult(ctx, userID, input, result)
	if err != nil {
		return domain.ToolResult{}, err
	}
	for index := range result.Options {
		result.Options[index].ImageURL = s.absoluteURL(result.Options[index].ImageURL)
	}
	return result, nil
}

func (s *Service) SaveToolResult(ctx context.Context, userID, resultID string) (domain.ToolResult, error) {
	if _, err := uuid.Parse(resultID); err != nil {
		return domain.ToolResult{}, repository.ErrNotFound
	}
	result, err := s.repo.SaveToolResult(ctx, userID, resultID)
	if err != nil {
		return domain.ToolResult{}, err
	}
	for index := range result.Options {
		result.Options[index].ImageURL = s.absoluteURL(result.Options[index].ImageURL)
	}
	return result, nil
}

var hairStyleNames = map[string]string{"sharp": "锁骨层次发", "warm": "空气微卷", "natural": "自然偏分"}

func (s *Service) CreateHairPreview(ctx context.Context, userID string, input domain.HairPreviewInput) (domain.HairPreview, error) {
	if _, err := uuid.Parse(input.MediaID); err != nil {
		return domain.HairPreview{}, fmt.Errorf("%w: 请先上传一张清晰正脸照", ErrValidation)
	}
	styleName := hairStyleNames[input.StyleID]
	if styleName == "" {
		return domain.HairPreview{}, fmt.Errorf("%w: 请选择一个发型方向", ErrValidation)
	}
	if input.ReportID != "" {
		if _, err := uuid.Parse(input.ReportID); err != nil {
			return domain.HairPreview{}, fmt.Errorf("%w: 无效的形象档案", ErrValidation)
		}
	}
	if input.Scene == "" {
		input.Scene = "daily"
	}
	if !map[string]bool{"general": true, "daily": true, "interview": true, "wedding": true, "date": true}[input.Scene] {
		return domain.HairPreview{}, fmt.Errorf("%w: 不支持的使用场景", ErrValidation)
	}
	preview, err := s.repo.CreateHairPreview(ctx, userID, input, styleName)
	if err == nil {
		preview.SourceImageURL = s.absoluteURL(preview.SourceImageURL)
	}
	return preview, err
}

func (s *Service) GetHairPreview(ctx context.Context, userID, previewID string) (domain.HairPreview, error) {
	if _, err := uuid.Parse(previewID); err != nil {
		return domain.HairPreview{}, repository.ErrNotFound
	}
	preview, err := s.repo.GetHairPreview(ctx, userID, previewID)
	if err == nil {
		preview.SourceImageURL = s.absoluteURL(preview.SourceImageURL)
		if preview.ResultImageURL != "" {
			preview.ResultImageURL = s.absoluteURL(preview.ResultImageURL)
		}
	}
	return preview, err
}

func (s *Service) ListSavedHairPreviews(ctx context.Context, userID string) ([]domain.HairPreview, error) {
	items, err := s.repo.ListSavedHairPreviews(ctx, userID)
	for index := range items {
		items[index].SourceImageURL = s.absoluteURL(items[index].SourceImageURL)
		items[index].ResultImageURL = s.absoluteURL(items[index].ResultImageURL)
	}
	return items, err
}

func (s *Service) SaveHairPreview(ctx context.Context, userID, previewID string) (domain.HairPreview, error) {
	if _, err := uuid.Parse(previewID); err != nil {
		return domain.HairPreview{}, repository.ErrNotFound
	}
	preview, err := s.repo.SaveHairPreview(ctx, userID, previewID)
	if err == nil {
		preview.SourceImageURL = s.absoluteURL(preview.SourceImageURL)
		preview.ResultImageURL = s.absoluteURL(preview.ResultImageURL)
	}
	return preview, err
}

func buildToolResult(kind, scene string) domain.ToolResult {
	sceneNames := map[string]string{"general": "当前场景", "daily": "日常", "interview": "面试", "wedding": "婚礼", "date": "约会"}
	sceneName := sceneNames[scene]
	switch kind {
	case "hair":
		return domain.ToolResult{
			Kind: "hair", Scene: scene, Conclusion: "首选锁骨层次发",
			PriorityTitle: "提高发型重心，露出肩颈",
			PriorityCopy:  "比贴脸长直发更能突出眉眼与头肩比例，同时保留自然亲和感。",
			Tags:          []string{"重心提高", "肩颈更清晰", "容易打理"},
			Options: []domain.ToolOption{
				{ID: "sharp", Name: "锁骨层次发", ImageURL: "/assets/looks/sharp.png", Note: "首选推荐", Reason: "提高视觉重心并保留脸侧空气感。", Tags: []string{"重心提高", "肩颈清晰"}},
				{ID: "warm", Name: "空气微卷", ImageURL: "/assets/looks/warm.png", Note: "柔和表达", Reason: "发尾弧度保留亲和感，更适合沟通场景。", Tags: []string{"自然柔和", "上镜"}},
				{ID: "natural", Name: "自然偏分", ImageURL: "/assets/looks/natural.png", Note: "低维护", Reason: "只调整分缝与耳侧线条，日常最容易维持。", Tags: []string{"改动小", "低维护"}},
			},
		}
	case "outfit":
		return domain.ToolResult{
			Kind: "outfit", Scene: scene, Conclusion: "整体方向对了，先改一处",
			PriorityTitle: "把深色内搭换成象牙白",
			PriorityCopy:  fmt.Sprintf("不换整套衣服，就能让眉眼更清晰、上半身更轻盈，也更适合%s。", sceneName),
			Tags:          []string{"预计 3 分钟", "无需购买新衣", "变化明显"},
			Findings: []domain.ToolFinding{
				{Label: "上身配色偏沉", Category: "color", Tone: "improve"},
				{Label: "肩线不够清晰", Category: "silhouette", Tone: "improve"},
				{Label: "腰线可以上移", Category: "proportion", Tone: "optional"},
			},
		}
	default:
		return domain.ToolResult{
			Kind: "purchase", Scene: scene, Conclusion: "比较适合",
			PriorityTitle: fmt.Sprintf("适合%s，但建议搭配挺括下装", sceneName),
			PriorityCopy:  "清晰肩线有利于头肩比例，低饱和颜色也容易与现有衣橱组合。",
			Tags:          []string{"象牙白内搭", "深灰直筒裤", "黑色低跟鞋"},
			Findings: []domain.ToolFinding{
				{Label: "清晰肩线能改善头肩比例", Category: "silhouette", Tone: "positive"},
				{Label: "低饱和颜色容易组合", Category: "color", Tone: "positive"},
				{Label: "正式场合需要更挺括下装", Category: "fabric", Tone: "caution"},
			},
		}
	}
}

func (s *Service) DeleteUserData(ctx context.Context, userID string) error {
	keys, err := s.repo.DeleteUserData(ctx, userID)
	if err != nil {
		return err
	}
	for _, key := range keys {
		if err := s.storage.Delete(ctx, key); err != nil {
			s.logger.Warn("delete object", "error", err)
		}
	}
	return nil
}

func (s *Service) RunWorker(ctx context.Context, poll time.Duration) {
	go s.runHairPreviewWorker(ctx, poll)
	go s.runPlanLookWorker(ctx, poll)
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	s.logger.Info("analysis worker started", "poll", poll)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			job, ok, err := s.repo.ClaimAnalysisJob(ctx)
			if err != nil {
				s.logger.Error("claim analysis job", "error", err)
				continue
			}
			if !ok {
				continue
			}
			s.processJob(ctx, job)
		}
	}
}

func (s *Service) runHairPreviewWorker(ctx context.Context, poll time.Duration) {
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	s.logger.Info("hair preview worker started", "poll", poll)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			job, ok, err := s.repo.ClaimHairPreview(ctx)
			if err != nil {
				s.logger.Error("claim hair preview", "error", err)
				continue
			}
			if ok {
				s.processHairPreview(ctx, job)
			}
		}
	}
}

func (s *Service) runPlanLookWorker(ctx context.Context, poll time.Duration) {
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	s.logger.Info("plan look worker started", "poll", poll)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			job, ok, err := s.repo.ClaimPlanLook(ctx)
			if err != nil {
				s.logger.Error("claim plan look", "error", err)
				continue
			}
			if ok {
				s.processPlanLook(ctx, job)
			}
		}
	}
}

func (s *Service) processPlanLook(ctx context.Context, job domain.PlanLookJob) {
	jobCtx, cancel := context.WithTimeout(ctx, planLookJobTimeout)
	defer cancel()
	if s.lookGenerator == nil {
		_ = s.repo.FailPlanLook(jobCtx, job, errors.New("plan look generator is not configured"))
		return
	}
	output, err := s.lookGenerator.Generate(jobCtx, provider.LookInput{Name: job.Name, Slug: job.Slug, Why: job.Why, Steps: job.Steps, MediaIDs: job.MediaIDs})
	if err != nil {
		_ = s.repo.FailPlanLook(jobCtx, job, err)
		return
	}
	extension := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp"}[output.MIMEType]
	if extension == "" {
		_ = s.repo.FailPlanLook(jobCtx, job, errors.New("look provider returned an unsupported image format"))
		return
	}
	storageKey := fmt.Sprintf("%s/generated/looks/%s%s", job.UserID, uuid.NewString(), extension)
	storedKey, saveErr := s.storage.Save(jobCtx, storageKey, bytes.NewReader(output.ImageData))
	if saveErr != nil {
		_ = s.repo.FailPlanLook(jobCtx, job, saveErr)
		return
	}
	if err := s.repo.CompletePlanLook(jobCtx, job, "/uploads/"+storedKey, storedKey, output.ProviderVersion); err != nil {
		_ = s.storage.Delete(jobCtx, storedKey)
		s.logger.Error("complete plan look", "plan_id", job.PlanID, "error", err)
		_ = s.repo.FailPlanLook(jobCtx, job, err)
	}
}

func (s *Service) processHairPreview(ctx context.Context, job domain.HairPreviewJob) {
	jobCtx, cancel := context.WithTimeout(ctx, hairPreviewJobTimeout)
	defer cancel()
	output, err := s.hairGenerator.Generate(jobCtx, job.Input)
	if err != nil {
		_ = s.repo.FailHairPreview(jobCtx, job, err)
		return
	}
	resultURL, storageKey := output.ImageURL, ""
	if len(output.ImageData) > 0 {
		extension := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp"}[output.MIMEType]
		if extension == "" {
			_ = s.repo.FailHairPreview(jobCtx, job, errors.New("hair preview provider returned an unsupported image format"))
			return
		}
		storageKey = fmt.Sprintf("%s/generated/hair/%s%s", job.UserID, uuid.NewString(), extension)
		storedKey, saveErr := s.storage.Save(jobCtx, storageKey, bytes.NewReader(output.ImageData))
		if saveErr != nil {
			_ = s.repo.FailHairPreview(jobCtx, job, saveErr)
			return
		}
		storageKey = storedKey
		resultURL = "/uploads/" + storedKey
	}
	if resultURL == "" {
		_ = s.repo.FailHairPreview(jobCtx, job, errors.New("hair preview provider returned no image"))
		return
	}
	if err := s.repo.CompleteHairPreview(jobCtx, job, resultURL, storageKey, output.ProviderVersion); err != nil {
		if storageKey != "" {
			_ = s.storage.Delete(jobCtx, storageKey)
		}
		s.logger.Error("complete hair preview", "preview_id", job.PreviewID, "error", err)
		_ = s.repo.FailHairPreview(jobCtx, job, err)
	}
}

const (
	analysisJobTimeout   = 5 * time.Minute
	hairPreviewJobTimeout = 5 * time.Minute
	planLookJobTimeout   = 5 * time.Minute
)

func (s *Service) processJob(ctx context.Context, job domain.AnalysisJob) {
	jobCtx, cancel := context.WithTimeout(ctx, analysisJobTimeout)
	defer cancel()
	jobCtx = provider.WithProgressReporter(jobCtx, func(progress int, stage string) {
		_ = s.repo.UpdateAnalysisProgress(jobCtx, job.AnalysisID, progress, stage)
	})
	output, err := s.analyzer.Analyze(jobCtx, job.Input)
	if err != nil {
		var rejected *provider.PhotoRejectedError
		if errors.As(err, &rejected) {
			if failErr := s.repo.RejectAnalysis(jobCtx, job, rejected.UserMessage()); failErr != nil {
				s.logger.Error("reject analysis", "analysis_id", job.AnalysisID, "error", failErr)
			}
			return
		}
		_ = s.repo.FailAnalysis(jobCtx, job, err)
		return
	}
	_ = s.repo.UpdateAnalysisProgress(jobCtx, job.AnalysisID, 82, "正在组合发型、妆容与穿搭方案")
	currentImageURL, err := s.analysisPreviewURL(jobCtx, job.UserID, job.Input.MediaIDs)
	if err != nil {
		_ = s.repo.FailAnalysis(jobCtx, job, err)
		return
	}
	// The "current" image is source evidence, never a provider-generated or
	// stock reference. This keeps every provider and fallback on the same
	// user-photo contract.
	output.CurrentImageURL = currentImageURL
	_ = s.repo.UpdateAnalysisProgress(jobCtx, job.AnalysisID, 95, "正在保存形象档案")
	if _, err := s.repo.CompleteAnalysis(jobCtx, job, output); err != nil {
		if errors.Is(err, repository.ErrAnalysisRemoved) {
			s.logger.Info("analysis removed while processing; discarding result", "analysis_id", job.AnalysisID)
			return
		}
		s.logger.Error("complete analysis", "analysis_id", job.AnalysisID, "error", err)
		_ = s.repo.FailAnalysis(jobCtx, job, err)
	}
}

// resolveAssetURL expands a stored asset reference to a currently loadable
// URL. Reports written before previews were persisted in relative form still
// carry the store's own signed URL, whose TTL may already have expired; when
// the store recognizes the URL as one of its own, re-sign it instead of
// returning the stale value verbatim.
func (s *Service) resolveAssetURL(value string) string {
	if strings.Contains(value, "://") {
		if refresher, ok := s.storage.(storage.StoredURLRefresher); ok {
			if refreshed, hit := refresher.RefreshURL(value, s.assetURLTTL); hit {
				return refreshed
			}
		}
		return value
	}
	return s.absoluteURL(value)
}

func (s *Service) absoluteURL(value string) string {
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return value
	}
	// Bundled mini-program assets must stay relative. Turning them into a local
	// HTTP URL makes WeChat 3.17+ reject the image even in development tools.
	if strings.HasPrefix(value, "/assets/") {
		return value
	}
	if strings.HasPrefix(value, "/uploads/") {
		if signer, ok := s.storage.(storage.SignedURLStorage); ok {
			key := strings.TrimPrefix(value, "/uploads/")
			if signed, err := signer.SignedURL(context.Background(), key, s.assetURLTTL); err == nil {
				return signed
			} else if s.logger != nil {
				s.logger.Error("sign asset URL", "error", err)
			}
		}
	}
	return s.publicBaseURL + "/" + strings.TrimPrefix(value, "/")
}
