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

type CreatePreviewInput struct {
	ReportID     string
	MediaAssetID string
	StyleID      string
}

// OperationStarter 在一个事务边界内创建公开 Operation 与首个内容任务。
type OperationStarter interface {
	StartPreviewOperation(ctx context.Context, userID string, previewID string) (domain.OperationRef, error)
}

// Renderer 是发型预览的异步生成端口：Worker 侧调用；结果必须先落
// quarantined candidate，过渲染质量门禁发布后才对用户可见。
type Renderer interface {
	Generate(ctx context.Context, userID string, previewID string) error
}

// Writer 持久化发型预览。
type Writer interface {
	InsertHairPreview(ctx context.Context, userID string, preview Preview) (Preview, error)
	GetHairPreviewRow(ctx context.Context, userID string, id string) (Preview, error)
	GetInFlightHairPreview(ctx context.Context, userID string) (Preview, error)
	ListSavedHairPreviewRows(ctx context.Context, userID string) ([]Preview, error)
	MarkHairPreviewSavedByID(ctx context.Context, userID string, id string) (Preview, error)
}

// Hairstyler 是发型推荐（GET /v1/hairstyles）的 AI 能力接口。
type Hairstyler interface {
	RecommendHairstyles(ctx context.Context, userID string) ([]domain.HairStyle, error)
}

type Service struct {
	reader     Reader
	writer     Writer
	starter    OperationStarter
	hairstyler Hairstyler
}

func New(reader Reader, writer Writer, starter OperationStarter, hairstyler Hairstyler) *Service {
	return &Service{reader: reader, writer: writer, starter: starter, hairstyler: hairstyler}
}

// CreatePreview 建预览行 + 公开 Operation（kind=render, subject_type=hair_preview）。
// 响应里 media 永远为 null：图像只有过了质量门禁发布后才可见。
func (s *Service) CreatePreview(ctx context.Context, userID string, input CreatePreviewInput) (Preview, domain.OperationRef, error) {
	grounding, err := s.reader.ReadHairGrounding(ctx, userID, input.ReportID)
	if err != nil {
		return Preview{}, domain.OperationRef{}, err
	}
	if grounding.Face.AssetID == "" {
		return Preview{}, domain.OperationRef{}, ErrFaceMissing
	}
	preview := Preview{
		StyleID: input.StyleID,
		State:   StateQueued,
		SourceMedia: &domain.RenderMediaView{
			AssetID: grounding.Face.AssetID, SourceKind: "user_original", DisplayLabel: "原本",
		},
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	created, err := s.writer.InsertHairPreview(ctx, userID, preview)
	if err != nil {
		return Preview{}, domain.OperationRef{}, err
	}
	operation, err := s.starter.StartPreviewOperation(ctx, userID, created.ID)
	if err != nil {
		return Preview{}, domain.OperationRef{}, err
	}
	created.Operation = operation
	return created, operation, nil
}

func (s *Service) Get(ctx context.Context, userID string, id string) (Preview, error) {
	return s.writer.GetHairPreviewRow(ctx, userID, id)
}

func (s *Service) Active(ctx context.Context, userID string) (Preview, error) {
	return s.writer.GetInFlightHairPreview(ctx, userID)
}

func (s *Service) ListSaved(ctx context.Context, userID string) ([]Preview, error) {
	return s.writer.ListSavedHairPreviewRows(ctx, userID)
}

func (s *Service) Save(ctx context.Context, userID string, id string) (Preview, error) {
	return s.writer.MarkHairPreviewSavedByID(ctx, userID, id)
}

// Recommend 走 hair.Reader 的 grounding 生成发型推荐。
// Recommend 返回发型目录（GET /v1/hairstyles）。目录是产品事实；
// grounding 读取作为存在性校验（无报告照样可以浏览目录，所以忽略 NotFound）。
func (s *Service) Recommend(ctx context.Context, userID string, reportID string) ([]domain.HairStyle, error) {
	if _, err := s.reader.ReadHairGrounding(ctx, userID, reportID); err != nil {
		return s.hairstyler.RecommendHairstyles(ctx, userID)
	}
	return s.hairstyler.RecommendHairstyles(ctx, userID)
}

// ErrFaceMissing 表示没有可用的正脸照（GET grounding 未命中 face 槽）。
var ErrFaceMissing = errFaceMissingType{}

type errFaceMissingType struct{}

func (errFaceMissingType) Error() string { return "hair preview requires a face photo" }
