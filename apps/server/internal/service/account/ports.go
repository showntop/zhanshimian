package account

import (
	"context"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/provider/identity"
)

// 错误哨兵沿用 legacy 语义；文案以 "<哨兵>: <细节>" 约定拼接，传输层按
// errors.Is 分类为 400/403/429。
var (
	ErrValidation  = errValidation{}
	ErrForbidden   = errForbidden{}
	ErrRateLimited = errRateLimited{}
)

type errValidation struct{}

func (errValidation) Error() string { return "validation error" }

type errForbidden struct{}

func (errForbidden) Error() string { return "forbidden" }

type errRateLimited struct{}

func (errRateLimited) Error() string { return "rate limited" }

// IdentityRepository 建档与列出登录身份。
type IdentityRepository interface {
	CreateDevUser(ctx context.Context, nickname string) (domain.User, error)
	EnsureUserByIdentity(ctx context.Context, providerName, identifier, nickname string) (domain.User, error)
	ListIdentities(ctx context.Context, userID string) ([]domain.Identity, error)
}

// SessionRepository 创建/查/吊销会话。
type SessionRepository interface {
	CreateSession(ctx context.Context, userID string, tokenDigest []byte, expiresAt time.Time) error
	UserByTokenDigest(ctx context.Context, tokenDigest []byte) (domain.User, error)
	DeleteSessionByTokenDigest(ctx context.Context, tokenDigest []byte) error
}

// MeRepository 账号字段、头像与补充资料。
type MeRepository interface {
	GetUserProfile(ctx context.Context, userID string) (domain.UserProfile, error)
	SaveUserProfile(ctx context.Context, userID string, profile domain.UserProfile) (domain.UserProfile, error)
	UpdateUserNickname(ctx context.Context, userID string, nickname string) error
	UpdateUserAvatar(ctx context.Context, userID string, avatarMediaID string) error
	GetUserAvatar(ctx context.Context, userID string) (domain.MediaAsset, error)
}

// SmsRepository 验证码存取与限额。
type SmsRepository interface {
	CreateSmsCode(ctx context.Context, phone string, codeDigest []byte, expiresAt time.Time) error
	LatestSmsCode(ctx context.Context, phone string) (domain.SmsCode, error)
	ConsumeSmsCode(ctx context.Context, id string) error
	CountSmsCodesSince(ctx context.Context, phone string, since time.Time) (int64, error)
}

// DataEraser 「删除我的数据」：返回需回收的对象 key，行级删除由实现负责。
type DataEraser interface {
	DeleteUserData(ctx context.Context, userID string) ([]string, error)
}

// Authenticators 复用 identity 子包的适配器端口（不重写算法）。
type (
	WeChatAuthenticator    = identity.WeChatAuthenticator
	WeChatAppAuthenticator = identity.WeChatAuthenticator
	AppleAuthenticator     = identity.AppleAuthenticator
	SmsSender              = identity.SmsSender
)
