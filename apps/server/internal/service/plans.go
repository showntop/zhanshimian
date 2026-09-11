package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/google/uuid"
)

var validPlanScenes = map[string]bool{"general": true, "daily": true, "interview": true, "wedding": true, "date": true, "gathering": true}

func (s *Service) resolvePlanURLs(plan *domain.Plan) {
	plan.ImageURL = s.resolveAssetURL(plan.ImageURL)
	plan.CurrentImageURL = s.resolveAssetURL(plan.CurrentImageURL)
	if plan.GeneratedImageURL != "" {
		plan.GeneratedImageURL = s.resolveAssetURL(plan.GeneratedImageURL)
	}
}

// attachPlanLookTasks embeds each plan's latest plan_look task so plan lists
// render image generation state without a second request. The plan-facing
// generation_status/generation_error fields are projections of that task.
func (s *Service) attachPlanLookTasks(ctx context.Context, userID string, plans []domain.Plan) error {
	ids := make([]string, 0, len(plans))
	for _, plan := range plans {
		ids = append(ids, plan.ID)
	}
	tasks, err := s.repo.LatestTasksByRef(ctx, userID, domain.TaskTypePlanLook, "plan_id", ids)
	if err != nil {
		return err
	}
	for index := range plans {
		plan := &plans[index]
		if plan.GeneratedImageURL != "" {
			plan.GenerationStatus = domain.TaskCompleted
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
	return nil
}

func ptrTaskView(view domain.TaskView) *domain.TaskView { return &view }

func (s *Service) ListPlans(ctx context.Context, userID, reportID, scene string) ([]domain.Plan, error) {
	plans, err := s.repo.ListPlans(ctx, userID, reportID, scene)
	if err != nil {
		return nil, err
	}
	if err := s.attachPlanLookTasks(ctx, userID, plans); err != nil {
		return nil, err
	}
	for index := range plans {
		s.resolvePlanURLs(&plans[index])
	}
	return plans, nil
}

func (s *Service) GetPlan(ctx context.Context, userID, planID string) (domain.Plan, error) {
	plan, err := s.repo.GetPlan(ctx, userID, planID)
	if err != nil {
		return plan, err
	}
	if err := s.attachPlanLookTasks(ctx, userID, []domain.Plan{plan}); err != nil {
		return domain.Plan{}, err
	}
	s.resolvePlanURLs(&plan)
	return plan, nil
}

// PutReportPlans is the idempotent create-or-refresh of a report's plan
// group. It merges the old scene-plans and plan-looks endpoints: text is
// produced synchronously, image tasks are enqueued (embedded as each plan's
// look_task), and calling it twice with the same answers creates nothing new.
// PutReportPlans 幂等确保某场景的方案组存在：general 空组时触发 plan_group
// 生成任务（返回 task 视图，HTTP 202），已有组只补缺失的形象图任务。
func (s *Service) PutReportPlans(ctx context.Context, userID, reportID string, input domain.PlansUpsertInput) ([]domain.Plan, *domain.TaskView, error) {
	if _, err := uuid.Parse(reportID); err != nil {
		return nil, nil, fmt.Errorf("%w: 无效的形象报告", ErrValidation)
	}
	if input.Scene == "" {
		input.Scene = "general"
	}
	if !validPlanScenes[input.Scene] {
		return nil, nil, fmt.Errorf("%w: 不支持的使用场景", ErrValidation)
	}
	if _, err := s.repo.GetReport(ctx, userID, reportID); err != nil {
		return nil, nil, err
	}
	var plans []domain.Plan
	if input.Scene == "general" {
		var err error
		if plans, err = s.repo.ListPlans(ctx, userID, reportID, "general"); err != nil {
			return nil, nil, err
		}
		if len(plans) == 0 {
			// 报告与方案解耦：general 组由 plan_group 任务从报告内容生成。
			// 幂等：已有排队/进行中的生成任务直接复用，不重复入队。
			latest, err := s.repo.LatestTasksByRef(ctx, userID, domain.TaskTypePlanGroup, "report_id", []string{reportID})
			if err != nil {
				return nil, nil, err
			}
			var task domain.Task
			if existing, ok := latest[reportID]; ok && (existing.Status == domain.TaskQueued || existing.Status == domain.TaskProcessing) {
				task = existing
			} else {
				task, err = s.repo.CreateTask(ctx, userID, domain.TaskInput{
					Type:    domain.TaskTypePlanGroup,
					Payload: domain.PlanGroupTaskPayload{ReportID: reportID},
					Stage:   "正在准备生成三套方案",
				})
				if err != nil {
					return nil, nil, err
				}
			}
			view := decodeTaskErrorView(taskView(task))
			return nil, &view, nil
		}
	} else {
		if len(input.Answers) == 0 {
			return nil, nil, fmt.Errorf("%w: 请先完成%s信息", ErrValidation, sceneLabels[input.Scene])
		}
		sceneInput := domain.ScenePlanInput{Scene: input.Scene, Answers: input.Answers}
		normalized, err := normalizedSceneAnswers(sceneInput)
		if err != nil {
			return nil, nil, err
		}
		sceneInput.Answers = normalized
		var err2 error
		if plans, err2 = s.repo.UpsertScenePlans(ctx, userID, reportID, sceneInput, buildScenePlans(sceneInput)); err2 != nil {
			return nil, nil, err2
		}
	}
	if _, err := s.enqueueMissingPlanLooks(ctx, userID, plans); err != nil {
		return nil, nil, err
	}
	if err := s.attachPlanLookTasks(ctx, userID, plans); err != nil {
		return nil, nil, err
	}
	for index := range plans {
		s.resolvePlanURLs(&plans[index])
	}
	return plans, nil, nil
}

// enqueueMissingPlanLooks adds a plan_look task for every plan without a
// generated image and without a queued/running task. This is what makes the
// whole PUT idempotent: plans already rendering or done are left alone.
func (s *Service) enqueueMissingPlanLooks(ctx context.Context, userID string, plans []domain.Plan) ([]domain.Task, error) {
	ids := make([]string, 0, len(plans))
	for _, plan := range plans {
		ids = append(ids, plan.ID)
	}
	latest, err := s.repo.LatestTasksByRef(ctx, userID, domain.TaskTypePlanLook, "plan_id", ids)
	if err != nil {
		return nil, err
	}
	created := make([]domain.Task, 0, len(plans))
	for _, plan := range plans {
		if plan.GeneratedImageURL != "" {
			continue
		}
		if task, ok := latest[plan.ID]; ok && (task.Status == domain.TaskQueued || task.Status == domain.TaskProcessing) {
			continue
		}
		task, err := s.repo.CreateTask(ctx, userID, domain.TaskInput{
			Type:    domain.TaskTypePlanLook,
			Payload: domain.PlanLookTaskPayload{PlanID: plan.ID},
		})
		if err != nil {
			return nil, err
		}
		created = append(created, task)
	}
	return created, nil
}

// RegeneratePlanLook retries one plan's image (202). A queued or running task
// is returned as-is; a finished or failed one is superseded by a new task.
func (s *Service) RegeneratePlanLook(ctx context.Context, userID, planID string) (domain.Plan, domain.TaskView, error) {
	if s.lookGenerator == nil {
		return domain.Plan{}, domain.TaskView{}, fmt.Errorf("%w: 当前环境暂未开启本人方案生成", ErrValidation)
	}
	plan, err := s.repo.GetPlan(ctx, userID, planID)
	if err != nil {
		return domain.Plan{}, domain.TaskView{}, err
	}
	latest, err := s.repo.LatestTasksByRef(ctx, userID, domain.TaskTypePlanLook, "plan_id", []string{plan.ID})
	if err != nil {
		return domain.Plan{}, domain.TaskView{}, err
	}
	var task domain.Task
	if existing, ok := latest[plan.ID]; ok && (existing.Status == domain.TaskQueued || existing.Status == domain.TaskProcessing) {
		task = existing
	} else {
		task, err = s.repo.CreateTask(ctx, userID, domain.TaskInput{
			Type:    domain.TaskTypePlanLook,
			Payload: domain.PlanLookTaskPayload{PlanID: plan.ID},
		})
		if err != nil {
			return domain.Plan{}, domain.TaskView{}, err
		}
	}
	plan.LookTask = ptrTaskView(decodeTaskErrorView(taskView(task)))
	s.resolvePlanURLs(&plan)
	return plan, decodeTaskErrorView(taskView(task)), nil
}

func (s *Service) SelectPlan(ctx context.Context, userID, planID string) error {
	return s.repo.SelectPlan(ctx, userID, planID)
}

func (s *Service) GetChecklist(ctx context.Context, userID, planID string) ([]domain.ChecklistItem, error) {
	return s.repo.GetChecklist(ctx, userID, planID)
}

func (s *Service) SetChecklistItem(ctx context.Context, userID, planID, itemID string, completed bool) (domain.ChecklistItem, error) {
	if _, err := uuid.Parse(itemID); err != nil {
		return domain.ChecklistItem{}, repository.ErrNotFound
	}
	return s.repo.SetChecklistItem(ctx, userID, planID, itemID, completed)
}

// FeedbackAck is the response of POST /v1/plans/{id}/feedback. message is the
// G3 personalised promise copy produced server-side.
type FeedbackAck struct {
	Saved   bool   `json:"saved"`
	Message string `json:"message"`
}

func (s *Service) AddPlanFeedback(ctx context.Context, userID, planID string, input domain.FeedbackInput) (FeedbackAck, error) {
	if len(input.Tags) == 0 {
		return FeedbackAck{}, fmt.Errorf("%w: 请选择至少一个反馈标签", ErrValidation)
	}
	if len(input.Comment) > 500 {
		return FeedbackAck{}, fmt.Errorf("%w: 补充说明最多 500 字", ErrValidation)
	}
	plan, err := s.repo.GetPlan(ctx, userID, planID)
	if err != nil {
		return FeedbackAck{}, err
	}
	if input.MediaID != "" {
		assets, err := s.repo.GetMediaAssetsForUser(ctx, userID, []string{input.MediaID})
		if err != nil || len(assets) != 1 || assets[0].Kind != "feedback" {
			return FeedbackAck{}, repository.ErrNotFound
		}
	}
	if err := s.repo.AddFeedback(ctx, userID, plan.ID, input); err != nil {
		return FeedbackAck{}, err
	}
	return FeedbackAck{Saved: true, Message: feedbackMessage(plan.Name, input)}, nil
}

// feedbackMessage is the G3 personalised copy: it quotes the plan name and
// commits to keeping what worked, so the feedback page closes with a promise
// instead of a bare "saved".
func feedbackMessage(planName string, input domain.FeedbackInput) string {
	tags := strings.Join(input.Tags, "、")
	message := fmt.Sprintf("我们记住了「%s」对你有效的部分（%s），下次方案会优先保留这些做法。", planName, tags)
	if strings.TrimSpace(input.Comment) != "" {
		message += "你的补充说明也已记录，会用来微调后续建议。"
	}
	return message
}
