package advisor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/limits"
)

// 顾问用量限额（与 legacy billing_rules decideAdvisor 一致）。
// USAGE_ADVISOR_PER_DAY / USAGE_ADVISOR_PER_HOUR 可覆盖，0 = 不限（内测用）。
var (
	limitAdvisorPerDay  = limits.FromEnv(limits.EnvAdvisorPerDay, 20)
	limitAdvisorPerHour = limits.FromEnv(limits.EnvAdvisorPerHour, 10)
)

var (
	// ErrValidation 标记无效输入：httpapi 映射 400。
	ErrValidation = errors.New("advisor validation error")
	// ErrRateLimited 标记用量超限：httpapi 映射 429 rate_limited（与
	// account/billing 的各自限流哨兵同一公开形状；不跨包引用 billing 避免
	// postgres→advisor→billing 导入环）。
	ErrRateLimited = errors.New("rate limited")
)

// Chat 是顾问对话的 AI 能力接口：由消费方（advisor 服务）定义，
// provider/ai 提供实现——避免 service→provider/ai 的导入环。
type Chat interface {
	Reply(ctx context.Context, question string, grounding any) (string, error)
}

// Writer 持久化顾问会话。
type Writer interface {
	// AppendExchange 追加一轮问答（用户消息 + 助手回复），返回助手消息。
	AppendExchange(ctx context.Context, userID string, question string, reply string, actions []domain.AdvisorAction) (domain.AdvisorMessage, error)
	// ListMessages 返回当前会话的全部消息。
	ListMessages(ctx context.Context, userID string) ([]domain.AdvisorMessage, error)
	// ApplyAction 标记一个动作已应用，返回更新后的动作。
	ApplyAction(ctx context.Context, userID string, actionID string) (domain.AdvisorAction, error)
}

type SendInput struct {
	Content string
}

type Service struct {
	reader Reader
	writer Writer
	chat   Chat
	usage  UsageGate
}

func New(reader Reader, writer Writer, chat Chat) *Service {
	return &Service{reader: reader, writer: writer, chat: chat}
}

// WithUsageGate 装配顾问用量闸（与 assessment.WithBilling 同一链式做法）。
// Nil 容忍：未装配时不限流（单测/降级组装）。
func (s *Service) WithUsageGate(gate UsageGate) *Service {
	s.usage = gate
	return s
}

// Send 读取只读 grounding → 生成回复 → 追加会话。
// grounding 只含 ProfileSnapshot{HeightCM, Role, Budget, Preferences, Avoidances}、
// 报告 observation/recommendation、所选方案步骤、衣橱与反馈记忆——不含身体测量。
func (s *Service) Send(ctx context.Context, userID string, input SendInput) (domain.AdvisorMessage, error) {
	if strings.TrimSpace(input.Content) == "" || len([]rune(input.Content)) > 500 {
		return domain.AdvisorMessage{}, fmt.Errorf("%w: 请输入 1–500 字的问题", ErrValidation)
	}
	if err := s.charge(ctx, userID); err != nil {
		return domain.AdvisorMessage{}, err
	}
	grounding, err := s.reader.ReadAdvisorGrounding(ctx, userID)
	if err != nil {
		return domain.AdvisorMessage{}, err
	}
	reply, err := s.chat.Reply(ctx, input.Content, GroundingView(grounding))
	if err != nil {
		return domain.AdvisorMessage{}, err
	}
	return s.writer.AppendExchange(ctx, userID, input.Content, reply, buildActions(grounding))
}

// charge 顾问用量闸：20 次/日 + 10 次/时，仓储自计数不扣次数
// （旧线 decideAdvisor 语义：先日界后时界，只 bump usage 不动钱包）。
func (s *Service) charge(ctx context.Context, userID string) error {
	if s.usage == nil {
		return nil
	}
	return s.usage.ApplyBilling(ctx, userID, time.Now(), 0, func(snap domain.BillingSnapshot) (domain.BillingDecision, error) {
		if limitAdvisorPerDay > 0 && snap.DayAdvisor+1 > limitAdvisorPerDay {
			return domain.BillingDecision{}, fmt.Errorf("%w: 今日咨询次数已用完，明天再来", ErrRateLimited)
		}
		if limitAdvisorPerHour > 0 && snap.HourAdvisor+1 > limitAdvisorPerHour {
			return domain.BillingDecision{}, fmt.Errorf("%w: 咨询过于频繁，请稍后再试", ErrRateLimited)
		}
		return domain.BillingDecision{DayAdvisorDelta: 1, HourAdvisorDelta: 1}, nil
	})
}

// buildActions 恢复旧线动作卡：每条顾问回复带一个「加入今日调整」动作，
// payload 记录落地目标与可用上下文（report/衣橱单品），客户端按 label 渲染、
// 按 id 应用。动作类型对齐契约 AdvisorAction 的 adjust_plan_step。
func buildActions(g Grounding) []domain.AdvisorAction {
	payload := map[string]any{"target": "today_plan"}
	if g.Report != nil && g.Report.ID != "" {
		payload["report_id"] = g.Report.ID
	}
	if len(g.Wardrobe) > 0 {
		ids := make([]string, 0, len(g.Wardrobe))
		for _, item := range g.Wardrobe {
			ids = append(ids, item.ID)
		}
		payload["wardrobe_item_ids"] = ids
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	return []domain.AdvisorAction{{
		ID: uuid.NewString(), Kind: "adjust_plan_step", Label: "加入今日调整", Payload: payloadJSON,
	}}
}

func (s *Service) List(ctx context.Context, userID string) ([]domain.AdvisorMessage, error) {
	return s.writer.ListMessages(ctx, userID)
}

func (s *Service) ApplyAction(ctx context.Context, userID string, actionID string) (domain.AdvisorAction, error) {
	return s.writer.ApplyAction(ctx, userID, actionID)
}

// groundingView 是给 AI 的上下文投影：显式排除身体测量字段。
type groundingView struct {
	Role         string                         `json:"role"`
	HeightCM     int                            `json:"height_cm"`
	Budget       string                         `json:"budget"`
	Report       *domain.ReportGrounding        `json:"report,omitempty"`
	SelectedPlan *domain.PlanVariantGrounding   `json:"selected_plan,omitempty"`
	Wardrobe     []domain.WardrobeGroundingItem `json:"wardrobe,omitempty"`
	Feedback     []domain.FeedbackMemoryItem    `json:"feedback,omitempty"`
}

// GroundingView 把 grounding 转成 AI 上下文：只含 role/身高/预算，
// 不含 weight_kg/bust_cm/waist_cm/hip_cm。
func GroundingView(g Grounding) groundingView {
	return groundingView{
		Role: g.Profile.Role, HeightCM: g.Profile.HeightCM, Budget: g.Profile.Budget,
		Report: g.Report, SelectedPlan: g.SelectedPlan, Wardrobe: g.Wardrobe, Feedback: g.Feedback,
	}
}
