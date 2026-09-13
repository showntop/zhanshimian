package httpapi

import (
	"context"

	"github.com/zhanshimian/server/internal/domain"
)

// AccountService 是登录/会话/账号的最小依赖（service/account 的 Service）。
type AccountService interface {
	DevLogin(ctx context.Context, nickname string) (domain.Session, error)
	WeChatLogin(ctx context.Context, code, nickname string) (domain.Session, error)
	WeChatAppLogin(ctx context.Context, code, nickname string) (domain.Session, error)
	AppleLogin(ctx context.Context, identityToken, nickname string) (domain.Session, error)
	RequestSmsCode(ctx context.Context, rawPhone, clientIP string) (devCode string, err error)
	VerifySmsCode(ctx context.Context, rawPhone, code, nickname string) (domain.Session, error)
	Authenticate(ctx context.Context, token string) (domain.User, error)
	Logout(ctx context.Context, token string) error
	GetAccount(ctx context.Context, user domain.User) (domain.MeAccount, error)
	UpdateAccount(ctx context.Context, user domain.User, nickname, avatarMediaID string) (domain.MeAccount, error)
	GetProfile(ctx context.Context, userID string) (domain.UserProfile, error)
	UpdateProfile(ctx context.Context, userID string, profile domain.UserProfile) (domain.UserProfile, error)
	DeleteUserData(ctx context.Context, userID string, deleteObject func(key string) error) error
}

// BillingService 是购买/权益的最小依赖（service/billing 的 Orders）。
type BillingService interface {
	BillingSummary(ctx context.Context, userID string) (domain.BillingSummary, error)
	CreateBillingOrder(ctx context.Context, userID, skuID, loginCode string) (domain.BillingOrder, error)
	SyncBillingOrder(ctx context.Context, userID, orderID string) (domain.BillingOrder, error)
	HandleBillingNotify(ctx context.Context, raw []byte, signature string) error
}

// EventWriter 是埋点的窄写入端口（静默失败：埋点永不影响业务流程）。
type EventWriter interface {
	TrackProductEvent(ctx context.Context, userID string, input domain.ProductEventInput) error
}

// JobsReader 是 healthz 的队列指标只读端口（*postgres.Store 直接实现）。
type JobsReader interface {
	HealthJobs(ctx context.Context) (domain.JobsHealth, error)
}

// DemoMediaCreator 是 Demo 媒体行的窄写入端口。
type DemoMediaCreator interface {
	CreateDemoMedia(ctx context.Context, userID, kind string) (domain.MediaAsset, error)
}
