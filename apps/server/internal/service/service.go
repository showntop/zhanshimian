package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/provider"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/storage"
)

var ErrValidation = errors.New("validation error")
var ErrForbidden = errors.New("forbidden")
var ErrRateLimited = errors.New("rate limited")

type Service struct {
	repo                   repository.Repository
	storage                storage.ObjectStorage
	analyzer               provider.Analyzer
	hairGenerator          provider.HairPreviewGenerator
	lookGenerator          provider.LookGenerator
	outfitAdvisor          provider.OutfitAdvisor
	purchaseAdvisor        provider.OutfitAdvisor
	advisorChat            provider.AdvisorChat
	todayPlanner           provider.TodayPlanner
	weather                provider.WeatherProvider
	wechat                 provider.WeChatAuthenticator
	wechatApp              provider.WeChatAuthenticator
	apple                  provider.AppleAuthenticator
	sms                    provider.SmsSender
	smsRatePerPhonePerHour int64
	ipLimiter              *slidingWindowLimiter
	handlers               map[domain.TaskType]TaskHandler
	publicBaseURL          string
	assetURLTTL            time.Duration
	sessionTTL             time.Duration
	maxUpload              int64
	logger                 *slog.Logger
}

type ProviderOptions struct {
	Hair        provider.HairPreviewGenerator
	Look        provider.LookGenerator
	Outfit      provider.OutfitAdvisor
	Purchase    provider.OutfitAdvisor
	Advisor     provider.AdvisorChat
	Today       provider.TodayPlanner
	Weather     provider.WeatherProvider
	WeChat      provider.WeChatAuthenticator
	WeChatApp   provider.WeChatAuthenticator
	Apple       provider.AppleAuthenticator
	Sms         provider.SmsSender
	SmsPerPhone int64
	AssetURLTTL time.Duration
}

func New(repo repository.Repository, objects storage.ObjectStorage, analyzer provider.Analyzer, publicBaseURL string, sessionTTL time.Duration, maxUpload int64, logger *slog.Logger, options ...ProviderOptions) *Service {
	hairGenerator := provider.HairPreviewGenerator(provider.NewDemoHairGenerator())
	outfitAdvisor := provider.OutfitAdvisor(provider.NewDemoOutfitAdvisor())
	var purchaseAdvisor provider.OutfitAdvisor
	var advisorChat provider.AdvisorChat
	todayPlanner := provider.TodayPlanner(provider.NewDemoTodayPlanner())
	weather := provider.WeatherProvider(provider.NewDemoWeatherProvider())
	var wechat provider.WeChatAuthenticator
	var wechatApp provider.WeChatAuthenticator
	var apple provider.AppleAuthenticator
	var smsSender provider.SmsSender = provider.NewConsoleSms(logger, true)
	smsPerPhone := int64(5)
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
		if options[0].Today != nil {
			todayPlanner = options[0].Today
		}
		if options[0].Weather != nil {
			weather = options[0].Weather
		}
		wechat = options[0].WeChat
		wechatApp = options[0].WeChatApp
		apple = options[0].Apple
		if options[0].Sms != nil {
			smsSender = options[0].Sms
		}
		if options[0].SmsPerPhone > 0 {
			smsPerPhone = options[0].SmsPerPhone
		}
		lookGenerator = options[0].Look
		if options[0].AssetURLTTL > 0 {
			assetURLTTL = options[0].AssetURLTTL
		}
	}
	service := &Service{
		repo: repo, storage: objects, analyzer: analyzer, hairGenerator: hairGenerator,
		lookGenerator: lookGenerator, outfitAdvisor: outfitAdvisor, purchaseAdvisor: purchaseAdvisor,
		advisorChat: advisorChat, todayPlanner: todayPlanner, weather: weather,
		wechat: wechat, wechatApp: wechatApp, apple: apple, sms: smsSender,
		smsRatePerPhonePerHour: smsPerPhone,
		ipLimiter:              newSlidingWindowLimiter(10, time.Minute),
		publicBaseURL:          strings.TrimSuffix(publicBaseURL, "/"),
		assetURLTTL:            assetURLTTL, sessionTTL: sessionTTL, maxUpload: maxUpload, logger: logger,
	}
	service.handlers = map[domain.TaskType]TaskHandler{
		domain.TaskTypeAnalysis:    analysisTaskHandler{service},
		domain.TaskTypeHairPreview: hairPreviewTaskHandler{service},
		domain.TaskTypePlanLook:    planLookTaskHandler{service},
		domain.TaskTypeTodayLook:   todayLookTaskHandler{service},
	}
	return service
}

// ---- 登录与身份 ----

func defaultNickname(nickname string) string {
	if strings.TrimSpace(nickname) == "" {
		return "UP一下用户"
	}
	return strings.TrimSpace(nickname)
}

func (s *Service) DevLogin(ctx context.Context, nickname string) (domain.Session, error) {
	user, err := s.repo.CreateDevUser(ctx, defaultNickname(nickname))
	if err != nil {
		return domain.Session{}, err
	}
	return s.createSession(ctx, user)
}

func (s *Service) WeChatLogin(ctx context.Context, code, nickname string) (domain.Session, error) {
	if s.wechat == nil {
		return domain.Session{}, provider.ErrWeChatUnavailable
	}
	identity, err := s.wechat.ExchangeCode(ctx, code)
	if err != nil {
		return domain.Session{}, err
	}
	return s.loginWithIdentity(ctx, domain.ProviderWeChatMiniApp, identity.OpenID, nickname)
}

func (s *Service) WeChatAppLogin(ctx context.Context, code, nickname string) (domain.Session, error) {
	if s.wechatApp == nil {
		return domain.Session{}, provider.ErrWeChatUnavailable
	}
	identity, err := s.wechatApp.ExchangeCode(ctx, code)
	if err != nil {
		return domain.Session{}, err
	}
	identifier := identity.UnionID
	if identifier == "" {
		identifier = identity.OpenID
	}
	return s.loginWithIdentity(ctx, domain.ProviderWeChatApp, identifier, nickname)
}

func (s *Service) AppleLogin(ctx context.Context, identityToken, nickname string) (domain.Session, error) {
	if s.apple == nil {
		return domain.Session{}, provider.ErrAppleUnavailable
	}
	sub, err := s.apple.VerifyToken(ctx, identityToken)
	if err != nil {
		return domain.Session{}, err
	}
	return s.loginWithIdentity(ctx, domain.ProviderApple, sub, nickname)
}

// loginWithIdentity is the shared multi-channel path: one (provider,
// identifier) pair always maps back to the same account, so a user who logs
// in on the phone today and the mini-program tomorrow keeps one dataset.
func (s *Service) loginWithIdentity(ctx context.Context, providerName, identifier, nickname string) (domain.Session, error) {
	user, err := s.repo.EnsureUserByIdentity(ctx, providerName, identifier, defaultNickname(nickname))
	if err != nil {
		return domain.Session{}, err
	}
	return s.createSession(ctx, user)
}

func (s *Service) createSession(ctx context.Context, user domain.User) (domain.Session, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return domain.Session{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	digest := sha256.Sum256([]byte(token))
	expiresAt := time.Now().Add(s.sessionTTL)
	if err := s.repo.CreateSession(ctx, user.ID, digest[:], expiresAt); err != nil {
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

func (s *Service) Logout(ctx context.Context, token string) error {
	digest := sha256.Sum256([]byte(token))
	return s.repo.DeleteSessionByTokenDigest(ctx, digest[:])
}

func (s *Service) GetAccount(ctx context.Context, user domain.User) (domain.MeAccount, error) {
	identities, err := s.repo.ListIdentities(ctx, user.ID)
	if err != nil {
		return domain.MeAccount{}, err
	}
	return domain.MeAccount{ID: user.ID, Nickname: user.Nickname, Identities: identities}, nil
}

// ---- 补充资料 ----

// GetProfile returns the persisted 补充资料; callers translate a missing row
// into a null payload (clients show the profile onboarding).
func (s *Service) GetProfile(ctx context.Context, userID string) (domain.UserProfile, error) {
	return s.repo.GetUserProfile(ctx, userID)
}

func (s *Service) UpdateProfile(ctx context.Context, userID string, profile domain.UserProfile) (domain.UserProfile, error) {
	if profile.HeightCM < 100 || profile.HeightCM > 250 {
		return domain.UserProfile{}, fmt.Errorf("%w: 身高需在 100–250 cm 之间", ErrValidation)
	}
	if strings.TrimSpace(profile.Role) == "" || strings.TrimSpace(profile.Budget) == "" {
		return domain.UserProfile{}, fmt.Errorf("%w: 请填写职业与预算", ErrValidation)
	}
	if len([]rune(profile.Role)) > 60 || len([]rune(profile.Budget)) > 60 {
		return domain.UserProfile{}, fmt.Errorf("%w: 职业与预算最多 60 字", ErrValidation)
	}
	for _, check := range []struct {
		name  string
		value *float64
		min   float64
		max   float64
	}{
		{"体重", profile.WeightKG, 25, 300},
		{"胸围", profile.BustCM, 40, 200},
		{"腰围", profile.WaistCM, 40, 200},
		{"臀围", profile.HipCM, 40, 200},
	} {
		if check.value != nil && (*check.value < check.min || *check.value > check.max) {
			return domain.UserProfile{}, fmt.Errorf("%w: %s需在 %.0f–%.0f 之间", ErrValidation, check.name, check.min, check.max)
		}
	}
	return s.repo.SaveUserProfile(ctx, userID, profile)
}

// ---- 短信验证码 ----

// normalizePhone accepts the common Chinese formats (1xxxxxxxxxx, +86 prefix,
// spaces/dashes) and returns the bare 11-digit number.
func normalizePhone(input string) (string, error) {
	value := strings.TrimSpace(input)
	value = strings.ReplaceAll(value, " ", "")
	value = strings.ReplaceAll(value, "-", "")
	value = strings.TrimPrefix(value, "+86")
	if len(value) != 11 || value[0] != '1' {
		return "", fmt.Errorf("%w: 请输入正确的手机号", ErrValidation)
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return "", fmt.Errorf("%w: 请输入正确的手机号", ErrValidation)
		}
	}
	return value, nil
}

func smsCodeDigest(phone, code string) []byte {
	digest := sha256.Sum256([]byte(phone + ":" + code))
	return digest[:]
}

const (
	smsCodeCooldown    = 60 * time.Second
	smsCodeTTL         = 5 * time.Minute
	smsCodeHourlyLimit = 5
)

// RequestSmsCode issues one verification code. Limits: 60s cooldown while the
// previous code is still unused, 10/min/IP, 5/hour/phone. The cooldown only
// applies to unused codes: after a successful verify the user may immediately
// request a fresh code (e.g. logging in again on another device).
func (s *Service) RequestSmsCode(ctx context.Context, rawPhone, clientIP string) (devCode string, err error) {
	phone, err := normalizePhone(rawPhone)
	if err != nil {
		return "", err
	}
	if !s.ipLimiter.Allow(clientIP, time.Now()) {
		return "", fmt.Errorf("%w: 验证码请求过于频繁，请稍后再试", ErrRateLimited)
	}
	if s.sms == nil {
		return "", provider.ErrSmsConfig
	}
	latest, latestErr := s.repo.LatestSmsCode(ctx, phone)
	if latestErr != nil && !errors.Is(latestErr, repository.ErrNotFound) {
		return "", latestErr
	}
	if latestErr == nil && latest.UsedAt == nil && time.Since(latest.CreatedAt) < smsCodeCooldown {
		return "", fmt.Errorf("%w: 验证码已发送，请 1 分钟后再试", ErrRateLimited)
	}
	if count, countErr := s.repo.CountSmsCodesSince(ctx, phone, time.Now().Add(-time.Hour)); countErr == nil && count >= s.maxSmsPerPhoneHour() {
		return "", fmt.Errorf("%w: 该手机号今日验证码请求次数过多，请 1 小时后再试", ErrRateLimited)
	}
	code := provider.DevSmsCode
	if !s.sms.ReturnsCode() {
		if code, err = randomSmsCode(); err != nil {
			return "", err
		}
	}
	if err := s.sms.Send(ctx, phone, code); err != nil {
		return "", err
	}
	if err := s.repo.CreateSmsCode(ctx, phone, smsCodeDigest(phone, code), time.Now().Add(smsCodeTTL)); err != nil {
		return "", err
	}
	if s.sms.ReturnsCode() {
		return code, nil
	}
	return "", nil
}

func (s *Service) maxSmsPerPhoneHour() int64 {
	limit := s.smsRatePerPhonePerHour
	if limit <= 0 {
		limit = smsCodeHourlyLimit
	}
	return limit
}

func randomSmsCode() (string, error) {
	buffer := make([]byte, 4)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	value := uint32(buffer[0])<<24 | uint32(buffer[1])<<16 | uint32(buffer[2])<<8 | uint32(buffer[3])
	return fmt.Sprintf("%06d", value%1000000), nil
}

// VerifySmsCode checks the code against the newest row for that phone and
// burns it exactly once, then returns a session for the phone identity.
func (s *Service) VerifySmsCode(ctx context.Context, rawPhone, code, nickname string) (domain.Session, error) {
	phone, err := normalizePhone(rawPhone)
	if err != nil {
		return domain.Session{}, err
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return domain.Session{}, fmt.Errorf("%w: 请输入验证码", ErrValidation)
	}
	latest, err := s.repo.LatestSmsCode(ctx, phone)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return domain.Session{}, fmt.Errorf("%w: 请先获取验证码", ErrValidation)
		}
		return domain.Session{}, err
	}
	if time.Now().After(latest.ExpiresAt) {
		return domain.Session{}, fmt.Errorf("%w: 验证码已过期，请重新获取", ErrValidation)
	}
	expected := smsCodeDigest(phone, code)
	if subtle.ConstantTimeCompare(expected, latest.Digest) != 1 {
		return domain.Session{}, fmt.Errorf("%w: 验证码不正确", ErrValidation)
	}
	if err := s.repo.ConsumeSmsCode(ctx, latest.ID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return domain.Session{}, fmt.Errorf("%w: 验证码已使用，请重新获取", ErrValidation)
		}
		return domain.Session{}, err
	}
	return s.loginWithIdentity(ctx, domain.ProviderPhone, phone, nickname)
}

// slidingWindowLimiter is a tiny in-memory limiter for per-IP request rates.
// The API runs single-instance; phone-level limits live in the database.
type slidingWindowLimiter struct {
	mu     sync.Mutex
	events map[string][]time.Time
	limit  int
	window time.Duration
}

func newSlidingWindowLimiter(limit int, window time.Duration) *slidingWindowLimiter {
	return &slidingWindowLimiter{events: map[string][]time.Time{}, limit: limit, window: window}
}

func (l *slidingWindowLimiter) Allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	events := l.events[key]
	kept := events[:0]
	for _, event := range events {
		if now.Sub(event) < l.window {
			kept = append(kept, event)
		}
	}
	if len(kept) >= l.limit {
		l.events[key] = kept
		return false
	}
	l.events[key] = append(kept, now)
	if len(l.events) > 4096 {
		for eventKey, eventList := range l.events {
			if len(eventList) == 0 || now.Sub(eventList[len(eventList)-1]) >= l.window {
				delete(l.events, eventKey)
			}
		}
	}
	return true
}

// ---- 媒体 ----

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
	asset.Demo = true
	asset.URL = s.demoMediaURL(kind)
	return asset, nil
}

// ---- 分析与报告 ----

func (s *Service) CreateAnalysis(ctx context.Context, userID string, input domain.CreateAnalysisInput) (domain.Analysis, *domain.Task, error) {
	if len(input.MediaIDs) != 3 {
		return domain.Analysis{}, nil, fmt.Errorf("%w: 请上传正脸、侧脸和全身三张照片", ErrValidation)
	}
	seen := map[string]bool{}
	for _, id := range input.MediaIDs {
		if _, err := uuid.Parse(id); err != nil || seen[id] {
			return domain.Analysis{}, nil, fmt.Errorf("%w: invalid media ids", ErrValidation)
		}
		seen[id] = true
	}
	if input.Scene == "" {
		input.Scene = "general"
	}
	// Grounding: the request profile wins; an empty one falls back to the
	// persisted 补充资料 so users never re-type what they already saved.
	if input.Profile == (domain.Profile{}) {
		if profile, err := s.repo.GetUserProfile(ctx, userID); err == nil {
			input.Profile = domain.Profile{HeightCM: profile.HeightCM, Role: profile.Role, Budget: profile.Budget}
		}
	}
	analysis, task, err := s.repo.CreateAnalysis(ctx, userID, input)
	if err != nil {
		return domain.Analysis{}, nil, err
	}
	// 幂等返回的旧分析已经带原始照片；只有真正新建的分析才回填本次照片，
	// 避免响应里挂上和实际分析输入不符的 media ids。
	if len(analysis.MediaIDs) == 0 {
		analysis.MediaIDs = input.MediaIDs
	}
	hydrated, err := s.hydrateAnalysisMedia(ctx, userID, analysis)
	return hydrated, task, err
}

func (s *Service) GetAnalysis(ctx context.Context, userID, id string) (domain.Analysis, error) {
	analysis, err := s.repo.GetAnalysis(ctx, userID, id)
	if err != nil {
		return domain.Analysis{}, err
	}
	// 读路径兜底：所有自愈机制（僵尸回收、重试上限、failContext）都活在
	// worker 进程里；worker 整体死亡时 processing/queued 行永远无人触碰，
	// 客户端会无限轮询且拿不到任何报错。worker 活着时每次认领/重排/进度
	// 上报都会刷新 updated_at（任务最长 5 分钟 × 3 次重试也在刷新），
	// 因此长时间零更新只可能是孤儿行：在读路径直接落失败终态。
	staleAfter := time.Duration(0)
	switch analysis.Status {
	case "processing":
		staleAfter = staleAnalysisProcessingTimeout
	case "queued":
		staleAfter = staleAnalysisQueuedTimeout
	}
	if staleAfter > 0 && time.Since(analysis.UpdatedAt) > staleAfter {
		message := "分析时间过长，请重新发起"
		failCtx, cancel := failContext()
		defer cancel()
		if failErr := s.repo.FailAnalysisPresentation(failCtx, id, "分析未完成", message); failErr != nil {
			s.loggerOrDefault().Error("fail stale analysis", "analysis_id", id, "error", failErr)
		} else {
			s.loggerOrDefault().Warn("failed stale orphan analysis", "analysis_id", id, "status", analysis.Status, "idle", time.Since(analysis.UpdatedAt).String())
			analysis.Status = "failed"
			analysis.Stage = "分析未完成"
			analysis.ErrorMessage = message
		}
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
		assets[index].Demo = strings.HasPrefix(assets[index].StorageKey, "demo/")
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

// GetCurrentReport returns the user's most recent report. Clients key their
// local cache by report ID, so a wiped cache (reinstalled mini-program) needs
// this to rediscover the profile that still lives on the server.
func (s *Service) GetCurrentReport(ctx context.Context, userID string) (domain.Report, error) {
	report, err := s.repo.LatestReport(ctx, userID)
	if err == nil {
		report.CurrentImageURL = s.resolveAssetURL(report.CurrentImageURL)
	}
	return report, err
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
