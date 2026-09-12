package ai

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/service/planning"
)

const (
	CapabilityPlanSetGeneration         = "plan_set_generation"
	CapabilityPlanGroundingVerification = "plan_grounding_verification"
)

// ErrGeneratorContract marks structurally invalid generator output: the
// candidate violates the frozen plan_set.v1 contract and must never retry the
// same prompt blindly.
var ErrGeneratorContract = errors.New("plan set generator contract violation")

// ErrVerifierContract marks structurally invalid verifier output.
var ErrVerifierContract = errors.New("plan verifier contract violation")

//go:embed schemas/plan_set.v1.json
var planSetSchemaJSON []byte

//go:embed schemas/plan_verification.v1.json
var planVerificationSchemaJSON []byte

// PlanSetSchema returns the decoded plan_set.v1 output schema.
func PlanSetSchema() map[string]any {
	return decodeSchemaJSON("plan_set.v1", planSetSchemaJSON)
}

// PlanVerificationSchema returns the decoded plan_verification.v1 schema.
func PlanVerificationSchema() map[string]any {
	return decodeSchemaJSON("plan_verification.v1", planVerificationSchemaJSON)
}

// StructuredRuntime is the narrow runtime surface planning adapters need.
type StructuredRuntime = Runtime

// PlanSetGenerator wraps the plan_set_generation capability.
type PlanSetGenerator struct {
	runtime StructuredRuntime
}

// NewPlanSetGenerator builds the generator adapter. The runtime stays
// capability-routed; no vendor or model identity appears here.
func NewPlanSetGenerator(runtime StructuredRuntime) *PlanSetGenerator {
	return &PlanSetGenerator{runtime: runtime}
}

const planSetInstructions = `你是严谨的中文形象方案策划。基于可信的形象报告、用户资料快照和场景答案，输出恰好三套有实质差异、可执行的方案。禁止外貌、身材、年龄、敏感属性评分；不编造衣橱单品、品牌、价格、材质或身体特征；方案文字不得与报告矛盾。每一步必须引用稳定 source_id 说明依据，不得使用无法核验的自由文本 ID。`

func (g *PlanSetGenerator) Generate(ctx context.Context, input planning.GenerationInput) (planning.GeneratedPlanSet, error) {
	result, err := g.runtime.Structured(ctx, StructuredRequest{
		Capability:      CapabilityPlanSetGeneration,
		Instructions:    planSetInstructions,
		Prompt:          buildPlanSetPrompt(input),
		SchemaName:      CapabilityPlanSetGeneration,
		Schema:          PlanSetSchema(),
		MaxOutputTokens: 4000,
		Validate:        validatePlanSetPayload,
	})
	if err != nil {
		return planning.GeneratedPlanSet{}, err
	}
	// Re-validate at the adapter boundary: contract enforcement must not
	// depend on the runtime honoring the Validate hook.
	if err := validatePlanSetPayload(result.JSON); err != nil {
		return planning.GeneratedPlanSet{}, err
	}
	candidate, err := decodePlanSetPayload(result.JSON)
	if err != nil {
		return planning.GeneratedPlanSet{}, err
	}
	return planning.GeneratedPlanSet{InvocationID: result.Meta.InvocationID, Variants: candidate}, nil
}

// buildPlanSetPrompt renders the full report, profile snapshot, normalized
// brief, style rule IDs and retry context. The grounding contract is spelled
// out with stable IDs so the model can only cite what exists.
func buildPlanSetPrompt(input planning.GenerationInput) string {
	findings := make([]map[string]any, 0, len(input.Report.Findings))
	for _, finding := range input.Report.Findings {
		findings = append(findings, map[string]any{
			"id":                  finding.ID,
			"category":            finding.Category,
			"priority":            finding.Priority,
			"label":               finding.Label,
			"visible_observation": finding.VisibleObservation,
			"recommendation":      finding.Recommendation,
		})
	}
	priorityFindingID := input.Report.PriorityFindingID
	sceneAnswers := map[string]string{}
	for name, value := range input.Brief.Answers {
		sceneAnswers[name] = value
	}
	briefRepr, _ := json.Marshal(map[string]any{
		"scene":          input.Brief.Scene,
		"schema_version": input.Brief.SchemaVersion,
		"answers":        sceneAnswers,
	})
	payload := map[string]any{
		"report": map[string]any{
			"id":                 input.Report.ID,
			"photo_set_id":       input.Report.PhotoSetID,
			"impression_tags":    input.Report.ImpressionTags,
			"priority_title":     input.Report.PriorityTitle,
			"priority_copy":      input.Report.PriorityCopy,
			"priority_finding_id": priorityFindingID,
			"findings":           findings,
		},
		"profile_snapshot": json.RawMessage(defaultJSON(input.Report.ProfileSnapshot)),
		"brief":            json.RawMessage(briefRepr),
		"style_rule_ids":   planning.StyleRuleIDs,
		"grounding_source_types": []string{
			string(domain.SourceReportFinding),
			string(domain.SourceSceneAnswer),
			string(domain.SourceProfilePreference),
			string(domain.SourceStyleRule),
		},
		"grounding_example": []map[string]string{{
			"source_type": "report_finding",
			"source_id":   "引用上面 findings 中的 id",
			"reason":      "一句话说明这一步为什么落实该依据",
		}},
		"content_attempt":    input.ContentAttempt,
		"prior_reason_codes": orEmpty(input.PriorReasonCodes),
	}
	if input.ContentAttempt > 1 {
		payload["retry_contract"] = "这是唯一一次内容补生成：只针对 prior_reason_codes 列出的被拒原因修正，不要改动其余通过的内容结构。"
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, encoded, "", " "); err != nil {
		return string(encoded)
	}
	return pretty.String()
}

// ---- payload structs, schema-level validation and decoding ----

type planSetPayload struct {
	Variants []planVariantPayload `json:"variants"`
}

type planVariantPayload struct {
	Slot           int               `json:"slot"`
	Key            string            `json:"key"`
	Name           string            `json:"name"`
	Descriptor     string            `json:"descriptor"`
	Rationale      string            `json:"rationale"`
	Recommended    bool              `json:"recommended"`
	OutcomeTags    []string          `json:"outcome_tags"`
	DifferenceTags []string          `json:"difference_tags"`
	Steps          []planStepPayload `json:"steps"`
}

type planStepPayload struct {
	Category   string                 `json:"category"`
	Action     string                 `json:"action"`
	Title      string                 `json:"title"`
	Summary    string                 `json:"summary"`
	Details    planDetailsPayload     `json:"details"`
	Groundings []planGroundingPayload `json:"groundings"`
}

type planDetailsPayload struct {
	Target     string   `json:"target"`
	Intensity  string   `json:"intensity"`
	Silhouette string   `json:"silhouette"`
	Palette    []string `json:"palette"`
	Layers     []string `json:"layers"`
	Avoid      []string `json:"avoid"`
	Formality  string   `json:"formality"`
}

type planGroundingPayload struct {
	SourceType string `json:"source_type"`
	SourceID   string `json:"source_id"`
	Reason     string `json:"reason"`
}

// validatePlanSetPayload enforces the plan_set.v1 schema shape before the
// candidate leaves the runtime boundary. Set-completeness (which keys, which
// slots, recommended count) and grounding reference existence stay in the
// deterministic planning gate.
func validatePlanSetPayload(data []byte) error {
	var payload planSetPayload
	decode := json.NewDecoder(strings.NewReader(string(data)))
	decode.DisallowUnknownFields()
	if err := decode.Decode(&payload); err != nil {
		return fmt.Errorf("%w: %v", ErrGeneratorContract, err)
	}
	if len(payload.Variants) != 3 {
		return fmt.Errorf("%w: want exactly 3 variants, got %d", ErrGeneratorContract, len(payload.Variants))
	}
	seenSlot := map[int]bool{}
	seenKey := map[string]bool{}
	for _, variant := range payload.Variants {
		if variant.Slot < 1 || variant.Slot > 3 || seenSlot[variant.Slot] {
			return fmt.Errorf("%w: bad slot %d", ErrGeneratorContract, variant.Slot)
		}
		seenSlot[variant.Slot] = true
		if !planVariantKeys[variant.Key] || seenKey[variant.Key] {
			return fmt.Errorf("%w: bad variant key %q", ErrGeneratorContract, variant.Key)
		}
		seenKey[variant.Key] = true
		if err := requirePlanText("name", variant.Name, 80); err != nil {
			return err
		}
		if err := requirePlanText("descriptor", variant.Descriptor, 160); err != nil {
			return err
		}
		if err := requirePlanText("rationale", variant.Rationale, 240); err != nil {
			return err
		}
		if err := validateStringArray("outcome_tags", variant.OutcomeTags, 8, 40); err != nil {
			return err
		}
		if err := validateStringArray("difference_tags", variant.DifferenceTags, 8, 40); err != nil {
			return err
		}
		if len(variant.Steps) != 3 {
			return fmt.Errorf("%w: want exactly 3 steps, got %d", ErrGeneratorContract, len(variant.Steps))
		}
		if err := validatePlanSteps(variant.Steps); err != nil {
			return err
		}
	}
	return nil
}

var planVariantKeys = map[string]bool{"sharp": true, "warm": true, "natural": true}
var planStepCategories = map[string]bool{"hair": true, "makeup": true, "outfit": true}
var planStepActions = map[string]bool{"keep": true, "adjust": true}
var planGroundingSourceTypes = map[string]bool{
	"report_finding": true, "scene_answer": true, "profile_preference": true, "style_rule": true,
}

func validatePlanSteps(steps []planStepPayload) error {
	seenCategory := map[string]bool{}
	for _, step := range steps {
		if !planStepCategories[step.Category] || seenCategory[step.Category] {
			return fmt.Errorf("%w: bad step category %q", ErrGeneratorContract, step.Category)
		}
		seenCategory[step.Category] = true
		if !planStepActions[step.Action] {
			return fmt.Errorf("%w: bad step action %q", ErrGeneratorContract, step.Action)
		}
		if err := requirePlanText("title", step.Title, 120); err != nil {
			return err
		}
		if err := requirePlanText("summary", step.Summary, 240); err != nil {
			return err
		}
		if err := validatePlanDetails(step.Category, step.Details); err != nil {
			return err
		}
		if len(step.Groundings) < 1 || len(step.Groundings) > 8 {
			return fmt.Errorf("%w: step %s wants 1-8 groundings, got %d", ErrGeneratorContract, step.Category, len(step.Groundings))
		}
		for _, grounding := range step.Groundings {
			if !planGroundingSourceTypes[grounding.SourceType] {
				return fmt.Errorf("%w: unknown grounding source_type %q", ErrGeneratorContract, grounding.SourceType)
			}
			if err := requirePlanText("source_id", grounding.SourceID, 200); err != nil {
				return err
			}
			if err := requirePlanText("reason", grounding.Reason, 240); err != nil {
				return err
			}
		}
	}
	return nil
}

func validatePlanDetails(category string, details planDetailsPayload) error {
	switch category {
	case "hair", "makeup":
		if details.Silhouette != "" || details.Formality != "" ||
			len(details.Palette) != 0 || len(details.Layers) != 0 {
			return fmt.Errorf("%w: %s details carry outfit-only fields", ErrGeneratorContract, category)
		}
		if details.Intensity != "low" && details.Intensity != "medium" {
			return fmt.Errorf("%w: %s intensity must be low|medium, got %q", ErrGeneratorContract, category, details.Intensity)
		}
		return requirePlanText("target", details.Target, 160)
	case "outfit":
		if details.Target != "" || details.Intensity != "" {
			return fmt.Errorf("%w: outfit details carry hair/makeup-only fields", ErrGeneratorContract)
		}
		if len(details.Palette) < 1 || len(details.Palette) > 8 {
			return fmt.Errorf("%w: outfit palette wants 1-8 colors, got %d", ErrGeneratorContract, len(details.Palette))
		}
		if len(details.Layers) > 8 || len(details.Avoid) > 8 {
			return fmt.Errorf("%w: outfit layers/avoid exceed 8 items", ErrGeneratorContract)
		}
		if len(details.Formality) > 40 {
			return fmt.Errorf("%w: outfit formality exceeds 40 chars", ErrGeneratorContract)
		}
		return requirePlanText("silhouette", details.Silhouette, 160)
	}
	return fmt.Errorf("%w: unknown category %q", ErrGeneratorContract, category)
}

func decodePlanSetPayload(data []byte) ([]planning.GeneratedPlanVariant, error) {
	var payload planSetPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrGeneratorContract, err)
	}
	variants := make([]planning.GeneratedPlanVariant, 0, len(payload.Variants))
	for _, variant := range payload.Variants {
		steps := make([]planning.GeneratedPlanStep, 0, len(variant.Steps))
		for _, step := range variant.Steps {
			groundings := make([]planning.GeneratedGrounding, 0, len(step.Groundings))
			for _, grounding := range step.Groundings {
				if !planGroundingSourceTypes[grounding.SourceType] {
					return nil, fmt.Errorf("%w: unknown grounding source_type %q", ErrGeneratorContract, grounding.SourceType)
				}
				groundings = append(groundings, planning.GeneratedGrounding{
					SourceType: domain.GroundingSourceType(grounding.SourceType),
					SourceID:   grounding.SourceID,
					Reason:     grounding.Reason,
				})
			}
			steps = append(steps, planning.GeneratedPlanStep{
				Category:   domain.StepCategory(step.Category),
				Action:     domain.StepAction(step.Action),
				Title:      step.Title,
				Summary:    step.Summary,
				Details:    domain.PlanStepDetails(step.Details),
				Groundings: groundings,
			})
		}
		variants = append(variants, planning.GeneratedPlanVariant{
			Slot:           variant.Slot,
			Key:            domain.PlanVariantKey(variant.Key),
			Name:           variant.Name,
			Descriptor:     variant.Descriptor,
			Rationale:      variant.Rationale,
			Recommended:    variant.Recommended,
			OutcomeTags:    variant.OutcomeTags,
			DifferenceTags: variant.DifferenceTags,
			Steps:          steps,
		})
	}
	return variants, nil
}

func requirePlanText(field, value string, maxLength int) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("%w: %s must not be empty", ErrGeneratorContract, field)
	}
	if len([]rune(trimmed)) > maxLength {
		return fmt.Errorf("%w: %s exceeds %d chars", ErrGeneratorContract, field, maxLength)
	}
	return nil
}

func validateStringArray(field string, values []string, maxItems, maxLen int) error {
	if len(values) > maxItems {
		return fmt.Errorf("%w: %s exceeds %d items", ErrGeneratorContract, field, maxItems)
	}
	seen := map[string]bool{}
	for _, value := range values {
		if err := requirePlanText(field, value, maxLen); err != nil {
			return err
		}
		if seen[value] {
			return fmt.Errorf("%w: %s has duplicate %q", ErrGeneratorContract, field, value)
		}
		seen[value] = true
	}
	return nil
}

func defaultJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	return string(raw)
}

func orEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func decodeSchemaJSON(name string, data []byte) map[string]any {
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		panic("embedded schema " + name + " is invalid: " + err.Error())
	}
	return schema
}
