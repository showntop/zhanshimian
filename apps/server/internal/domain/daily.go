package domain

import (
	"encoding/json"
	"time"
)

// ---------- 形象基因（稳定层） ----------
// 与 packages/core/src/daily/types.ts 的 StyleGene 同形：只有特征描述，
// 不含任何评分、体型判断或敏感属性。V1 由服务端从用户资料派生，
// 不依赖用户当天穿什么——这是「不臆想」的结构性保证。

type StyleGene struct {
	SkinTone   string `json:"skinTone"`
	Contrast   string `json:"contrast"`
	Shoulder   string `json:"shoulder"`
	Frame      string `json:"frame"`
	Vertical   string `json:"vertical"`
	Neck       string `json:"neck"`
	Face       string `json:"face"`
	HeightBand string `json:"heightBand"`
}

// DefaultGene 未建档/资料不全时的中性基因：所有知识事实里声明条件的维度
// 都可能不匹配，因此选品只会落到「不挑人」的事实上——宁可少推，不推错的。
func DefaultGene() StyleGene {
	return StyleGene{
		SkinTone: "neutral", Contrast: "medium", Shoulder: "medium", Frame: "medium",
		Vertical: "balanced", Neck: "medium", Face: "oval", HeightBand: "average",
	}
}

// Value 按维度取值（维度名与 TS 端 GeneCondition 的 key 一致）。
func (g StyleGene) Value(dimension string) string {
	switch dimension {
	case "skinTone":
		return g.SkinTone
	case "contrast":
		return g.Contrast
	case "shoulder":
		return g.Shoulder
	case "frame":
		return g.Frame
	case "vertical":
		return g.Vertical
	case "neck":
		return g.Neck
	case "face":
		return g.Face
	case "heightBand":
		return g.HeightBand
	}
	return ""
}

// Map 供落库（收藏的 geneSnapshot）与去重用。
func (g StyleGene) Map() map[string]string {
	return map[string]string{
		"skinTone":   g.SkinTone,
		"contrast":   g.Contrast,
		"shoulder":   g.Shoulder,
		"frame":      g.Frame,
		"vertical":   g.Vertical,
		"neck":       g.Neck,
		"face":       g.Face,
		"heightBand": g.HeightBand,
	}
}

// GeneCondition 声明一条知识事实对哪些基因取值成立：
// key 是维度名，value 是取值白名单；缺失的维度表示不挑人。
type GeneCondition map[string][]string

// Matches 未声明的维度不参与判断；白名单为空数组视为不挑人。
func (c GeneCondition) Matches(gene StyleGene) bool {
	for dimension, allowed := range c {
		if len(allowed) == 0 {
			continue
		}
		value := gene.Value(dimension)
		hit := false
		for _, candidate := range allowed {
			if candidate == value {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	return true
}

// ---------- 知识事实（人工编辑生产、顾问确认） ----------

type KnowledgeFact struct {
	ID        string        `json:"id"`
	Domain    string        `json:"domain"`
	Fact      string        `json:"fact"`
	Boundary  string        `json:"boundary"`
	GeneFit   GeneCondition `json:"gene_fit"`
	Season    []string      `json:"season"`
	Source    string        `json:"source"`
	Reviewed  bool          `json:"reviewed"`
	CreatedAt time.Time     `json:"created_at"`
}

// ---------- 生成产物 ----------

// ContentVisual 与客户端 ContentVisual 同形：spec 由内容自己声明（画什么），
// 组件不做猜测；解析不出来时降级到 alt 文字。
type ContentVisual struct {
	Modality string         `json:"modality"`
	Spec     map[string]any `json:"spec"`
	Alt      string         `json:"alt"`
}

// ContentLock 换装洗牌的定格参数（语义值，词表在 service/daily）。
// 与内容同源：由生成建议的同一次 LLM 调用产出，落库随行——
// 「定格的那套必须就是建议本身」。任何一维为空都允许（服务端按维回退稳定随机）。
type ContentLock struct {
	Look  string `json:"look,omitempty"`
	Color string `json:"color,omitempty"`
	Waist string `json:"waist,omitempty"`
	Hair  string `json:"hair,omitempty"`
}

// DailyContent 每天每用户一条；user_id 为空的行是公共兜底池条目。
type DailyContent struct {
	ID        string        `json:"id"`
	UserID    string        `json:"user_id,omitempty"`
	GenDate   string        `json:"gen_date"`
	Category  string        `json:"category"`
	Topic     string        `json:"topic"`
	Lead      string        `json:"lead"`
	FitText   string        `json:"fit_text"`
	Why       string        `json:"why"`
	Visual    ContentVisual `json:"visual"`
	// Lock 与建议同源的洗牌定格参数（可空：兜底池条目没有它）。
	Lock      *ContentLock  `json:"lock,omitempty"`
	FactIDs   []string      `json:"fact_ids"`
	Source    string        `json:"source"`
	ModelKey  string        `json:"model_key,omitempty"`
	DedupeKey string        `json:"dedupe_key"`
	CreatedAt time.Time     `json:"created_at"`
}

// ContentType 是客户端的 ContentType 枚举（silhouette/item 这类历史命名保留，
// 因为小程序海报的底板与文案按它取）。分类 ↔ 类型是双向映射，见
// CategoryToContentType / ContentTypeToCategory。
func CategoryToContentType(category string) string {
	switch category {
	case "fit":
		return "silhouette"
	case "outfit":
		return "item"
	default:
		return category
	}
}

func ContentTypeToCategory(contentType string) string {
	switch contentType {
	case "silhouette":
		return "fit"
	case "item":
		return "outfit"
	default:
		return contentType
	}
}

// ---------- 生成审计 ----------

type GenerationRun struct {
	ID               string         `json:"id"`
	UserID           string         `json:"user_id,omitempty"`
	GenDate          string         `json:"gen_date,omitempty"`
	FactIDs          []string       `json:"fact_ids"`
	PromptHash       string         `json:"prompt_hash"`
	Output           map[string]any `json:"output,omitempty"`
	Validation       map[string]any `json:"validation,omitempty"`
	Outcome          string         `json:"outcome"`
	ModelKey         string         `json:"model_key,omitempty"`
	LatencyMS        int            `json:"latency_ms"`
	EstimatedCostCNY *float64       `json:"estimated_cost_cny,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
}

// ---------- 收藏（内容引用 + 完整副本 + 多态素材 + 生命周期） ----------

type CollectionStatus = string

const (
	CollectionStatusSaved CollectionStatus = "saved"
	CollectionStatusTried CollectionStatus = "tried"
	CollectionStatusKept  CollectionStatus = "kept"
)

// ContentSnapshot 收下那一刻固化的副本：内容池后续迭代（改文案/换素材/下架）
// 不影响用户手册——他收下的是「当时的那条建议」。fit 在落库前必须已渲染成字符串。
type ContentSnapshot struct {
	ID       string        `json:"id"`
	Type     string        `json:"type"`
	Topic    string        `json:"topic"`
	Lead     string        `json:"lead"`
	FitText  string        `json:"fitText"`
	Why      string        `json:"why"`
	Visual   ContentVisual `json:"visual"`
	Category string        `json:"category"`
}

// CollectionAsset 多态素材：新增一种素材 = 加一个 kind，不动表。
type CollectionAsset struct {
	Kind string `json:"kind"`
	// colors
	Colors []string `json:"colors,omitempty"`
	// media
	MediaID string `json:"media_id,omitempty"`
	Role    string `json:"role,omitempty"`
	// 引用已有实体：只存 id，不复制数据
	ItemID   string `json:"item_id,omitempty"`
	OutfitID string `json:"outfit_id,omitempty"`
	PlanID   string `json:"plan_id,omitempty"`
	Source   string `json:"source,omitempty"`
	Ref      string `json:"ref,omitempty"`
}

// SavedContext 收下时的语境：回看「我当时为什么收这条」。
type SavedContext struct {
	Temperature *int   `json:"temperature,omitempty"`
	Condition   string `json:"condition,omitempty"`
	DayType     string `json:"dayType,omitempty"`
	Season      string `json:"season,omitempty"`
	City        string `json:"city,omitempty"`
	Schedule    string `json:"schedule,omitempty"`
}

type DailyCollection struct {
	ID         string            `json:"id"`
	UserID     string            `json:"user_id"`
	ContentID  string            `json:"content_id"`
	ContentKey string            `json:"content_key"`
	Snapshot   ContentSnapshot   `json:"-"`
	Title      string            `json:"title"`
	Summary    string            `json:"summary"`
	Category   string            `json:"category"`
	Assets     []CollectionAsset `json:"assets"`
	Context    SavedContext      `json:"context"`
	Gene       map[string]string `json:"gene_snapshot,omitempty"`
	Status     CollectionStatus  `json:"status"`
	Note       string            `json:"note"`
	SavedAt    time.Time         `json:"saved_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
}

// MarshalJSON 把副本作为 content_snapshot 输出（表里的列名也是它）。
func (c DailyCollection) MarshalJSON() ([]byte, error) {
	type alias DailyCollection
	wrapped := struct {
		alias
		Snapshot ContentSnapshot `json:"content_snapshot"`
	}{alias: alias(c), Snapshot: c.Snapshot}
	return json.Marshal(wrapped)
}
