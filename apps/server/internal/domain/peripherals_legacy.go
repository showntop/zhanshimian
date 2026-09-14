package domain

import (
	"encoding/json"
	"time"
)

// 本文件承接旧 domain.go 中仍被保留代码引用的外围类型（机械迁出，字段不变）。
// 其余旧类型（Analysis/Plan/TaskView/ToolResult 等）随 legacy service 删除。

// ---- 账号（service/account 消费） ----

type MeAccount struct {
	ID         string          `json:"id"`
	Nickname   string          `json:"nickname"`
	AvatarURL  string          `json:"avatar_url,omitempty"`
	Identities []Identity      `json:"identities"`
	Billing    *BillingSummary `json:"billing,omitempty"`
}

type SmsCode struct {
	ID        string
	Phone     string
	Digest    []byte
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}

// ---- 今日（service/today 消费） ----

type TodayContext struct {
	Date        string `json:"date"`
	City        string `json:"city"`
	Condition   string `json:"condition"`
	Temperature int    `json:"temperature"`
	DayType     string `json:"day_type"`
	Schedule    string `json:"schedule"`
}

type TodayPlanStep struct {
	Category string `json:"category"`
	Label    string `json:"label"`
	Title    string `json:"title"`
	Copy     string `json:"copy"`
}

// ---- 顾问（service/advisor 消费） ----

type AdvisorAction struct {
	ID      string          `json:"id"`
	Kind    string          `json:"kind"`
	Label   string          `json:"label"`
	Payload json.RawMessage `json:"payload"`
	Applied bool            `json:"applied"`
}

type AdvisorMessage struct {
	ID             string          `json:"id"`
	ConversationID string          `json:"conversation_id"`
	Role           string          `json:"role"`
	Content        string          `json:"content"`
	Actions        []AdvisorAction `json:"actions,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
}

// ---- 基础设施（healthz/埋点） ----

type JobsHealth struct {
	OldestQueuedSeconds int64 `json:"oldest_queued_seconds"`
	FailedLastHour      int64 `json:"failed_last_hour"`
}

type ProductEventInput struct {
	Name    string          `json:"name"`
	Payload json.RawMessage `json:"payload"`
}
