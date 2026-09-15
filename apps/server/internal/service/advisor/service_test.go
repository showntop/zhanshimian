package advisor_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/service/advisor"
)

// 编译期契约：Reader 的签名是本计划冻结的边界。
type advisorReaderFake struct {
	grounding advisor.Grounding
	err       error
}

func (f advisorReaderFake) ReadAdvisorGrounding(context.Context, string) (advisor.Grounding, error) {
	return f.grounding, f.err
}

var _ advisor.Reader = advisorReaderFake{}

type writerFake struct {
	message domain.AdvisorMessage
	actions []domain.AdvisorAction
}

func (f *writerFake) AppendExchange(_ context.Context, _ string, _ string, reply string, actions []domain.AdvisorAction) (domain.AdvisorMessage, error) {
	f.actions = actions
	return domain.AdvisorMessage{ID: "msg-1", ConversationID: "conv-1", Role: "assistant", Content: reply, Actions: actions, CreatedAt: time.Now()}, nil
}

func (f *writerFake) ListMessages(context.Context, string) ([]domain.AdvisorMessage, error) {
	return []domain.AdvisorMessage{f.message}, nil
}

func (f *writerFake) ApplyAction(_ context.Context, _ string, actionID string) (domain.AdvisorAction, error) {
	for _, action := range f.actions {
		if action.ID == actionID {
			action.Applied = true
			return action, nil
		}
	}
	return domain.AdvisorAction{}, errors.New("not found")
}

type chatFake struct {
	reply string
	err   error
}

func (f chatFake) Reply(context.Context, string, any) (string, error) { return f.reply, f.err }

type usageGateFake struct {
	snap     domain.BillingSnapshot
	decision domain.BillingDecision
	calls    int
	err      error
}

func (f *usageGateFake) ApplyBilling(_ context.Context, _ string, _ time.Time, _ int, decide func(domain.BillingSnapshot) (domain.BillingDecision, error)) error {
	if f.err != nil {
		return f.err
	}
	f.calls++
	decision, err := decide(f.snap)
	if err != nil {
		return err
	}
	f.decision = decision
	return nil
}

func newSendService(gate advisor.UsageGate, grounding advisor.Grounding) (*advisor.Service, *writerFake) {
	writer := &writerFake{}
	svc := advisor.New(advisorReaderFake{grounding: grounding}, writer, chatFake{reply: "可以，先保留下装的垂感。"})
	if gate != nil {
		svc = svc.WithUsageGate(gate)
	}
	return svc, writer
}

// 动作卡恢复（旧线语义）：每条顾问回复必须带一个可应用动作，否则客户端
// 动作按钮 UI 永远渲染不出。
func TestSendAttachesAdjustPlanAction(t *testing.T) {
	grounding := advisor.Grounding{
		Report:   &domain.ReportGrounding{ID: "report-1"},
		Wardrobe: []domain.WardrobeGroundingItem{{ID: "item-1"}, {ID: "item-2"}},
	}
	svc, _ := newSendService(&usageGateFake{}, grounding)
	message, err := svc.Send(context.Background(), "user-1", advisor.SendInput{Content: "明天面试怎么穿？"})
	if err != nil {
		t.Fatal(err)
	}
	if len(message.Actions) != 1 {
		t.Fatalf("actions = %d, want 1", len(message.Actions))
	}
	action := message.Actions[0]
	if action.ID == "" || action.Kind != "adjust_plan_step" || action.Label != "加入今日调整" || action.Applied {
		t.Fatalf("action = %#v", action)
	}
	var payload map[string]any
	if err := json.Unmarshal(action.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["target"] != "today_plan" || payload["report_id"] != "report-1" {
		t.Fatalf("payload = %v", payload)
	}
	ids, ok := payload["wardrobe_item_ids"].([]any)
	if !ok || len(ids) != 2 {
		t.Fatalf("wardrobe_item_ids = %v", payload["wardrobe_item_ids"])
	}
}

// 无报告/衣橱时动作卡仍在，payload 只带落地目标。
func TestSendAttachesActionWithoutGrounding(t *testing.T) {
	svc, _ := newSendService(&usageGateFake{}, advisor.Grounding{})
	message, err := svc.Send(context.Background(), "user-1", advisor.SendInput{Content: "今天怎么穿？"})
	if err != nil {
		t.Fatal(err)
	}
	if len(message.Actions) != 1 {
		t.Fatalf("actions = %d, want 1", len(message.Actions))
	}
	var payload map[string]any
	if err := json.Unmarshal(message.Actions[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["target"] != "today_plan" || payload["report_id"] != nil || payload["wardrobe_item_ids"] != nil {
		t.Fatalf("payload = %v", payload)
	}
}

// 输入闸：1–500 字（旧线 SendAdvisorMessage 同款），超限不进入 AI 与计费。
func TestSendRejectsOutOfRangeContent(t *testing.T) {
	gate := &usageGateFake{}
	svc, _ := newSendService(gate, advisor.Grounding{})
	for _, content := range []string{"", "   ", strings.Repeat("字", 501)} {
		if _, err := svc.Send(context.Background(), "user-1", advisor.SendInput{Content: content}); !errors.Is(err, advisor.ErrValidation) {
			t.Fatalf("content %q error = %v, want ErrValidation", content, err)
		}
	}
	if gate.calls != 0 {
		t.Fatalf("validation must run before the usage gate: calls=%d", gate.calls)
	}
	if _, err := svc.Send(context.Background(), "user-1", advisor.SendInput{Content: strings.Repeat("字", 500)}); err != nil {
		t.Fatalf("500 runes must pass: %v", err)
	}
}

// 顾问限额（旧线 decideAdvisor）：20 次/日 + 10 次/时，仓储自计数不扣次数。
func TestSendRateLimitedByDay(t *testing.T) {
	gate := &usageGateFake{snap: domain.BillingSnapshot{DayAdvisor: 20}}
	svc, writer := newSendService(gate, advisor.Grounding{})
	_, err := svc.Send(context.Background(), "user-1", advisor.SendInput{Content: "今天怎么穿？"})
	if !errors.Is(err, advisor.ErrRateLimited) {
		t.Fatalf("Send error = %v, want ErrRateLimited", err)
	}
	if writer.actions != nil {
		t.Fatal("limited send must not persist a message")
	}
}

func TestSendRateLimitedByHour(t *testing.T) {
	gate := &usageGateFake{snap: domain.BillingSnapshot{DayAdvisor: 5, HourAdvisor: 10}}
	svc, _ := newSendService(gate, advisor.Grounding{})
	_, err := svc.Send(context.Background(), "user-1", advisor.SendInput{Content: "今天怎么穿？"})
	if !errors.Is(err, advisor.ErrRateLimited) {
		t.Fatalf("Send error = %v, want ErrRateLimited", err)
	}
}

func TestSendCountsUsageWithoutChargingCredits(t *testing.T) {
	gate := &usageGateFake{snap: domain.BillingSnapshot{DayAdvisor: 19, HourAdvisor: 9}}
	svc, _ := newSendService(gate, advisor.Grounding{})
	if _, err := svc.Send(context.Background(), "user-1", advisor.SendInput{Content: "今天怎么穿？"}); err != nil {
		t.Fatal(err)
	}
	if gate.decision.DayAdvisorDelta != 1 || gate.decision.HourAdvisorDelta != 1 {
		t.Fatalf("usage deltas = %+v", gate.decision)
	}
	if gate.decision.CreditsDelta != 0 || len(gate.decision.Refs) != 0 {
		t.Fatalf("advisor must not charge credits: %+v", gate.decision)
	}
}
