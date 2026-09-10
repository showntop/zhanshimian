package provider

import (
	"context"
	"testing"

	"github.com/zhanshimian/server/internal/domain"
)

func TestDemoOutfitAdvisorReturnsRespectfulResult(t *testing.T) {
	result, err := NewDemoOutfitAdvisor().Diagnose(context.Background(), domain.DiagnosticInput{Kind: "outfit", Scene: "daily"})
	if err != nil || result.ProviderVersion != "demo-outfit-v1" || len(result.Findings) != 3 {
		t.Fatalf("unexpected demo outfit result: %#v err=%v", result, err)
	}
}

func TestOutfitPayloadRejectsUnsafeCopy(t *testing.T) {
	payload := outfitPayload{Conclusion: "不适合", PriorityTitle: "身材评分", PriorityCopy: "需要调整", Tags: []string{"一", "二", "三"}, Findings: []outfitFinding{{Label: "一", Category: "color", Tone: "improve"}, {Label: "二", Category: "fabric", Tone: "optional"}, {Label: "三", Category: "styling", Tone: "positive"}}}
	if err := validateOutfitPayload(payload); err == nil {
		t.Fatal("expected unsafe outfit copy to be rejected")
	}
}
