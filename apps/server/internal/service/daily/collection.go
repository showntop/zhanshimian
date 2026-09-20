package daily

import (
	"context"
	"strings"
	"time"

	"github.com/zhanshimian/server/internal/domain"
)

// 收藏 = 内容引用 + 完整副本 + 多态素材 + 生命周期（docs/daily-collection-schema.md）。
//
// 副本是必需的：内容池会迭代（改文案、换素材、甚至下架），但用户手册里的
// 东西不能跟着变——他收下的是「当时的那条建议」。

const maxNoteRunes = 200
const maxCollectionLimit = 100

// CreateCollection 收下一条：服务端按 content_id 取回内容并固化副本
// （不信任客户端上报的文案），幂等键 (user_id, content_key)。
func (s *Service) CreateCollection(ctx context.Context, userID string, contentID string, note string) (domain.DailyCollection, error) {
	if strings.TrimSpace(contentID) == "" {
		return domain.DailyCollection{}, ErrValidation
	}
	if runes(note) > maxNoteRunes {
		return domain.DailyCollection{}, ErrValidation
	}
	content, err := s.content.ContentByID(ctx, userID, contentID)
	if err != nil {
		return domain.DailyCollection{}, err
	}
	item := domain.DailyCollection{
		UserID:     userID,
		ContentID:  content.ID,
		ContentKey: content.DedupeKey,
		Snapshot:   snapshotOf(content),
		Title:      content.Topic,
		Summary:    content.Lead,
		Category:   content.Category,
		Assets:     assetsOf(content),
		Context:    s.savedContext(ctx, userID),
		Gene:       s.savedGene(ctx, userID),
		Status:     domain.CollectionStatusSaved,
		Note:       strings.TrimSpace(note),
	}
	return s.collections.CreateCollection(ctx, item)
}

// ListCollections 手册按格拉取；category 为空表示全部。
func (s *Service) ListCollections(ctx context.Context, userID string, category string, limit int) ([]domain.DailyCollection, error) {
	if category != "" && !isCategory(category) {
		return nil, ErrValidation
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > maxCollectionLimit {
		limit = maxCollectionLimit
	}
	return s.collections.ListCollections(ctx, userID, category, limit)
}

// UpdateCollection 生命周期推进：收下 → 试过 → 留下了。
func (s *Service) UpdateCollection(ctx context.Context, userID string, id string, status string, note string) (domain.DailyCollection, error) {
	if strings.TrimSpace(id) == "" {
		return domain.DailyCollection{}, ErrValidation
	}
	if status != "" && status != domain.CollectionStatusSaved && status != domain.CollectionStatusTried && status != domain.CollectionStatusKept {
		return domain.DailyCollection{}, ErrValidation
	}
	if runes(note) > maxNoteRunes {
		return domain.DailyCollection{}, ErrValidation
	}
	return s.collections.UpdateCollection(ctx, userID, id, status, note)
}

func (s *Service) DeleteCollection(ctx context.Context, userID string, id string) error {
	if strings.TrimSpace(id) == "" {
		return ErrValidation
	}
	return s.collections.DeleteCollection(ctx, userID, id)
}

// CollectionStats 手册七格计数（选品补薄格也用它）。
func (s *Service) CollectionStats(ctx context.Context, userID string) (CollectionStats, error) {
	counts, err := s.collections.CountCollections(ctx, userID)
	if err != nil {
		return CollectionStats{}, err
	}
	normalized := map[string]int{}
	total := 0
	for _, category := range allCategories {
		count := counts[category]
		normalized[category] = count
		total += count
	}
	return CollectionStats{Counts: normalized, Total: total}, nil
}

// snapshotOf 固化副本：fit 已经是渲染后的字符串（生成时就按该用户基因渲染）。
func snapshotOf(content domain.DailyContent) domain.ContentSnapshot {
	return domain.ContentSnapshot{
		ID:       content.ID,
		Type:     domain.CategoryToContentType(content.Category),
		Topic:    content.Topic,
		Lead:     content.Lead,
		FitText:  content.FitText,
		Why:      content.Why,
		Visual:   content.Visual,
		Category: content.Category,
	}
}

// assetsOf 素材派生：色卡内容的色值即素材；其余素材类型（衣橱单品/方案/媒体）
// 由后续能力补充——引用已有实体 id，不复制数据。
func assetsOf(content domain.DailyContent) []domain.CollectionAsset {
	assets := []domain.CollectionAsset{}
	if content.Visual.Modality == "swatch" {
		colors := swatchColors(content.Visual)
		if len(colors) > 0 {
			assets = append(assets, domain.CollectionAsset{Kind: "colors", Colors: colors})
		}
	}
	return assets
}

func swatchColors(visual domain.ContentVisual) []string {
	raw, ok := visual.Spec["items"]
	if !ok {
		return nil
	}
	list, ok := raw.([]map[string]any)
	if !ok {
		return nil
	}
	colors := make([]string, 0, len(list))
	for _, item := range list {
		if tone, ok := item["tone"].(string); ok && tone != "" {
			colors = append(colors, tone)
		}
	}
	return colors
}

// savedContext 收下时的语境（天气/日期/季节），供回看「当时为什么收这条」。
func (s *Service) savedContext(ctx context.Context, userID string) domain.SavedContext {
	saved := domain.SavedContext{Season: seasonOf(s.today())}
	weather := s.currentWeather(ctx, "")
	if weather.Temperature != 0 {
		temperature := weather.Temperature
		saved.Temperature = &temperature
	}
	saved.Condition = weather.Condition
	saved.City = weather.City
	saved.DayType = dayTypeOf(s.clock.Now())
	saved.Schedule = scheduleOf(s.clock.Now())
	return saved
}

// savedGene 收下时的形象基因快照：判断哪些建议只在特定条件下成立。
func (s *Service) savedGene(ctx context.Context, userID string) map[string]string {
	grounding, err := s.reader.ReadDailyGrounding(ctx, userID)
	if err != nil && !isNotFound(err) {
		return nil
	}
	return deriveGene(grounding).Map()
}

func dayTypeOf(now time.Time) string {
	switch now.Weekday() {
	case time.Saturday, time.Sunday:
		return "weekend"
	default:
		return "weekday"
	}
}

// scheduleOf 红线：场景叫「日常」不叫「通勤」。
func scheduleOf(now time.Time) string {
	switch now.Weekday() {
	case time.Saturday, time.Sunday:
		return "休息"
	default:
		return "日常"
	}
}

func isCategory(value string) bool {
	for _, category := range allCategories {
		if category == value {
			return true
		}
	}
	return false
}
