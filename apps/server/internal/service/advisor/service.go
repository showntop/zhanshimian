package advisor

import (
	"context"

	"github.com/zhanshimian/server/internal/domain"
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
}

func New(reader Reader, writer Writer, chat Chat) *Service {
	return &Service{reader: reader, writer: writer, chat: chat}
}

// Send 读取只读 grounding → 生成回复 → 追加会话。
// grounding 只含 ProfileSnapshot{HeightCM, Role, Budget, Preferences, Avoidances}、
// 报告 observation/recommendation、所选方案步骤、衣橱与反馈记忆——不含身体测量。
func (s *Service) Send(ctx context.Context, userID string, input SendInput) (domain.AdvisorMessage, error) {
	grounding, err := s.reader.ReadAdvisorGrounding(ctx, userID)
	if err != nil {
		return domain.AdvisorMessage{}, err
	}
	reply, err := s.chat.Reply(ctx, input.Content, grounding)
	if err != nil {
		return domain.AdvisorMessage{}, err
	}
	return s.writer.AppendExchange(ctx, userID, input.Content, reply, nil)
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
