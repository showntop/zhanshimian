package wardrobe

import (
	"context"
	"time"

	"github.com/zhanshimian/server/internal/domain"
)

// Item 是持久化的衣橱单品（OpenAPI WardrobeItem 形状）。
type Item struct {
	ID           string                  `json:"id"`
	MediaAssetID string                  `json:"-"`
	Media        *domain.RenderMediaView `json:"media"`
	Name         string                  `json:"name"`
	Category     string                  `json:"category"`
	Color        string                  `json:"color"`
	Season       string                  `json:"season"`
	Formality    string                  `json:"formality"`
	Scenes       []string                `json:"scenes"`
	Favorite     bool                    `json:"favorite"`
	WearCount    int                     `json:"wear_count"`
	CreatedAt    time.Time               `json:"created_at"`
	UpdatedAt    time.Time               `json:"updated_at"`

	// MediaObjectKey 由读取侧 join media_assets 得到，供呈现层即时签名
	// （与 share.Card.ObjectKey 同一做法）；不出现在 JSON。
	MediaObjectKey string `json:"-"`
}

// Outfit 是持久化的衣橱组合（OpenAPI WardrobeOutfit 形状）。
type Outfit struct {
	ID             string              `json:"id"`
	Title          string              `json:"title"`
	Note           string              `json:"note"`
	Context        domain.TodayContext `json:"context"`
	ItemIDs        []string            `json:"item_ids"`
	Items          []Item              `json:"items"`
	SelectedPlanID string              `json:"-"`
	Worn           bool                `json:"worn"`
	CreatedAt      time.Time           `json:"created_at"`
}

type CreateItemInput struct {
	MediaAssetID string
	Name         string
	Category     string
	Color        string
	Season       string
	Formality    string
	Scenes       []string
}

type CreateOutfitInput struct {
	Title   string
	Note    string
	ItemIDs []string
}

// Writer 持久化与读取衣橱单品与组合。
type Writer interface {
	GetWardrobeItems(ctx context.Context, userID string) ([]Item, error)
	InsertWardrobeItem(ctx context.Context, userID string, item Item) (Item, error)
	RemoveWardrobeItem(ctx context.Context, userID string, id string) error
	InsertWardrobeOutfit(ctx context.Context, userID string, outfit Outfit) (Outfit, error)
	SetWardrobeOutfitWorn(ctx context.Context, userID string, id string) (Outfit, error)
}

type Service struct {
	reader Reader
	writer Writer
	signer MediaSigner
}

func New(reader Reader, writer Writer) *Service {
	return &Service{reader: reader, writer: writer}
}

// WithMediaSigner 装配读路径媒体签名器（与 assessment.WithBilling 同一链式做法）。
func (s *Service) WithMediaSigner(signer MediaSigner) *Service {
	s.signer = signer
	return s
}

func (s *Service) ListItems(ctx context.Context, userID string) ([]Item, error) {
	items, err := s.writer.GetWardrobeItems(ctx, userID)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if err := s.signItemMedia(ctx, &items[i]); err != nil {
			return nil, err
		}
	}
	return items, nil
}

// signItemMedia 给单品照片补可读 URL：客户端投影对空 url 一律拒渲染
// （衣橱单品照片不显示的根因）。未装配签名器时保持无 URL（单测/降级组装）。
func (s *Service) signItemMedia(ctx context.Context, item *Item) error {
	if item.Media == nil || s.signer == nil || item.MediaObjectKey == "" {
		return nil
	}
	url, expiresAt, err := s.signer.SignedURL(ctx, item.MediaObjectKey)
	if err != nil {
		return err
	}
	item.Media.URL = url
	item.Media.URLExpiresAt = expiresAt
	return nil
}

func (s *Service) CreateItem(ctx context.Context, userID string, input CreateItemInput) (Item, error) {
	now := time.Now().UTC()
	return s.writer.InsertWardrobeItem(ctx, userID, Item{
		MediaAssetID: input.MediaAssetID,
		Name:         input.Name, Category: input.Category, Color: input.Color,
		Season: input.Season, Formality: input.Formality, Scenes: input.Scenes,
		CreatedAt: now, UpdatedAt: now,
	})
}

func (s *Service) DeleteItem(ctx context.Context, userID string, id string) error {
	return s.writer.RemoveWardrobeItem(ctx, userID, id)
}

// CreateOutfit 从质量核心 grounding 取稳定方案摘要，落一份组合快照；不写 PlanSet/RenderPublication。
func (s *Service) CreateOutfit(ctx context.Context, userID string, input CreateOutfitInput) (Outfit, error) {
	grounding, err := s.reader.ReadWardrobeGrounding(ctx, userID)
	if err != nil {
		return Outfit{}, err
	}
	selected := ""
	if grounding.SelectedPlan != nil {
		selected = grounding.SelectedPlan.ID
	}
	return s.writer.InsertWardrobeOutfit(ctx, userID, Outfit{
		Title:          input.Title,
		Note:           input.Note,
		ItemIDs:        input.ItemIDs,
		SelectedPlanID: selected,
		CreatedAt:      time.Now().UTC(),
	})
}

func (s *Service) WearOutfit(ctx context.Context, userID string, id string) (Outfit, error) {
	return s.writer.SetWardrobeOutfitWorn(ctx, userID, id)
}
