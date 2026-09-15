package wardrobe

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

// ErrValidation 标记无效输入：httpapi 映射 400。
var ErrValidation = errors.New("wardrobe validation error")

// 单品类目白名单（旧线 CreateWardrobeItem 与 miniapp CATEGORIES 一致）。
var validCategories = map[string]bool{"top": true, "bottom": true, "outer": true, "shoes": true, "bag": true}

// 搭配备注默认文案（旧线 CreateWardrobeOutfit 同款）。
const defaultOutfitNote = "优先复用你常穿的单品，用颜色与比例完成这套表达。"

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
	media  MediaChecker
}

func New(reader Reader, writer Writer) *Service {
	return &Service{reader: reader, writer: writer}
}

// WithMediaSigner 装配读路径媒体签名器（与 assessment.WithBilling 同一链式做法）。
func (s *Service) WithMediaSigner(signer MediaSigner) *Service {
	s.signer = signer
	return s
}

// WithMediaChecker 装配单品照片的归属/用途校验（同一链式做法）。
// 未装配时带 media_id 的建档拒绝——归属校验不可降级跳过（防越权建档）。
func (s *Service) WithMediaChecker(checker MediaChecker) *Service {
	s.media = checker
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

// CreateItem 恢复旧线校验与默认值：name/color 必填、category 白名单、
// season/formality/scenes 缺省补齐；media 归属/用途不符一律 404（越权不泄露存在性）。
func (s *Service) CreateItem(ctx context.Context, userID string, input CreateItemInput) (Item, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Color = strings.TrimSpace(input.Color)
	if input.Name == "" || input.Color == "" || !validCategories[input.Category] {
		return Item{}, fmt.Errorf("%w: 请填写单品名称、类别与颜色", ErrValidation)
	}
	if input.Season == "" {
		input.Season = "all"
	}
	if input.Formality == "" {
		input.Formality = "proper"
	}
	if len(input.Scenes) == 0 {
		input.Scenes = []string{"daily"}
	}
	if input.MediaAssetID != "" {
		if s.media == nil {
			return Item{}, repository.ErrNotFound
		}
		if err := s.media.CheckWardrobeMedia(ctx, userID, input.MediaAssetID); err != nil {
			return Item{}, err
		}
	}
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
// 恢复旧线校验：标题 1–60 字、单品 1–12 件、item 归属不符一律 404、note 默认文案、
// 上下文快照缺省按当前日期推导；响应内嵌单品详情（契约 201 形状）。
func (s *Service) CreateOutfit(ctx context.Context, userID string, input CreateOutfitInput) (Outfit, error) {
	title := strings.TrimSpace(input.Title)
	if title == "" || len([]rune(title)) > 60 {
		return Outfit{}, fmt.Errorf("%w: 请填写 60 字以内的搭配名称", ErrValidation)
	}
	if len(input.ItemIDs) == 0 || len(input.ItemIDs) > 12 {
		return Outfit{}, fmt.Errorf("%w: 请选择 1–12 件单品组成搭配", ErrValidation)
	}
	grounding, err := s.reader.ReadWardrobeGrounding(ctx, userID)
	if err != nil {
		return Outfit{}, err
	}
	selected := ""
	if grounding.SelectedPlan != nil {
		selected = grounding.SelectedPlan.ID
	}
	items, err := s.writer.GetWardrobeItems(ctx, userID)
	if err != nil {
		return Outfit{}, err
	}
	byID := make(map[string]Item, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}
	selectedItems := make([]Item, 0, len(input.ItemIDs))
	for _, id := range input.ItemIDs {
		item, ok := byID[id]
		if !ok {
			return Outfit{}, repository.ErrNotFound
		}
		if err := s.signItemMedia(ctx, &item); err != nil {
			return Outfit{}, err
		}
		selectedItems = append(selectedItems, item)
	}
	note := strings.TrimSpace(input.Note)
	if note == "" {
		note = defaultOutfitNote
	}
	outfit, err := s.writer.InsertWardrobeOutfit(ctx, userID, Outfit{
		Title:          title,
		Note:           note,
		Context:        defaultOutfitContext(time.Now()),
		ItemIDs:        input.ItemIDs,
		SelectedPlanID: selected,
		CreatedAt:      time.Now().UTC(),
	})
	if err != nil {
		return outfit, err
	}
	outfit.Items = selectedItems
	return outfit, nil
}

// defaultOutfitContext 固化创建时的上下文快照：旧线快照含天气，新线衣橱服务
// 不依赖天气源，快照保留日期/日期类型/日程默认推导（与 today 服务同一措辞：
// 工作日「日常」、周末「休息」，不叫「通勤」）。
func defaultOutfitContext(now time.Time) domain.TodayContext {
	ctxOut := domain.TodayContext{Date: now.Format("2006-01-02")}
	switch now.Weekday() {
	case time.Saturday, time.Sunday:
		ctxOut.DayType = "周末"
		ctxOut.Schedule = "休息"
	default:
		ctxOut.DayType = "工作日"
		ctxOut.Schedule = "日常"
	}
	return ctxOut
}

func (s *Service) WearOutfit(ctx context.Context, userID string, id string) (Outfit, error) {
	return s.writer.SetWardrobeOutfitWorn(ctx, userID, id)
}
