package domain

import (
	"encoding/json"
	"time"
)

// MediaSourceKind 是暴露给客户端的 source_kind 枚举（强类型来源，禁止按 URL/后缀推断）。
// 与内部 MediaOrigin / DisplayKind 不同：这是 OpenAPI 的展示语义，四值固定。
type MediaSourceKind string

const (
	MediaSourceUserOriginal     MediaSourceKind = "user_original"
	MediaSourceGeneratedPreview MediaSourceKind = "generated_preview"
	MediaSourceBundledReference MediaSourceKind = "bundled_reference"
	MediaSourceDemoExample      MediaSourceKind = "demo_example"
)

// SourceKindOf 把内部 MediaOrigin 映射为客户端 source_kind。
func SourceKindOf(origin MediaOrigin) MediaSourceKind {
	switch origin {
	case MediaOriginUserUpload:
		return MediaSourceUserOriginal
	case MediaOriginProviderOutput:
		return MediaSourceGeneratedPreview
	case MediaOriginBundledReference:
		return MediaSourceBundledReference
	case MediaOriginDemo:
		return MediaSourceDemoExample
	default:
		return MediaSourceUserOriginal
	}
}

// DisplayLabelOf 把内部 DisplayKind 映射为客户端展示角标。
func DisplayLabelOf(kind DisplayKind) string {
	switch kind {
	case DisplayKindOriginal:
		return "原本"
	case DisplayKindGeneratedReference, DisplayKindStyleReference:
		return "风格参考"
	case DisplayKindEffectExample:
		return "效果示例"
	default:
		return "风格参考"
	}
}

// ProfileSummary 是首页的档案摘要投影，形状与 OpenAPI UserProfile 一致。
// 身体测量（体重/三围）是选填的，指针区分「未填」与「填了 0」。
type ProfileSummary struct {
	HeightCM int      `json:"height_cm"`
	Role     string   `json:"role"`
	Budget   string   `json:"budget"`
	WeightKG *float64 `json:"weight_kg,omitempty"`
	BustCM   *float64 `json:"bust_cm,omitempty"`
	WaistCM  *float64 `json:"waist_cm,omitempty"`
	HipCM    *float64 `json:"hip_cm,omitempty"`
}

// ReportCard 是首页的报告摘要卡。
type ReportCard struct {
	ID             string    `json:"id"`
	PriorityTitle  string    `json:"priority_title"`
	PriorityCopy   string    `json:"priority_copy"`
	ImpressionTags []string  `json:"impression_tags"`
	CreatedAt      time.Time `json:"created_at"`
}

// TodayCard 是首页的今日方案摘要卡。
type TodayCard struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Active  bool   `json:"active"`
	State   string `json:"state"`
}

// PlanVariantCard 是首页的最近方案变体摘要卡。
type PlanVariantCard struct {
	ID             string `json:"id"`
	PlanSetID      string `json:"plan_set_id"`
	Name           string `json:"name"`
	Slot           int    `json:"slot"`
	Descriptor     string `json:"descriptor"`
	Recommended    bool   `json:"recommended"`
	HasRenderMedia bool   `json:"has_render_media"`
}

// ProfileSnapshot 是 grounding 用的结构化档案快照（非 UI 摘要，保留偏好原文）。
type ProfileSnapshot struct {
	Role        string
	HeightCM    int
	Budget      string
	Preferences json.RawMessage
	Avoidances  json.RawMessage
}

// FindingGrounding 是 grounding 用的报告发现投影：只带 AI 需要的字段，不带锚点坐标。
type FindingGrounding struct {
	ID                 string
	Category           string
	Label              string
	VisibleObservation string
	Recommendation     string
	Priority           int
	SourceRole         PhotoRole
}

// PlanVariantGrounding 是 grounding 用的方案变体投影。
type PlanVariantGrounding struct {
	ID          string
	PlanSetID   string
	Name        string
	Descriptor  string
	OutcomeTags []string
	Steps       []PlanStep
}

// PublishedMedia 是已发布渲染媒体的投影：只带 object key 与 asset id，不带签名 URL
// （签名 URL 由读取投影时即时生成）。
type PublishedMedia struct {
	AssetID      string
	ObjectKey    string
	SourceKind   MediaSourceKind
	DisplayLabel string
	PublishedAt  time.Time
}

// ReportGrounding 是 grounding 用的整份报告投影。
type ReportGrounding struct {
	ID             string
	PriorityTitle  string
	PriorityCopy   string
	ImpressionTags []string
	Findings       []FindingGrounding
}

// WardrobeGroundingItem 是 grounding 用的衣橱单品投影。
type WardrobeGroundingItem struct {
	ID        string
	Name      string
	Category  string
	Color     string
	Formality string
	ObjectKey string
}

// MediaInput 是 grounding 用的媒体引用：object key 与 asset id，绝不含签名 URL。
type MediaInput struct {
	AssetID   string
	ObjectKey string
	MIMEType  string
}
