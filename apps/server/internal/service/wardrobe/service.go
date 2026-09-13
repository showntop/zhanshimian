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
}

func New(reader Reader, writer Writer) *Service {
	return &Service{reader: reader, writer: writer}
}

func (s *Service) ListItems(ctx context.Context, userID string) ([]Item, error) {
	return s.writer.GetWardrobeItems(ctx, userID)
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
