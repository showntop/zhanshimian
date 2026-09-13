package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/zhanshimian/server/internal/service/advisor"
)

const CapabilityAdvisorChat = "advisor_chat"

// StructuredAdvisorChat 走能力路由的顾问对话；实现 advisor.Chat。
type StructuredAdvisorChat struct{ runtime StructuredRuntime }

func NewAdvisorChat(runtime StructuredRuntime) *StructuredAdvisorChat {
	return &StructuredAdvisorChat{runtime: runtime}
}

var _ advisor.Chat = (*StructuredAdvisorChat)(nil)

const advisorInstructions = "你是用户的私人形象顾问。只能使用提供的形象报告、今日方案和衣橱信息回答；如果资料不足，明确说明需要什么，不要编造用户拥有的单品。回答中文、简洁、具体、可执行，不评价颜值和身体。"

func (c *StructuredAdvisorChat) Reply(ctx context.Context, question string, grounding any) (string, error) {
	contextJSON, err := json.Marshal(grounding)
	if err != nil {
		return "", err
	}
	schema := map[string]any{"type": "object", "additionalProperties": false, "required": []string{"reply"}, "properties": map[string]any{"reply": map[string]any{"type": "string", "minLength": 1, "maxLength": 500}}}
	result, err := c.runtime.Structured(ctx, StructuredRequest{
		Capability:   CapabilityAdvisorChat,
		Instructions: advisorInstructions,
		Prompt:       "用户问题：" + question + "\n可用上下文：" + string(contextJSON),
		SchemaName:   CapabilityAdvisorChat, Schema: schema, MaxOutputTokens: 800,
		Validate: validateAdvisorReply,
	})
	if err != nil {
		return "", err
	}
	var payload struct {
		Reply string `json:"reply"`
	}
	if err := json.Unmarshal(result.JSON, &payload); err != nil {
		return "", fmt.Errorf("decode advisor reply: %w", err)
	}
	return strings.TrimSpace(payload.Reply), nil
}

func validateAdvisorReply(data []byte) error {
	var candidate struct {
		Reply string `json:"reply"`
	}
	if err := json.Unmarshal(data, &candidate); err != nil {
		return err
	}
	candidate.Reply = strings.TrimSpace(candidate.Reply)
	if candidate.Reply == "" || len([]rune(candidate.Reply)) > 500 {
		return errors.New("advisor model returned an invalid reply")
	}
	return nil
}
