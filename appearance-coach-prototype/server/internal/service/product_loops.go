package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/example/jianwo/server/internal/domain"
	"github.com/example/jianwo/server/internal/provider"
	"github.com/example/jianwo/server/internal/repository"
	"github.com/google/uuid"
)

func (s *Service) buildTodayContext(ctx context.Context, city, schedule string) (domain.TodayContext, error) {
	now := time.Now()
	weather, err := s.weather.Current(ctx, city)
	if err != nil {
		// 天气失败时降级为城市+日期类型+日程的上下文：今日方案仍然可用，
		// 也不把天气故障传播成 GET /v1/today/context 的 500。
		if s.logger != nil {
			s.logger.Warn("weather lookup failed; building degraded today context", "city", city, "error", err)
		}
		weather = provider.Weather{City: strings.TrimSpace(city)}
	}
	dayType := "工作日"
	if now.Weekday() == time.Saturday || now.Weekday() == time.Sunday {
		dayType = "休息日"
	}
	// 日程未指定时按工作日/休息日给出合理默认，不再对所有用户硬编码"通勤"
	if strings.TrimSpace(schedule) == "" {
		schedule = "通勤"
		if dayType == "休息日" {
			schedule = "休息"
		}
	}
	return domain.TodayContext{Date: now.Format("2006-01-02"), City: weather.City, Condition: weather.Condition, Temperature: weather.Temperature, DayType: dayType, Schedule: schedule}, nil
}

func (s *Service) GetTodayContext(ctx context.Context, city, schedule string) (domain.TodayContext, error) {
	return s.buildTodayContext(ctx, city, schedule)
}

func (s *Service) GetTodayPlan(ctx context.Context, userID string) (domain.TodayPlan, error) {
	item, err := s.repo.GetTodayPlan(ctx, userID)
	if err == nil {
		item.ImageURL = s.absoluteURL(item.ImageURL)
	}
	return item, err
}

// todayPlanImages 是今日方案的示意图，客户端会把 /assets/ 路径标注为「风格参考」。
var todayPlanImages = []string{"/assets/plans/sharp.jpg", "/assets/plans/warm.jpg", "/assets/plans/natural.jpg"}

func (s *Service) GenerateTodayPlan(ctx context.Context, userID string, input domain.TodayPlanInput) (domain.TodayPlan, error) {
	existing, err := s.repo.GetTodayPlan(ctx, userID)
	if err == nil && !input.Refresh {
		existing.ImageURL = s.absoluteURL(existing.ImageURL)
		return existing, nil
	}
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return domain.TodayPlan{}, err
	}
	variant := 0
	if err == nil {
		variant = (existing.RegenerateCount + 1) % 3
	}
	contexts, err := s.buildTodayContext(ctx, input.City, input.Schedule)
	if err != nil {
		return domain.TodayPlan{}, err
	}
	grounding := provider.TodayPlanGrounding{Context: contexts, Variant: variant}
	if err == nil {
		grounding.PreviousTitle = existing.Title
	}
	if input.ReportID != "" {
		if _, parseErr := uuid.Parse(input.ReportID); parseErr != nil {
			return domain.TodayPlan{}, fmt.Errorf("%w: 无效的形象档案", ErrValidation)
		}
		report, reportErr := s.repo.GetReport(ctx, userID, input.ReportID)
		if reportErr == nil {
			grounding.Report = &report
		} else if !errors.Is(reportErr, repository.ErrNotFound) {
			return domain.TodayPlan{}, reportErr
		}
	}
	// 真实 provider 失败时直接报错，绝不回退到模板假数据；模板内容只由
	// demo provider 在本地开发环境输出。
	output, err := s.todayPlanner.Generate(ctx, grounding)
	if err != nil {
		return domain.TodayPlan{}, fmt.Errorf("generate today plan: %w", err)
	}
	plan := domain.TodayPlan{
		ReportID: input.ReportID, Context: contexts, Title: output.Title, Summary: output.Summary,
		ImageURL: todayPlanImages[variant], RegenerateCount: variant,
		Steps: output.Steps,
	}
	item, err := s.repo.SaveTodayPlan(ctx, userID, plan)
	if err == nil {
		item.ImageURL = s.absoluteURL(item.ImageURL)
	}
	return item, err
}

func (s *Service) ActivateTodayPlan(ctx context.Context, userID, planID string) (domain.TodayPlan, error) {
	item, err := s.repo.ActivateTodayPlan(ctx, userID, planID)
	if err == nil {
		item.ImageURL = s.absoluteURL(item.ImageURL)
	}
	return item, err
}

func (s *Service) FeedbackTodayPlan(ctx context.Context, userID, planID, feedback string) (domain.TodayPlan, error) {
	if !map[string]bool{"适合我": true, "太正式": true, "想更轻松": true, "今天穿了": true}[feedback] {
		return domain.TodayPlan{}, fmt.Errorf("%w: 请选择反馈", ErrValidation)
	}
	item, err := s.repo.FeedbackTodayPlan(ctx, userID, planID, feedback)
	if err == nil {
		item.ImageURL = s.absoluteURL(item.ImageURL)
	}
	return item, err
}

func (s *Service) CreateShareCard(ctx context.Context, userID string, input domain.ShareCardInput) (domain.ShareCard, error) {
	if _, err := uuid.Parse(input.SourceID); err != nil {
		return domain.ShareCard{}, fmt.Errorf("%w: 无效的分享来源", ErrValidation)
	}
	var snapshot any
	switch input.SourceType {
	case "today":
		item, err := s.repo.GetTodayPlan(ctx, userID)
		if err != nil || item.ID != input.SourceID {
			return domain.ShareCard{}, repository.ErrNotFound
		}
		snapshot = map[string]any{"title": item.Title, "summary": item.Summary, "image_url": item.ImageURL, "steps": item.Steps, "context": item.Context, "label": "今日造型"}
	case "plan":
		item, err := s.repo.GetPlan(ctx, userID, input.SourceID)
		if err != nil {
			return domain.ShareCard{}, err
		}
		imageURL := item.GeneratedImageURL
		if imageURL == "" {
			imageURL = item.ImageURL
		}
		// Keep the snapshot URL relative: GetShareCard expands and re-signs it
		// on every view, so a stored signed URL cannot expire on the viewer.
		snapshot = map[string]any{"title": item.Name, "summary": item.Descriptor, "image_url": imageURL, "tags": item.OutcomeTags, "label": "我的形象方案"}
	default:
		return domain.ShareCard{}, fmt.Errorf("%w: 不支持的分享来源", ErrValidation)
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return domain.ShareCard{}, err
	}
	return s.repo.CreateShareCard(ctx, userID, input, data)
}

func (s *Service) GetShareCard(ctx context.Context, token string) (domain.ShareCard, error) {
	if len(token) < 20 {
		return domain.ShareCard{}, repository.ErrNotFound
	}
	card, err := s.repo.GetShareCard(ctx, token)
	if err != nil {
		return card, err
	}
	card.Snapshot = s.hydrateShareSnapshot(card.Snapshot)
	return card, nil
}

// hydrateShareSnapshot expands the snapshot image URL at view time. Cards
// created before snapshots were persisted relative may still carry an expired
// signed URL; resolveAssetURL refreshes those and expands relative paths.
func (s *Service) hydrateShareSnapshot(snapshot json.RawMessage) json.RawMessage {
	var object map[string]any
	if json.Unmarshal(snapshot, &object) != nil {
		return snapshot
	}
	imageURL, ok := object["image_url"].(string)
	if !ok || imageURL == "" {
		return snapshot
	}
	object["image_url"] = s.resolveAssetURL(imageURL)
	data, err := json.Marshal(object)
	if err != nil {
		return snapshot
	}
	return data
}

func (s *Service) RevokeShareCard(ctx context.Context, userID, cardID string) error {
	return s.repo.RevokeShareCard(ctx, userID, cardID)
}

func (s *Service) CreateWardrobeItem(ctx context.Context, userID string, input domain.WardrobeItemInput) (domain.WardrobeItem, error) {
	validCategories := map[string]bool{"top": true, "bottom": true, "outer": true, "shoes": true, "bag": true}
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Color) == "" || !validCategories[input.Category] {
		return domain.WardrobeItem{}, fmt.Errorf("%w: 请填写单品名称、类别与颜色", ErrValidation)
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
	imageURL := map[string]string{"top": "/assets/plans/warm.jpg", "bottom": "/assets/plans/sharp.jpg", "outer": "/assets/plans/natural.jpg", "shoes": "/assets/reports/sharp.jpg", "bag": "/assets/reports/warm.jpg"}[input.Category]
	if input.MediaID != "" {
		assets, err := s.repo.GetMediaAssetsForUser(ctx, userID, []string{input.MediaID})
		if err != nil || len(assets) != 1 || assets[0].Kind != "wardrobe" {
			return domain.WardrobeItem{}, repository.ErrNotFound
		}
		imageURL = "/uploads/" + assets[0].StorageKey
	}
	item, err := s.repo.CreateWardrobeItem(ctx, userID, input, imageURL)
	if err == nil {
		item.ImageURL = s.absoluteURL(item.ImageURL)
	}
	return item, err
}

func (s *Service) ListWardrobeItems(ctx context.Context, userID string) ([]domain.WardrobeItem, error) {
	items, err := s.repo.ListWardrobeItems(ctx, userID)
	for index := range items {
		items[index].ImageURL = s.absoluteURL(items[index].ImageURL)
	}
	return items, err
}

func (s *Service) DeleteWardrobeItem(ctx context.Context, userID, itemID string) error {
	return s.repo.DeleteWardrobeItem(ctx, userID, itemID)
}

func (s *Service) CreateWardrobeOutfit(ctx context.Context, userID string, contextData domain.TodayContext) (domain.WardrobeOutfit, error) {
	items, err := s.repo.ListWardrobeItems(ctx, userID)
	if err != nil {
		return domain.WardrobeOutfit{}, err
	}
	if len(items) < 2 {
		return domain.WardrobeOutfit{}, fmt.Errorf("%w: 至少添加 2 件单品后再生成搭配", ErrValidation)
	}
	selected := make([]domain.WardrobeItem, 0, 4)
	seen := map[string]bool{}
	for _, item := range items {
		if !seen[item.Category] && len(selected) < 4 {
			selected = append(selected, item)
			seen[item.Category] = true
		}
	}
	contextJSON, _ := json.Marshal(contextData)
	outfit, err := s.repo.CreateWardrobeOutfit(ctx, userID, selected, contextJSON)
	for index := range outfit.Items {
		outfit.Items[index].ImageURL = s.absoluteURL(outfit.Items[index].ImageURL)
	}
	return outfit, err
}

func (s *Service) MarkWardrobeOutfitWorn(ctx context.Context, userID, outfitID string) (domain.WardrobeOutfit, error) {
	outfit, err := s.repo.MarkWardrobeOutfitWorn(ctx, userID, outfitID)
	for index := range outfit.Items {
		outfit.Items[index].ImageURL = s.absoluteURL(outfit.Items[index].ImageURL)
	}
	return outfit, err
}

func (s *Service) CreateAdvisorConversation(ctx context.Context, userID string, input domain.AdvisorMessageInput) (domain.AdvisorConversation, error) {
	contextJSON, _ := json.Marshal(map[string]string{"report_id": input.ReportID, "today_plan_id": input.TodayPlanID})
	return s.repo.CreateAdvisorConversation(ctx, userID, contextJSON)
}

type advisorGrounding struct {
	Today    *domain.TodayPlan
	Wardrobe []domain.WardrobeItem
	Report   *domain.Report
}

func advisorContextForAI(grounding advisorGrounding) map[string]any {
	result := map[string]any{}
	if grounding.Report != nil {
		findings := make([]map[string]string, 0, len(grounding.Report.Findings))
		for _, finding := range grounding.Report.Findings {
			findings = append(findings, map[string]string{"label": finding.Label, "category": finding.Category})
		}
		result["appearance"] = map[string]any{
			"impression_tags": grounding.Report.ImpressionTags,
			"priority_title":  grounding.Report.PriorityTitle,
			"priority_copy":   grounding.Report.PriorityCopy,
			"findings":        findings,
		}
	}
	if grounding.Today != nil {
		result["today"] = map[string]any{"context": grounding.Today.Context, "title": grounding.Today.Title, "summary": grounding.Today.Summary, "steps": grounding.Today.Steps, "feedback": grounding.Today.Feedback}
	}
	wardrobe := make([]map[string]any, 0, min(len(grounding.Wardrobe), 20))
	for index, item := range grounding.Wardrobe {
		if index == 20 {
			break
		}
		wardrobe = append(wardrobe, map[string]any{"name": item.Name, "category": item.Category, "color": item.Color, "season": item.Season, "formality": item.Formality, "scenes": item.Scenes, "favorite": item.Favorite})
	}
	result["wardrobe"] = wardrobe
	return result
}

func itemNames(items []domain.WardrobeItem, limit int) string {
	names := make([]string, 0, limit)
	for _, item := range items {
		if len(names) == limit {
			break
		}
		names = append(names, item.Color+item.Name)
	}
	return strings.Join(names, "、")
}

func advisorReply(content string, grounding advisorGrounding) (string, []domain.AdvisorAction) {
	text := strings.TrimSpace(content)
	response := "可以。先保留你已经适合的部分，只调整一个重点：把上半身配色提亮，并让肩线更清楚，整体会更有精神，也不会显得刻意。"
	if grounding.Report != nil && grounding.Report.PriorityTitle != "" {
		response = "结合你的形象档案，今天仍优先处理「" + grounding.Report.PriorityTitle + "」。不必整套推翻，先做一处可见调整就够了。"
	}
	if strings.Contains(text, "面试") {
		response = "明天面试建议选择清晰肩线、明亮内搭和低对比妆容。这样先建立可信感，同时保留你的自然亲和。鞋包保持简洁，不需要购买整套新品。"
	} else if strings.Contains(text, "轻松") || strings.Contains(text, "正式") {
		response = "可以把正式度降低一级：保留外套的肩线，换成柔软内搭并减少深色面积。这样仍然得体，但会更松弛、更像你。"
	} else if strings.Contains(text, "衣橱") || strings.Contains(text, "现有") {
		if len(grounding.Wardrobe) > 0 {
			response = "可以只用现有衣橱。我先从你已经录入的「" + itemNames(grounding.Wardrobe, 3) + "」里组合：上身留一处明亮颜色，下装保持垂感，再用鞋包统一颜色；今天不需要新增单品。"
		} else {
			response = "可以只用现有衣橱。你还没有录入常穿单品，先添加 2–3 件上装和下装，我会直接按实物给组合，不需要建立完整衣橱。"
		}
	} else if strings.Contains(text, "下雨") {
		response = "下雨天先保证轻便和不拖沓：裤脚不要过长，避开麂皮鞋，发型减少复杂卷度。保留一处明亮颜色，阴天也不会显沉。"
	} else if grounding.Today != nil {
		context := grounding.Today.Context
		kept := "已经适合你的部分"
		if len(grounding.Today.Steps) > 0 {
			kept = grounding.Today.Steps[0].Title
		}
		response = fmt.Sprintf("我按你今天的「%s」方案继续调整。%s·%s·%d°，%s场景下先保留%s，只把你最犹豫的一项替换掉。", grounding.Today.Title, context.City, context.Condition, context.Temperature, context.Schedule, kept)
	}
	payload := map[string]any{"target": "today_plan"}
	if grounding.Today != nil {
		payload["today_plan_id"] = grounding.Today.ID
	}
	if grounding.Report != nil {
		payload["report_id"] = grounding.Report.ID
	}
	if len(grounding.Wardrobe) > 0 {
		ids := make([]string, 0, len(grounding.Wardrobe))
		for _, item := range grounding.Wardrobe {
			ids = append(ids, item.ID)
		}
		payload["wardrobe_item_ids"] = ids
	}
	payloadJSON, _ := json.Marshal(payload)
	actions := []domain.AdvisorAction{{Kind: "today_note", Label: "加入今日调整", Payload: payloadJSON}}
	return response, actions
}

func (s *Service) loadAdvisorGrounding(ctx context.Context, userID string, input domain.AdvisorMessageInput) (advisorGrounding, error) {
	var grounding advisorGrounding
	today, err := s.repo.GetTodayPlan(ctx, userID)
	if err == nil && (input.TodayPlanID == "" || input.TodayPlanID == today.ID) {
		grounding.Today = &today
	} else if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return grounding, err
	}

	reportID := input.ReportID
	if reportID == "" && grounding.Today != nil {
		reportID = grounding.Today.ReportID
	}
	if reportID != "" {
		report, err := s.repo.GetReport(ctx, userID, reportID)
		if err == nil {
			grounding.Report = &report
		} else if !errors.Is(err, repository.ErrNotFound) {
			return grounding, err
		}
	}

	items, err := s.repo.ListWardrobeItems(ctx, userID)
	if err != nil {
		return grounding, err
	}
	grounding.Wardrobe = items
	return grounding, nil
}

func (s *Service) SendAdvisorMessage(ctx context.Context, userID string, input domain.AdvisorMessageInput) (domain.AdvisorMessage, error) {
	if strings.TrimSpace(input.Content) == "" || len([]rune(input.Content)) > 500 {
		return domain.AdvisorMessage{}, fmt.Errorf("%w: 请输入 1–500 字的问题", ErrValidation)
	}
	conversationID := input.ConversationID
	if conversationID == "" {
		conversation, err := s.CreateAdvisorConversation(ctx, userID, input)
		if err != nil {
			return domain.AdvisorMessage{}, err
		}
		conversationID = conversation.ID
	}
	grounding, err := s.loadAdvisorGrounding(ctx, userID, input)
	if err != nil {
		return domain.AdvisorMessage{}, err
	}
	reply, actions := advisorReply(input.Content, grounding)
	if s.advisorChat != nil {
		reply, err = s.advisorChat.Reply(ctx, input.Content, advisorContextForAI(grounding))
		if err != nil {
			return domain.AdvisorMessage{}, err
		}
	}
	return s.repo.AddAdvisorExchange(ctx, userID, conversationID, input.Content, reply, actions)
}

func (s *Service) ListAdvisorMessages(ctx context.Context, userID, conversationID string) ([]domain.AdvisorMessage, error) {
	return s.repo.ListAdvisorMessages(ctx, userID, conversationID)
}

func (s *Service) ApplyAdvisorAction(ctx context.Context, userID, actionID string) (domain.AdvisorAction, error) {
	return s.repo.ApplyAdvisorAction(ctx, userID, actionID)
}

func validEventName(name string) bool {
	if len(name) < 2 || len(name) > 64 || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for _, character := range name[1:] {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' {
			return false
		}
	}
	return true
}

func (s *Service) TrackProductEvent(ctx context.Context, userID string, input domain.ProductEventInput) error {
	input.Name = strings.TrimSpace(input.Name)
	if !validEventName(input.Name) || len(input.Payload) > 4096 {
		return fmt.Errorf("%w: 埋点名称或参数不合法", ErrValidation)
	}
	if len(input.Payload) == 0 {
		input.Payload = json.RawMessage(`{}`)
	}
	var object map[string]any
	if !json.Valid(input.Payload) || json.Unmarshal(input.Payload, &object) != nil || object == nil {
		return fmt.Errorf("%w: 埋点参数必须是 JSON 对象", ErrValidation)
	}
	return s.repo.TrackProductEvent(ctx, userID, input)
}
