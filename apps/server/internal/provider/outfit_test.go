package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/jianwo/server/internal/domain"
)

type outfitAdvisorFunc func(context.Context, domain.ToolInput) (domain.ToolResult, error)

func (fn outfitAdvisorFunc) Diagnose(ctx context.Context, input domain.ToolInput) (domain.ToolResult, error) {
	return fn(ctx, input)
}

func TestOpenAIOutfitAdvisorUsesPrivateStructuredVision(t *testing.T) {
	payload := outfitPayload{
		Conclusion: "整体方向对了，先改一处", PriorityTitle: "把内搭换成更明亮的颜色",
		PriorityCopy: "保留现有外套和下装，只调整内搭就能让上半身更轻盈。", Tags: []string{"无需买新衣", "三分钟完成", "配色更清晰"},
		Findings: []outfitFinding{
			{Label: "上身配色偏沉", Category: "color", Tone: "improve", AnchorX: .35, AnchorY: .38},
			{Label: "肩线可以更清晰", Category: "silhouette", Tone: "optional", AnchorX: .65, AnchorY: .3},
			{Label: "腰线位置自然", Category: "proportion", Tone: "positive", AnchorX: .48, AnchorY: .64},
		},
	}
	structured, _ := json.Marshal(payload)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing bearer token")
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		raw, _ := json.Marshal(body)
		if body["store"] != false || strings.Count(string(raw), "data:image/") != 1 || !strings.Contains(string(raw), `"strict":true`) {
			t.Errorf("outfit request must be private, visual and schema constrained")
		}
		_ = json.NewEncoder(response).Encode(map[string]any{"status": "completed", "output": []map[string]any{{"type": "message", "content": []map[string]any{{"type": "output_text", "text": string(structured)}}}}})
	}))
	defer server.Close()
	advisor, err := NewOpenAIOutfitAdvisor(OpenAIConfig{APIKey: "test-key", BaseURL: server.URL, Model: "gpt-5-mini"}, stubMediaLoader{images: []AnalysisImage{{ID: "outfit", Kind: "outfit", MIMEType: "image/jpeg", Data: []byte("photo")}}}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	result, err := advisor.Diagnose(context.Background(), domain.ToolInput{Kind: "outfit", MediaID: "outfit", Scene: "interview"})
	if err != nil {
		t.Fatal(err)
	}
	if result.ProviderVersion != "openai-responses-outfit:gpt-5-mini" || len(result.Findings) != 3 || len(result.Tags) != 3 {
		t.Fatalf("unexpected diagnosis: %#v", result)
	}
}

func TestOutfitPayloadRejectsUnsafeCopy(t *testing.T) {
	payload := outfitPayload{Conclusion: "不适合", PriorityTitle: "身材评分", PriorityCopy: "需要调整", Tags: []string{"一", "二", "三"}, Findings: []outfitFinding{{Label: "一", Category: "color", Tone: "improve"}, {Label: "二", Category: "fabric", Tone: "optional"}, {Label: "三", Category: "styling", Tone: "positive"}}}
	if err := validateOutfitPayload(payload); err == nil {
		t.Fatal("expected unsafe outfit copy to be rejected")
	}
}

func TestFallbackOutfitAdvisorUsesDemo(t *testing.T) {
	primary := outfitAdvisorFunc(func(context.Context, domain.ToolInput) (domain.ToolResult, error) {
		return domain.ToolResult{}, errors.New("upstream unavailable")
	})
	result, err := NewFallbackOutfitAdvisor(primary, NewDemoOutfitAdvisor()).Diagnose(context.Background(), domain.ToolInput{Kind: "outfit", Scene: "daily"})
	if err != nil || result.ProviderVersion != "demo-outfit-v1" {
		t.Fatalf("fallback failed: %#v %v", result, err)
	}
}
