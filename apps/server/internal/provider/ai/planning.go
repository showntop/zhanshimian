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

const planSetInstructions = `你是严谨的中文形象方案策划。基于可信的形象报告、用户资料快照和场景答案，输出恰好三套有实质差异、可执行的方案。禁止外貌、身材、年龄、敏感属性评分；不编造衣橱单品、品牌、价格、材质或身体特征；方案文字不得与报告矛盾。

grounding 输出契约（逐字遵守，下游会逐条确定性校验）：
1. report_finding 的 source_id 必须逐字等于 report.findings 数组中某一条的 id（UUID 字符串）；不得引用 report.id，也不得使用 finding 的 label 或自由文本。
2. scene_answer 的 source_id 必须是 brief.answers 的字段名本身（例如 focus），不得写成 brief.answers.focus，也不得使用答案值。
3. brief.answers 的每个字段都必须至少出现在一条 scene_answer grounding 中。
4. style_rule 的 source_id 只能逐字来自 style_rule_ids 列表，不得发明新 ID。
5. report.priority_finding_id 指向的 finding 至少被一条 report_finding grounding 引用。
6. profile_snapshot 为空对象时不得使用 profile_preference；没有 profile_preference grounding 的 outfit 步骤，其文案不得出现材质词（真丝、桑蚕丝、羊绒、纯棉、皮革、醋酸面料）。
7. 任何用户可见文案不得出现价格（¥、元、块钱）与“颜值、身材分、缺陷严重、医学诊断、年龄判定、族裔”。`

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

// PlanSetVerifier wraps the plan_grounding_verification capability. It only
// runs after the deterministic gate is clean.
type PlanSetVerifier struct {
	runtime StructuredRuntime
}

// NewPlanSetVerifier builds the semantic text-consistency verifier.
func NewPlanSetVerifier(runtime StructuredRuntime) *PlanSetVerifier {
	return &PlanSetVerifier{runtime: runtime}
}

const planVerifierInstructions = `你是独立的文字一致性核验器。只对照报告观察/建议、场景答案、候选方案和全部 grounding 引用做判断，不接收图片。候选不得与报告矛盾，不得编造衣橱单品、品牌、价格、材质或身体特征，方案场景不得偏离。`

var planVerifierAllowedCodes = map[string]bool{
	"plan.report_contradiction":       true,
	"plan.unsupported_wardrobe_claim": true,
	"plan.unsupported_brand_claim":    true,
	"plan.unsupported_price_claim":    true,
	"plan.unsupported_material_claim": true,
	"plan.unsupported_body_claim":     true,
	"plan.scene_mismatch":             true,
}

func (v *PlanSetVerifier) Verify(ctx context.Context, input planning.VerificationInput) (planning.VerificationResult, error) {
	result, err := v.runtime.Structured(ctx, StructuredRequest{
		Capability:      CapabilityPlanGroundingVerification,
		Instructions:    planVerifierInstructions,
		Prompt:          buildPlanVerificationPrompt(input),
		SchemaName:      CapabilityPlanGroundingVerification,
		Schema:          PlanVerificationSchema(),
		MaxOutputTokens: 800,
		Validate:        validatePlanVerificationPayload,
	})
	if err != nil {
		return planning.VerificationResult{}, err
	}
	return decodePlanVerificationPayload(result.JSON, result.Meta.InvocationID)
}

func buildPlanVerificationPrompt(input planning.VerificationInput) string {
	findings := make([]map[string]string, 0, len(input.Report.Findings))
	for _, finding := range input.Report.Findings {
		findings = append(findings, map[string]string{
			"id":                  finding.ID,
			"visible_observation": finding.VisibleObservation,
			"recommendation":      finding.Recommendation,
		})
	}
	variants := make([]planVariantPayload, 0, len(input.Candidate.Variants))
	for _, variant := range input.Candidate.Variants {
		steps := make([]planStepPayload, 0, len(variant.Steps))
		for _, step := range variant.Steps {
			groundings := make([]planGroundingPayload, 0, len(step.Groundings))
			for _, grounding := range step.Groundings {
				groundings = append(groundings, planGroundingPayload{
					SourceType: string(grounding.SourceType),
					SourceID:   grounding.SourceID,
					Reason:     grounding.Reason,
				})
			}
			steps = append(steps, planStepPayload{
				Category:   string(step.Category),
				Action:     string(step.Action),
				Title:      step.Title,
				Summary:    step.Summary,
				Details:    planDetailsPayload(step.Details),
				Groundings: groundings,
			})
		}
		variants = append(variants, planVariantPayload{
			Slot: variant.Slot, Key: string(variant.Key), Name: variant.Name,
			Descriptor: variant.Descriptor, Rationale: variant.Rationale,
			Recommended: variant.Recommended, OutcomeTags: variant.OutcomeTags,
			DifferenceTags: variant.DifferenceTags, Steps: steps,
		})
	}
	encoded, err := json.Marshal(map[string]any{
		"report": map[string]any{
			"id":                  input.Report.ID,
			"priority_title":      input.Report.PriorityTitle,
			"priority_copy":       input.Report.PriorityCopy,
			"priority_finding_id": input.Report.PriorityFindingID,
			"findings":            findings,
		},
		"profile_snapshot": json.RawMessage(defaultJSON(input.Report.ProfileSnapshot)),
		"brief":            input.Brief,
		"candidate":        map[string]any{"variants": variants},
	})
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

type planVerificationPayload struct {
	Decision    string   `json:"decision"`
	ReasonCodes []string `json:"reason_codes"`
	Violations  []string `json:"violations"`
}

func validatePlanVerificationPayload(data []byte) error {
	var payload planVerificationPayload
	decode := json.NewDecoder(strings.NewReader(string(data)))
	decode.DisallowUnknownFields()
	if err := decode.Decode(&payload); err != nil {
		return fmt.Errorf("%w: %v", ErrVerifierContract, err)
	}
	if payload.Decision != "pass" && payload.Decision != "reject" {
		return fmt.Errorf("%w: decision must be pass|reject, got %q", ErrVerifierContract, payload.Decision)
	}
	if payload.Decision == "reject" && len(payload.ReasonCodes) == 0 {
		return fmt.Errorf("%w: reject requires at least one reason code", ErrVerifierContract)
	}
	if payload.Decision == "pass" && len(payload.ReasonCodes) > 0 {
		return fmt.Errorf("%w: pass must not carry reason codes", ErrVerifierContract)
	}
	for _, code := range payload.ReasonCodes {
		if !planVerifierAllowedCodes[code] {
			return fmt.Errorf("%w: unknown reason code %q", ErrVerifierContract, code)
		}
	}
	if len(payload.Violations) > 16 {
		return fmt.Errorf("%w: too many violations", ErrVerifierContract)
	}
	return nil
}

func decodePlanVerificationPayload(data []byte, invocationID string) (planning.VerificationResult, error) {
	if err := validatePlanVerificationPayload(data); err != nil {
		return planning.VerificationResult{}, err
	}
	var payload planVerificationPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return planning.VerificationResult{}, fmt.Errorf("%w: %v", ErrVerifierContract, err)
	}
	return planning.VerificationResult{
		InvocationID: invocationID,
		Decision:     payload.Decision,
		ReasonCodes:  payload.ReasonCodes,
		Violations:   payload.Violations,
	}, nil
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
			"id":                  input.Report.ID,
			"photo_set_id":        input.Report.PhotoSetID,
			"impression_tags":     input.Report.ImpressionTags,
			"priority_title":      input.Report.PriorityTitle,
			"priority_copy":       input.Report.PriorityCopy,
			"priority_finding_id": priorityFindingID,
			"findings":            findings,
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
		"grounding_example": []map[string]string{
			{
				"source_type": "report_finding",
				"source_id":   "逐字引用 report.findings[].id 的 UUID，例如 " + exampleFindingID(input.Report),
				"reason":      "一句话说明这一步为什么落实该依据",
			},
			{
				"source_type": "scene_answer",
				"source_id":   "brief.answers 的字段名本身，例如 " + exampleBriefField(input.Brief),
				"reason":      "一句话说明这一步如何回应场景答案",
			},
		},
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

// exampleFindingID 给 grounding 示例一个真实可引用的 finding UUID，
// 避免模型把 report.id 或 label 当成合法 source_id。
func exampleFindingID(report planning.ReportSnapshot) string {
	if len(report.Findings) > 0 {
		return report.Findings[0].ID
	}
	return "<findings[].id>"
}

// exampleBriefField 给 scene_answer 示例一个真实字段名，
// 避免模型写出 brief.answers.focus 之类的路径形式。
func exampleBriefField(brief domain.SceneBrief) string {
	for name := range brief.Answers {
		return name
	}
	return "<answers 字段名>"
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
