package diagnostic

import (
	"context"
	"time"

	"github.com/zhanshimian/server/internal/domain"
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
}

type Service struct {
	reader  Reader
	writer  Writer
	advisor Advisor
	signer  MediaSigner
}

func New(reader Reader, writer Writer, advisor Advisor) *Service {
	return &Service{reader: reader, writer: writer, advisor: advisor}
}

// WithMediaSigner 装配读路径媒体签名器（与 assessment.WithBilling 同一链式做法）。
func (s *Service) WithMediaSigner(signer MediaSigner) *Service {
	s.signer = signer
	return s
}

// Run 同步跑一次诊断：grounding → AI → 落库。AI 失败必须返回错误，
// 绝不落一条模板结果（计划 TestDiagnosisProviderFailureNeverReturnsTemplate）。
func (s *Service) Run(ctx context.Context, userID string, input RunInput) (Diagnosis, error) {
	grounding, err := s.reader.ReadDiagnosticGrounding(ctx, userID, input.ReportID, input.Kind)
	if err != nil {
		return Diagnosis{}, err
	}
	output, err := s.advisor.Diagnose(ctx, DiagnosticRequest{
		Kind: input.Kind, Scene: input.Scene, MediaAssetID: input.MediaAssetID,
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
