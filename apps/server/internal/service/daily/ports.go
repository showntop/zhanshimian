// Package daily 每日内容：一次 LLM 成文（语境驱动选题）+ 自动校验 + 降级（兜底池）
// + 收藏（内容引用 + 完整副本 + 多态素材 + 生命周期）。
//
// 两条铁律（方案 §1）：
//  1. generate 永远返回 200 + 内容，source 只有 generated / fallback；
//     降级对客户端透明，客户端没有「生成失败」分支，只有「内容来源」字段。
//  2. 同一用户同一天只生成一次：幂等键 (user_id, gen_date)。
//
// 选题与成文合并为单次 LLM 调用（spec 2026-09-20-daily-llm-driven）：
// prompt 带齐用户语境（天气/画像/近推历史/收藏信号）+ 可选参考事实，
// category 由模型自报、事后归类；知识事实不再强制引用。
package daily

import (
	"context"
	"errors"
	"time"

	"github.com/zhanshimian/server/internal/domain"
)

var (
	// ErrValidation 输入不合法（真错误，回 400；不是内容降级）。
	ErrValidation = errors.New("daily_validation_error")
	// ErrNoContent 兜底池与静态问候都拿不到内容（理论上不可达，留作哨兵）。
	ErrNoContent = errors.New("daily_no_content")
)

// 领域枚举：与 contracts/openapi.yaml 的 Daily* schema 一一对应。
const (
	SourceGenerated = "generated"
	SourceFallback  = "fallback"
)

// Grounding 生成所需的稳定特征。V1 只从用户资料派生（身高/体重），
// 不依赖用户当天穿什么——这是「不臆想」的结构性保证。
type Grounding struct {
	HeightCM int
	WeightKG *float64
}

// Weather 今日语境（复用服务端既有天气 provider）。
type Weather struct {
	City        string
	Condition   string
	Temperature int
}

// Reader 稳定特征读取。
type Reader interface {
	ReadDailyGrounding(ctx context.Context, userID string) (Grounding, error)
}

// KnowledgeStore 知识事实检索：已确认、当季。
type KnowledgeStore interface {
	ListReviewedFacts(ctx context.Context, excludeIDs []string, season string) ([]domain.KnowledgeFact, error)
}

// ContentStore 生成产物读写（含公共兜底池）。
type ContentStore interface {
	TodayContent(ctx context.Context, userID string, genDate string) (domain.DailyContent, error)
	ContentByID(ctx context.Context, userID string, id string) (domain.DailyContent, error)
	FallbackPool(ctx context.Context) ([]domain.DailyContent, error)
	// RecentContents 近 N 天已生成的内容（新到旧）：prompt 历史 + topic 去重都靠它。
	RecentContents(ctx context.Context, userID string, since time.Time, limit int) ([]domain.DailyContent, error)
	RecentContentKeys(ctx context.Context, userID string, since time.Time) ([]string, error)
	SaveContent(ctx context.Context, content domain.DailyContent) (domain.DailyContent, error)
	// ReplaceContent 覆盖当天内容（upsert）。只给非生产调试开关用：
	// 正常链路必须保持「同一天只生成一次」的幂等语义。
	ReplaceContent(ctx context.Context, content domain.DailyContent) (domain.DailyContent, error)
}

// RunStore 生成审计（评估与回溯）。
type RunStore interface {
	RecordRun(ctx context.Context, run domain.GenerationRun) error
}

// CollectionStore 收藏。
type CollectionStore interface {
	CreateCollection(ctx context.Context, item domain.DailyCollection) (domain.DailyCollection, error)
	ListCollections(ctx context.Context, userID string, category string, limit int) ([]domain.DailyCollection, error)
	CollectionByID(ctx context.Context, userID string, id string) (domain.DailyCollection, error)
	UpdateCollection(ctx context.Context, userID string, id string, status string, note string) (domain.DailyCollection, error)
	DeleteCollection(ctx context.Context, userID string, id string) error
	CountCollections(ctx context.Context, userID string) (map[string]int, error)
}

// Clock 时间源（单测可注入）。
type Clock interface{ Now() time.Time }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func NewClock() Clock { return systemClock{} }

// WeatherProvider 天气 provider 的最小抽象（demo / 高德同一形状）。
type WeatherProvider interface {
	Current(ctx context.Context, city string) (Weather, error)
}

// ReferenceFact 注入 prompt 的参考事实（检索增强：可用可不用，不强制引用）。
type ReferenceFact struct {
	Domain   string
	Fact     string
	Boundary string
}

// VisualItem 程序化视觉的一项（色卡/位置轴共用）。
type VisualItem struct {
	Label string
	Tone  string
	State string
}

// VisualSide 对比的一侧。
type VisualSide struct {
	Label   string
	Tone    string
	Layered bool
}

// VisualDraft 模型给出的视觉提示；由 service 严格落成形后才能上屏。
type VisualDraft struct {
	Modality string
	Kind     string
	Alt      string
	Items    []VisualItem
	Left     *VisualSide
	Right    *VisualSide
	Marker   string
}

// ContentRequest 生成请求（不含任何厂商/模型信息，能力名在 provider 里）。
// 语境驱动：模型自行决定讲什么，ReferenceFacts 只是可选参考。
type ContentRequest struct {
	// ContextText 日期/星期/季节/天气。
	ContextText string
	// GeneText 用户画像（中性特征词）。
	GeneText string
	// HistoryText 近期已推内容清单（去重主机制）。
	HistoryText string
	// InterestText 收藏信号：用户对什么感兴趣。
	InterestText string
	// ReferenceFacts 检索到的参考事实（可用可不用）。
	ReferenceFacts []ReferenceFact
	// RetryHint 校验失败后的修正提示（只重试一次）。
	RetryHint string
}

// ContentOutput 生成结果（未校验）。Category 由模型自报（手册各格或 general）。
type ContentOutput struct {
	Topic         string
	Lead          string
	Fit           string
	Why           string
	Category      string
	Visual        VisualDraft
	ModelKey      string
	LatencyMS     int
	EstimatedCost *float64
}

// ContentPlanner 走 AI 能力路由（capability=daily_content）的生成器。
type ContentPlanner interface {
	Generate(ctx context.Context, input ContentRequest) (ContentOutput, error)
}

// PrepareResult POST /v1/daily/prepare 的返回（纯缓存探测）。
// 命中时 Scenario 为当日内容的分类（信息性），未命中为空串——
// 选题由 generate 阶段的 LLM 决定，客户端播通用过场动画。
type PrepareResult struct {
	GenDate  string
	Scenario string
	CacheHit bool
	// Presentation 等待期（roam）脚本，与用户无关、可缓存。
	// 命中当天内容时为 nil——缓存命中不播等待动画。
	Presentation *Presentation
}

// GenerateResult POST /v1/daily/generate 的返回（永远 200）。
type GenerateResult struct {
	Source  string
	Content domain.DailyContent
	// Presentation 收敛 + 揭晓脚本。定格的那一套就是 Content 本身，
	// 所以脚本由内容反推，不是随机编排。
	Presentation *Presentation
}

// CollectionStats 手册全部分格计数（收藏信号也用它）。
type CollectionStats struct {
	Counts map[string]int
	Total  int
}
