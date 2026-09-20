package daily

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

// pickSnapshot 是 prepare/generate 两段之间共用的选品快照。
// 不下发给客户端：它含用户历史摘要（方案 §2.3）。
type pickSnapshot struct {
	UserID       string
	GenDate      string
	Facts        []domain.KnowledgeFact
	FactIDs      []string
	Gene         domain.StyleGene
	GeneText     string
	Weather      Weather
	ContextText  string
	HistoryText  string
	Angle        string
	Category     string
	BucketCounts map[string]int
}

// pickStore 内存 LRU：prepare 存、generate 取，5 分钟有效。
// 多实例部署时换 Redis——接口不变（方案 §2.3）。
type pickStore struct {
	mu       sync.Mutex
	ttl      time.Duration
	capacity int
	items    map[string]*list.Element
	order    *list.List
	seq      uint64
}

type pickEntry struct {
	token    string
	userID   string
	snapshot pickSnapshot
	expireAt time.Time
}

func newPickStore(ttl time.Duration, capacity int) *pickStore {
	if capacity <= 0 {
		capacity = 512
	}
	return &pickStore{ttl: ttl, capacity: capacity, items: map[string]*list.Element{}, order: list.New()}
}

func (s *pickStore) put(userID string, snapshot pickSnapshot) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	token := hashToken(userID, snapshot.GenDate, s.seq)
	entry := &pickEntry{token: token, userID: userID, snapshot: snapshot, expireAt: time.Now().Add(s.ttl)}
	element := s.order.PushBack(entry)
	s.items[token] = element
	for s.order.Len() > s.capacity {
		oldest := s.order.Front()
		if oldest == nil {
			break
		}
		s.order.Remove(oldest)
		if old, ok := oldest.Value.(*pickEntry); ok {
			delete(s.items, old.token)
		}
	}
	return token
}

// take 取回并删除（一次性）：token 必须是该用户本人的。
func (s *pickStore) take(token string, userID string) (pickSnapshot, bool) {
	if token == "" {
		return pickSnapshot{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	element, ok := s.items[token]
	if !ok {
		return pickSnapshot{}, false
	}
	entry, ok := element.Value.(*pickEntry)
	if !ok {
		return pickSnapshot{}, false
	}
	s.order.Remove(element)
	delete(s.items, token)
	if entry.userID != userID || time.Now().After(entry.expireAt) {
		return pickSnapshot{}, false
	}
	return entry.snapshot, true
}

func hashToken(userID, genDate string, seq uint64) string {
	sum := sha256.Sum256([]byte(userID + "|" + genDate + "|" + strconv.FormatUint(seq, 10)))
	return hex.EncodeToString(sum[:16])
}

// Prepare 选品（纯规则，目标 < 50ms）：幂等检查 → 知识检索 → 抽 3~5 条 → 存快照。
func (s *Service) Prepare(ctx context.Context, userID string, city string) (PrepareResult, error) {
	genDate := s.today()
	result := PrepareResult{GenDate: genDate}

	// ① 幂等：当天已生成 → 客户端直接拉内容，不播动画。
	// 调试开关下跳过：让每次进首页都走完整选品 + 生成。
	if !s.forceRegen {
		if content, err := s.content.TodayContent(ctx, userID, genDate); err == nil {
			result.CacheHit = true
			result.Scenario = content.Category
			return result, nil
		} else if err != nil && !isNotFound(err) {
			// DB 异常不算致命：继续选品，失败会在 generate 里落到兜底。
			result.Scenario = ScenarioFallback
			return result, nil
		}
	} else if _, err := s.content.TodayContent(ctx, userID, genDate); err != nil && !isNotFound(err) {
		// DB 异常不算致命：继续选品，失败会在 generate 里落到兜底。
		result.Scenario = ScenarioFallback
		return result, nil
	}

	snapshot := s.selectFacts(ctx, userID, genDate, city)
	result.Scenario = snapshot.Category
	if snapshot.Category == "" {
		result.Scenario = ScenarioFallback
	}
	if len(snapshot.Facts) > 0 {
		result.PickToken = s.picks.put(userID, snapshot)
	}
	return result, nil
}

// selectFacts 规则选品：已确认事实 × 基因匹配 × 季节 − 近期已推，
// 权重 = 补最薄的那一格；同分按稳定哈希排序（同一天结果固定，不随机）。
func (s *Service) selectFacts(ctx context.Context, userID string, genDate string, city string) pickSnapshot {
	// 调试开关：给选品种子加一次性 nonce —— 否则「同一天固定」的稳定哈希
	// 会让每次重生成都选到同一批事实，等于没重生成。
	nonce := ""
	if s.forceRegen {
		nonce = strconv.FormatInt(s.clock.Now().UnixNano(), 10)
	}
	snapshot := pickSnapshot{UserID: userID, GenDate: genDate, Angle: s.angleFor(userID, genDate+nonce)}
	snapshot.Weather = s.currentWeather(ctx, city)
	snapshot.ContextText = contextText(genDate, snapshot.Weather)
	snapshot.GeneText = ""

	grounding, err := s.reader.ReadDailyGrounding(ctx, userID)
	if err != nil && !isNotFound(err) {
		return snapshot
	}
	gene := deriveGene(grounding)
	snapshot.Gene = gene
	snapshot.GeneText = geneText(gene)

	// 近期已推的事实与已收的内容：30 天内不重复。
	// 调试开关下放开事实去重：否则调试几次后候选被自己推过的记录吃光。
	since := s.clock.Now().AddDate(0, 0, -seenWindowDays)
	seenFacts, _ := s.content.RecentFactIDs(ctx, userID, since)
	if s.forceRegen {
		seenFacts = nil
	}
	seenKeys, _ := s.content.RecentContentKeys(ctx, userID, since)
	snapshot.HistoryText = historyText(seenKeys)

	buckets, err := s.collections.CountCollections(ctx, userID)
	if err != nil {
		buckets = map[string]int{}
	}
	snapshot.BucketCounts = buckets

	facts, err := s.knowledge.ListReviewedFacts(ctx, seenFacts, seasonOf(genDate))
	if err != nil {
		return snapshot
	}
	candidates := make([]domain.KnowledgeFact, 0, len(facts))
	for _, fact := range facts {
		if fact.GeneFit.Matches(gene) {
			candidates = append(candidates, fact)
		}
	}
	if len(candidates) == 0 {
		return snapshot
	}

	// 权重：格子里内容越少越优先（补最薄的格），再加一个稳定的日内抖动，
	// 保证「同一天固定、跨天会换」。
	thinnest := thinnestCount(buckets)
	seed := genDate + "|" + userID + nonce
	type scored struct {
		fact  domain.KnowledgeFact
		score float64
	}
	ranked := make([]scored, 0, len(candidates))
	for _, fact := range candidates {
		gap := float64(thinnest - (buckets[fact.Domain]))
		if gap < 0 {
			gap = 0
		}
		jitter := float64(stableHash(seed+"|"+fact.ID) % 100)
		ranked = append(ranked, scored{fact: fact, score: gap*100 + jitter})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].fact.ID < ranked[j].fact.ID
	})

	want := 3 + int(stableHash(seed)%3) // 3~5 条
	if want > len(ranked) {
		want = len(ranked)
	}
	picked := make([]domain.KnowledgeFact, 0, want)
	for _, item := range ranked[:want] {
		picked = append(picked, item.fact)
	}
	// 事实条目按领域聚类后取最多的那一格作为本条内容的分类（决定等待动画与手册格）。
	snapshot.Facts = picked
	snapshot.FactIDs = make([]string, 0, len(picked))
	for _, fact := range picked {
		snapshot.FactIDs = append(snapshot.FactIDs, fact.ID)
	}
	snapshot.Category = dominantDomain(picked, buckets, thinnest)
	return snapshot
}

// dominantDomain 优先补最薄的格：命中缺口领域的事实存在时直接用它；
// 否则取事实条数最多的领域（并列时按领域名字典序，保证稳定）。
func dominantDomain(facts []domain.KnowledgeFact, buckets map[string]int, thinnest int) string {
	if len(facts) == 0 {
		return ""
	}
	counts := map[string]int{}
	for _, fact := range facts {
		counts[fact.Domain]++
	}
	best := ""
	bestScore := -1
	domains := make([]string, 0, len(counts))
	for domain := range counts {
		domains = append(domains, domain)
	}
	sort.Strings(domains)
	for _, domain := range domains {
		gap := thinnest - (buckets[domain])
		if gap < 0 {
			gap = 0
		}
		score := gap*10 + counts[domain]
		if score > bestScore {
			bestScore = score
			best = domain
		}
	}
	return best
}

func thinnestCount(buckets map[string]int) int {
	min := 0
	first := true
	for _, category := range allCategories {
		count := buckets[category]
		if first || count < min {
			min = count
			first = false
		}
	}
	return min
}

// angles 选题角度枚举：轮换防同质化（方案 §2.4）。
var angles = []string{
	"讲清一个原理：为什么会这样",
	"给一个今天就能做的动作",
	"纠正常见误解：大家以为的不是真的",
	"给一个判断标准：以后自己能挑",
}

func (s *Service) angleFor(userID string, genDate string) string {
	if len(angles) == 0 {
		return ""
	}
	return angles[stableHash(userID+"|"+genDate+"|angle")%uint64(len(angles))]
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
	text := "今天：" + genDate
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

func historyText(seenKeys []string) string {
	if len(seenKeys) == 0 {
		return "近 30 天没有推过内容。"
	}
	if len(seenKeys) > 8 {
		seenKeys = seenKeys[:8]
	}
	return "近 30 天已推过：" + strings.Join(seenKeys, "、") + "。请换一个角度，不要重复。"
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
