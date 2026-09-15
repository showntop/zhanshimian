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

// DiagnosisFinding 是诊断的可提升点（tone 是建议语气，不是缺陷严重度）。
type DiagnosisFinding struct {
	Label    string   `json:"label"`
	Category string   `json:"category"`
	Tone     string   `json:"tone"`
	AnchorX  *float64 `json:"anchor_x,omitempty"`
	AnchorY  *float64 `json:"anchor_y,omitempty"`
}

// DiagnosisOption 是诊断给出的可选方向及其参考媒体。
type DiagnosisOption struct {
	ID     string          `json:"id"`
	Name   string          `json:"name"`
	Media  RenderMediaView `json:"media"`
	Note   string          `json:"note,omitempty"`
	Reason string          `json:"reason,omitempty"`
	Tags   []string        `json:"tags,omitempty"`
}

// HairStyle 是发型目录条目（GET /v1/hairstyles）。
type HairStyle struct {
	ID     string          `json:"id"`
	Name   string          `json:"name"`
	Media  RenderMediaView `json:"media"`
	Note   string          `json:"note,omitempty"`
	Reason string          `json:"reason,omitempty"`
	Tags   []string        `json:"tags,omitempty"`
	// MediaObjectKey 仅供读路径即时签名（敏感信息红线：库不存签名 URL），不外发。
	MediaObjectKey string `json:"-"`
}
