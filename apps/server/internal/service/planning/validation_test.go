package planning

import (
	"testing"

	"github.com/zhanshimian/server/internal/domain"
)

func TestValidateCandidateRequiresTwoPairwiseDifferences(t *testing.T) {
	input := validValidationInput()
	input.Candidate.Variants[1].Steps = cloneSteps(input.Candidate.Variants[0].Steps)
	if !hasViolation(ValidateCandidate(input), ReasonDifferenceInsufficient) {
		t.Fatalf("violations = %#v", ValidateCandidate(input))
	}
}

func TestValidateCandidateRequiresEveryStepGrounded(t *testing.T) {
	input := validValidationInput()
	input.Candidate.Variants[0].Steps[0].Groundings = nil
	if !hasViolation(ValidateCandidate(input), ReasonStepGroundingMissing) {
		t.Fatal("missing grounding passed")
	}
}

func TestValidateCandidateCoversPriorityFindingAndEveryBriefAnswer(t *testing.T) {
	input := validValidationInput()
	removeGrounding(&input.Candidate, domain.SourceReportFinding, input.Report.PriorityFindingID)
	removeGrounding(&input.Candidate, domain.SourceSceneAnswer, "weather")
	got := ValidateCandidate(input)
	if !hasViolation(got, ReasonReportPriorityUncovered) || !hasViolation(got, ReasonSceneConstraintUncovered) {
		t.Fatalf("violations = %#v", got)
	}
}

func TestValidateCandidateRejectsWrongKeySlotAndRecommendedCounts(t *testing.T) {
	input := validValidationInput()
	input.Candidate.Variants[1].Key = domain.VariantSharp
	input.Candidate.Variants[2].Recommended = true
	got := ValidateCandidate(input)
	if !hasViolation(got, ReasonVariantKeySet) || !hasViolation(got, ReasonRecommendedCount) {
		t.Fatalf("violations = %#v", got)
	}
}

func TestValidateCandidateRejectsUnknownGroundingReferences(t *testing.T) {
	input := validValidationInput()
	input.Candidate.Variants[0].Steps[0].Groundings = []GeneratedGrounding{
		{SourceType: domain.SourceReportFinding, SourceID: "21000000-0000-0000-0000-000000000099", Reason: "不存在的 finding"},
	}
	if !hasViolation(ValidateCandidate(input), ReasonGroundingUnknownID) {
		t.Fatal("unknown finding id passed")
	}
	input.Candidate.Variants[1].Steps[2].Groundings = []GeneratedGrounding{
		{SourceType: domain.SourceFeedbackMemory, SourceID: "memory-1", Reason: "本阶段不允许"},
	}
	if !hasViolation(ValidateCandidate(input), ReasonGroundingUnknownSrc) {
		t.Fatal("feedback_memory passed this phase")
	}
	input.Candidate.Variants[2].Steps[2].Groundings = []GeneratedGrounding{
		{SourceType: domain.SourceProfilePreference, SourceID: "/nonexistent", Reason: "指针不存在"},
	}
	if !hasViolation(ValidateCandidate(input), ReasonGroundingUnknownID) {
		t.Fatal("unresolvable profile pointer passed")
	}
}

func TestValidateCandidateRejectsBannedWordingAndPriceClaims(t *testing.T) {
	input := validValidationInput()
	input.Candidate.Variants[0].Name = "颜值飞跃方案"
	if !hasViolation(ValidateCandidate(input), ReasonCopyPolicyViolation) {
		t.Fatal("banned wording passed")
	}
	input = validValidationInput()
	input.Candidate.Variants[1].Descriptor = "全套约 300 元即可完成"
	if !hasViolation(ValidateCandidate(input), ReasonCopyPolicyViolation) {
		t.Fatal("price claim passed")
	}
	input = validValidationInput()
	input.Candidate.Variants[2].Steps[2].Details.Layers = []string{"真丝围巾"}
	if !hasViolation(ValidateCandidate(input), ReasonCopyPolicyViolation) {
		t.Fatal("ungrounded material claim passed")
	}
	input = validValidationInput()
	input.Candidate.Variants[2].Steps[2].Groundings = append(input.Candidate.Variants[2].Steps[2].Groundings,
		GeneratedGrounding{SourceType: domain.SourceProfilePreference, SourceID: "/role", Reason: "资料中已有真丝单品"})
	input.Candidate.Variants[2].Steps[2].Details.Layers = []string{"真丝围巾"}
	if hasViolation(ValidateCandidate(input), ReasonCopyPolicyViolation) {
		t.Fatal("material word inside profile-grounded outfit must pass")
	}
}

func TestValidateCandidateRejectsInvalidDetailsAndCategories(t *testing.T) {
	input := validValidationInput()
	input.Candidate.Variants[0].Steps[0].Details.Intensity = "high"
	if !hasViolation(ValidateCandidate(input), ReasonStepDetailsInvalid) {
		t.Fatal("high intensity passed")
	}
	input = validValidationInput()
	input.Candidate.Variants[1].Steps[1].Category = domain.CategoryHair
	input.Candidate.Variants[1].Steps[1].Action = "tweak"
	if !hasViolation(ValidateCandidate(input), ReasonStepCategorySet) || !hasViolation(ValidateCandidate(input), ReasonStepActionInvalid) {
		t.Fatalf("violations = %#v", ValidateCandidate(input))
	}
}

func TestValidateCandidateReturnsSortedDeduplicatedCodes(t *testing.T) {
	input := validValidationInput()
	input.Candidate.Variants[0].Steps[0].Groundings = nil
	input.Candidate.Variants[1].Steps[0].Groundings = nil
	got := ValidateCandidate(input)
	for i := 1; i < len(got); i++ {
		if got[i-1].Code > got[i].Code {
			t.Fatalf("violations not sorted: %#v", got)
		}
		if got[i-1] == got[i] {
			t.Fatalf("violations not deduplicated: %#v", got)
		}
	}
}

// ---- helpers ----

func hasViolation(violations []Violation, code string) bool {
	for _, violation := range violations {
		if violation.Code == code {
			return true
		}
	}
	return false
}

func cloneSteps(steps []GeneratedPlanStep) []GeneratedPlanStep {
	out := make([]GeneratedPlanStep, len(steps))
	copy(out, steps)
	return out
}

func removeGrounding(candidate *GeneratedPlanSet, sourceType domain.GroundingSourceType, sourceID string) {
	for vi := range candidate.Variants {
		for si := range candidate.Variants[vi].Steps {
			kept := candidate.Variants[vi].Steps[si].Groundings[:0]
			for _, grounding := range candidate.Variants[vi].Steps[si].Groundings {
				if grounding.SourceType == sourceType && grounding.SourceID == sourceID {
					continue
				}
				kept = append(kept, grounding)
			}
			if len(kept) == 0 {
				// 保持最少一个合法 grounding，避免叠加无关 violation。
				kept = []GeneratedGrounding{{SourceType: domain.SourceStyleRule, SourceID: StyleRuleSceneFormality, Reason: "场景正式度兜底"}}
			}
			candidate.Variants[vi].Steps[si].Groundings = kept
		}
	}
}

// validValidationInput builds a candidate that passes every deterministic
// check; individual tests mutate one dimension at a time.
func validValidationInput() ValidationInput {
	return ValidationInput{
		Report: validReport("20000000-0000-0000-0000-000000000001"),
		Brief:  validBrief(),
		Candidate: GeneratedPlanSet{InvocationID: "inv-gate", Variants: []GeneratedPlanVariant{
			validGeneratedVariant(1, domain.VariantSharp, "轮廓利落", true,
				"颅顶蓬松", "low", "合肩直线版型", []string{"象牙白"}, "smart"),
			validGeneratedVariant(2, domain.VariantWarm, "温度感暖调", false,
				"侧线内收", "medium", "落肩宽松版型", []string{"燕麦色", "深棕"}, "casual"),
			validGeneratedVariant(3, domain.VariantNatural, "自然通勤", false,
				"保持原生线条", "low", "微廓直筒版型", []string{"灰白", "藏蓝"}, "smart_casual"),
		}},
	}
}

func validBrief() domain.SceneBrief {
	brief, err := NormalizeBrief(domain.SceneDaily, map[string]string{
		"activity": "office", "weather": "air_conditioned", "preparation": "closet", "impression": "natural",
	})
	if err != nil {
		panic(err)
	}
	return brief
}

func validGeneratedVariant(slot int, key domain.PlanVariantKey, name string, recommended bool, hairTarget, intensity, silhouette string, palette []string, formality string) GeneratedPlanVariant {
	return GeneratedPlanVariant{
		Slot: slot, Key: key, Name: name,
		Descriptor:     "落实报告优先建议并覆盖场景约束",
		Rationale:      "依据报告发型重心与场景约束给出方向",
		Recommended:    recommended,
		OutcomeTags:    []string{"易执行"},
		DifferenceTags: []string{"发型线条", "配色层次"},
		Steps: []GeneratedPlanStep{
			{
				Category: domain.CategoryHair, Action: domain.ActionAdjust,
				Title: "抬高发型重心", Summary: "保持原有长度，只整理颅顶和耳侧线条。",
				Details: domain.PlanStepDetails{Target: hairTarget, Intensity: intensity, Avoid: []string{"不改变发长"}},
				Groundings: []GeneratedGrounding{
					{SourceType: domain.SourceReportFinding, SourceID: "21000000-0000-0000-0000-000000000001", Reason: "报告观察到发型重心偏低"},
				},
			},
			{
				Category: domain.CategoryMakeup, Action: domain.ActionKeep,
				Title: "保持干净眉形", Summary: "现有眉形清晰，无需调整。",
				Details: domain.PlanStepDetails{Target: "保持眉形清洁", Intensity: "low"},
				Groundings: []GeneratedGrounding{
					{SourceType: domain.SourceStyleRule, SourceID: StyleRuleLowIntensityMakeup, Reason: "无妆容 finding，保持低强度"},
				},
			},
			{
				Category: domain.CategoryOutfit, Action: domain.ActionAdjust,
				Title: "整理上装轮廓", Summary: "以合肩版型和一层内搭应对空调环境。",
				Details: domain.PlanStepDetails{Silhouette: silhouette, Palette: palette, Layers: []string{"浅色内搭"}, Avoid: []string{"夸张图案"}, Formality: formality},
				Groundings: []GeneratedGrounding{
					{SourceType: domain.SourceReportFinding, SourceID: "21000000-0000-0000-0000-000000000002", Reason: "报告观察到肩线偏塌"},
					{SourceType: domain.SourceSceneAnswer, SourceID: "activity", Reason: "办公场景需要利落轮廓"},
					{SourceType: domain.SourceSceneAnswer, SourceID: "weather", Reason: "空调环境需要一层外搭"},
					{SourceType: domain.SourceSceneAnswer, SourceID: "preparation", Reason: "从现有衣橱出发"},
					{SourceType: domain.SourceSceneAnswer, SourceID: "impression", Reason: "以自然印象为目标"},
				},
			},
		},
	}
}
