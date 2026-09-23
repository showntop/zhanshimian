package today

import (
	"context"

	"github.com/zhanshimian/server/internal/domain"
)

// Weather 是今日方案生成所需的天气快照（provider 无关，由 bootstrap 适配）。
type Weather struct {
	City        string
	Condition   string
	Temperature int
}

// TodayPlanRequest 是今日方案生成所需的质量核心 grounding。
type TodayPlanRequest struct {
	ReportID     string
	Profile      domain.ProfileSnapshot
	Findings     []domain.FindingGrounding
	SelectedPlan *domain.PlanVariantGrounding
	Weather      Weather
	Schedule     string
}

// TodayPlanStep 是生成方案里的一步建议。
type TodayPlanStep struct {
	Category string
	Label    string
	Title    string
	Copy     string
}

// TodayPlanOutput 是校验通过的生成器输出。
type TodayPlanOutput struct {
	Title   string
	Summary string
	Steps   []TodayPlanStep
}

// TodayPlanner 从质量核心 grounding 生成一套今日方案。
// 接口由消费方（today 服务）定义，provider/ai 提供实现——避免 service 反向依赖 provider/ai。
type TodayPlanner interface {
	Generate(context.Context, TodayPlanRequest) (TodayPlanOutput, error)
}
