package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/example/jianwo/server/internal/domain"
)

var sceneLabels = map[string]string{
	"interview": "面试",
	"wedding":   "婚礼",
	"date":      "约会",
	"daily":     "日常",
}

var sceneImpressions = map[string]string{
	"energetic": "更有精神",
	"reliable":  "更可信",
	"natural":   "更自然",
	"memorable": "有记忆点",
}

// Scene-specific answer dictionaries keep the brief meaningful without making
// every occasion look like the same generic survey.
var sceneBriefOptions = map[string]map[string]map[string]string{
	"interview": {
		"when":        {"today": "今天", "three-days": "3 天内", "week": "1 周后", "later": "还没确定"},
		"format":      {"onsite": "线下面试", "video": "视频面试", "final": "终面 / 见客户"},
		"preparation": {"closet": "只用现有衣橱", "key-piece": "补一件关键单品", "complete": "可完整准备"},
		"impression":  sceneImpressions,
	},
	"wedding": {
		"role":       {"guest": "普通宾客", "bridal-party": "伴娘 / 伴郎", "family": "重要亲友", "speaker": "需要上台"},
		"timing":     {"lunch": "午间", "afternoon": "下午", "dinner": "晚宴", "unknown": "还没确定"},
		"dress-code": {"relaxed": "轻松婚礼", "elegant": "得体优雅", "formal": "正式礼服"},
		"impression": sceneImpressions,
	},
	"date": {
		"activity":    {"coffee": "咖啡 / 散步", "dinner": "晚餐", "exhibition": "展览 / 电影", "outdoor": "户外活动"},
		"timing":      {"afternoon": "下午", "evening": "傍晚", "night": "晚上", "unknown": "还没确定"},
		"preparation": {"closet": "只用现有衣橱", "key-piece": "添一点新意", "complete": "可完整准备"},
		"impression":  sceneImpressions,
	},
	"daily": {
		"activity":    {"commute": "通勤上班", "weekend": "周末出门", "friends": "朋友见面", "walk": "城市漫游"},
		"weather":     {"office": "空调房为主", "walking": "室外走很多", "rain": "可能下雨", "mild": "温和舒适"},
		"preparation": {"closet": "只用现有衣橱", "key-piece": "可添一件单品", "complete": "不设限"},
		"impression":  sceneImpressions,
	},
}

var sceneBriefFields = map[string][]string{
	"interview": {"when", "format", "preparation", "impression"},
	"wedding":   {"role", "timing", "dress-code", "impression"},
	"date":      {"activity", "timing", "preparation", "impression"},
	"daily":     {"activity", "weather", "preparation", "impression"},
}

func (s *Service) CreateScenePlans(ctx context.Context, userID, reportID string, input domain.ScenePlanInput) ([]domain.Plan, error) {
	answers, err := normalizedSceneAnswers(input)
	if err != nil {
		return nil, err
	}
	input.Answers = answers
	plans := buildScenePlans(input)
	created, err := s.repo.CreateScenePlans(ctx, userID, reportID, input, plans)
	if err != nil {
		return nil, err
	}
	for index := range created {
		created[index].ImageURL = s.absoluteURL(created[index].ImageURL)
	}
	return created, nil
}

func validateScenePlanInput(input domain.ScenePlanInput) error {
	_, err := normalizedSceneAnswers(input)
	return err
}

func normalizedSceneAnswers(input domain.ScenePlanInput) (map[string]string, error) {
	fields, ok := sceneBriefFields[input.Scene]
	if !ok {
		return nil, fmt.Errorf("%w: 不支持的使用场景", ErrValidation)
	}
	answers := make(map[string]string, len(fields))
	for key, value := range input.Answers {
		answers[key] = value
	}
	if len(answers) == 0 {
		answers = legacySceneAnswers(input)
	}
	for _, field := range fields {
		value := answers[field]
		if sceneBriefOptions[input.Scene][field][value] == "" {
			return nil, fmt.Errorf("%w: 请完成%s信息", ErrValidation, sceneLabels[input.Scene])
		}
	}
	return answers, nil
}

func legacySceneAnswers(input domain.ScenePlanInput) map[string]string {
	preparation := map[string]string{"low": "closet", "mid": "key-piece", "high": "complete"}[input.Budget]
	if preparation == "" {
		preparation = "key-piece"
	}
	impression := input.Impression
	if sceneImpressions[impression] == "" {
		impression = "reliable"
	}
	switch input.Scene {
	case "interview":
		when := input.Time
		if sceneBriefOptions["interview"]["when"][when] == "" {
			when = "week"
		}
		return map[string]string{"when": when, "format": "onsite", "preparation": preparation, "impression": impression}
	case "wedding":
		dressCode := input.Formality
		if dressCode == "proper" {
			dressCode = "elegant"
		}
		if sceneBriefOptions["wedding"]["dress-code"][dressCode] == "" {
			dressCode = "elegant"
		}
		return map[string]string{"role": "guest", "timing": "dinner", "dress-code": dressCode, "impression": impression}
	case "date":
		return map[string]string{"activity": "dinner", "timing": "evening", "preparation": preparation, "impression": impression}
	default:
		return map[string]string{"activity": "commute", "weather": "office", "preparation": preparation, "impression": impression}
	}
}

func sceneAnswer(input domain.ScenePlanInput, field string) string {
	if input.Answers != nil {
		return input.Answers[field]
	}
	answers, _ := normalizedSceneAnswers(input)
	return answers[field]
}

func sceneAnswerLabel(input domain.ScenePlanInput, field string) string {
	return sceneBriefOptions[input.Scene][field][sceneAnswer(input, field)]
}

func sceneTone(input domain.ScenePlanInput) string {
	switch input.Scene {
	case "interview":
		return map[string]string{"onsite": "专业有分寸", "video": "镜头前更利落", "final": "可信有边界"}[sceneAnswer(input, "format")]
	case "wedding":
		return map[string]string{"relaxed": "轻松不失礼", "elegant": "得体有光泽", "formal": "正式有边界"}[sceneAnswer(input, "dress-code")]
	case "date":
		return map[string]string{"coffee": "轻松有呼吸感", "dinner": "精致不刻意", "exhibition": "知性有细节", "outdoor": "舒展有活力"}[sceneAnswer(input, "activity")]
	default:
		return map[string]string{"office": "舒适仍利落", "walking": "轻盈好活动", "rain": "方便应对天气", "mild": "自然有层次"}[sceneAnswer(input, "weather")]
	}
}

func preparationDescriptor(input domain.ScenePlanInput) string {
	if label := sceneAnswerLabel(input, "preparation"); label != "" {
		return label
	}
	if label := sceneAnswerLabel(input, "dress-code"); label != "" {
		return label
	}
	return sceneTone(input)
}

func sceneContext(input domain.ScenePlanInput) string {
	labels := make([]string, 0, 3)
	for _, field := range sceneBriefFields[input.Scene] {
		if field == "impression" || field == "preparation" || field == "dress-code" {
			continue
		}
		if label := sceneAnswerLabel(input, field); label != "" {
			labels = append(labels, label)
		}
	}
	return strings.Join(labels, " · ")
}

func buildScenePlans(input domain.ScenePlanInput) []domain.Plan {
	if input.Answers == nil {
		input.Answers, _ = normalizedSceneAnswers(input)
	}
	styleNames := map[string]string{"sharp": "清晰利落", "warm": "柔和明亮", "natural": "松弛知性"}
	styleNotes := map[string]string{
		"sharp":   "让轮廓更清楚，适合需要快速建立专业感的时刻。",
		"warm":    "用柔和的光泽和弧度保留亲和力，上镜也不显用力。",
		"natural": "保留你的熟悉感，只整理重点，适合希望自在出现的场合。",
	}
	hairs := map[string][]string{
		"sharp":   {"轻盈侧分", "颅顶自然蓬松", "耳侧线条收干净"},
		"warm":    {"空气微卷", "发尾保留弧度", "脸侧留一点呼吸感"},
		"natural": {"自然偏分", "减少贴头皮感", "只整理耳侧与发尾"},
	}
	impression := sceneAnswer(input, "impression")
	order := []string{"sharp", "warm", "natural"}
	plans := make([]domain.Plan, 0, len(order))
	for index, slug := range order {
		plans = append(plans, domain.Plan{
			Name:           sceneLabels[input.Scene] + " · " + styleNames[slug],
			Slug:           slug,
			Scene:          input.Scene,
			ImageURL:       "/assets/plans/" + slug + ".jpg",
			Descriptor:     sceneImpressions[impression] + " · " + sceneTone(input),
			Why:            styleNotes[slug] + " 这次是" + sceneContext(input) + "，优先" + preparationDescriptor(input) + "。",
			OutcomeTags:    []string{sceneImpressions[impression], sceneTone(input), preparationDescriptor(input)},
			DifferenceTags: []string{hairs[slug][0], makeupTitle(impression), outfitTitle(input)},
			Sort:           index + 1,
			Steps: []domain.PlanStep{
				{Category: "hair", Title: hairs[slug][0], Summary: strings.Join(hairs[slug][1:], "，"), Details: sceneDetails(hairs[slug]), Sort: 1},
				{Category: "makeup", Title: makeupTitle(impression), Summary: makeupSummary(impression), Details: sceneDetails([]string{"底妆", "眉眼", "唇色"}), Sort: 2},
				{Category: "outfit", Title: outfitTitle(input), Summary: outfitSummary(input), Details: sceneDetails([]string{sceneContext(input), preparationDescriptor(input), sceneTone(input)}), Sort: 3},
			},
		})
	}
	for index := range plans {
		if (impression == "natural" && plans[index].Slug == "natural") || (impression == "memorable" && plans[index].Slug == "warm") || (impression != "natural" && impression != "memorable" && plans[index].Slug == "sharp") {
			plans[index].Recommended = true
			break
		}
	}
	return plans
}

func sceneDetails(values []string) json.RawMessage {
	items := make([]map[string]string, 0, len(values))
	for _, value := range values {
		items = append(items, map[string]string{"label": value, "value": "按场合调整"})
	}
	data, _ := json.Marshal(items)
	return data
}

func makeupTitle(impression string) string {
	return map[string]string{"energetic": "清透底妆 · 眉眼更精神", "reliable": "干净眉形 · 克制眼妆", "natural": "轻薄底妆 · 保留原生感", "memorable": "柔和眼尾 · 提亮唇色"}[impression]
}

func makeupSummary(impression string) string {
	return map[string]string{"energetic": "提亮眼下与眉眼对比，脸部更有精神。", "reliable": "控制光泽与色彩，让专业感更稳定。", "natural": "弱化妆感，只让气色更均匀。", "memorable": "用一个清晰重点留下印象，不堆叠妆感。"}[impression]
}

func outfitTitle(input domain.ScenePlanInput) string {
	base := map[string]string{"interview": "利落肩线 · 有秩序的内搭", "wedding": "有光泽的面料 · 得体露肤", "date": "轻盈层次 · 保留柔和感", "daily": "舒展版型 · 一点颜色重点"}[input.Scene]
	return base + " · " + sceneTone(input)
}

func outfitSummary(input domain.ScenePlanInput) string {
	base := map[string]string{
		"interview": "用清晰肩线和干净内搭，让可信感先被看见。",
		"wedding":   "避免过度隆重，用质感和比例保持上镜分寸。",
		"date":      "保留舒适与亲和力，用一处细节增加记忆点。",
		"daily":     "以已有衣物为主，优先处理配色、腰线和鞋包。",
	}[input.Scene]
	return base + " 结合“" + sceneContext(input) + "”调整。"
}
