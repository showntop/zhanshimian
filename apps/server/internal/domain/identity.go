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
	ID          string    `json:"-"`
	UserID      string    `json:"-"`
	Token       string    `json:"token"`
	TokenDigest []byte    `json:"-"`
	ExpiresAt   time.Time `json:"expires_at"`
	CreatedAt   time.Time `json:"-"`
}
