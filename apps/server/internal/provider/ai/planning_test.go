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
