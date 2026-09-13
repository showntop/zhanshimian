package account

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/provider/identity"
	"github.com/zhanshimian/server/internal/repository"
)

// AvatarURLResolver 把头像 object key 解析为可加载 URL（legacy resolveAssetURL 的窄面）。
type AvatarURLResolver interface {
	ResolveAssetURL(objectKey string) string
}

// BillingSummaryProvider 供 MeAccount 附带权益摘要；可选（缺省不带）。
type BillingSummaryProvider interface {
	BillingSummary(ctx context.Context, userID string) (domain.BillingSummary, error)
}

// DefaultNickname 兜底称呼；与 legacy 语义一致。
func DefaultNickname(nickname string) string {
	if strings.TrimSpace(nickname) == "" {
		return "uplook用户"
	}
	return strings.TrimSpace(nickname)
}

// Config 是构造参数：TTL、限额与可选依赖。
type Config struct {
	SessionTTL             time.Duration
	SmsRatePerPhonePerHour int64
}

type Service struct {
	identities IdentityRepository
	sessions   SessionRepository
	me         MeRepository
	sms        SmsRepository
	eraser     DataEraser
	wechat     WeChatAuthenticator
	wechatApp  WeChatAuthenticator
	apple      AppleAuthenticator
	smsSender  SmsSender
	avatars    AvatarURLResolver
	billing    BillingSummaryProvider
	cfg        Config
	ipLimiter  *slidingWindowLimiter
}

// New 组装账户服务。authenticators 可为 nil（开发登录 / 对应渠道停用）。
func New(identities IdentityRepository, sessions SessionRepository, me MeRepository,
	smsRepo SmsRepository, eraser DataEraser,
	wechat WeChatAuthenticator, wechatApp WeChatAuthenticator, apple AppleAuthenticator,
	smsSender SmsSender, avatars AvatarURLResolver, billing BillingSummaryProvider, cfg Config,
) *Service {
	return &Service{
		identities: identities, sessions: sessions, me: me, sms: smsRepo, eraser: eraser,
		wechat: wechat, wechatApp: wechatApp, apple: apple, smsSender: smsSender,
		avatars: avatars, billing: billing, cfg: cfg,
		ipLimiter: newIPLimiter(10, time.Minute),
	}
}

// ---- 登录 ----

func (s *Service) DevLogin(ctx context.Context, nickname string) (domain.Session, error) {
	user, err := s.identities.CreateDevUser(ctx, DefaultNickname(nickname))
	if err != nil {
		return domain.Session{}, err
	}
	return s.createSession(ctx, user)
}

func (s *Service) WeChatLogin(ctx context.Context, code, nickname string) (domain.Session, error) {
	if s.wechat == nil {
		return domain.Session{}, identity.ErrWeChatUnavailable
	}
	identityResult, err := s.wechat.ExchangeCode(ctx, code)
	if err != nil {
		return domain.Session{}, err
	}
	return s.loginWithIdentity(ctx, domain.ProviderWeChatMiniApp, identityResult.OpenID, nickname)
}

func (s *Service) WeChatAppLogin(ctx context.Context, code, nickname string) (domain.Session, error) {
	if s.wechatApp == nil {
		return domain.Session{}, identity.ErrWeChatUnavailable
	}
	identityResult, err := s.wechatApp.ExchangeCode(ctx, code)
	if err != nil {
		return domain.Session{}, err
	}
	identifier := identityResult.UnionID
	if identifier == "" {
		identifier = identityResult.OpenID
	}
	return s.loginWithIdentity(ctx, domain.ProviderWeChatApp, identifier, nickname)
}

func (s *Service) AppleLogin(ctx context.Context, identityToken, nickname string) (domain.Session, error) {
	if s.apple == nil {
		return domain.Session{}, identity.ErrAppleUnavailable
	}
	sub, err := s.apple.VerifyToken(ctx, identityToken)
	if err != nil {
		return domain.Session{}, err
	}
	return s.loginWithIdentity(ctx, domain.ProviderApple, sub, nickname)
}

// loginWithIdentity 是共享的多渠道登录路径：一个 (provider, identifier) 对
// 永远映射回同一个账号，换设备登录不产生第二份数据。
func (s *Service) loginWithIdentity(ctx context.Context, providerName, identifier, nickname string) (domain.Session, error) {
	user, err := s.identities.EnsureUserByIdentity(ctx, providerName, identifier, DefaultNickname(nickname))
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
	expiresAt := time.Now().Add(s.cfg.SessionTTL)
	if err := s.sessions.CreateSession(ctx, user.ID, digest[:], expiresAt); err != nil {
		return domain.Session{}, err
	}
	return domain.Session{UserID: user.ID, Token: token, TokenDigest: digest[:], ExpiresAt: expiresAt}, nil
}

// Authenticate 按 bearer token 找回用户；未知 token 一律 ErrForbidden。
func (s *Service) Authenticate(ctx context.Context, token string) (domain.User, error) {
	if token == "" {
		return domain.User{}, ErrForbidden
	}
	digest := sha256.Sum256([]byte(token))
	user, err := s.sessions.UserByTokenDigest(ctx, digest[:])
	if errors.Is(err, repository.ErrNotFound) {
		return domain.User{}, ErrForbidden
	}
	return user, err
}

func (s *Service) Logout(ctx context.Context, token string) error {
	digest := sha256.Sum256([]byte(token))
	return s.sessions.DeleteSessionByTokenDigest(ctx, digest[:])
}

// ---- 账号 ----

func (s *Service) GetAccount(ctx context.Context, user domain.User) (domain.MeAccount, error) {
	identities, err := s.identities.ListIdentities(ctx, user.ID)
	if err != nil {
		return domain.MeAccount{}, err
	}
	account := domain.MeAccount{ID: user.ID, Nickname: user.Nickname, Identities: identities}
	if avatar, err := s.me.GetUserAvatar(ctx, user.ID); err == nil {
		account.AvatarURL = s.avatars.ResolveAssetURL(avatarObjectKey(avatar))
	} else if !errors.Is(err, repository.ErrNotFound) {
		return domain.MeAccount{}, err
	}
	if s.billing != nil {
		if summary, err := s.billing.BillingSummary(ctx, user.ID); err == nil {
			account.Billing = &summary
		}
	}
	return account, nil
}

func (s *Service) UpdateAccount(ctx context.Context, user domain.User, nickname, avatarMediaID string) (domain.MeAccount, error) {
	nickname = strings.TrimSpace(nickname)
	avatarMediaID = strings.TrimSpace(avatarMediaID)
	if nickname == "" && avatarMediaID == "" {
		return domain.MeAccount{}, fmt.Errorf("%w: 请填写要修改的内容", ErrValidation)
	}
	if nickname != "" {
		runes := []rune(nickname)
		if len(runes) < 1 || len(runes) > 20 {
			return domain.MeAccount{}, fmt.Errorf("%w: 称呼需在 1–20 字之间", ErrValidation)
		}
		if err := s.me.UpdateUserNickname(ctx, user.ID, nickname); err != nil {
			return domain.MeAccount{}, err
		}
		user.Nickname = nickname
	}
	if avatarMediaID != "" {
		if err := s.me.UpdateUserAvatar(ctx, user.ID, avatarMediaID); err != nil {
			return domain.MeAccount{}, err
		}
	}
	return s.GetAccount(ctx, user)
}

// ---- 补充资料 ----

// GetProfile 返回持久化的补充资料；缺行由调用方翻成 null（客户端进 onboarding）。
func (s *Service) GetProfile(ctx context.Context, userID string) (domain.UserProfile, error) {
	return s.me.GetUserProfile(ctx, userID)
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
	return s.me.SaveUserProfile(ctx, userID, profile)
}

// ---- 删除我的数据 ----

// DeleteUserData 删除行级数据并回收对象；删除失败只告警不阻塞（与 legacy 一致）。
func (s *Service) DeleteUserData(ctx context.Context, userID string, deleteObject func(key string) error) error {
	keys, err := s.eraser.DeleteUserData(ctx, userID)
	if err != nil {
		return err
	}
	for _, key := range keys {
		if deleteObject != nil {
			if err := deleteObject(key); err != nil {
				continue // 对象回收失败不阻塞账号删除；GC 队列兜底
			}
		}
	}
	return nil
}

// ---- 短信验证码 ----

const (
	smsCodeCooldown    = 60 * time.Second
	smsCodeTTL         = 5 * time.Minute
	smsCodeHourlyLimit = 5
)

// RequestSmsCode 发一条验证码。限额：未用码 60s 冷却、10/min/IP、5/hour/手机号。
// 冷却只针对未使用的码：验证成功后可立即再要新码（换设备再登录）。
func (s *Service) RequestSmsCode(ctx context.Context, rawPhone, clientIP string) (devCode string, err error) {
	phone, err := normalizePhone(rawPhone)
	if err != nil {
		return "", err
	}
	if !s.ipLimiter.Allow(clientIP, time.Now()) {
		return "", fmt.Errorf("%w: 验证码请求过于频繁，请稍后再试", ErrRateLimited)
	}
	if s.smsSender == nil {
		return "", identity.ErrSmsConfig
	}
	latest, latestErr := s.sms.LatestSmsCode(ctx, phone)
	if latestErr != nil && !errors.Is(latestErr, repository.ErrNotFound) {
		return "", latestErr
	}
	if latestErr == nil && latest.UsedAt == nil && time.Since(latest.CreatedAt) < smsCodeCooldown {
		return "", fmt.Errorf("%w: 验证码已发送，请 1 分钟后再试", ErrRateLimited)
	}
	if count, countErr := s.sms.CountSmsCodesSince(ctx, phone, time.Now().Add(-time.Hour)); countErr == nil && count >= s.maxSmsPerPhoneHour() {
		return "", fmt.Errorf("%w: 该手机号今日验证码请求次数过多，请 1 小时后再试", ErrRateLimited)
	}
	code := identity.DevSmsCode
	if !s.smsSender.ReturnsCode() {
		if code, err = randomSmsCode(); err != nil {
			return "", err
		}
	}
	if err := s.smsSender.Send(ctx, phone, code); err != nil {
		return "", err
	}
	if err := s.sms.CreateSmsCode(ctx, phone, smsCodeDigest(phone, code), time.Now().Add(smsCodeTTL)); err != nil {
		return "", err
	}
	if s.smsSender.ReturnsCode() {
		return code, nil
	}
	return "", nil
}

func (s *Service) maxSmsPerPhoneHour() int64 {
	limit := s.cfg.SmsRatePerPhonePerHour
	if limit <= 0 {
		limit = smsCodeHourlyLimit
	}
	return limit
}

// VerifySmsCode 校验该手机号最新一条验证码并烧掉它，然后为手机号身份建会话。
func (s *Service) VerifySmsCode(ctx context.Context, rawPhone, code, nickname string) (domain.Session, error) {
	phone, err := normalizePhone(rawPhone)
	if err != nil {
		return domain.Session{}, err
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return domain.Session{}, fmt.Errorf("%w: 请输入验证码", ErrValidation)
	}
	latest, err := s.sms.LatestSmsCode(ctx, phone)
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
	if err := s.sms.ConsumeSmsCode(ctx, latest.ID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return domain.Session{}, fmt.Errorf("%w: 验证码已使用，请重新获取", ErrValidation)
		}
		return domain.Session{}, err
	}
	return s.loginWithIdentity(ctx, domain.ProviderPhone, phone, nickname)
}

// ---- helpers（与 legacy 逐字一致） ----

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

func randomSmsCode() (string, error) {
	buffer := make([]byte, 4)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	value := uint32(buffer[0])<<24 | uint32(buffer[1])<<16 | uint32(buffer[2])<<8 | uint32(buffer[3])
	return fmt.Sprintf("%06d", value%1000000), nil
}

// slidingWindowLimiter 单实例内存 IP 限流器；手机号级限额在数据库里。
type slidingWindowLimiter struct {
	mu     sync.Mutex
	events map[string][]time.Time
	limit  int
	window time.Duration
}

func newIPLimiter(limit int, window time.Duration) *slidingWindowLimiter {
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
	return true
}

// avatarObjectKey 从头像资产里取 object key（legacy relativeAssetURL 的输入）。
func avatarObjectKey(asset domain.MediaAsset) string {
	return asset.ObjectKey
}
