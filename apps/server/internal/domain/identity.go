package domain

import "time"

const (
	ProviderWeChatMiniApp = "wechat_miniapp"
	ProviderWeChatApp     = "wechat_app"
	ProviderApple         = "apple"
	ProviderPhone         = "phone"
)

type User struct {
	ID        string
	Nickname  string
	CreatedAt time.Time
}

type Identity struct {
	ID         string
	UserID     string
	Provider   string
	Identifier string
	CreatedAt  time.Time
}

type Session struct {
	ID          string
	UserID      string
	TokenDigest []byte
	ExpiresAt   time.Time
	CreatedAt   time.Time
}
