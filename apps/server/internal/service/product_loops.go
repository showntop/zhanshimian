package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/provider"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/google/uuid"
)

// ---- 今日方案 ----

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
	if now.Weekday() == 0 || now.Weekday() == 6 {
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

func (s *Service) GetTodayContext(ctx context.Context, userID, city, schedule string) (domain.TodayContext, error) {
	// 资料注入：上下文本身不依赖身高，但保持与其他读路径一致的签名。
	_ = userID
	return s.buildTodayContext(ctx, city, schedule)
}

// attachTodayLookTask projects the today_look task onto the plan-facing
// generation_status/generation_error fields and embeds look_task.
func (s *Service) attachTodayLookTask(ctx context.Context, userID string, plan *domain.TodayPlan) {
	if plan.GeneratedImageURL != "" {
		plan.GenerationStatus = domain.TaskCompleted
	}
	tasks, err := s.repo.LatestTasksByRef(ctx, userID, domain.TaskTypeTodayLook, "plan_id", []string{plan.ID})
	if err != nil {
		return
	}
	if task, ok := tasks[plan.ID]; ok {
		view := decodeTaskErrorView(taskView(task))
		plan.LookTask = ptrTaskView(view)
		plan.GenerationStatus = task.Status
		if task.Status == domain.TaskFailed && view.Error != nil {
			plan.GenerationError = view.Error.Message
		}
	}
}

func (s *Service) hydrateTodayPlan(ctx context.Context, userID string, plan *domain.TodayPlan) {
	plan.ImageURL = s.resolveAssetURL(plan.ImageURL)
	plan.GeneratedImageURL = s.resolveAssetURL(plan.GeneratedImageURL)
	s.attachTodayLookTask(ctx, userID, plan)
}

func (s *Service) GetTodayPlan(ctx context.Context, userID string) (domain.TodayPlan, error) {
	item, err := s.repo.GetTodayPlan(ctx, userID)
	if err != nil {
		return item, err
	}
	s.hydrateTodayPlan(ctx, userID, &item)
	return item, nil
}

// todayPlanImages 是今日方案的示意图，客户端会把 /assets/ 路径标注为「风格参考」。
var todayPlanImages = []string{"/assets/plans/sharp.jpg", "/assets/plans/warm.jpg", "/assets/plans/natural.jpg"}

// GenerateTodayPlan builds today's plan (201) and enqueues the today_look
// render. The plan is always grounded in a report: report_id defaults to the
// latest one and a user without any report gets a 404.
func (s *Service) GenerateTodayPlan(ctx context.Context, userID string, input domain.TodayPlanInput) (domain.TodayPlan, *domain.Task, error) {
	existing, err := s.repo.GetTodayPlan(ctx, userID)
	if err == nil && !input.Refresh {
		s.hydrateTodayPlan(ctx, userID, &existing)
		return existing, nil, nil
	}
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return domain.TodayPlan{}, nil, err
	}
	variant := 0
	if err == nil {
		variant = (existing.RegenerateCount + 1) % 3
	}
	contexts, err := s.buildTodayContext(ctx, input.City, input.Schedule)
	if err != nil {
		return domain.TodayPlan{}, nil, err
	}
	grounding := provider.TodayPlanGrounding{Context: contexts, Variant: variant}
	if err == nil {
		grounding.PreviousTitle = existing.Title
	}
	// 资料注入：已填写的补充资料参与 grounding，缺失时安全降级。
	if profile, profileErr := s.repo.GetUserProfile(ctx, userID); profileErr == nil {
		grounding.Profile = &profile
	}
	if input.ReportID != "" {
		if _, parseErr := uuid.Parse(input.ReportID); parseErr != nil {
			return domain.TodayPlan{}, nil, fmt.Errorf("%w: 无效的形象档案", ErrValidation)
		}
		report, reportErr := s.repo.GetReport(ctx, userID, input.ReportID)
		if reportErr == nil {
			grounding.Report = &report
		} else if !errors.Is(reportErr, repository.ErrNotFound) {
			return domain.TodayPlan{}, nil, reportErr
		}
	} else {
		report, reportErr := s.repo.LatestReport(ctx, userID)
		if reportErr != nil {
			return domain.TodayPlan{}, nil, reportErr
		}
		grounding.Report = &report
		input.ReportID = report.ID
	}
	// 真实 provider 失败时直接报错，绝不回退到模板假数据；模板内容只由
	// demo provider 在本地开发环境输出。
	output, err := s.todayPlanner.Generate(ctx, grounding)
	if err != nil {
		return domain.TodayPlan{}, nil, fmt.Errorf("generate today plan: %w", err)
	}
	plan := domain.TodayPlan{
		ReportID: input.ReportID, Context: contexts, Title: output.Title, Summary: output.Summary,
		ImageURL: todayPlanImages[variant], RegenerateCount: variant,
		Steps: output.Steps,
	}
	item, err := s.repo.SaveTodayPlan(ctx, userID, plan)
	if err != nil {
		return domain.TodayPlan{}, nil, err
	}
	var task *domain.Task
	if item.ReportID != "" && s.lookGenerator != nil {
		created, taskErr := s.repo.CreateTask(ctx, userID, domain.TaskInput{
			Type:    domain.TaskTypeTodayLook,
			Payload: domain.TodayLookTaskPayload{PlanID: item.ID},
		})
		if taskErr != nil {
			return domain.TodayPlan{}, nil, taskErr
		}
		task = &created
	}
	s.hydrateTodayPlan(ctx, userID, &item)
	return item, task, nil
}

func (s *Service) ActivateTodayPlan(ctx context.Context, userID, planID string) (domain.TodayPlan, error) {
	item, err := s.repo.ActivateTodayPlan(ctx, userID, planID)
	if err != nil {
		return item, err
	}
	s.hydrateTodayPlan(ctx, userID, &item)
	return item, nil
}

func (s *Service) FeedbackTodayPlan(ctx context.Context, userID, planID, feedback string) (domain.TodayPlan, error) {
	if !map[string]bool{"适合我": true, "太正式": true, "想更轻松": true, "今天穿了": true}[feedback] {
		return domain.TodayPlan{}, fmt.Errorf("%w: 请选择反馈", ErrValidation)
	}
	item, err := s.repo.FeedbackTodayPlan(ctx, userID, planID, feedback)
	if err != nil {
		return item, err
	}
	s.hydrateTodayPlan(ctx, userID, &item)
	return item, nil
}

// ---- 分享 ----

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

func (s *Service) DeleteShareCard(ctx context.Context, userID, cardID string) error {
	return s.repo.DeleteShareCard(ctx, userID, cardID)
}

// ---- 衣橱 ----

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
	if err != nil {
		return item, err
	}
	item.ImageURL = s.absoluteURL(item.ImageURL)
	return item, nil
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

// CreateWardrobeOutfit freezes a client-composed combination (title + item
// ids + optional context snapshot). Item ids are re-validated against the
// owner so a foreign id surfaces as 404.
func (s *Service) CreateWardrobeOutfit(ctx context.Context, userID string, input domain.WardrobeOutfitInput) (domain.WardrobeOutfit, error) {
	title := strings.TrimSpace(input.Title)
	if title == "" || len([]rune(title)) > 60 {
		return domain.WardrobeOutfit{}, fmt.Errorf("%w: 请填写 60 字以内的搭配名称", ErrValidation)
	}
	if len(input.ItemIDs) == 0 || len(input.ItemIDs) > 12 {
		return domain.WardrobeOutfit{}, fmt.Errorf("%w: 请选择 1–12 件单品组成搭配", ErrValidation)
	}
	items, err := s.repo.ListWardrobeItems(ctx, userID)
	if err != nil {
		return domain.WardrobeOutfit{}, err
	}
	byID := make(map[string]domain.WardrobeItem, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}
	selected := make([]domain.WardrobeItem, 0, len(input.ItemIDs))
	for _, id := range input.ItemIDs {
		item, ok := byID[id]
		if !ok {
			return domain.WardrobeOutfit{}, repository.ErrNotFound
		}
		selected = append(selected, item)
	}
	note := input.Note
	if note == "" {
		note = "优先复用你常穿的单品，用颜色与比例完成这套表达。"
	}
	var contextData json.RawMessage
	if input.Context != nil {
		contextData, _ = json.Marshal(input.Context)
	} else if contextValue, contextErr := s.buildTodayContext(ctx, "", ""); contextErr == nil {
		contextData, _ = json.Marshal(contextValue)
	}
	outfit, err := s.repo.CreateWardrobeOutfit(ctx, userID, domain.WardrobeOutfitInput{Title: title, Note: note, ItemIDs: input.ItemIDs}, contextData, selected)
	if err != nil {
		return outfit, err
	}
	for index := range outfit.Items {
		outfit.Items[index].ImageURL = s.absoluteURL(outfit.Items[index].ImageURL)
	}
	return outfit, nil
}

func (s *Service) MarkWardrobeOutfitWorn(ctx context.Context, userID, outfitID string) (domain.WardrobeOutfit, error) {
	outfit, err := s.repo.MarkWardrobeOutfitWorn(ctx, userID, outfitID)
	for index := range outfit.Items {
		outfit.Items[index].ImageURL = s.absoluteURL(outfit.Items[index].ImageURL)
	}
	return outfit, err
}

// ---- 顾问 ----

type advisorGrounding struct {
	Today    *domain.TodayPlan
	Wardrobe []domain.WardrobeItem
	Report   *domain.Report
	Profile  *domain.UserProfile
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
	if grounding.Profile != nil {
		profile := map[string]any{"height_cm": grounding.Profile.HeightCM}
		if grounding.Profile.WeightKG != nil {
			profile["weight_kg"] = *grounding.Profile.WeightKG
		}
		if grounding.Profile.BustCM != nil {
			profile["bust_cm"] = *grounding.Profile.BustCM
		}
		if grounding.Profile.WaistCM != nil {
			profile["waist_cm"] = *grounding.Profile.WaistCM
		}
		if grounding.Profile.HipCM != nil {
			profile["hip_cm"] = *grounding.Profile.HipCM
		}
		if grounding.Profile.Role != "" {
			profile["role"] = grounding.Profile.Role
		}
		if grounding.Profile.Budget != "" {
			profile["budget"] = grounding.Profile.Budget
		}
		result["profile"] = profile
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

	if profile, err := s.repo.GetUserProfile(ctx, userID); err == nil {
		grounding.Profile = &profile
	}

	items, err := s.repo.ListWardrobeItems(ctx, userID)
	if err != nil {
		return grounding, err
	}
	grounding.Wardrobe = items
	return grounding, nil
}

func (s *Service) CreateAdvisorConversation(ctx context.Context, userID string, input domain.AdvisorMessageInput) (domain.AdvisorConversation, error) {
	contextJSON, _ := json.Marshal(map[string]string{"report_id": input.ReportID, "today_plan_id": input.TodayPlanID})
	return s.repo.CreateAdvisorConversation(ctx, userID, contextJSON)
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

// ---- 埋点 ----

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

// ---- 诊断与发型（原 tools 重组为资源端点） ----

func (s *Service) RunDiagnostic(ctx context.Context, userID string, input domain.DiagnosticInput) (domain.ToolResult, error) {
	validKinds := map[string]bool{"outfit": true, "purchase": true}
	if !validKinds[input.Kind] {
		return domain.ToolResult{}, fmt.Errorf("%w: 不支持的诊断类型", ErrValidation)
	}
	if input.Scene == "" {
		input.Scene = "daily"
	}
	if !validPlanScenes[input.Scene] {
		return domain.ToolResult{}, fmt.Errorf("%w: 不支持的使用场景", ErrValidation)
	}
	toolContext := &domain.ToolContext{}
	if input.ReportID == "" {
		// report_id 缺省用当前报告；没有任何报告时照常诊断，只是缺少 grounding。
		if report, err := s.repo.LatestReport(ctx, userID); err == nil {
			input.ReportID = report.ID
		}
	}
	if input.ReportID != "" {
		if _, err := uuid.Parse(input.ReportID); err != nil {
			return domain.ToolResult{}, fmt.Errorf("%w: 无效的形象档案", ErrValidation)
		}
		report, err := s.repo.GetReport(ctx, userID, input.ReportID)
		if err != nil {
			return domain.ToolResult{}, err
		}
		toolContext.ImpressionTags, toolContext.PriorityTitle, toolContext.PriorityCopy = report.ImpressionTags, report.PriorityTitle, report.PriorityCopy
	}
	if profile, err := s.repo.GetUserProfile(ctx, userID); err == nil {
		toolContext.Profile = &profile
	}
	if input.Kind == "purchase" {
		items, err := s.repo.ListWardrobeItems(ctx, userID)
		if err != nil {
			return domain.ToolResult{}, err
		}
		for index, item := range items {
			if index == 20 {
				break
			}
			toolContext.Wardrobe = append(toolContext.Wardrobe, domain.ToolWardrobeItem{Name: item.Name, Category: item.Category, Color: item.Color, Season: item.Season, Formality: item.Formality, Scenes: item.Scenes})
		}
	}
	if input.ReportID != "" || len(toolContext.Wardrobe) > 0 {
		input.Context = toolContext
	}
	if input.MediaID == "" {
		return domain.ToolResult{}, fmt.Errorf("%w: 请先上传需要判断的照片", ErrValidation)
	}
	if _, err := uuid.Parse(input.MediaID); err != nil {
		return domain.ToolResult{}, fmt.Errorf("%w: 无效的照片", ErrValidation)
	}
	assets, err := s.repo.GetMediaAssetsForUser(ctx, userID, []string{input.MediaID})
	if err != nil {
		return domain.ToolResult{}, err
	}
	expectedKind := map[string]string{"outfit": "outfit", "purchase": "product"}[input.Kind]
	if len(assets) != 1 || assets[0].Kind != expectedKind {
		return domain.ToolResult{}, fmt.Errorf("%w: 照片类型与诊断不匹配", ErrValidation)
	}
	result := buildToolResult(input.Kind, input.Scene)
	if input.Kind == "outfit" {
		result, err = s.outfitAdvisor.Diagnose(ctx, input)
		if err != nil {
			return domain.ToolResult{}, err
		}
	} else if s.purchaseAdvisor != nil {
		result, err = s.purchaseAdvisor.Diagnose(ctx, input)
		if err != nil {
			return domain.ToolResult{}, err
		}
	}
	result, err = s.repo.CreateDiagnostic(ctx, userID, input, result)
	if err != nil {
		return domain.ToolResult{}, err
	}
	return s.hydrateDiagnostic(ctx, userID, result)
}

func (s *Service) GetDiagnostic(ctx context.Context, userID, diagnosticID string) (domain.ToolResult, error) {
	if _, err := uuid.Parse(diagnosticID); err != nil {
		return domain.ToolResult{}, repository.ErrNotFound
	}
	result, err := s.repo.GetDiagnostic(ctx, userID, diagnosticID)
	if err != nil {
		return domain.ToolResult{}, err
	}
	return s.hydrateDiagnostic(ctx, userID, result)
}

func (s *Service) LatestDiagnostic(ctx context.Context, userID, kind string) (domain.ToolResult, error) {
	if !map[string]bool{"outfit": true, "purchase": true}[kind] {
		return domain.ToolResult{}, fmt.Errorf("%w: 不支持的诊断类型", ErrValidation)
	}
	result, err := s.repo.LatestDiagnostic(ctx, userID, kind)
	if err != nil {
		return domain.ToolResult{}, err
	}
	return s.hydrateDiagnostic(ctx, userID, result)
}

func (s *Service) SetDiagnosticSaved(ctx context.Context, userID, diagnosticID string, saved bool) (domain.ToolResult, error) {
	if _, err := uuid.Parse(diagnosticID); err != nil {
		return domain.ToolResult{}, repository.ErrNotFound
	}
	result, err := s.repo.SetDiagnosticSaved(ctx, userID, diagnosticID, saved)
	if err != nil {
		return domain.ToolResult{}, err
	}
	return s.hydrateDiagnostic(ctx, userID, result)
}

func (s *Service) hydrateDiagnostic(ctx context.Context, userID string, result domain.ToolResult) (domain.ToolResult, error) {
	for index := range result.Options {
		result.Options[index].ImageURL = s.absoluteURL(result.Options[index].ImageURL)
	}
	if result.MediaID == "" {
		return result, nil
	}
	assets, err := s.repo.GetMediaAssetsForUser(ctx, userID, []string{result.MediaID})
	if err != nil || len(assets) != 1 {
		return result, nil
	}
	result.ImageURL = s.mediaAssetURL(assets[0])
	return result, nil
}

// Hairstyles is the pure-read hairstyle recommendation (GET /v1/hairstyles):
// nothing is persisted, the three reference options come from the bundled
// catalogue and stay labelled as 风格参考 on the client. report_id defaults
// to the latest report; a user without any report gets a 404.
func (s *Service) Hairstyles(ctx context.Context, userID, reportID string) ([]domain.ToolOption, error) {
	if reportID == "" {
		if _, err := s.repo.LatestReport(ctx, userID); err != nil {
			return nil, err
		}
	} else if _, err := uuid.Parse(reportID); err != nil {
		return nil, fmt.Errorf("%w: 无效的形象档案", ErrValidation)
	} else if _, err := s.repo.GetReport(ctx, userID, reportID); err != nil {
		return nil, err
	}
	result := buildToolResult("hair", "daily")
	for index := range result.Options {
		result.Options[index].ImageURL = s.absoluteURL(result.Options[index].ImageURL)
	}
	return result.Options, nil
}

// ---- 发型预览 ----

var hairStyleNames = map[string]string{"sharp": "锁骨层次发", "warm": "空气微卷", "natural": "自然偏分"}

func (s *Service) CreateHairPreview(ctx context.Context, userID string, input domain.HairPreviewInput) (domain.HairPreview, *domain.Task, error) {
	if _, err := uuid.Parse(input.MediaID); err != nil {
		return domain.HairPreview{}, nil, fmt.Errorf("%w: 请先上传一张清晰正脸照", ErrValidation)
	}
	styleName := hairStyleNames[input.StyleID]
	if styleName == "" {
		return domain.HairPreview{}, nil, fmt.Errorf("%w: 请选择一个发型方向", ErrValidation)
	}
	if input.ReportID != "" {
		if _, err := uuid.Parse(input.ReportID); err != nil {
			return domain.HairPreview{}, nil, fmt.Errorf("%w: 无效的形象档案", ErrValidation)
		}
	}
	if input.Scene == "" {
		input.Scene = "daily"
	}
	if !validPlanScenes[input.Scene] {
		return domain.HairPreview{}, nil, fmt.Errorf("%w: 不支持的使用场景", ErrValidation)
	}
	preview, task, err := s.repo.CreateHairPreview(ctx, userID, input, styleName)
	if err != nil {
		return domain.HairPreview{}, nil, err
	}
	preview.SourceImageURL = s.absoluteURL(preview.SourceImageURL)
	return preview, task, nil
}

// attachHairPreviewTask projects the hair_preview task onto the preview's
// status/progress/stage/error fields and embeds task.
func (s *Service) attachHairPreviewTask(ctx context.Context, userID string, preview *domain.HairPreview) {
	if preview.ResultImageURL != "" {
		preview.Status = domain.TaskCompleted
		preview.Progress = 100
		preview.Stage = "预览已生成"
	}
	tasks, err := s.repo.LatestTasksByRef(ctx, userID, domain.TaskTypeHairPreview, "preview_id", []string{preview.ID})
	if err != nil {
		return
	}
	if task, ok := tasks[preview.ID]; ok {
		view := decodeTaskErrorView(taskView(task))
		preview.Task = ptrTaskView(view)
		preview.Status = task.Status
		preview.Progress = task.Progress
		preview.Stage = task.Stage
		if task.Status == domain.TaskFailed && view.Error != nil {
			preview.ErrorMessage = view.Error.Message
		}
	}
}

// GetActiveHairPreview backs GET /v1/hair-previews/active: clients that lost
// their local preview reference (cache clear, second device) rediscover the
// in-flight generation here; 404 when nothing is running.
func (s *Service) GetActiveHairPreview(ctx context.Context, userID string) (domain.HairPreview, error) {
	preview, err := s.repo.GetActiveHairPreview(ctx, userID)
	if err != nil {
		return domain.HairPreview{}, err
	}
	preview.SourceImageURL = s.absoluteURL(preview.SourceImageURL)
	if preview.ResultImageURL != "" {
		preview.ResultImageURL = s.absoluteURL(preview.ResultImageURL)
	}
	s.attachHairPreviewTask(ctx, userID, &preview)
	return preview, nil
}

func (s *Service) GetHairPreview(ctx context.Context, userID, previewID string) (domain.HairPreview, error) {
	if _, err := uuid.Parse(previewID); err != nil {
		return domain.HairPreview{}, repository.ErrNotFound
	}
	preview, err := s.repo.GetHairPreview(ctx, userID, previewID)
	if err != nil {
		return domain.HairPreview{}, err
	}
	preview.SourceImageURL = s.absoluteURL(preview.SourceImageURL)
	if preview.ResultImageURL != "" {
		preview.ResultImageURL = s.absoluteURL(preview.ResultImageURL)
	}
	s.attachHairPreviewTask(ctx, userID, &preview)
	return preview, nil
}

func (s *Service) ListSavedHairPreviews(ctx context.Context, userID string) ([]domain.HairPreview, error) {
	items, err := s.repo.ListSavedHairPreviews(ctx, userID)
	if err != nil {
		return nil, err
	}
	for index := range items {
		items[index].SourceImageURL = s.absoluteURL(items[index].SourceImageURL)
		items[index].ResultImageURL = s.absoluteURL(items[index].ResultImageURL)
	}
	return items, nil
}

func (s *Service) SaveHairPreview(ctx context.Context, userID, previewID string) (domain.HairPreview, error) {
	if _, err := uuid.Parse(previewID); err != nil {
		return domain.HairPreview{}, repository.ErrNotFound
	}
	preview, err := s.repo.SaveHairPreview(ctx, userID, previewID)
	if err != nil {
		return domain.HairPreview{}, err
	}
	preview.SourceImageURL = s.absoluteURL(preview.SourceImageURL)
	preview.ResultImageURL = s.absoluteURL(preview.ResultImageURL)
	return preview, nil
}

// buildToolResult produces the deterministic template every diagnosis falls
// back to (demo provider output and purchase defaults).
func buildToolResult(kind, scene string) domain.ToolResult {
	sceneNames := map[string]string{"general": "当前场景", "daily": "日常", "interview": "面试", "wedding": "婚礼", "date": "约会", "gathering": "聚会"}
	sceneName := sceneNames[scene]
	switch kind {
	case "hair":
		return domain.ToolResult{
			Kind: "hair", Scene: scene, Conclusion: "首选锁骨层次发",
			PriorityTitle: "提高发型重心，露出肩颈",
			PriorityCopy:  "比贴脸长直发更能突出眉眼与头肩比例，同时保留自然亲和感。",
			Tags:          []string{"重心提高", "肩颈更清晰", "容易打理"},
			Options: []domain.ToolOption{
				{ID: "sharp", Name: "锁骨层次发", ImageURL: "/assets/looks/sharp.png", Note: "首选推荐", Reason: "提高视觉重心并保留脸侧空气感。", Tags: []string{"重心提高", "肩颈清晰"}},
				{ID: "warm", Name: "空气微卷", ImageURL: "/assets/looks/warm.png", Note: "柔和表达", Reason: "发尾弧度保留亲和感，更适合沟通场景。", Tags: []string{"自然柔和", "上镜"}},
				{ID: "natural", Name: "自然偏分", ImageURL: "/assets/looks/natural.png", Note: "低维护", Reason: "只调整分缝与耳侧线条，日常最容易维持。", Tags: []string{"改动小", "低维护"}},
			},
		}
	case "outfit":
		return domain.ToolResult{
			Kind: "outfit", Scene: scene, Conclusion: "整体方向对了，先改一处",
			PriorityTitle: "把深色内搭换成象牙白",
			PriorityCopy:  fmt.Sprintf("不换整套衣服，就能让眉眼更清晰、上半身更轻盈，也更适合%s。", sceneName),
			Tags:          []string{"预计 3 分钟", "无需购买新衣", "变化明显"},
			Findings: []domain.ToolFinding{
				{Label: "上身配色偏沉", Category: "color", Tone: "improve"},
				{Label: "肩线不够清晰", Category: "silhouette", Tone: "improve"},
				{Label: "腰线可以上移", Category: "proportion", Tone: "optional"},
			},
		}
	default:
		return domain.ToolResult{
			Kind: "purchase", Scene: scene, Conclusion: "比较适合",
			PriorityTitle: fmt.Sprintf("适合%s，但建议搭配挺括下装", sceneName),
			PriorityCopy:  "清晰肩线有利于头肩比例，低饱和颜色也容易与现有衣橱组合。",
			Tags:          []string{"象牙白内搭", "深灰直筒裤", "黑色低跟鞋"},
			Findings: []domain.ToolFinding{
				{Label: "清晰肩线能改善头肩比例", Category: "silhouette", Tone: "positive"},
				{Label: "低饱和颜色容易组合", Category: "color", Tone: "positive"},
				{Label: "正式场合需要更挺括下装", Category: "fabric", Tone: "caution"},
			},
		}
	}
}
