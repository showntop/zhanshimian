package diagnostic

import (
	"context"
	"fmt"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/service/account"
)

// Diagnosis 是持久化的诊断结果（OpenAPI Diagnosis 形状）。
type Diagnosis struct {
	ID            string                    `json:"id"`
	Kind          string                    `json:"kind"`
	Scene         string                    `json:"scene"`
	Conclusion    string                    `json:"conclusion"`
	PriorityTitle string                    `json:"priority_title"`
	PriorityCopy  string                    `json:"priority_copy"`
	Tags          []string                  `json:"tags"`
	Findings      []domain.DiagnosisFinding `json:"findings"`
	Options       []domain.DiagnosisOption  `json:"options"`
	SourceMedia   *domain.RenderMediaView   `json:"source_media,omitempty"`
	Saved         bool                      `json:"saved"`
	CreatedAt     time.Time                 `json:"created_at"`

	// MediaAssetID 写入 diagnostics.source_media_asset_id；不出现在 JSON 里。
	MediaAssetID string `json:"-"`

	// SourceMediaObjectKey/SourceMediaMIMEType 由读取侧 join media_assets 得到，
	// 供呈现层即时签名（与 share.Card.ObjectKey 同一做法）；不出现在 JSON。
	SourceMediaObjectKey string `json:"-"`
	SourceMediaMIMEType  string `json:"-"`
}

type RunInput struct {
	Kind         string // outfit | purchase
	Scene        string
	MediaAssetID string
	ReportID     string // 可选；缺省取当前报告
}

// DiagnosticRequest 是发给 AI 的诊断请求。
type DiagnosticRequest struct {
	Kind         string
	Scene        string
	MediaAssetID string
	Images       []Image
	Report       *domain.ReportGrounding
	Profile      domain.ProfileSnapshot
	Wardrobe     []domain.WardrobeGroundingItem
}

// DiagnosticOutput 是校验通过的 AI 输出。
type DiagnosticOutput struct {
	Conclusion    string
	PriorityTitle string
	PriorityCopy  string
	Tags          []string
	Findings      []domain.DiagnosisFinding
	Options       []domain.DiagnosisOption
}

// Advisor 是诊断的 AI 能力接口：由消费方定义，provider/ai 提供实现。
type Advisor interface {
	Diagnose(ctx context.Context, request DiagnosticRequest) (DiagnosticOutput, error)
}

// Writer 持久化与读取诊断结果。
type Writer interface {
	InsertDiagnostic(ctx context.Context, userID string, diagnosis Diagnosis) (Diagnosis, error)
	GetDiagnosticByID(ctx context.Context, userID string, id string) (Diagnosis, error)
	GetLatestDiagnosticByKind(ctx context.Context, userID string, kind string) (Diagnosis, error)
	UpdateDiagnosticSaved(ctx context.Context, userID string, id string, saved bool) (Diagnosis, error)
	// CountDiagnosticsSince 计用户在 since 之后落库的诊断条数（日限自计数，
	// 不依赖 billing 包；与 account.CountSmsCodesSince 同一形状）。
	CountDiagnosticsSince(ctx context.Context, userID string, since time.Time) (int, error)
}

// 诊断是同步 AI 端点，必须自带成本闸门：每用户每自然日（UTC，与 billing
// 摘要的 daily_remaining 同一口径）最多 8 次，与 limitDiagnosticPerDay 对齐。
const DailyLimitPerDay = 8

type Service struct {
	reader  Reader
	writer  Writer
	advisor Advisor
	loader  ImageLoader
	signer  MediaSigner
}

func New(reader Reader, writer Writer, advisor Advisor, loader ImageLoader) *Service {
	return &Service{reader: reader, writer: writer, advisor: advisor, loader: loader}
}

// WithMediaSigner 装配读路径媒体签名器（与 assessment.WithBilling 同一链式做法）。
func (s *Service) WithMediaSigner(signer MediaSigner) *Service {
	s.signer = signer
	return s
}

// Run 同步跑一次诊断：限额 → grounding → 源照片加载 → AI → 落库。AI 失败
// 必须返回错误，绝不落一条模板结果（计划 TestDiagnosisProviderFailureNeverReturnsTemplate）；
// 照片缺失/越权同样失败（404 语义），绝不静默退化成无图诊断。
func (s *Service) Run(ctx context.Context, userID string, input RunInput) (Diagnosis, error) {
	if input.MediaAssetID == "" {
		return Diagnosis{}, fmt.Errorf("%w: 请先上传需要判断的照片", account.ErrValidation)
	}
	now := time.Now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	count, err := s.writer.CountDiagnosticsSince(ctx, userID, dayStart)
	if err != nil {
		return Diagnosis{}, err
	}
	if count >= DailyLimitPerDay {
		return Diagnosis{}, fmt.Errorf("%w: 今日诊断已达上限，明天再来", account.ErrRateLimited)
	}
	grounding, err := s.reader.ReadDiagnosticGrounding(ctx, userID, input.ReportID, input.Kind)
	if err != nil {
		return Diagnosis{}, err
	}
	media, err := s.reader.ReadDiagnosticMedia(ctx, userID, input.MediaAssetID)
	if err != nil {
		return Diagnosis{}, err
	}
	image, err := s.loader.Load(ctx, media)
	if err != nil {
		return Diagnosis{}, fmt.Errorf("load diagnostic photo: %w", err)
	}
	// 图片角色沿用旧线资产类型语义（photoKindName 透传）：穿搭=outfit，购买=product。
	image.Role = input.Kind
	if input.Kind == "purchase" {
		image.Role = "product"
	}
	output, err := s.advisor.Diagnose(ctx, DiagnosticRequest{
		Kind: input.Kind, Scene: input.Scene, MediaAssetID: input.MediaAssetID,
		Images: []Image{image},
		Report: grounding.Report, Profile: grounding.Profile, Wardrobe: grounding.Wardrobe,
	})
	if err != nil {
		return Diagnosis{}, err
	}
	return s.writer.InsertDiagnostic(ctx, userID, Diagnosis{
		Kind: input.Kind, Scene: input.Scene,
		Conclusion:    output.Conclusion,
		PriorityTitle: output.PriorityTitle,
		PriorityCopy:  output.PriorityCopy,
		Tags:          output.Tags,
		Findings:      output.Findings,
		Options:       output.Options,
		MediaAssetID:  input.MediaAssetID,
		CreatedAt:     time.Now().UTC(),
	})
}

func (s *Service) Get(ctx context.Context, userID string, id string) (Diagnosis, error) {
	d, err := s.writer.GetDiagnosticByID(ctx, userID, id)
	if err != nil {
		return Diagnosis{}, err
	}
	return d, s.signSourceMedia(ctx, &d)
}

func (s *Service) Latest(ctx context.Context, userID string, kind string) (Diagnosis, error) {
	d, err := s.writer.GetLatestDiagnosticByKind(ctx, userID, kind)
	if err != nil {
		return Diagnosis{}, err
	}
	return d, s.signSourceMedia(ctx, &d)
}

func (s *Service) SetSaved(ctx context.Context, userID string, id string, saved bool) (Diagnosis, error) {
	d, err := s.writer.UpdateDiagnosticSaved(ctx, userID, id, saved)
	if err != nil {
		return Diagnosis{}, err
	}
	return d, s.signSourceMedia(ctx, &d)
}

// signSourceMedia 给诊断源照片补可读 URL：复访恢复（GET latest/{id}）时
// 客户端投影对空 url 一律拒渲染。未装配签名器时保持无 URL（单测/降级组装）。
func (s *Service) signSourceMedia(ctx context.Context, d *Diagnosis) error {
	if d.SourceMedia == nil || s.signer == nil || d.SourceMediaObjectKey == "" {
		return nil
	}
	url, expiresAt, err := s.signer.SignedURL(ctx, d.SourceMediaObjectKey)
	if err != nil {
		return err
	}
	d.SourceMedia.URL = url
	d.SourceMedia.URLExpiresAt = expiresAt
	if d.SourceMedia.MIMEType == "" {
		d.SourceMedia.MIMEType = d.SourceMediaMIMEType
	}
	return nil
}
