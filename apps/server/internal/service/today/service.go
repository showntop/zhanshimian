package today

import (
	"context"
	"time"

	"github.com/zhanshimian/server/internal/domain"
)

// Plan 是持久化的今日方案（OpenAPI TodayPlan 形状）。与旧 domain.TodayPlan 不同：
// 没有 look_task / generated_image_url / provider 等内部字段，只保留公开状态。
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
}

func New(reader Reader, writer Writer, planner TodayPlanner, weather WeatherProvider, clock Clock) *Service {
	return &Service{reader: reader, writer: writer, planner: planner, weather: weather, clock: clock}
}

// Generate 从质量核心 grounding 生成并落库一套今日方案。
func (s *Service) Generate(ctx context.Context, userID string, input CreateInput) (Plan, error) {
	grounding, err := s.reader.ReadTodayGrounding(ctx, userID)
	if err != nil {
		return Plan{}, err
	}

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
		Schedule:     input.Schedule,
	})
	if err != nil {
		return Plan{}, err
	}

	now := s.clock.Now()
	return s.writer.CreateTodayPlan(ctx, userID, Plan{
		ReportID:  grounding.ReportID,
		Context:   domain.TodayContext{City: weather.City, Condition: weather.Condition, Temperature: weather.Temperature, Schedule: input.Schedule},
		Title:     output.Title,
		Summary:   output.Summary,
		Steps:     mapSteps(output.Steps),
		State:     "planning",
		CreatedAt: now, UpdatedAt: now,
	})
}

func (s *Service) Current(ctx context.Context, userID string) (Plan, error) {
	return s.writer.CurrentTodayPlan(ctx, userID)
}

func (s *Service) Activate(ctx context.Context, userID string, id string) (Plan, error) {
	return s.writer.MarkTodayPlanActive(ctx, userID, id)
}

func (s *Service) Feedback(ctx context.Context, userID string, id string, feedback string) (Plan, error) {
	return s.writer.RecordTodayPlanFeedback(ctx, userID, id, feedback)
}

// Context 返回今日的天气/日程上下文（GET /v1/today/context）。
func (s *Service) Context(ctx context.Context, city string, schedule string) domain.TodayContext {
	now := s.clock.Now()
	ctxOut := domain.TodayContext{
		Date:     now.Format("2006-01-02"),
		City:     city,
		Schedule: schedule,
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
