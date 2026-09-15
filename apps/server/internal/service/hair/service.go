package hair

import (
	"context"
	"time"

	"github.com/zhanshimian/server/internal/domain"
)

// Preview 是持久化的发型预览（OpenAPI HairPreview 形状）。
type Preview struct {
	ID          string                  `json:"id"`
	StyleID     string                  `json:"style_id"`
	StyleName   string                  `json:"style_name"`
	State       string                  `json:"state"`
	Retryable   bool                    `json:"retryable"`
	Operation   domain.OperationRef     `json:"operation"`
	SourceMedia *domain.RenderMediaView `json:"source_media"`
	Media       *domain.RenderMediaView `json:"media"`
	Saved       bool                    `json:"saved"`
	CreatedAt   time.Time               `json:"created_at"`
	UpdatedAt   time.Time               `json:"updated_at"`

	// object key 仅供读路径即时签名，不外发（库不存签名 URL）。
	SourceObjectKey string `json:"-"`
	ResultObjectKey string `json:"-"`
}

// states 与渲染状态机一致。
const (
	StateQueued      = "queued"
	StateGenerating  = "generating"
	StateChecking    = "checking"
	StateReady       = "ready"
	StateFailed      = "failed"
	StateUnavailable = "unavailable"
)

// inFlightStates 是「进行中」状态集：客户端据此恢复任务展示（与契约
// state=active 过滤同义）。
var inFlightStates = []string{StateQueued, StateGenerating, StateChecking}

type CreatePreviewInput struct {
	ReportID     string
	MediaAssetID string
	StyleID      string
}

// ListFilter 是 GET /v1/hair-previews 的契约过滤：无参返回全部历史，
// saved 只留已保存/未保存，active 只留进行中。
type ListFilter struct {
	Saved  *bool
	Active bool
}

// CreateRunParams 是一次预览创建的全部落库输入。
type CreateRunParams struct {
	StyleID       string
	StyleName     string
	SourceAssetID string
	MaxAttempts   int
}

// TaskMaxAttempts 是 hair_preview 任务的重试预算：tasks.max_attempts 与
// worker Definition 必须一致（此处为唯一事实来源）。
const TaskMaxAttempts = 3

// RunCreator 在一个事务边界内创建预览行、公开 Operation 与首个内容任务。
type RunCreator interface {
	CreateHairPreviewRun(ctx context.Context, userID string, params CreateRunParams) (Preview, domain.OperationRef, error)
}

// Writer 持久化发型预览读模型。
type Writer interface {
	GetHairPreviewRow(ctx context.Context, userID string, id string) (Preview, error)
	GetInFlightHairPreview(ctx context.Context, userID string) (Preview, error)
	ListHairPreviewRows(ctx context.Context, userID string, filter ListFilter) ([]Preview, error)
	MarkHairPreviewSavedByID(ctx context.Context, userID string, id string) (Preview, error)
}

// Hairstyler 是发型推荐（GET /v1/hairstyles）的 AI 能力接口。
type Hairstyler interface {
	RecommendHairstyles(ctx context.Context, userID string) ([]domain.HairStyle, error)
}

type Service struct {
	reader     Reader
	writer     Writer
	runner     RunCreator
	hairstyler Hairstyler
	signer     MediaSigner
}

func New(reader Reader, writer Writer, runner RunCreator, hairstyler Hairstyler, signer MediaSigner) *Service {
	return &Service{reader: reader, writer: writer, runner: runner, hairstyler: hairstyler, signer: signer}
}

// CreatePreview 建预览行 + 公开 Operation + hair_preview 任务（单事务）。
// 响应里 media 永远为 null：图像生成落库前不可见。
// 正脸来源：客户端显式上传的 media_id 优先（demo 照片按 demo_example 投影），
// 否则回退报告建档 face；两者都没有时报 ErrFaceMissing。
func (s *Service) CreatePreview(ctx context.Context, userID string, input CreatePreviewInput) (Preview, domain.OperationRef, error) {
	source, sourceAssetID, sourceObjectKey, err := s.resolveFace(ctx, userID, input)
	if err != nil {
		return Preview{}, domain.OperationRef{}, err
	}
	preview, operation, err := s.runner.CreateHairPreviewRun(ctx, userID, CreateRunParams{
		StyleID:       input.StyleID,
		StyleName:     s.styleName(ctx, userID, input.StyleID),
		SourceAssetID: sourceAssetID,
		MaxAttempts:   TaskMaxAttempts,
	})
	if err != nil {
		return Preview{}, domain.OperationRef{}, err
	}
	preview.SourceMedia = source
	preview.SourceObjectKey = sourceObjectKey
	preview.Operation = operation
	return preview, operation, s.signPreviews(ctx, &preview)
}

func (s *Service) resolveFace(ctx context.Context, userID string, input CreatePreviewInput) (*domain.RenderMediaView, string, string, error) {
	if input.MediaAssetID != "" {
		asset, err := s.reader.ReadFaceMedia(ctx, userID, input.MediaAssetID)
		if err != nil {
			return nil, "", "", err
		}
		return &domain.RenderMediaView{
			AssetID:      asset.ID,
			MIMEType:     asset.MIMEType,
			SourceKind:   string(domain.SourceKindOf(asset.Origin)),
			DisplayLabel: domain.DisplayLabelOf(asset.DisplayKind),
		}, asset.ID, asset.ObjectKey, nil
	}
	if input.ReportID == "" {
		return nil, "", "", ErrFaceMissing
	}
	grounding, err := s.reader.ReadHairGrounding(ctx, userID, input.ReportID)
	if err != nil {
		return nil, "", "", err
	}
	if grounding.Face.AssetID == "" {
		return nil, "", "", ErrFaceMissing
	}
	return &domain.RenderMediaView{
		AssetID:      grounding.Face.AssetID,
		MIMEType:     grounding.Face.MIMEType,
		SourceKind:   string(domain.SourceKindOf(domain.MediaOriginUserUpload)),
		DisplayLabel: domain.DisplayLabelOf(domain.DisplayKindOriginal),
	}, grounding.Face.AssetID, grounding.Face.ObjectKey, nil
}

// styleName 从目录解析展示名；目录缺失/未收录时保持空串（预览照常创建）。
func (s *Service) styleName(ctx context.Context, userID string, styleID string) string {
	if styleID == "" {
		return ""
	}
	styles, err := s.hairstyler.RecommendHairstyles(ctx, userID)
	if err != nil {
		return ""
	}
	for _, style := range styles {
		if style.ID == styleID {
			return style.Name
		}
	}
	return ""
}

func (s *Service) Get(ctx context.Context, userID string, id string) (Preview, error) {
	preview, err := s.writer.GetHairPreviewRow(ctx, userID, id)
	if err != nil {
		return Preview{}, err
	}
	return preview, s.signPreviews(ctx, &preview)
}

func (s *Service) Active(ctx context.Context, userID string) (Preview, error) {
	preview, err := s.writer.GetInFlightHairPreview(ctx, userID)
	if err != nil {
		return Preview{}, err
	}
	return preview, s.signPreviews(ctx, &preview)
}

// List 按契约返回预览历史（新到旧）：无过滤返回全部（含进行中）。
func (s *Service) List(ctx context.Context, userID string, filter ListFilter) ([]Preview, error) {
	items, err := s.writer.ListHairPreviewRows(ctx, userID, filter)
	if err != nil {
		return nil, err
	}
	refs := make([]*Preview, 0, len(items))
	for i := range items {
		refs = append(refs, &items[i])
	}
	if err := s.signPreviews(ctx, refs...); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Service) Save(ctx context.Context, userID string, id string) (Preview, error) {
	preview, err := s.writer.MarkHairPreviewSavedByID(ctx, userID, id)
	if err != nil {
		return Preview{}, err
	}
	return preview, s.signPreviews(ctx, &preview)
}

// signPreviews 给读模型即时签 URL；签名能力缺失时保持空 URL（本地无
// PUBLIC_BASE_URL 的降级场景），绝不回退到别的图。
func (s *Service) signPreviews(ctx context.Context, previews ...*Preview) error {
	if s.signer == nil {
		return nil
	}
	for _, preview := range previews {
		if preview.SourceMedia != nil && preview.SourceObjectKey != "" {
			if err := s.signInto(ctx, preview.SourceObjectKey, preview.SourceMedia); err != nil {
				return err
			}
		}
		if preview.Media != nil && preview.ResultObjectKey != "" {
			if err := s.signInto(ctx, preview.ResultObjectKey, preview.Media); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) signInto(ctx context.Context, objectKey string, view *domain.RenderMediaView) error {
	url, expiresAt, err := s.signer.SignedURL(ctx, objectKey)
	if err != nil {
		return err
	}
	view.URL = url
	view.URLExpiresAt = expiresAt
	return nil
}

// Recommend 返回发型目录（GET /v1/hairstyles）。目录是产品事实；
// 缩略图与预览同一签名链路。
func (s *Service) Recommend(ctx context.Context, userID string, reportID string) ([]domain.HairStyle, error) {
	styles, err := s.hairstyler.RecommendHairstyles(ctx, userID)
	if err != nil {
		return nil, err
	}
	if s.signer != nil {
		for i := range styles {
			if styles[i].MediaObjectKey == "" {
				continue
			}
			if err := s.signInto(ctx, styles[i].MediaObjectKey, &styles[i].Media); err != nil {
				return nil, err
			}
		}
	}
	return styles, nil
}

// ErrFaceMissing 表示没有可用的正脸照（既无显式 media_id 也无报告 face 槽）。
var ErrFaceMissing = errFaceMissingType{}

type errFaceMissingType struct{}

func (errFaceMissingType) Error() string { return "hair preview requires a face photo" }
