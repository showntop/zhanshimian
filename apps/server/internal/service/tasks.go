package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/media"
	"github.com/zhanshimian/server/internal/provider"
	"github.com/zhanshimian/server/internal/repository"
)

// TaskHandler processes one claimed task of its type. Handlers persist all
// domain-side results themselves; the worker loop owns the tasks-table state
// transitions. Returning repository.ErrTaskRemoved closes the task silently —
// the target row is gone, so the outcome is moot.
type TaskHandler interface {
	TaskType() domain.TaskType
	Handle(ctx context.Context, task domain.Task) (resultRef string, err error)
}

type analysisTaskHandler struct{ service *Service }

func (analysisTaskHandler) TaskType() domain.TaskType { return domain.TaskTypeAnalysis }

type hairPreviewTaskHandler struct{ service *Service }

func (hairPreviewTaskHandler) TaskType() domain.TaskType { return domain.TaskTypeHairPreview }

type planGroupTaskHandler struct{ service *Service }

func (planGroupTaskHandler) TaskType() domain.TaskType { return domain.TaskTypePlanGroup }

type planLookTaskHandler struct{ service *Service }

func (planLookTaskHandler) TaskType() domain.TaskType { return domain.TaskTypePlanLook }

type todayLookTaskHandler struct{ service *Service }

func (todayLookTaskHandler) TaskType() domain.TaskType { return domain.TaskTypeTodayLook }

type bodyOrbitTaskHandler struct{ service *Service }

func (bodyOrbitTaskHandler) TaskType() domain.TaskType { return domain.TaskTypeBodyOrbit }

// ---- 重试策略（一张表） ----

// taskMaxAttempts is the whole retry budget: content jobs get three runs,
// image renders two. It is mirrored in the repository's zombie-reclaim SQL
// (taskAttemptsCap) — keep the two in sync.
var taskMaxAttempts = map[domain.TaskType]int{
	domain.TaskTypeAnalysis:    3,
	domain.TaskTypeHairPreview: 3,
	domain.TaskTypePlanGroup:   3,
	domain.TaskTypePlanLook:    2,
	domain.TaskTypeTodayLook:   2,
	domain.TaskTypeBodyOrbit:   2,
}

var taskTimeouts = map[domain.TaskType]time.Duration{
	domain.TaskTypeAnalysis:    5 * time.Minute,
	domain.TaskTypeHairPreview: 5 * time.Minute,
	domain.TaskTypePlanGroup:   5 * time.Minute,
	domain.TaskTypePlanLook:    5 * time.Minute,
	domain.TaskTypeTodayLook:   5 * time.Minute,
	domain.TaskTypeBodyOrbit:   5 * time.Minute,
}

// staleAnalysisTimeout 是 GetAnalysis 读路径兜底的孤儿判定阈：
// worker 活着时僵尸回收（10 分钟窗口）与重试（每次认领/重排都刷新
// updated_at）保证行不会静默超过该时长；超过即视为 worker 死亡遗留。
// queued 用更宽的阈（20 分钟），避免多任务占满并发池时排队等待被误杀。
const (
	staleAnalysisProcessingTimeout = 11 * time.Minute
	staleAnalysisQueuedTimeout     = 20 * time.Minute
)

// taskTypeOrder is the claiming order of the single worker loop.
var taskTypeOrder = []domain.TaskType{
	domain.TaskTypeAnalysis,
	domain.TaskTypeHairPreview,
	domain.TaskTypePlanGroup,
	domain.TaskTypePlanLook,
	domain.TaskTypeTodayLook,
	domain.TaskTypeBodyOrbit,
}

// permanentTaskError wraps causes that must never be retried (provider not
// configured, contract violations raised by handlers).
type permanentTaskError struct{ cause error }

func (e *permanentTaskError) Error() string { return e.cause.Error() }
func (e *permanentTaskError) Unwrap() error { return e.cause }

func newPermanentTaskError(cause error) error { return &permanentTaskError{cause: cause} }

// retryableTaskError carries the permanent-error判定集 migrated from the old
// retryPlanLookError: authentication, entitlement and response-contract
// errors will not change on a second identical request.
func retryableTaskError(cause error) bool {
	if cause == nil {
		return false
	}
	var permanent *permanentTaskError
	if errors.As(cause, &permanent) {
		return false
	}
	message := strings.ToLower(cause.Error())
	for _, marker := range []string{
		" returned status 401", " returned status 403", "accessdenied", "unauthorized",
		"authentication", "insufficient_quota", "quota", "unpurchased", "api key",
		"unsupported image format", "unsupported format", "no usable image", "no output",
	} {
		if strings.Contains(message, marker) {
			return false
		}
	}
	return true
}

func terminalTaskFailure(task domain.Task, cause error) bool {
	if errors.Is(cause, repository.ErrTaskRemoved) {
		return true
	}
	maxAttempts := taskMaxAttempts[domain.TaskType(task.Type)]
	return task.Attempts >= maxAttempts || !retryableTaskError(cause)
}

func taskRetryDelay(taskType domain.TaskType, attempt int) time.Duration {
	switch taskType {
	case domain.TaskTypeAnalysis:
		return time.Duration(attempt*attempt) * time.Second
	case domain.TaskTypeHairPreview:
		return time.Duration(attempt) * time.Second
	default:
		return time.Duration(attempt*5) * time.Second
	}
}

// Task failure codes surfaced through error.code.
const (
	taskErrorCodePhotoRejected = "photo_rejected"
	taskErrorCodeTimeout       = "timeout"
	taskErrorCodeTaskFailed    = "task_failed"
)

func taskUserMessage(taskType domain.TaskType) string {
	switch taskType {
	case domain.TaskTypeAnalysis:
		return "分析暂时没有完成，请稍后重试"
	case domain.TaskTypeHairPreview:
		return "预览暂时没有生成，请稍后重试"
	case domain.TaskTypePlanGroup:
		return "方案暂时没有生成，请稍后重试"
	default:
		return "形象图暂时没有生成，请稍后重试"
	}
}

// photoRejectionReasons renders one Chinese reason per rejected photo for the
// task error payload (GET /v1/tasks/{id} error.photo_reasons).
func photoRejectionReasons(rejected *provider.PhotoRejectedError) []string {
	kindNames := map[string]string{"face": "正脸照", "side": "侧脸照", "body": "全身照"}
	reasons := make([]string, 0, len(rejected.Rejections))
	for _, item := range rejected.Rejections {
		name := kindNames[item.Kind]
		if name == "" {
			name = item.Kind
		}
		reasons = append(reasons, name+"："+item.Reason)
	}
	return reasons
}

// taskView converts a stored task into its API shape.
func taskView(task domain.Task) domain.TaskView {
	view := domain.TaskView{
		ID: task.ID, Type: task.Type, Status: task.Status, Progress: task.Progress,
		Stage: task.Stage, ResultRef: task.ResultRef, CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt,
	}
	if task.Status == domain.TaskFailed {
		view.Error = &domain.TaskError{Message: task.LastError}
	}
	return view
}

// decodeTaskErrorView re-applies the failure envelope so clients get a
// machine-readable code and structured per-photo reasons when photos were
// rejected. Plain provider text (transient requeue leftovers) keeps the
// generic task_failed code.
func decodeTaskErrorView(view domain.TaskView) domain.TaskView {
	if view.Error == nil || view.Error.Message == "" {
		return view
	}
	view.Error = &domain.TaskError{Code: taskErrorCodeTaskFailed, Message: view.Error.Message}
	var envelope struct {
		Code         string   `json:"code"`
		Message      string   `json:"message"`
		PhotoReasons []string `json:"photo_reasons"`
	}
	if json.Unmarshal([]byte(view.Error.Message), &envelope) == nil && envelope.Message != "" {
		view.Error = &domain.TaskError{Code: envelope.Code, Message: envelope.Message, PhotoReasons: envelope.PhotoReasons}
		if view.Error.Code == "" {
			view.Error.Code = taskErrorCodeTaskFailed
		}
	}
	return view
}

func viewTasks(tasks []domain.Task) []domain.TaskView {
	views := make([]domain.TaskView, 0, len(tasks))
	for _, task := range tasks {
		views = append(views, decodeTaskErrorView(taskView(task)))
	}
	return views
}

// ---- 任务读取（GET /v1/tasks） ----

// GetCurrentAnalysis returns the user's in-flight analysis (queued/processing).
// Clients that lost the local analysis id (tab switch, storage wipe) use this
// instead of bouncing home.
func (s *Service) GetCurrentAnalysis(ctx context.Context, userID string) (domain.Analysis, error) {
	tasks, err := s.repo.ActiveTasks(ctx, userID, 10)
	if err != nil {
		return domain.Analysis{}, err
	}
	for _, task := range tasks {
		if task.Type != string(domain.TaskTypeAnalysis) {
			continue
		}
		if task.Status != domain.TaskQueued && task.Status != domain.TaskProcessing {
			continue
		}
		var payload domain.AnalysisTaskPayload
		if json.Unmarshal(task.Payload, &payload) != nil || payload.AnalysisID == "" {
			continue
		}
		return s.GetAnalysis(ctx, userID, payload.AnalysisID)
	}
	return domain.Analysis{}, repository.ErrNotFound
}

func (s *Service) GetTask(ctx context.Context, userID, taskID string) (domain.TaskView, error) {
	task, err := s.repo.GetTask(ctx, userID, taskID)
	if err != nil {
		return domain.TaskView{}, err
	}
	return decodeTaskErrorView(taskView(task)), nil
}

func (s *Service) GetTasksByIDs(ctx context.Context, userID string, ids []string) ([]domain.TaskView, error) {
	tasks, err := s.repo.GetTasksByIDs(ctx, userID, ids)
	if err != nil {
		return nil, err
	}
	return viewTasks(tasks), nil
}

// ---- 单个通用认领循环 ----

// RunWorker is the one loop behind all four async flows. Each tick claims at
// most one task per type (in taskTypeOrder) and dispatches it to a bounded
// pool so a slow image render cannot starve analyses. New task types are a
// handler registration in New, not a loop change.
func (s *Service) RunWorker(ctx context.Context, poll time.Duration) {
	if poll <= 0 {
		poll = 700 * time.Millisecond
	}
	semaphore := make(chan struct{}, 4)
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	s.loggerOrDefault().Info("unified task worker started", "poll", poll.String())
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweepFailedTaskCharges(ctx)
			for _, taskType := range taskTypeOrder {
				task, ok, err := s.repo.ClaimTask(ctx, taskType)
				if err != nil {
					s.loggerOrDefault().Error("claim task", "type", string(taskType), "error", err)
					continue
				}
				if !ok {
					continue
				}
				semaphore <- struct{}{}
				go func(task domain.Task) {
					defer func() { <-semaphore }()
					s.runClaimedTask(ctx, task)
				}(task)
			}
		}
	}
}

func (s *Service) runClaimedTask(ctx context.Context, task domain.Task) {
	handler := s.handlers[domain.TaskType(task.Type)]
	if handler == nil {
		s.loggerOrDefault().Error("no handler registered for task", "type", task.Type, "task_id", task.ID)
		failCtx, cancel := failContext()
		defer cancel()
		_ = s.repo.FailTask(failCtx, task.ID, taskErrorCodeTaskFailed, "no handler registered for task type", nil, time.Time{})
		s.refundTaskCharge(failCtx, task.ID)
		return
	}
	s.guardWorkerJob(task.Type, task.ID, func(cause error) {
		s.finishTask(task, cause)
	}, func() {
		jobCtx, cancel := context.WithTimeout(ctx, taskTimeouts[domain.TaskType(task.Type)])
		defer cancel()
		resultRef, err := handler.Handle(jobCtx, task)
		if err == nil {
			writeCtx, writeCancel := writeContext()
			defer writeCancel()
			if completeErr := s.repo.CompleteTask(writeCtx, task.ID, resultRef); completeErr != nil {
				s.loggerOrDefault().Error("complete task", "task_id", task.ID, "error", completeErr)
				s.finishTask(task, completeErr)
			}
			return
		}
		s.finishTask(task, err)
	})
}

// finishTask maps a handler error onto the queue state machine: photo
// rejections fail permanently with per-photo reasons, terminal failures fail
// with a friendly message, and transient ones requeue with backoff.
func (s *Service) finishTask(task domain.Task, cause error) {
	failCtx, cancel := failContext()
	defer cancel()
	var rejected *provider.PhotoRejectedError
	switch {
	case errors.Is(cause, repository.ErrTaskRemoved):
		if err := s.repo.CompleteTask(failCtx, task.ID, ""); err != nil {
			s.loggerOrDefault().Error("close removed-target task", "task_id", task.ID, "error", err)
		}
		s.loggerOrDefault().Info("task target removed; discarding result", "type", task.Type, "task_id", task.ID)
	case errors.As(cause, &rejected):
		if err := s.repo.FailTask(failCtx, task.ID, taskErrorCodePhotoRejected, rejected.UserMessage(), photoRejectionReasons(rejected), time.Time{}); err != nil {
			s.loggerOrDefault().Error("fail task writeback", "task_id", task.ID, "error", err)
		}
		s.refundTaskCharge(failCtx, task.ID)
		s.loggerOrDefault().Error("task failed: photos rejected", "type", task.Type, "task_id", task.ID)
	case terminalTaskFailure(task, cause):
		if err := s.repo.FailTask(failCtx, task.ID, taskErrorCodeTaskFailed, taskUserMessage(domain.TaskType(task.Type)), nil, time.Time{}); err != nil {
			s.loggerOrDefault().Error("fail task writeback", "task_id", task.ID, "error", err)
		}
		s.refundTaskCharge(failCtx, task.ID)
		s.loggerOrDefault().Error("task failed permanently", "type", task.Type, "task_id", task.ID, "attempt", task.Attempts, "error", cause)
	default:
		delay := taskRetryDelay(domain.TaskType(task.Type), task.Attempts)
		if err := s.repo.FailTask(failCtx, task.ID, "", cause.Error(), nil, time.Now().Add(delay)); err != nil {
			s.loggerOrDefault().Error("requeue task writeback", "task_id", task.ID, "error", err)
		}
		s.loggerOrDefault().Warn("task failed, retry scheduled", "type", task.Type, "task_id", task.ID, "attempt", task.Attempts, "retry_in", delay.String(), "error", cause)
	}
}

// ---- handlers：四类任务 ----

func decodeTaskPayload(task domain.Task, target any) error {
	if err := json.Unmarshal(task.Payload, target); err != nil {
		return newPermanentTaskError(fmt.Errorf("decode %s task payload: %w", task.Type, err))
	}
	return nil
}

func (s *Service) processAnalysis(ctx context.Context, task domain.Task) (string, error) {
	var payload domain.AnalysisTaskPayload
	if err := decodeTaskPayload(task, &payload); err != nil {
		return "", err
	}
	input, err := s.repo.GetAnalysisInput(ctx, task.UserID, payload.AnalysisID)
	if errors.Is(err, repository.ErrNotFound) {
		return "", repository.ErrTaskRemoved
	}
	if err != nil {
		return "", err
	}
	// 任务标识随 ctx 传入 AI runtime，让每次调用的日志能关联到具体任务。
	jobCtx := provider.WithInvocationSource(ctx, "analysis:"+payload.AnalysisID)
	reportProgress := func(progress int, stage string) {
		_ = s.repo.UpdateTaskProgress(jobCtx, task.ID, progress, stage)
		_ = s.repo.UpdateAnalysisProgress(jobCtx, payload.AnalysisID, progress, stage)
	}
	reportProgress(maxInt(task.Progress, 15), "正在排队等待分析")
	jobCtx = provider.WithProgressReporter(jobCtx, reportProgress)
	output, err := s.analyzer.Analyze(jobCtx, input)
	if err != nil {
		// failContext 必须在 Analyze 返回后现开：photo_check 一轮就要 1–3 分钟，
		// 开始前开的 10 秒窗口返回时已过期，分析行写不进去。
		failCtx, failCancel := failContext()
		defer failCancel()
		var rejected *provider.PhotoRejectedError
		if errors.As(err, &rejected) {
			// photo_rejected 是永久失败：照片不合格重试也不会变化。
			if failErr := s.repo.FailAnalysisPresentation(failCtx, payload.AnalysisID, "照片未通过检查", rejected.UserMessage()); failErr != nil {
				s.loggerOrDefault().Error("reject analysis writeback", "analysis_id", payload.AnalysisID, "error", failErr)
			}
			return "", err
		}
		if terminalTaskFailure(task, err) {
			if failErr := s.repo.FailAnalysisPresentation(failCtx, payload.AnalysisID, "分析未完成", taskUserMessage(domain.TaskTypeAnalysis)); failErr != nil {
				s.loggerOrDefault().Error("fail analysis writeback", "analysis_id", payload.AnalysisID, "error", failErr)
			}
		}
		return "", err
	}
	reportProgress(82, "正在组合发型、妆容与穿搭方案")
	writeCtx, writeCancel := writeContext()
	defer writeCancel()
	currentImageURL, err := s.analysisPreviewURL(writeCtx, task.UserID, input.MediaIDs)
	if err != nil {
		return "", err
	}
	// The "current" image is source evidence, never a provider-generated or
	// stock reference. This keeps every provider and fallback on the same
	// user-photo contract.
	output.CurrentImageURL = currentImageURL
	reportProgress(95, "正在保存形象档案")
	reportID, err := s.repo.CompleteAnalysis(writeCtx, task.UserID, payload.AnalysisID, output)
	if err != nil {
		if errors.Is(err, repository.ErrTaskRemoved) {
			s.loggerOrDefault().Info("analysis removed while processing; discarding result", "analysis_id", payload.AnalysisID)
			return "", repository.ErrTaskRemoved
		}
		s.loggerOrDefault().Error("complete analysis", "analysis_id", payload.AnalysisID, "error", err)
		return "", err
	}
	return reportID, nil
}

func (s *Service) processHairPreview(ctx context.Context, task domain.Task) (string, error) {
	var payload domain.HairPreviewTaskPayload
	if err := decodeTaskPayload(task, &payload); err != nil {
		return "", err
	}
	input, err := s.repo.GetHairPreviewInput(ctx, task.UserID, payload.PreviewID)
	if errors.Is(err, repository.ErrNotFound) {
		return "", repository.ErrTaskRemoved
	}
	if err != nil {
		return "", err
	}
	jobCtx := provider.WithInvocationSource(ctx, "hair_preview:"+payload.PreviewID)
	_ = s.repo.UpdateTaskProgress(jobCtx, task.ID, 32, "保留五官并重塑发型")
	output, err := s.hairGenerator.Generate(jobCtx, input)
	if err != nil {
		return "", err
	}
	resultURL, storageKey := output.ImageURL, ""
	writeCtx, writeCancel := writeContext()
	defer writeCancel()
	if len(output.ImageData) > 0 {
		extension := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp"}[output.MIMEType]
		if extension == "" {
			return "", newPermanentTaskError(errors.New("hair preview provider returned an unsupported image format"))
		}
		storageKey = fmt.Sprintf("%s/generated/hair/%s%s", task.UserID, newTaskObjectID(), extension)
		storedKey, saveErr := s.storage.Save(writeCtx, storageKey, bytes.NewReader(output.ImageData))
		if saveErr != nil {
			return "", saveErr
		}
		storageKey = storedKey
		resultURL = "/uploads/" + storedKey
	}
	if resultURL == "" {
		return "", newPermanentTaskError(errors.New("hair preview provider returned no image"))
	}
	if err := s.repo.ApplyHairPreviewResult(writeCtx, payload.PreviewID, resultURL, storageKey, output.ProviderVersion); err != nil {
		if storageKey != "" {
			_ = s.storage.Delete(writeCtx, storageKey)
		}
		if errors.Is(err, repository.ErrTaskRemoved) {
			return "", repository.ErrTaskRemoved
		}
		s.loggerOrDefault().Error("complete hair preview", "preview_id", payload.PreviewID, "error", err)
		return "", err
	}
	return payload.PreviewID, nil
}

// processPlanGroup generates the general plan group from the stored report
// (analysis no longer authors plans). Idempotent: an existing group completes
// the task immediately; otherwise AI-authored plans are persisted and their
// look tasks enqueued.
func (s *Service) processPlanGroup(ctx context.Context, task domain.Task) (string, error) {
	var payload domain.PlanGroupTaskPayload
	if err := decodeTaskPayload(task, &payload); err != nil {
		return "", err
	}
	report, err := s.repo.GetReport(ctx, task.UserID, payload.ReportID)
	if errors.Is(err, repository.ErrNotFound) {
		return "", repository.ErrTaskRemoved
	}
	if err != nil {
		return "", err
	}
	jobCtx := provider.WithInvocationSource(ctx, "plan_group:"+payload.ReportID)
	// 幂等兜底：并发触发或重复入队时，已有完整 general 组直接完成。
	existing, err := s.repo.ListPlans(jobCtx, task.UserID, payload.ReportID, "general")
	if err != nil {
		return "", err
	}
	if len(existing) == 0 {
		if s.planGroupGenerator == nil {
			return "", newPermanentTaskError(errors.New("plan group generator is not configured"))
		}
		_ = s.repo.UpdateTaskProgress(jobCtx, task.ID, 32, "正在整理你的形象特点")
		output, err := s.planGroupGenerator.Generate(jobCtx, provider.PlanGroupInput{
			ImpressionTags: report.ImpressionTags,
			PriorityTitle:  report.PriorityTitle,
			PriorityCopy:   report.PriorityCopy,
			Findings:       report.Findings,
		})
		if err != nil {
			return "", err
		}
		_ = s.repo.UpdateTaskProgress(jobCtx, task.ID, 78, "正在保存三套方案")
		writeCtx, writeCancel := writeContext()
		defer writeCancel()
		plans, err := s.repo.UpsertScenePlans(writeCtx, task.UserID, payload.ReportID, domain.ScenePlanInput{Scene: "general", Answers: map[string]string{}}, output.Plans)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return "", repository.ErrTaskRemoved
			}
			s.loggerOrDefault().Error("complete plan group", "report_id", payload.ReportID, "error", err)
			return "", err
		}
		// 方案文字就绪后自动接续形象图任务，与场景方案链路一致。
		if _, err := s.enqueueMissingPlanLooks(writeCtx, task.UserID, plans); err != nil {
			s.loggerOrDefault().Error("enqueue plan looks after group", "report_id", payload.ReportID, "error", err)
		}
	}
	return payload.ReportID, nil
}

func (s *Service) processPlanLook(ctx context.Context, task domain.Task) (string, error) {
	var payload domain.PlanLookTaskPayload
	if err := decodeTaskPayload(task, &payload); err != nil {
		return "", err
	}
	job, err := s.repo.GetPlanLookJob(ctx, task.UserID, payload.PlanID)
	if errors.Is(err, repository.ErrNotFound) {
		return "", repository.ErrTaskRemoved
	}
	if err != nil {
		return "", err
	}
	jobCtx := provider.WithInvocationSource(ctx, "plan_look:"+job.PlanID)
	if s.lookGenerator == nil {
		return "", newPermanentTaskError(errors.New("plan look generator is not configured"))
	}
	output, err := s.lookGenerator.Generate(jobCtx, provider.LookInput{Name: job.Name, Slug: job.Slug, Why: job.Why, Steps: job.Steps, MediaIDs: job.MediaIDs})
	if err != nil {
		return "", err
	}
	extension := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp"}[output.MIMEType]
	if extension == "" {
		return "", newPermanentTaskError(errors.New("look provider returned an unsupported image format"))
	}
	// 生成可能已经用满 jobCtx，存储与回写改用独立上下文，避免成果被丢弃重排。
	writeCtx, writeCancel := writeContext()
	defer writeCancel()
	storageKey := fmt.Sprintf("%s/generated/looks/%s%s", job.UserID, newTaskObjectID(), extension)
	storedKey, saveErr := s.storage.Save(writeCtx, storageKey, bytes.NewReader(output.ImageData))
	if saveErr != nil {
		return "", saveErr
	}
	if err := s.repo.ApplyPlanLookResult(writeCtx, job.PlanID, "/uploads/"+storedKey, storedKey, output.ProviderVersion); err != nil {
		_ = s.storage.Delete(writeCtx, storedKey)
		if errors.Is(err, repository.ErrTaskRemoved) {
			return "", repository.ErrTaskRemoved
		}
		s.loggerOrDefault().Error("complete plan look", "plan_id", job.PlanID, "error", err)
		return "", err
	}
	return job.PlanID, nil
}

func (s *Service) processTodayLook(ctx context.Context, task domain.Task) (string, error) {
	var payload domain.TodayLookTaskPayload
	if err := decodeTaskPayload(task, &payload); err != nil {
		return "", err
	}
	job, err := s.repo.GetTodayPlanLookJob(ctx, task.UserID, payload.PlanID)
	if errors.Is(err, repository.ErrNotFound) {
		return "", repository.ErrTaskRemoved
	}
	if err != nil {
		return "", err
	}
	jobCtx := provider.WithInvocationSource(ctx, "today_look:"+job.PlanID)
	if s.lookGenerator == nil {
		return "", newPermanentTaskError(errors.New("plan look generator is not configured"))
	}
	steps := make([]domain.PlanStep, 0, len(job.Steps))
	for _, step := range job.Steps {
		steps = append(steps, domain.PlanStep{Category: step.Category, Title: step.Title, Summary: step.Copy})
	}
	output, err := s.lookGenerator.Generate(jobCtx, provider.LookInput{Name: job.Title, Why: job.Summary, Steps: steps, MediaIDs: job.MediaIDs})
	if err != nil {
		return "", err
	}
	extension := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp"}[output.MIMEType]
	if extension == "" {
		return "", newPermanentTaskError(errors.New("look provider returned an unsupported image format"))
	}
	// 生成可能已经用满 jobCtx，存储与回写改用独立上下文，避免成果被丢弃重排。
	writeCtx, writeCancel := writeContext()
	defer writeCancel()
	storageKey := fmt.Sprintf("%s/generated/today/%s%s", job.UserID, newTaskObjectID(), extension)
	storedKey, saveErr := s.storage.Save(writeCtx, storageKey, bytes.NewReader(output.ImageData))
	if saveErr != nil {
		return "", saveErr
	}
	if err := s.repo.ApplyTodayLookResult(writeCtx, job.PlanID, "/uploads/"+storedKey, storedKey, output.ProviderVersion); err != nil {
		_ = s.storage.Delete(writeCtx, storedKey)
		if errors.Is(err, repository.ErrTaskRemoved) {
			return "", repository.ErrTaskRemoved
		}
		s.loggerOrDefault().Error("complete today plan look", "plan_id", job.PlanID, "error", err)
		return "", err
	}
	return job.PlanID, nil
}

func (s *Service) processBodyOrbit(ctx context.Context, task domain.Task) (string, error) {
	var payload domain.BodyOrbitTaskPayload
	if err := decodeTaskPayload(task, &payload); err != nil {
		return "", err
	}
	work, err := s.repo.GetBodyOrbitWork(ctx, task.UserID, payload.PresentationID)
	if errors.Is(err, repository.ErrNotFound) {
		return "", repository.ErrTaskRemoved
	}
	if err != nil {
		return "", err
	}
	jobCtx := provider.WithInvocationSource(ctx, "body_orbit:"+payload.PresentationID)
	_ = s.repo.UpdateTaskProgress(jobCtx, task.ID, 12, "正在读取全身和正脸")
	body, face, bodyMIME, faceMIME, err := s.loadOrbitPhotos(jobCtx, work)
	if err != nil {
		return "", err
	}
	if s.orbitGenerator == nil {
		return "", newPermanentTaskError(errors.New("orbit generator is not configured"))
	}
	_ = s.repo.UpdateTaskProgress(jobCtx, task.ID, 40, "正在生成环绕预览")
	output, err := s.orbitGenerator.Generate(jobCtx, provider.OrbitInput{
		Body: body, Face: face, BodyMIME: bodyMIME, FaceMIME: faceMIME,
	})
	if err != nil {
		return "", err
	}
	if len(output.VideoData) == 0 || output.MIMEType != "video/mp4" {
		return "", newPermanentTaskError(errors.New("orbit provider returned an unsupported video format"))
	}
	_ = s.repo.UpdateTaskProgress(jobCtx, task.ID, 72, "正在抽出转盘静帧")
	var extracted []media.Frame
	var extractErr error
	if s.orbitExtractor != nil {
		extracted, extractErr = s.orbitExtractor.Extract(jobCtx, output.VideoData, output.Duration, media.OrbitFrameCount)
	} else {
		extractErr = errors.New("orbit extractor is not configured")
	}
	if extractErr != nil || len(extracted) < media.OrbitMinKeepFrames {
		s.loggerOrDefault().Warn("body orbit extract degraded; keeping video without frames",
			"presentation_id", payload.PresentationID, "frames", len(extracted), "error", extractErr)
		extracted = nil
	}

	writeCtx, writeCancel := writeContext()
	defer writeCancel()
	_ = s.repo.UpdateTaskProgress(writeCtx, task.ID, 95, "正在保存")
	videoKey := fmt.Sprintf("%s/generated/body-orbit/%s.mp4", task.UserID, payload.PresentationID)
	storedVideoKey, saveErr := s.storage.Save(writeCtx, videoKey, bytes.NewReader(output.VideoData))
	if saveErr != nil {
		return "", saveErr
	}
	savedKeys := []string{storedVideoKey}
	videoURL := "/uploads/" + storedVideoKey
	orbitFrames := make([]domain.OrbitFrame, 0, len(extracted))
	frameKeys := make([]string, 0, len(extracted))
	for i, frame := range extracted {
		key := fmt.Sprintf("%s/generated/body-orbit/%s/%d.jpg", task.UserID, payload.PresentationID, i)
		storedKey, frameErr := s.storage.Save(writeCtx, key, bytes.NewReader(frame.JPEG))
		if frameErr != nil {
			for _, saved := range savedKeys {
				_ = s.storage.Delete(writeCtx, saved)
			}
			return "", frameErr
		}
		savedKeys = append(savedKeys, storedKey)
		orbitFrames = append(orbitFrames, domain.OrbitFrame{Yaw: frame.Yaw, URL: "/uploads/" + storedKey})
		frameKeys = append(frameKeys, storedKey)
	}
	durationMS := int(output.Duration / time.Millisecond)
	if err := s.repo.ApplyBodyOrbitResult(writeCtx, payload.PresentationID, videoURL, storedVideoKey, durationMS, orbitFrames, frameKeys, output.ProviderVersion); err != nil {
		for _, saved := range savedKeys {
			_ = s.storage.Delete(writeCtx, saved)
		}
		if errors.Is(err, repository.ErrTaskRemoved) {
			return "", repository.ErrTaskRemoved
		}
		s.loggerOrDefault().Error("complete body orbit", "presentation_id", payload.PresentationID, "error", err)
		return "", err
	}
	return payload.PresentationID, nil
}

func (s *Service) loadOrbitPhotos(ctx context.Context, work domain.BodyPresentationInput) (body, face []byte, bodyMIME, faceMIME string, err error) {
	images, err := s.mediaLoader.Load(provider.WithImageBudget(ctx, provider.ImageBudgetEdit), []string{work.BodyMediaID, work.FaceMediaID})
	if errors.Is(err, repository.ErrNotFound) {
		return nil, nil, "", "", newPermanentTaskError(errors.New("body orbit photos are missing"))
	}
	if err != nil {
		return nil, nil, "", "", err
	}
	for _, img := range images {
		switch img.Kind {
		case "body":
			body, bodyMIME = img.Data, img.MIMEType
		case "face":
			face, faceMIME = img.Data, img.MIMEType
		}
	}
	if len(body) == 0 || len(face) == 0 {
		return nil, nil, "", "", newPermanentTaskError(errors.New("body orbit photos are missing"))
	}
	return body, face, bodyMIME, faceMIME, nil
}

func (h analysisTaskHandler) Handle(ctx context.Context, task domain.Task) (string, error) {
	return h.service.processAnalysis(ctx, task)
}

func (h hairPreviewTaskHandler) Handle(ctx context.Context, task domain.Task) (string, error) {
	return h.service.processHairPreview(ctx, task)
}

func (h planGroupTaskHandler) Handle(ctx context.Context, task domain.Task) (string, error) {
	return h.service.processPlanGroup(ctx, task)
}

func (h planLookTaskHandler) Handle(ctx context.Context, task domain.Task) (string, error) {
	return h.service.processPlanLook(ctx, task)
}

func (h todayLookTaskHandler) Handle(ctx context.Context, task domain.Task) (string, error) {
	return h.service.processTodayLook(ctx, task)
}

func (h bodyOrbitTaskHandler) Handle(ctx context.Context, task domain.Task) (string, error) {
	return h.service.processBodyOrbit(ctx, task)
}

// ---- worker 基础设施（沿用已验证语义） ----

// loggerOrDefault 兜底 nil logger：测试常用结构体字面量构造 Service，
// 失败路径的日志不允许因缺 logger 而 panic。
func (s *Service) loggerOrDefault() *slog.Logger {
	if s.logger != nil {
		return s.logger
	}
	return slog.Default()
}

// failContext 返回一个从 context.Background() 派生的 10 秒超时上下文，
// 专供任务失败后的状态回写使用。worker 的 jobCtx 在 provider 调用因 5 分钟
// 超时取消后已经过期，继续用它调回写会让 SQL 全部因 DeadlineExceeded 失败，
// 任务永远停在 running。
// failWriteTimeout 是失败回写窗口。测试里会收短，用来证明长 Analyze 之后
// 仍必须在回写当下开新的 failContext，而不是复用 Analyze 开始时的那个。
var failWriteTimeout = 10 * time.Second

func failContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), failWriteTimeout)
}

// writeContext 返回一个独立的 1 分钟超时上下文，供生成完成后的对象存储写入
// 与状态回写使用。provider 调用可能几乎用满 jobCtx 的 5 分钟，继续用 jobCtx
// 会把已经生成完的图丢弃并把任务重新排队，用户看到的是方案一直"生成中"。
func writeContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), time.Minute)
}

// guardWorkerJob 把单个任务的 panic 转成普通失败回写，避免一个"毒任务"直接
// 崩掉 worker 进程（RUN_WORKER 内嵌时会连 API 一起崩），导致所有已领取的
// 任务永远停在 running。
func (s *Service) guardWorkerJob(kind, id string, fail func(error), run func()) {
	defer func() {
		if recovered := recover(); recovered != nil {
			s.loggerOrDefault().Error("worker job panicked", "kind", kind, "id", id, "panic", recovered)
			if fail != nil {
				fail(fmt.Errorf("worker %s job panicked: %v", kind, recovered))
			}
		}
	}()
	run()
}

func maxInt(value, floor int) int {
	if value < floor {
		return floor
	}
	return value
}

// newTaskObjectID names generated objects distinctly from user uploads.
func newTaskObjectID() string { return uuid.NewString() }
