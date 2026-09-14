package planning

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/zhanshimian/server/internal/domain"
)

// Violation is one deterministic gate rejection with a stable reason code.
type Violation struct {
	Code   string
	Detail string
}

// ValidationInput is everything the deterministic gate may look at: no
// images, no model output beyond the candidate.
type ValidationInput struct {
	Report    ReportSnapshot
	Brief     domain.SceneBrief
	Candidate GeneratedPlanSet
}

// Stable gate reason codes. Codes must stay sorted and deduplicated in the
// returned slice.
const (
	ReasonVariantCount             = "plan.variant_count"
	ReasonVariantKeySet            = "plan.variant_key_set"
	ReasonVariantSlotSet           = "plan.variant_slot_set"
	ReasonRecommendedCount         = "plan.recommended_count"
	ReasonStepCategorySet          = "plan.step_category_set"
	ReasonStepActionInvalid        = "plan.step_action_invalid"
	ReasonStepDetailsInvalid       = "plan.step_details_invalid"
	ReasonStepGroundingMissing     = "plan.step_grounding_missing"
	ReasonGroundingUnknownSrc      = "plan.grounding_unknown_source"
	ReasonGroundingUnknownID       = "plan.grounding_unknown_id"
	ReasonReportPriorityUncovered  = "plan.report_priority_uncovered"
	ReasonSceneConstraintUncovered = "plan.scene_constraint_uncovered"
	ReasonDifferenceInsufficient   = "plan.difference_insufficient"
	ReasonCopyPolicyViolation      = "plan.copy_policy_violation"
	// ReasonGeneratorContract 不是确定性门禁的输出:它记录生成器输出违约
	// (ErrGeneratorContract) 消耗掉的内容重试,供下一次采样在 prompt 里看到。
	ReasonGeneratorContract = "plan.generator_contract_violation"
)

var copyPolicyBannedWords = []string{"颜值", "身材分", "缺陷严重", "医学诊断", "年龄判定", "族裔"}

// Price markers can never carry grounding in this phase: pricing claims are
// fabrication by definition.
var priceMarkers = []string{"¥", "￥", "元/件", "元起", " 元", "块钱"}

// Material claims are only acceptable when grounded in the profile snapshot
// (e.g. the user said they own one).
var materialWords = []string{"真丝", "桑蚕丝", "羊绒", "纯棉", "皮革", "醋酸面料"}

// DifferenceSignature compares two variants on the five dimensions the
// product treats as substantive. At least two must differ per pair.
type DifferenceSignature struct {
	Hair      string
	Makeup    string
	Outfit    string
	Palette   string
	Formality string
}

// ValidateCandidate runs every deterministic check. It never calls AI; the
// semantic verifier is only invoked when this returns no violations.
func ValidateCandidate(input ValidationInput) []Violation {
	var violations []Violation
	add := func(code, detail string) { violations = append(violations, Violation{Code: code, Detail: detail}) }

	candidate := input.Candidate
	if len(candidate.Variants) != 3 {
		add(ReasonVariantCount, "want exactly 3 variants")
		return sortViolations(violations)
	}

	keySeen := map[domain.PlanVariantKey]bool{}
	slotSeen := map[int]bool{}
	recommended := 0
	for _, variant := range candidate.Variants {
		keySeen[variant.Key] = true
		slotSeen[variant.Slot] = true
		if variant.Recommended {
			recommended++
		}
	}
	if len(keySeen) != 3 || !keySeen[domain.VariantSharp] || !keySeen[domain.VariantWarm] || !keySeen[domain.VariantNatural] {
		add(ReasonVariantKeySet, "variants must use exactly sharp/warm/natural")
	}
	if len(slotSeen) != 3 || !slotSeen[1] || !slotSeen[2] || !slotSeen[3] {
		add(ReasonVariantSlotSet, "variants must use slots 1/2/3")
	}
	if recommended != 1 {
		add(ReasonRecommendedCount, "want exactly one recommended variant")
	}

	signatures := make([]DifferenceSignature, 0, len(candidate.Variants))
	for _, variant := range candidate.Variants {
		stepByCategory := map[domain.StepCategory]GeneratedPlanStep{}
		for _, step := range variant.Steps {
			stepByCategory[step.Category] = step
		}
		for _, category := range []domain.StepCategory{domain.StepCategoryHair, domain.StepCategoryMakeup, domain.StepCategoryOutfit} {
			step, ok := stepByCategory[category]
			if !ok {
				add(ReasonStepCategorySet, string(category)+" step missing in "+string(variant.Key))
				continue
			}
			if step.Action != domain.ActionKeep && step.Action != domain.ActionAdjust {
				add(ReasonStepActionInvalid, string(variant.Key)+"/"+string(category))
			}
			if err := validateStepDetails(category, step.Details); err != nil {
				add(ReasonStepDetailsInvalid, string(variant.Key)+"/"+string(category)+": "+err.Error())
			}
			if len(step.Groundings) == 0 {
				add(ReasonStepGroundingMissing, string(variant.Key)+"/"+string(category))
			}
			for _, grounding := range step.Groundings {
				if detail, ok := groundingReferenceResolves(input, grounding); !ok {
					if groundingUnknownSource(grounding.SourceType) {
						add(ReasonGroundingUnknownSrc, string(grounding.SourceType))
					} else {
						add(ReasonGroundingUnknownID, string(grounding.SourceType)+":"+grounding.SourceID+" "+detail)
					}
				}
			}
			if violation, bad := stepCopyPolicy(category, step); bad {
				add(ReasonCopyPolicyViolation, string(variant.Key)+"/"+string(category)+": "+violation)
			}
		}
		if violation, bad := variantCopyPolicy(variant); bad {
			add(ReasonCopyPolicyViolation, string(variant.Key)+": "+violation)
		}
		signatures = append(signatures, differenceSignatureOf(stepByCategory))
	}

	if !coversFinding(candidate, input.Report.PriorityFindingID) {
		add(ReasonReportPriorityUncovered, input.Report.PriorityFindingID)
	}
	for name := range input.Brief.Answers {
		if !coversSceneAnswer(candidate, name) {
			add(ReasonSceneConstraintUncovered, name)
		}
	}

	for i := 0; i < len(signatures); i++ {
		for j := i + 1; j < len(signatures); j++ {
			if differencesBetween(signatures[i], signatures[j]) < 2 {
				add(ReasonDifferenceInsufficient,
					string(candidate.Variants[i].Key)+" vs "+string(candidate.Variants[j].Key))
			}
		}
	}

	return sortViolations(violations)
}

func sortViolations(violations []Violation) []Violation {
	if len(violations) == 0 {
		return nil
	}
	sort.Slice(violations, func(i, j int) bool {
		if violations[i].Code != violations[j].Code {
			return violations[i].Code < violations[j].Code
		}
		return violations[i].Detail < violations[j].Detail
	})
	deduped := violations[:1]
	for _, violation := range violations[1:] {
		if violation != deduped[len(deduped)-1] {
			deduped = append(deduped, violation)
		}
	}
	return deduped
}

// groundingReferenceResolves checks the source against the frozen ID rules:
// findings must exist, scene answers must be brief fields, profile pointers
// must resolve (RFC 6901), style rules must come from the frozen catalog.
func groundingReferenceResolves(input ValidationInput, grounding GeneratedGrounding) (string, bool) {
	switch grounding.SourceType {
	case domain.SourceReportFinding:
		for _, finding := range input.Report.Findings {
			if finding.ID == grounding.SourceID {
				return "", true
			}
		}
		return "unknown finding id", false
	case domain.SourceSceneAnswer:
		if _, ok := input.Brief.Answers[grounding.SourceID]; ok {
			return "", true
		}
		return "unknown brief field", false
	case domain.SourceProfilePreference:
		if jsonPointerResolves(input.Report.ProfileSnapshot, grounding.SourceID) {
			return "", true
		}
		return "unresolvable profile pointer", false
	case domain.SourceStyleRule:
		if IsStyleRuleID(grounding.SourceID) {
			return "", true
		}
		return "unknown style rule", false
	case domain.SourceFeedbackMemory:
		if feedbackMemoryIDResolves(input.Report.ProfileSnapshot, grounding.SourceID) {
			return "", true
		}
		return "unknown feedback memory id", false
	default:
		return "unknown source type", false
	}
}

func groundingUnknownSource(sourceType domain.GroundingSourceType) bool {
	switch sourceType {
	case domain.SourceReportFinding, domain.SourceSceneAnswer, domain.SourceProfilePreference,
		domain.SourceStyleRule, domain.SourceFeedbackMemory:
		return false
	default:
		return true
	}
}

// feedbackMemoryIDResolves checks whether sourceID names an item in the
// deterministic feedback_memory snapshot embedded in the profile snapshot.
func feedbackMemoryIDResolves(profileSnapshot json.RawMessage, sourceID string) bool {
	if len(profileSnapshot) == 0 {
		return false
	}
	root := map[string]json.RawMessage{}
	if err := json.Unmarshal(profileSnapshot, &root); err != nil {
		return false
	}
	encoded, ok := root["feedback_memory"]
	if !ok {
		return false
	}
	var snapshot domain.FeedbackMemorySnapshot
	if err := json.Unmarshal(encoded, &snapshot); err != nil {
		return false
	}
	for _, item := range snapshot.Items {
		if item.ID == sourceID {
			return true
		}
	}
	return false
}

// jsonPointerResolves walks an RFC 6901 pointer through the profile JSON.
func jsonPointerResolves(raw []byte, pointer string) bool {
	if !strings.HasPrefix(pointer, "/") {
		return false
	}
	var document any
	if err := jsonUnmarshal(raw, &document); err != nil {
		return false
	}
	current := document
	for _, token := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
		token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
		switch typed := current.(type) {
		case map[string]any:
			next, ok := typed[token]
			if !ok {
				return false
			}
			current = next
		case []any:
			index := 0
			if token == "" || len(token) > 1 || token < "0" || token > "9" {
				return false
			}
			index = int(token[0] - '0')
			if index >= len(typed) {
				return false
			}
			current = typed[index]
		default:
			return false
		}
	}
	return true
}

func validateStepDetails(category domain.StepCategory, details domain.PlanStepDetails) error {
	switch category {
	case domain.StepCategoryHair, domain.StepCategoryMakeup:
		if strings.TrimSpace(details.Target) == "" {
			return errStepDetails("target required")
		}
		if details.Intensity != "low" && details.Intensity != "medium" {
			return errStepDetails("intensity must be low|medium")
		}
		if len(details.Palette) != 0 || len(details.Layers) != 0 || details.Silhouette != "" || details.Formality != "" {
			return errStepDetails("outfit-only fields present")
		}
	case domain.StepCategoryOutfit:
		if details.Target != "" || details.Intensity != "" {
			return errStepDetails("hair/makeup-only fields present")
		}
		if strings.TrimSpace(details.Silhouette) == "" {
			return errStepDetails("silhouette required")
		}
		if len(details.Palette) < 1 {
			return errStepDetails("palette required")
		}
	}
	return nil
}

type stepDetailsError struct{ message string }

func (e *stepDetailsError) Error() string { return e.message }

func errStepDetails(message string) error { return &stepDetailsError{message: message} }

func coversFinding(candidate GeneratedPlanSet, findingID string) bool {
	if findingID == "" {
		return false
	}
	for _, variant := range candidate.Variants {
		for _, step := range variant.Steps {
			for _, grounding := range step.Groundings {
				if grounding.SourceType == domain.SourceReportFinding && grounding.SourceID == findingID {
					return true
				}
			}
		}
	}
	return false
}

func coversSceneAnswer(candidate GeneratedPlanSet, field string) bool {
	for _, variant := range candidate.Variants {
		for _, step := range variant.Steps {
			for _, grounding := range step.Groundings {
				if grounding.SourceType == domain.SourceSceneAnswer && grounding.SourceID == field {
					return true
				}
			}
		}
	}
	return false
}

func differenceSignatureOf(steps map[domain.StepCategory]GeneratedPlanStep) DifferenceSignature {
	hair := steps[domain.StepCategoryHair]
	makeup := steps[domain.StepCategoryMakeup]
	outfit := steps[domain.StepCategoryOutfit]
	layers := append([]string(nil), outfit.Details.Layers...)
	sort.Strings(layers)
	palette := append([]string(nil), outfit.Details.Palette...)
	sort.Strings(palette)
	return DifferenceSignature{
		Hair:      string(hair.Action) + "|" + normalizeText(hair.Details.Target) + "|" + hair.Details.Intensity,
		Makeup:    string(makeup.Action) + "|" + normalizeText(makeup.Details.Target) + "|" + makeup.Details.Intensity,
		Outfit:    string(outfit.Action) + "|" + normalizeText(outfit.Details.Silhouette) + "|" + strings.Join(layers, ","),
		Palette:   strings.Join(palette, ","),
		Formality: normalizeText(outfit.Details.Formality),
	}
}

func differencesBetween(left, right DifferenceSignature) int {
	differences := 0
	if left.Hair != right.Hair {
		differences++
	}
	if left.Makeup != right.Makeup {
		differences++
	}
	if left.Outfit != right.Outfit {
		differences++
	}
	if left.Palette != right.Palette {
		differences++
	}
	if left.Formality != right.Formality {
		differences++
	}
	return differences
}

func normalizeText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func jsonUnmarshal(data []byte, target any) error {
	return json.Unmarshal(data, target)
}

// stepCopyPolicy enforces the wording red lines on step-visible text.
func stepCopyPolicy(category domain.StepCategory, step GeneratedPlanStep) (string, bool) {
	texts := []string{step.Title, step.Summary, step.Details.Target, step.Details.Silhouette, step.Details.Formality}
	texts = append(texts, step.Details.Palette...)
	texts = append(texts, step.Details.Layers...)
	texts = append(texts, step.Details.Avoid...)
	joined := strings.Join(texts, "\n")
	for _, word := range copyPolicyBannedWords {
		if strings.Contains(joined, word) {
			return "banned wording " + word, true
		}
	}
	for _, marker := range priceMarkers {
		if strings.Contains(joined, marker) {
			return "price claim is never allowed", true
		}
	}
	if category == domain.StepCategoryOutfit && !hasProfileGrounding(step) {
		for _, word := range materialWords {
			if strings.Contains(joined, word) {
				return "material claim without profile grounding: " + word, true
			}
		}
	}
	return "", false
}

func variantCopyPolicy(variant GeneratedPlanVariant) (string, bool) {
	texts := append([]string{variant.Name, variant.Descriptor, variant.Rationale}, variant.OutcomeTags...)
	texts = append(texts, variant.DifferenceTags...)
	joined := strings.Join(texts, "\n")
	for _, word := range copyPolicyBannedWords {
		if strings.Contains(joined, word) {
			return "banned wording " + word, true
		}
	}
	for _, marker := range priceMarkers {
		if strings.Contains(joined, marker) {
			return "price claim is never allowed", true
		}
	}
	return "", false
}

func hasProfileGrounding(step GeneratedPlanStep) bool {
	for _, grounding := range step.Groundings {
		if grounding.SourceType == domain.SourceProfilePreference {
			return true
		}
	}
	return false
}
