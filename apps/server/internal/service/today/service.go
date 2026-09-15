package today

import (
	"context"
	"strings"
	"time"

	"github.com/zhanshimian/server/internal/domain"
)

// Plan 是持久化的今日方案（OpenAPI TodayPlan 形状）。与旧 domain.TodayPlan 不同：
// 只保留公开状态字段，不含任务/生成器等内部细节。
type Plan struct {
	ID        string                  `json:"id"`
	ReportID  string                  `json:"-"`
	Context   domain.TodayContext     `json:"context"`
	Title     string                  `json:"title"`
	Summary   string                  `json:"summary"`
	Steps     []domain.TodayPlanStep  `json:"steps"`
	Active    bool                    `json:"active"`
	State     string                  `json:"state"`
	Operation domain.OperationRef     `json:"operation"`
	Media     *domain.RenderMediaView `json:"media"`
	Feedback  *string                 `json:"feedback,omitempty"`
	CreatedAt time.Time               `json:"created_at"`
	UpdatedAt time.Time               `json:"updated_at"`

	// MediaObjectKey/MediaMIMEType 由读取侧 join media_assets 得到，供呈现层
	// 即时签名（与 share.Card.ObjectKey 同一做法）；不出现在 JSON。
	MediaObjectKey string `json:"-"`
	MediaMIMEType  string `json:"-"`
}

type CreateInput struct {
	City     string
	Schedule string
	ReportID string
	Refresh  bool
}

// WeatherProvider 是今日方案服务的天气依赖，返回本包的 Weather。
type WeatherProvider interface {
	Current(ctx context.Context, city string) (Weather, error)
}

// Writer 持久化与读取今日方案。
type Writer interface {
	CreateTodayPlan(ctx context.Context, userID string, plan Plan) (Plan, error)
	CurrentTodayPlan(ctx context.Context, userID string) (Plan, error)
	MarkTodayPlanActive(ctx context.Context, userID string, id string) (Plan, error)
	RecordTodayPlanFeedback(ctx context.Context, userID string, id string, feedback string) (Plan, error)
}

// Clock 是时间源的最小抽象。
type Clock interface{ Now() time.Time }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func NewClock() Clock { return systemClock{} }

type Service struct {
	reader  Reader
	writer  Writer
	planner TodayPlanner
	weather WeatherProvider
	clock   Clock
	signer  MediaSigner
}

func New(reader Reader, writer Writer, planner TodayPlanner, weather WeatherProvider, clock Clock) *Service {
	return &Service{reader: reader, writer: writer, planner: planner, weather: weather, clock: clock}
}

// WithMediaSigner 装配读路径媒体签名器（与 assessment.WithBilling 同一链式做法）。
func (s *Service) WithMediaSigner(signer MediaSigner) *Service {
	s.signer = signer
	return s
}

// Generate 从质量核心 grounding 生成并落库一套今日方案。
func (s *Service) Generate(ctx context.Context, userID string, input CreateInput) (Plan, error) {
	grounding, err := s.reader.ReadTodayGrounding(ctx, userID)
	if err != nil {
		return Plan{}, err
	}

	now := s.clock.Now()
	schedule := scheduleOrDefault(now, input.Schedule)

	weather := Weather{City: input.City}
	if s.weather != nil {
		if w, err := s.weather.Current(ctx, input.City); err == nil {
			weather = w
		}
	}

	output, err := s.planner.Generate(ctx, TodayPlanRequest{
		ReportID:     grounding.ReportID,
		Profile:      grounding.Profile,
		Findings:     grounding.Findings,
		SelectedPlan: grounding.SelectedPlan,
		Weather:      weather,
		Schedule:     schedule,
	})
	if err != nil {
		return Plan{}, err
	}

	return s.writer.CreateTodayPlan(ctx, userID, Plan{
		ReportID:  grounding.ReportID,
		Context:   domain.TodayContext{City: weather.City, Condition: weather.Condition, Temperature: weather.Temperature, Schedule: schedule},
		Title:     output.Title,
		Summary:   output.Summary,
		Steps:     mapSteps(output.Steps),
		State:     "planning",
		CreatedAt: now, UpdatedAt: now,
	})
}

// scheduleOrDefault 恢复旧线 buildTodayContext 的日程默认推导：日程为空时
// 按工作日/休息日给默认并进 AI prompt。红线：场景叫「日常」不叫「通勤」，
// 措辞与 Context 的 DayType（工作日/周末）及 packages/core SCENES 对齐。
func scheduleOrDefault(now time.Time, schedule string) string {
	if strings.TrimSpace(schedule) != "" {
		return schedule
	}
	switch now.Weekday() {
	case time.Saturday, time.Sunday:
		return "休息"
	default:
		return "日常"
	}
}

func (s *Service) Current(ctx context.Context, userID string) (Plan, error) {
	plan, err := s.writer.CurrentTodayPlan(ctx, userID)
	if err != nil {
		return Plan{}, err
	}
	return plan, s.signPlanMedia(ctx, &plan)
}

func (s *Service) Activate(ctx context.Context, userID string, id string) (Plan, error) {
	plan, err := s.writer.MarkTodayPlanActive(ctx, userID, id)
	if err != nil {
		return Plan{}, err
	}
	return plan, s.signPlanMedia(ctx, &plan)
}

func (s *Service) Feedback(ctx context.Context, userID string, id string, feedback string) (Plan, error) {
	plan, err := s.writer.RecordTodayPlanFeedback(ctx, userID, id, feedback)
	if err != nil {
		return Plan{}, err
	}
	return plan, s.signPlanMedia(ctx, &plan)
}

// signPlanMedia 给发布媒体补可读 URL：对象定位来自读模型 join，签名经
// MediaSigner；未装配签名器时保持无 URL（单测/降级组装）。URL 与 MIMEType
// 必须一起下发——客户端投影对空 url 或 generated 非 image/jpeg 一律拒渲染。
func (s *Service) signPlanMedia(ctx context.Context, plan *Plan) error {
	if plan.Media == nil || s.signer == nil || plan.MediaObjectKey == "" {
		return nil
	}
	url, expiresAt, err := s.signer.SignedURL(ctx, plan.MediaObjectKey)
	if err != nil {
		return err
	}
	plan.Media.URL = url
	plan.Media.URLExpiresAt = expiresAt
	if plan.Media.MIMEType == "" {
		plan.Media.MIMEType = plan.MediaMIMEType
	}
	return nil
}

// Context 返回今日的天气/日程上下文（GET /v1/today/context）。
func (s *Service) Context(ctx context.Context, city string, schedule string) domain.TodayContext {
	now := s.clock.Now()
	ctxOut := domain.TodayContext{
		Date:     now.Format("2006-01-02"),
		City:     city,
		Schedule: scheduleOrDefault(now, schedule),
	}
	switch now.Weekday() {
	case time.Saturday, time.Sunday:
		ctxOut.DayType = "周末"
	default:
		ctxOut.DayType = "工作日"
	}
	if s.weather != nil {
		if w, err := s.weather.Current(ctx, city); err == nil {
			ctxOut.City = w.City
			ctxOut.Condition = w.Condition
			ctxOut.Temperature = w.Temperature
		}
	}
	return ctxOut
}

func mapSteps(steps []TodayPlanStep) []domain.TodayPlanStep {
	out := make([]domain.TodayPlanStep, 0, len(steps))
	for _, step := range steps {
		out = append(out, domain.TodayPlanStep{Category: step.Category, Label: step.Label, Title: step.Title, Copy: step.Copy})
	}
	return out
}
