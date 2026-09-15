package postgres

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/zhanshimian/server/internal/domain"
)

// 顾问动作卡的持久化回环：service 生成带 ID 的动作 → jsonb 落库 →
// 列表原样读回 → ApplyAction 翻转 applied（客户端动作按钮的完整链路）。
func TestAdvisorExchangePersistsActions(t *testing.T) {
	store, userID := newMediaStore(t)
	payload, _ := json.Marshal(map[string]any{"target": "today_plan", "report_id": uuid.NewString()})
	actions := []domain.AdvisorAction{{
		ID: uuid.NewString(), Kind: "adjust_plan_step", Label: "加入今日调整", Payload: payload,
	}}
	message, err := store.AppendExchange(context.Background(), userID, "明天面试怎么穿？", "可以，先保留肩线。", actions)
	if err != nil {
		t.Fatal(err)
	}
	if message.Role != "assistant" || message.ConversationID == "" {
		t.Fatalf("message = %#v", message)
	}
	if len(message.Actions) != 1 || message.Actions[0].ID != actions[0].ID || message.Actions[0].Applied {
		t.Fatalf("actions = %#v", message.Actions)
	}

	messages, err := store.ListMessages(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 {
		t.Fatalf("messages = %d, want 2 (user + assistant)", len(messages))
	}
	var assistant *domain.AdvisorMessage
	for i := range messages {
		if messages[i].Role == "assistant" {
			assistant = &messages[i]
		}
	}
	if assistant == nil || len(assistant.Actions) != 1 || assistant.Actions[0].Kind != "adjust_plan_step" {
		t.Fatalf("persisted assistant message = %#v", assistant)
	}

	applied, err := store.ApplyAction(context.Background(), userID, actions[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !applied.Applied || applied.Label != "加入今日调整" {
		t.Fatalf("applied action = %#v", applied)
	}
	// 幂等：再次应用仍返回已应用。
	again, err := store.ApplyAction(context.Background(), userID, actions[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Applied {
		t.Fatal("re-apply must stay applied")
	}
}

