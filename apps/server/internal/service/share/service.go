package share

import (
	"context"
	"errors"
	"time"

	"github.com/zhanshimian/server/internal/domain"
)

// Snapshot 是不可变分享快照：资产身份而非 URL（计划 TestCreateSnapshotStoresAssetIdentityNotURL）。
type Snapshot struct {
	Title        string `json:"title"`
	Summary      string `json:"summary"`
	AssetID      string `json:"asset_id"`
	SourceKind   string `json:"source_kind"`
	DisplayLabel string `json:"display_label"`
}

// Card 是已创建的分享卡。
type Card struct {
	ID           string                  `json:"id"`
	Token        string                  `json:"token"`
	SourceType   string                  `json:"source_type"`
	SourceID     string                  `json:"source_id"`
	Snapshot     Snapshot                `json:"snapshot"`
	Media        *domain.RenderMediaView `json:"media,omitempty"`
	IncludePhoto bool                    `json:"include_photo"`
	Revoked      bool                    `json:"revoked"`
	ExpiresAt    time.Time               `json:"expires_at"`
	CreatedAt    time.Time               `json:"created_at"`

	// ObjectKey/MIMEType 由读取侧 join media_assets 得到；创建响应里为空。
	ObjectKey string `json:"-"`
	MIMEType  string `json:"-"`
}

// PublicView 是公开读取的投影：媒体按当前 object key 即时签名。
type PublicView struct {
	SourceType   string                  `json:"source_type"`
	Snapshot     Snapshot                `json:"snapshot"`
	Media        *domain.RenderMediaView `json:"media,omitempty"`
	IncludePhoto bool                    `json:"include_photo"`
	ExpiresAt    time.Time               `json:"expires_at"`
}

var ErrShareGone = errors.New("share no longer available")

type CreateInput struct {
	SourceType   string // plan_variant | today_plan
	SourceID     string
	IncludePhoto bool
}

// URLSigner 在读取时对不可变 object key 即时签名。
type URLSigner interface {
	SignedURL(ctx context.Context, objectKey string, ttl time.Duration) (string, time.Time, error)
}

// Writer 持久化分享卡。
type Writer interface {
	InsertShare(ctx context.Context, userID string, source Source, snapshot Snapshot, includePhoto bool) (Card, error)
	GetPublicShareByToken(ctx context.Context, token string) (Card, error)
	RevokeShareByID(ctx context.Context, userID string, id string) error
}

type Service struct {
	reader Reader
	writer Writer
	signer URLSigner
	ttl    time.Duration
}

func New(reader Reader, writer Writer, signer URLSigner, ttl time.Duration) *Service {
	return &Service{reader: reader, writer: writer, signer: signer, ttl: ttl}
}

// Create 读取来源的发布态资产，落一份不可变快照；来源未发布返回 ErrNotFound。
// 创建响应同样回填签名媒体：创建者立刻看到自己的分享卡（与公开读取同一呈现）。
func (s *Service) Create(ctx context.Context, userID string, input CreateInput) (Card, error) {
	source, err := s.reader.ReadShareSource(ctx, userID, input.SourceType, input.SourceID)
	if err != nil {
		return Card{}, err
	}
	card, err := s.writer.InsertShare(ctx, userID, source, Snapshot{
		Title: source.Title, Summary: source.Summary,
		AssetID: source.AssetID, SourceKind: string(source.SourceKind), DisplayLabel: source.DisplayLabel,
	}, input.IncludePhoto)
	if err != nil {
		return Card{}, err
	}
	media, err := s.signMedia(ctx, source.AssetID, source.ObjectKey, source.MIMEType, source.SourceKind, source.DisplayLabel)
	if err != nil {
		return Card{}, err
	}
	card.Media = media
	return card, nil
}

// GetPublic 按 token 读公开分享；快照里的 asset_id 指向的媒体必须仍是 published，
// 然后按当前 object key 即时签名。撤销/过期/资产不再发布一律 ErrNotFound。
func (s *Service) GetPublic(ctx context.Context, token string) (PublicView, error) {
	card, err := s.writer.GetPublicShareByToken(ctx, token)
	if err != nil {
		return PublicView{}, err
	}
	if card.Revoked || time.Now().After(card.ExpiresAt) {
		return PublicView{}, ErrShareGone
	}
	view := PublicView{
		SourceType: card.SourceType, Snapshot: card.Snapshot,
		IncludePhoto: card.IncludePhoto, ExpiresAt: card.ExpiresAt,
	}
	media, err := s.signMedia(ctx, card.Snapshot.AssetID, card.ObjectKey, card.MIMEType,
		domain.MediaSourceKind(card.Snapshot.SourceKind), card.Snapshot.DisplayLabel)
	if err != nil {
		return PublicView{}, err
	}
	view.Media = media
	return view, nil
}

// signMedia 按当前 object key 即时签名；无签名器或无资产时无媒体（纯文本卡）。
// MIMEType 必须随 URL 一起下发：客户端投影对 generated 类强制 image/jpeg。
func (s *Service) signMedia(ctx context.Context, assetID, objectKey, mimeType string, sourceKind domain.MediaSourceKind, displayLabel string) (*domain.RenderMediaView, error) {
	if s.signer == nil || assetID == "" || objectKey == "" {
		return nil, nil
	}
	url, expiresAt, err := s.signer.SignedURL(ctx, objectKey, s.ttl)
	if err != nil {
		return nil, err
	}
	return &domain.RenderMediaView{
		AssetID: assetID, URL: url, URLExpiresAt: expiresAt, MIMEType: mimeType,
		SourceKind: string(sourceKind), DisplayLabel: displayLabel,
	}, nil
}

// Revoke 撤销分享；撤销后公开读取 404。
func (s *Service) Revoke(ctx context.Context, userID string, id string) error {
	return s.writer.RevokeShareByID(ctx, userID, id)
}
