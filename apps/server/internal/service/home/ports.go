package home

import (
	"context"
	"time"

	"github.com/zhanshimian/server/internal/domain"
)

// Bootstrap 是 GET /v1/home/bootstrap 的契约投影（冻结 openapi.yaml
// HomeBootstrap）：档案摘要 + 完整已发布报告 + 最近方案集 + 今日方案 +
// 进行中操作 + 权益摘要。除 active_operations 外各键按需省略。
type Bootstrap struct {
	ProfileSummary   *domain.ProfileSummary `json:"profile_summary,omitempty"`
	Report           *reportDTO             `json:"report,omitempty"`
	PlanSet          *planSetDTO            `json:"plan_set,omitempty"`
	TodayPlan        *TodayPlan             `json:"today_plan,omitempty"`
	ActiveOperations []domain.OperationRef  `json:"active_operations"`
	Billing          *domain.BillingSummary `json:"billing,omitempty"`
}

// Snapshot 是首页聚合中纯 SQL 可投影的部分：档案摘要 + 进行中操作。
// 报告/方案集/今日/权益分别由对应领域的读路径供给，不走这份读模型。
type Snapshot struct {
	Profile          *domain.ProfileSummary
	ActiveOperations []domain.OperationRef
}

// MediaObject 是媒体资产的对象定位：签名/回退拼 URL 所需的 key 与类型。
type MediaObject struct {
	ObjectKey string
	MIMEType  string
}

// Reader 是首页聚合的 SQL 读模型端口：只做稳定投影，不越权读配置。
type Reader interface {
	ReadHome(ctx context.Context, userID string, now time.Time) (Snapshot, error)
	// LatestPublishedPlanSetID 定位最近一个已发布方案集（无则 ErrNotFound）。
	LatestPublishedPlanSetID(ctx context.Context, userID string) (string, error)
	// MediaObjectInfo 按资产 ID 读对象定位（越权/不存在/已删除一律 ErrNotFound）。
	MediaObjectInfo(ctx context.Context, userID, assetID string) (MediaObject, error)
}

// ---- 端口视图类型 ----
// home 不 import 兄弟 service 包（postgres 读模型实现 home.Reader，兄弟包的
// 测试 import postgres，直接引用会形成测试期 import cycle）。各端口以
// home 本地视图类型出参，bootstrap 适配器负责从具体服务做机械转换。

// PresentedMedia 是可渲染媒体：已签/回退拼好的 URL + 来源与角标。
type PresentedMedia struct {
	AssetID      string
	URL          string
	URLExpiresAt time.Time
	MIMEType     string
	SourceKind   string
	DisplayLabel string
}

// ReportPhoto 是报告源图（含拍摄角色）。
type ReportPhoto struct {
	ItemID string
	Role   domain.PhotoRole
	Media  PresentedMedia
}

// ReportSourceMedia 是报告的三张源图。
type ReportSourceMedia struct {
	Face ReportPhoto
	Side ReportPhoto
	Body ReportPhoto
}

// SourcePhotoRef 是发现锚定的源图引用。
type SourcePhotoRef struct {
	ItemID string
	Role   domain.PhotoRole
}

// FindingView 是报告单条发现的呈现视图。
type FindingView struct {
	ID, Category, Label, VisibleObservation, Recommendation string
	Priority, Position                                      int
	SourcePhoto                                             SourcePhotoRef
	Anchor                                                  domain.EvidenceAnchor
}

// ReportView 是已发布报告的呈现视图（与 service/assessment 的 ReportView
// 同构，经 ReportReader 端口进入 home）。
type ReportView struct {
	ID, PhotoSetID, HeroAssetID, PriorityTitle, PriorityCopy, SchemaVersion string
	ImpressionTags                                                          []string
	SourceMedia                                                             ReportSourceMedia
	Findings                                                                []FindingView
	CreatedAt                                                               time.Time
}

// TodayPlan 是今日方案的契约形状（与 GET /v1/today/plans/current 同构）。
type TodayPlan struct {
	ID        string                  `json:"id"`
	Context   domain.TodayContext     `json:"context"`
	Title     string                  `json:"title"`
	Summary   string                  `json:"summary"`
	Steps     []domain.TodayPlanStep  `json:"steps"`
	Active    bool                    `json:"active"`
	State     string                  `json:"state"`
	Operation domain.OperationRef     `json:"operation"`
	Media     *domain.RenderMediaView `json:"media"`
	Feedback  *string                 `json:"feedback,omitempty"`
	CreatedAt time.Time               `json:"created_at"`
	UpdatedAt time.Time               `json:"updated_at"`
}

// ---- 领域读端口 ----

// ReportReader 复用报告读路径的呈现（service/assessment，含 source_media
// 签名 URL），首页不再维护第二份报告投影。无已发布报告时返回 ErrNotFound。
type ReportReader interface {
	GetCurrentReport(ctx context.Context, userID string) (ReportView, error)
}

// PlanSetReader 复用 planning 的公开读方法拿 PlanSet 领域对象（变体的
// 渲染视图已由 planning 读模型合并，含签名媒体）。
type PlanSetReader interface {
	GetPlanSet(ctx context.Context, userID, planSetID string) (domain.PlanSet, error)
}

// TodayReader 读当前生效的今日方案（service/today）。无生效方案时返回
// ErrNotFound。
type TodayReader interface {
	Current(ctx context.Context, userID string) (TodayPlan, error)
}

// BillingReader 读权益摘要（与 GET /v1/billing/me 同源）。
type BillingReader interface {
	BillingSummary(ctx context.Context, userID string) (domain.BillingSummary, error)
}

// MediaSigner 给 today_plan 的发布媒体解析可读 URL（COS 短时签名，
// 本地存储由适配器回退公开路径）。
type MediaSigner interface {
	SignedURL(ctx context.Context, objectKey string) (url string, expiresAt time.Time, err error)
}
