package daily

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

// generateContext 一次成文的全部语境（buildContext 聚齐后注入 prompt）。
type generateContext struct {
	UserID  string
	GenDate string
	City    string
	// ContextText 日期/星期/季节/天气（一行）。
	ContextText string
	// GeneText 用户画像（中性特征词）。
	GeneText string
	// HistoryText 近期已推内容清单。
	HistoryText string
	// InterestText 收藏信号。
	InterestText string
	// recentTopics 近 window 天的 topic（去重闸用）。
	recentTopics []string
	// ReferenceFacts 检索注入的参考事实（可用可不用）。
	ReferenceFacts []ReferenceFact
	// referenceIDs 参考事实的 ID（落 fact_ids，审计：给了模型什么）。
	referenceIDs []string
}

// Prepare 纯缓存探测（一次 DB 读）：当天已生成 → cache_hit=true、
// scenario=当日分类（客户端不播动画直接拉内容）；未命中 → scenario 空串。
// 选题已移到 generate 的 LLM 调用里，这里只回答「今天有没有」。
func (s *Service) Prepare(ctx context.Context, userID string, city string) (PrepareResult, error) {
	genDate := s.today()
	result := PrepareResult{GenDate: genDate}

	content, err := s.content.TodayContent(ctx, userID, genDate)
	if err == nil {
		result.CacheHit = true
		result.Scenario = content.Category
		return result, nil
	}
	// 未命中、以及 DB 异常，都照给巡游脚本：巡游与内容、与 DB 都无关，
	// 漏掉这一支客户端就只能播内置兜底（也就会看到那个圆圈）。
	// DB 异常本身不算致命——generate 里自然会落到兜底。
	//
	// variant 在这里一并下发：等待期与收敛期同一套种子，客户端不必本地另算。
	_ = city // 保留参数位：天气改在 generate 阶段按同一城市查询
	roam := roamPresentation(userID, genDate, s.motionVariantOverride)
	result.Presentation = &roam
	return result, nil
}

// buildContext 聚齐一次成文的语境：天气/画像/近推历史/收藏信号/参考事实。
// 全部毫秒级读 + 一次天气查询；任何一步失败都静默降级（缺哪块讲哪块）。
func (s *Service) buildContext(ctx context.Context, userID string, genDate string, city string) generateContext {
	gctx := generateContext{UserID: userID, GenDate: genDate, City: city}
	gctx.ContextText = contextText(genDate, s.currentWeather(ctx, city))
	gctx.GeneText = ""

	if grounding, err := s.reader.ReadDailyGrounding(ctx, userID); err == nil {
		gctx.GeneText = geneText(deriveGene(grounding))
	}

	// 近期已推：prompt 历史（去重主机制）+ topic 去重闸共用。
	since := s.clock.Now().AddDate(0, 0, -seenWindowDays)
	recent, err := s.content.RecentContents(ctx, userID, since, historyLimit)
	if err == nil {
		gctx.recentTopics = topicsOf(recent)
		gctx.HistoryText = historyText(recent)
	}

	if interest, err := s.interestText(ctx, userID); err == nil {
		gctx.InterestText = interest
	}

	// 参考事实：当季 + 基因匹配，稳定抽样（同一天固定，跨天会换）。
	gctx.ReferenceFacts, gctx.referenceIDs = s.referenceFacts(ctx, userID, genDate)
	return gctx
}

// interestText 收藏信号：格子里收了多少 + 最近收过什么 topic。
// 旧「补最薄的格」逻辑反转：不补缺口，贴用户已表现出的兴趣。
func (s *Service) interestText(ctx context.Context, userID string) (string, error) {
	buckets, err := s.collections.CountCollections(ctx, userID)
	if err != nil {
		return "", err
	}
	saved, err := s.collections.ListCollections(ctx, userID, "", 5)
	if err != nil {
		return "", err
	}
	parts := []string{}
	if len(buckets) > 0 {
		counts := make([]string, 0, len(allCategories))
		for _, category := range allCategories {
			if buckets[category] > 0 {
				counts = append(counts, categoryLabel(category)+"×"+strconv.Itoa(buckets[category]))
			}
		}
		if len(counts) > 0 {
			parts = append(parts, "收藏分布："+strings.Join(counts, "、"))
		}
	}
	titles := make([]string, 0, len(saved))
	for _, item := range saved {
		if item.Title != "" {
			titles = append(titles, item.Title)
		}
	}
	if len(titles) > 0 {
		parts = append(parts, "最近收下："+strings.Join(titles, "、"))
	}
	return strings.Join(parts, "；"), nil
}

// referenceFacts 当季 + 基因匹配的参考事实，按日种子稳定抽 referenceCount 条。
// excludeIDs 传空：事实是参考不是配额，近期用过也可以再参考（去重靠 topic 闸）。
func (s *Service) referenceFacts(ctx context.Context, userID string, genDate string) ([]ReferenceFact, []string) {
	facts, err := s.knowledge.ListReviewedFacts(ctx, nil, seasonOf(genDate))
	if err != nil {
		return nil, nil
	}
	grounding, _ := s.reader.ReadDailyGrounding(ctx, userID)
	gene := deriveGene(grounding)
	candidates := make([]domain.KnowledgeFact, 0, len(facts))
	for _, fact := range facts {
		if fact.GeneFit.Matches(gene) {
			candidates = append(candidates, fact)
		}
	}
	seed := genDate + "|" + userID
	want := referenceCount
	if want > len(candidates) {
		want = len(candidates)
	}
	// 稳定哈希排序后取前 N：同一天固定、跨天换批。
	ranked := make([]domain.KnowledgeFact, 0, len(candidates))
	ranked = append(ranked, candidates...)
	for i := len(ranked) - 1; i > 0; i-- { // 简单稳定 shuffle（按哈希权重）
		j := int(stableHash(seed+"|"+ranked[i].ID) % uint64(i+1))
		ranked[i], ranked[j] = ranked[j], ranked[i]
	}
	picked := ranked[:want]
	out := make([]ReferenceFact, 0, len(picked))
	ids := make([]string, 0, len(picked))
	for _, fact := range picked {
		out = append(out, ReferenceFact{Domain: fact.Domain, Fact: fact.Fact, Boundary: fact.Boundary})
		ids = append(ids, fact.ID)
	}
	return out, ids
}

func topicsOf(contents []domain.DailyContent) []string {
	topics := make([]string, 0, len(contents))
	for _, content := range contents {
		topics = append(topics, content.Topic)
	}
	return topics
}

// historyText 近期已推清单：模型据此换主题（去重主机制）。
func historyText(recent []domain.DailyContent) string {
	if len(recent) == 0 {
		return "近 14 天没有推过内容，可以从任何角度开始。"
	}
	lines := make([]string, 0, len(recent))
	for _, content := range recent {
		lines = append(lines, content.Topic+"（"+categoryLabel(content.Category)+"）")
	}
	return "近 14 天已推过：" + strings.Join(lines, "、") + "。今天必须换一个新主题，不要换措辞重复其中任何一条。"
}

func categoryLabel(category string) string {
	if label, ok := categoryLabels[category]; ok {
		return label
	}
	return category
}

var categoryLabels = map[string]string{
	"color":      "颜色",
	"fit":        "版型",
	"proportion": "比例",
	"fabric":     "面料",
	"occasion":   "场合",
	"howto":      "技巧",
	"outfit":     "搭配",
	"hair":       "发型",
	"makeup":     "妆容",
	"accessory":  "配饰",
	"general":    "综合",
}

func stableHash(value string) uint64 {
	sum := sha256.Sum256([]byte(value))
	return binary.BigEndian.Uint64(sum[:8])
}

func hex64(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:16])
}

// deriveGene 由稳定资料派生形象基因。V1 只认身高与体重两项，
// 其余维度保持中性——宁可让「不挑人」的事实被选中，也不编造特征。
// 全部为内部特征描述，不进入任何用户可见文案。
func deriveGene(grounding Grounding) domain.StyleGene {
	gene := domain.DefaultGene()
	if grounding.HeightCM > 0 {
		switch {
		case grounding.HeightCM < 160:
			gene.HeightBand = "petite"
		case grounding.HeightCM > 172:
			gene.HeightBand = "tall"
		default:
			gene.HeightBand = "average"
		}
	}
	if grounding.WeightKG != nil && *grounding.WeightKG > 0 && grounding.HeightCM > 0 {
		meters := float64(grounding.HeightCM) / 100
		bmi := *grounding.WeightKG / (meters * meters)
		switch {
		case bmi < 18.5:
			gene.Frame = "small"
		case bmi >= 24:
			gene.Frame = "large"
		default:
			gene.Frame = "medium"
		}
	}
	return gene
}

// geneText 给模型的用户特征描述：只用中性特征词，不做任何评价。
func geneText(gene domain.StyleGene) string {
	parts := []string{heightLabel(gene.HeightBand), frameLabel(gene.Frame)}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			out = append(out, part)
		}
	}
	return strings.Join(out, " · ")
}

func heightLabel(band string) string {
	switch band {
	case "petite":
		return "身高中等偏下"
	case "tall":
		return "身高中等偏上"
	case "average":
		return "身高中等"
	}
	return ""
}

func frameLabel(frame string) string {
	switch frame {
	case "small":
		return "骨架偏小"
	case "large":
		return "骨架偏大"
	case "medium":
		return "骨架适中"
	}
	return ""
}

func isNotFound(err error) bool { return errors.Is(err, repository.ErrNotFound) }

func (s *Service) currentWeather(ctx context.Context, city string) Weather {
	if s.weather == nil {
		return Weather{City: city}
	}
	weather, err := s.weather.Current(ctx, city)
	if err != nil {
		return Weather{City: city}
	}
	return weather
}

func contextText(genDate string, weather Weather) string {
	text := "今天：" + genDate + "（" + weekdayOf(genDate) + "，" + seasonLabel(genDate) + "）"
	if weather.Temperature != 0 {
		text += " · " + strconv.Itoa(weather.Temperature) + "°"
	}
	if weather.Condition != "" {
		text += " " + weather.Condition
	}
	if weather.City != "" {
		text += " · " + weather.City
	}
	return text
}

func weekdayOf(genDate string) string {
	if date, err := time.Parse("2006-01-02", genDate); err == nil {
		names := []rune("日一二三四五六")
		return "周" + string(names[int(date.Weekday())])
	}
	return ""
}

func seasonLabel(genDate string) string {
	switch seasonOf(genDate) {
	case "spring":
		return "春"
	case "summer":
		return "夏"
	case "autumn":
		return "秋"
	case "winter":
		return "冬"
	}
	return ""
}

func seasonOf(genDate string) string {
	if len(genDate) < 7 {
		return ""
	}
	month := genDate[5:7]
	switch month {
	case "03", "04", "05":
		return "spring"
	case "06", "07", "08":
		return "summer"
	case "09", "10", "11":
		return "autumn"
	case "12", "01", "02":
		return "winter"
	}
	return ""
}
