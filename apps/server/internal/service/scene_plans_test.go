package service

import (
	"strings"
	"testing"

	"github.com/example/jianwo/server/internal/domain"
)

func TestBuildScenePlansCreatesThreeTailoredPlans(t *testing.T) {
	input := domain.ScenePlanInput{
		Scene: "interview",
		Answers: map[string]string{
			"when": "three-days", "format": "final", "preparation": "key-piece", "impression": "reliable",
		},
	}
	plans := buildScenePlans(input)
	if len(plans) != 3 {
		t.Fatalf("got %d plans, want 3", len(plans))
	}
	if plans[0].Slug != "sharp" || !plans[0].Recommended {
		t.Fatalf("first plan should be the recommended sharp direction: %#v", plans[0])
	}
	for _, plan := range plans {
		if plan.Scene != "interview" || !strings.Contains(plan.Name, "面试") {
			t.Fatalf("plan is not tied to the occasion: %#v", plan)
		}
		if len(plan.Steps) != 3 {
			t.Fatalf("plan %q has %d steps, want hair/makeup/outfit", plan.Slug, len(plan.Steps))
		}
		for index, category := range []string{"hair", "makeup", "outfit"} {
			if plan.Steps[index].Category != category || len(plan.Steps[index].Details) == 0 {
				t.Fatalf("plan %q step %d incomplete: %#v", plan.Slug, index, plan.Steps[index])
			}
		}
	}
}

func TestBuildScenePlansChangesPrimaryDirectionForGoal(t *testing.T) {
	plans := buildScenePlans(domain.ScenePlanInput{Scene: "date", Answers: map[string]string{
		"activity": "dinner", "timing": "evening", "preparation": "closet", "impression": "memorable",
	}})
	for _, plan := range plans {
		if plan.Slug == "warm" && !plan.Recommended {
			t.Fatal("memorable goal should recommend the warm direction")
		}
		if plan.Slug != "warm" && plan.Recommended {
			t.Fatalf("only warm should be recommended, got %#v", plans)
		}
	}
}

func TestValidateScenePlanInput(t *testing.T) {
	valid := domain.ScenePlanInput{Scene: "daily", Answers: map[string]string{
		"activity": "commute", "weather": "office", "preparation": "key-piece", "impression": "natural",
	}}
	if err := validateScenePlanInput(valid); err != nil {
		t.Fatalf("valid brief rejected: %v", err)
	}
	valid.Answers["weather"] = "unknown"
	if err := validateScenePlanInput(valid); err == nil {
		t.Fatal("invalid scene answer should be rejected")
	}
}

func TestLegacyScenePlanBriefIsStillAccepted(t *testing.T) {
	legacy := domain.ScenePlanInput{Scene: "interview", Time: "week", Budget: "mid", Formality: "proper", Impression: "reliable"}
	answers, err := normalizedSceneAnswers(legacy)
	if err != nil {
		t.Fatalf("legacy brief rejected: %v", err)
	}
	if answers["format"] != "onsite" || answers["preparation"] != "key-piece" {
		t.Fatalf("legacy fields were not normalized: %#v", answers)
	}
}
