package ai

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/service/planning"
)

type fakeStructuredRuntime struct {
	request StructuredRequest
	result  []byte
	err     error
}

func (f *fakeStructuredRuntime) Structured(_ context.Context, request StructuredRequest) (StructuredResult, error) {
	f.request = request
	if f.err != nil {
		return StructuredResult{}, f.err
	}
	return StructuredResult{JSON: f.result, Meta: InvocationMeta{InvocationID: "inv-plan-1"}}, nil
}

func TestPlanSetGeneratorSendsReportBriefAndGroundingIDs(t *testing.T) {
	runtime := &fakeStructuredRuntime{result: validGeneratedPlanSetJSON()}
	generator := NewPlanSetGenerator(runtime)
	got, err := generator.Generate(context.Background(), planning.GenerationInput{
		Report:         validPlanningReport(),
		Brief:          validDailyBrief(),
		ContentAttempt: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.request.Capability != CapabilityPlanSetGeneration || len(got.Variants) != 3 {
		t.Fatalf("unexpected generation: capability=%s variants=%d", runtime.request.Capability, len(got.Variants))
	}
	if !strings.Contains(runtime.request.Prompt, `"source_id"`) {
		t.Fatal("prompt must expose stable grounding IDs")
	}
	if !strings.Contains(runtime.request.Prompt, `"priority_finding_id"`) {
		t.Fatal("prompt must expose the priority finding")
	}
	if got.InvocationID != "inv-plan-1" {
		t.Fatalf("invocation = %q", got.InvocationID)
	}
	if runtime.request.Validate == nil || runtime.request.Schema == nil {
		t.Fatal("generator must attach schema and validate")
	}
}

func TestPlanSetGeneratorSecondAttemptCarriesReasonCodes(t *testing.T) {
	runtime := &fakeStructuredRuntime{result: validGeneratedPlanSetJSON()}
	_, err := NewPlanSetGenerator(runtime).Generate(context.Background(), planning.GenerationInput{
		Report:           validPlanningReport(),
		Brief:            validDailyBrief(),
		ContentAttempt:   2,
		PriorReasonCodes: []string{"plan.difference_insufficient"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(runtime.request.Prompt, "plan.difference_insufficient") {
		t.Fatal("retry prompt must carry prior reason codes")
	}
}

func TestPlanSetGeneratorRejectsUnknownGroundingSourceType(t *testing.T) {
	runtime := &fakeStructuredRuntime{result: generatedJSONWithSourceType("provider_guess")}
	_, err := NewPlanSetGenerator(runtime).Generate(context.Background(), validGenerationInput())
	if !errors.Is(err, ErrGeneratorContract) {
		t.Fatalf("got %v, want ErrGeneratorContract", err)
	}
}

// 变体内步骤类别重复(实测 kimi-k3 两次给出 hair+outfit+outfit)时,错误消息
// 是重试 prompt 唯一能看到的修正线索:必须说清"重复"、点名变体与类别,
// 笼统的 bad step category 让模型无从下手。
func TestPlanSetGeneratorNamesRepeatedStepCategory(t *testing.T) {
	base := validGeneratedPlanSetJSON()
	// 把 sharp 变体的 makeup 步骤整段换成 outfit 步骤(details 保持 outfit 形状),
	// 得到 hair+outfit+outfit —— 即第 10 轮 E2E kimi-k3 两次采出的违约形态。
	makeupStep := `{"category":"makeup","action":"keep","title":"保持干净眉形"`
	outfitStep := `{"category":"outfit","action":"adjust","title":"换合肩线上装","summary":"用合肩直线版型替代过塌肩线。","details":{"target":"","intensity":"","silhouette":"合肩直线版型","palette":["象牙白"],"layers":["浅色内搭"],"avoid":["夸张图案"],"formality":"smart_casual"},"groundings":[{"source_type":"report_finding","source_id":"21000000-0000-0000-0000-000000000002","reason":"报告观察到肩线偏塌"},{"source_type":"scene_answer","source_id":"weather","reason":"空调环境需要一层外搭"}]}`
	text := string(base)
	start := strings.Index(text, makeupStep)
	end := strings.Index(text, outfitStep)
	if start < 0 || end < 0 || end < start {
		t.Fatal("fixture layout changed")
	}
	duplicated := []byte(text[:start] + outfitStep + "," + text[end:])
	runtime := &fakeStructuredRuntime{result: duplicated}
	_, err := NewPlanSetGenerator(runtime).Generate(context.Background(), validGenerationInput())
	if !errors.Is(err, ErrGeneratorContract) {
		t.Fatalf("got %v, want ErrGeneratorContract", err)
	}
	if !strings.Contains(err.Error(), "sharp") || !strings.Contains(err.Error(), `"outfit"`) {
		t.Fatalf("error must name variant and category: %v", err)
	}
	if !strings.Contains(err.Error(), "repeats") && !strings.Contains(err.Error(), "重复") {
		t.Fatalf("error must say the category repeats: %v", err)
	}
}

// 第 13 轮 E2E 实测:attempt 1 违约 `json: unknown field "rationale"`(模型把
// rationale 写进步骤),attempt 2 修掉后又撞 `outfit details carry hair/makeup-
// only fields`——两条消息都不带变体/步骤位置,模型只能盲改。违约消息必须定位
// 到 variant + step,这是单次内容重试能自纠的前提。
func TestPlanSetGeneratorLocatesUnknownFieldInStep(t *testing.T) {
	base := validGeneratedPlanSetJSON()
	broken := []byte(strings.Replace(string(base), `{"category":"hair"`, `{"rationale":"步骤里多写的字段","category":"hair"`, 1))
	runtime := &fakeStructuredRuntime{result: broken}
	_, err := NewPlanSetGenerator(runtime).Generate(context.Background(), validGenerationInput())
	if !errors.Is(err, ErrGeneratorContract) {
		t.Fatalf("got %v, want ErrGeneratorContract", err)
	}
	if !strings.Contains(err.Error(), "sharp") {
		t.Fatalf("error must name the variant: %v", err)
	}
	if !strings.Contains(err.Error(), "rationale") {
		t.Fatalf("error must name the unknown field: %v", err)
	}
	if !strings.Contains(err.Error(), "step") {
		t.Fatalf("error must locate the step: %v", err)
	}
}

func TestPlanSetGeneratorLocatesDetailsViolation(t *testing.T) {
	base := validGeneratedPlanSetJSON()
	broken := []byte(strings.Replace(string(base), `"silhouette":"合肩直线版型"`, `"target":"颅顶","silhouette":"合肩直线版型"`, 1))
	runtime := &fakeStructuredRuntime{result: broken}
	_, err := NewPlanSetGenerator(runtime).Generate(context.Background(), validGenerationInput())
	if !errors.Is(err, ErrGeneratorContract) {
		t.Fatalf("got %v, want ErrGeneratorContract", err)
	}
	if !strings.Contains(err.Error(), "sharp") || !strings.Contains(err.Error(), "outfit") {
		t.Fatalf("error must name variant and step category: %v", err)
	}
}

func TestPlanSetGeneratorRejectsBrokenShape(t *testing.T) {
	runtime := &fakeStructuredRuntime{result: []byte(`{"variants":[{"slot":1,"key":"sharp"}]}`)}
	if _, err := NewPlanSetGenerator(runtime).Generate(context.Background(), validGenerationInput()); !errors.Is(err, ErrGeneratorContract) {
		t.Fatalf("got %v, want ErrGeneratorContract", err)
	}
	runtime = &fakeStructuredRuntime{result: generatedJSONWithIntensity("high")}
	if _, err := NewPlanSetGenerator(runtime).Generate(context.Background(), validGenerationInput()); !errors.Is(err, ErrGeneratorContract) {
		t.Fatalf("high intensity must be rejected: %v", err)
	}
}

// ---- local fixtures ----

func validPlanningReport() planning.ReportSnapshot {
	return planning.ReportSnapshot{
		ID:                "20000000-0000-0000-0000-000000000001",
		UserID:            "00000000-0000-0000-0000-000000000001",
		PhotoSetID:        "50000000-0000-0000-0000-000000000001",
		FaceAssetID:       "40000000-0000-0000-0000-000000000002",
		BodyAssetID:       "40000000-0000-0000-0000-000000000001",
		ProfileSnapshot:   []byte(`{"role":"designer"}`),
		ImpressionTags:    []string{"利落"},
		PriorityTitle:     "先整理额前碎发",
		PriorityCopy:      "额前碎发落到眉毛上方，先固定发根。",
		PriorityFindingID: "21000000-0000-0000-0000-000000000001",
		Findings: []planning.FindingSnapshot{
			{ID: "21000000-0000-0000-0000-000000000001", Category: "hair", Priority: 1, Label: "额前碎发", VisibleObservation: "额前碎发落到眉毛上方", Recommendation: "向后梳理并固定"},
			{ID: "21000000-0000-0000-0000-000000000002", Category: "outfit", Priority: 2, Label: "肩线偏塌", VisibleObservation: "上衣肩线低于自然肩点", Recommendation: "换成合肩线的上装"},
			{ID: "21000000-0000-0000-0000-000000000003", Category: "color", Priority: 3, Label: "整体色偏灰", VisibleObservation: "上装与背景同为灰色系", Recommendation: "用亮色内搭拉开层次"},
		},
	}
}

func validDailyBrief() domain.SceneBrief {
	brief, err := planning.NormalizeBrief(domain.SceneDaily, map[string]string{
		"activity": "office", "weather": "air_conditioned", "preparation": "closet", "impression": "natural",
	})
	if err != nil {
		panic(err)
	}
	return brief
}

func validGenerationInput() planning.GenerationInput {
	return planning.GenerationInput{Report: validPlanningReport(), Brief: validDailyBrief(), ContentAttempt: 1}
}

func validGeneratedPlanSetJSON() []byte {
	return []byte(`{"variants":[` +
		strings.Join([]string{planVariantJSON(1, "sharp", true), planVariantJSON(2, "warm", false), planVariantJSON(3, "natural", false)}, ",") +
		`]}`)
}

func planVariantJSON(slot int, key string, recommended bool) string {
	return `{"slot":` + strconv.Itoa(slot) + `,"key":"` + key + `","name":"方案` + strconv.Itoa(slot) + `","descriptor":"有精神且自然","rationale":"落实报告优先建议","recommended":` + boolText(recommended) + `,` +
		`"outcome_tags":["有精神"],"difference_tags":["发型线条"],` +
		`"steps":[` +
		`{"category":"hair","action":"adjust","title":"抬高发型重心","summary":"保持原有长度，只整理颅顶和耳侧线条。","details":{"target":"颅顶自然蓬松、耳侧线条整洁","intensity":"low","silhouette":"","palette":[],"layers":[],"avoid":["不改变发长"],"formality":""},"groundings":[{"source_type":"report_finding","source_id":"21000000-0000-0000-0000-000000000001","reason":"报告观察到发型重心偏低"}]},` +
		`{"category":"makeup","action":"keep","title":"保持干净眉形","summary":"现有眉形清晰，无需调整。","details":{"target":"保持眉形清洁","intensity":"low","silhouette":"","palette":[],"layers":[],"avoid":[],"formality":""},"groundings":[{"source_type":"style_rule","source_id":"style.low_intensity_makeup","reason":"无妆容 finding，保持低强度"}]},` +
		`{"category":"outfit","action":"adjust","title":"换合肩线上装","summary":"用合肩直线版型替代过塌肩线。","details":{"target":"","intensity":"","silhouette":"合肩直线版型","palette":["象牙白"],"layers":["浅色内搭"],"avoid":["夸张图案"],"formality":"smart_casual"},"groundings":[{"source_type":"report_finding","source_id":"21000000-0000-0000-0000-000000000002","reason":"报告观察到肩线偏塌"},{"source_type":"scene_answer","source_id":"weather","reason":"空调环境需要一层外搭"}]` +
		`}]}`
}

func generatedJSONWithSourceType(sourceType string) []byte {
	base := validGeneratedPlanSetJSON()
	return []byte(strings.Replace(string(base), `"source_type":"report_finding"`, `"source_type":"`+sourceType+`"`, 1))
}

func generatedJSONWithIntensity(intensity string) []byte {
	base := validGeneratedPlanSetJSON()
	return []byte(strings.Replace(string(base), `"intensity":"low"`, `"intensity":"`+intensity+`"`, 1))
}

func boolText(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func TestPlanSetVerifierPassesCleanCandidate(t *testing.T) {
	runtime := &fakeStructuredRuntime{result: []byte(`{"decision":"pass","reason_codes":[],"violations":[]}`)}
	got, err := NewPlanSetVerifier(runtime).Verify(context.Background(), planning.VerificationInput{
		Report: validPlanningReport(), Brief: validDailyBrief(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Decision != "pass" || len(got.ReasonCodes) != 0 || got.InvocationID != "inv-plan-1" {
		t.Fatalf("unexpected verification: %#v", got)
	}
	if runtime.request.Capability != CapabilityPlanGroundingVerification {
		t.Fatalf("capability = %s", runtime.request.Capability)
	}
	if len(runtime.request.Images) != 0 {
		t.Fatal("verifier must not receive images")
	}
}

func TestPlanSetVerifierDecodesRejection(t *testing.T) {
	runtime := &fakeStructuredRuntime{result: []byte(`{"decision":"reject","reason_codes":["plan.unsupported_brand_claim"],"violations":["方案提到具体品牌"]}`)}
	got, err := NewPlanSetVerifier(runtime).Verify(context.Background(), planning.VerificationInput{
		Report: validPlanningReport(), Brief: validDailyBrief(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Decision != "reject" || len(got.ReasonCodes) != 1 || got.ReasonCodes[0] != "plan.unsupported_brand_claim" {
		t.Fatalf("unexpected rejection: %#v", got)
	}
}

func TestPlanSetVerifierRejectsContractViolations(t *testing.T) {
	cases := [][]byte{
		[]byte(`{"decision":"reject","reason_codes":[],"violations":[]}`),
		[]byte(`{"decision":"pass","reason_codes":["plan.report_contradiction"],"violations":[]}`),
		[]byte(`{"decision":"maybe","reason_codes":[],"violations":[]}`),
	}
	for _, result := range cases {
		runtime := &fakeStructuredRuntime{result: result}
		if _, err := NewPlanSetVerifier(runtime).Verify(context.Background(), planning.VerificationInput{
			Report: validPlanningReport(), Brief: validDailyBrief(),
		}); !errors.Is(err, ErrVerifierContract) {
			t.Fatalf("got %v, want ErrVerifierContract for %s", err, result)
		}
	}
}

// 确定性门禁逐条执行的规则必须在 prompt 里写明——线上 kimi-k3 因为契约没说清
// 连续三轮产出 grounding_unknown_id / scene_constraint_uncovered /
// copy_policy_violation（把 report.id 当 finding 引用、把 scene_answer 写成
// brief.answers.focus、outfit 文案带"真丝"）。
func TestPlanSetPromptSpellsOutExactGroundingIDForms(t *testing.T) {
	runtime := &fakeStructuredRuntime{result: validGeneratedPlanSetJSON()}
	_, err := NewPlanSetGenerator(runtime).Generate(context.Background(), planning.GenerationInput{
		Report:         validPlanningReport(),
		Brief:          validDailyBrief(),
		ContentAttempt: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"不得引用 report.id",
		"字段名本身",
		"每个字段都必须至少出现在一条 scene_answer grounding 中",
		"不得发明新 ID",
		"priority_finding_id",
		"材质词",
		"真丝",
	} {
		if !strings.Contains(runtime.request.Instructions, want) {
			t.Fatalf("instructions must state the grounding contract (%q missing):\n%s", want, runtime.request.Instructions)
		}
	}
}

// 步骤结构契约只写在 schema blob 和校验器错误消息里，json_object 模式的小模型
// 读不懂 oneOf/const——线上 qwen3.8-flash 两次采样先后产出 hair+outfit+outfit
// 与 outfit 携带 target/intensity，两轮重试全烧完直接永久失败。确定性契约必须
// 和 grounding 契约一样在 instructions 里逐字写明。
func TestPlanSetPromptSpellsOutStepStructureContract(t *testing.T) {
	runtime := &fakeStructuredRuntime{result: validGeneratedPlanSetJSON()}
	_, err := NewPlanSetGenerator(runtime).Generate(context.Background(), planning.GenerationInput{
		Report:         validPlanningReport(),
		Brief:          validDailyBrief(),
		ContentAttempt: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"每个变体恰好三步",
		"各恰好一步",
		"只能是 low 或 medium",
		"silhouette、palette、layers、formality 必须为空字符串或空数组",
		"outfit 只用 silhouette",
		"target 与 intensity 必须为空字符串",
		"action 只能是 keep 或 adjust",
	} {
		if !strings.Contains(runtime.request.Instructions, want) {
			t.Fatalf("instructions must state the step structure contract (%q missing):\n%s", want, runtime.request.Instructions)
		}
	}
}

// 发给厂商的 response_format schema 只能包含校验形状;文件里的 JSON Schema
// 元字段($schema/$id/title)会被模型当成输出字段回显——线上 qwen3.7-flash
// 在 plan_grounding_verification 回包顶层塞了 "$id",撞 DisallowUnknownFields
// 变成 ErrVerifierContract,整个操作 unclassified 失败。
func TestPlanningSchemasCarryNoMetaFields(t *testing.T) {
	for name, schema := range map[string]map[string]any{
		"plan_set.v1":          PlanSetSchema(),
		"plan_verification.v1": PlanVerificationSchema(),
	} {
		for _, meta := range []string{"$schema", "$id", "title"} {
			if _, present := schema[meta]; present {
				t.Fatalf("%s must not send meta field %q to the provider", name, meta)
			}
		}
	}
}
