package ai

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/zhanshimian/server/internal/domain"
)

func TestAssessmentProvidersKeepImageOrderAndInternalConfidence(t *testing.T) {
	runtime := &runtimeSpy{responses: map[string][]byte{
		"photo_quality_check":          []byte(`{"photos":[{"role":"face","decision":"pass","reason_code":""},{"role":"side","decision":"pass","reason_code":""},{"role":"body","decision":"pass","reason_code":""}]}`),
		"photo_identity_consistency":   []byte(`{"decision":"pass","confidence":0.97}`),
		"appearance_analysis":          marshal(t, validReportJSON()),
		"report_evidence_verification": validEvidenceJSON(t, []string{"hair-fringe", "makeup-brow", "outfit-collar"}),
	}}
	providers := NewAssessmentProviders(runtime)
	images := orderedImages()
	quality, err := providers.PhotoContent.Check(context.Background(), images)
	if err != nil {
		t.Fatal(err)
	}
	if got := runtime.roles("photo_quality_check"); !equalStrings(got, []string{"face", "side", "body"}) {
		t.Fatalf("quality roles = %v, want face/side/body", got)
	}
	if len(quality.Photos) != 3 {
		t.Fatalf("quality photos = %d, want 3", len(quality.Photos))
	}
	identity, err := providers.Identity.Check(context.Background(), images)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(identity.Confidence-0.97) > 0.001 {
		t.Fatalf("identity confidence = %v, want 0.97", identity.Confidence)
	}
}

func TestReportSchemaAcceptsSnakeCaseAnchor(t *testing.T) {
	raw := []byte(`{"impression_tags":["利落"],"priority_title":"先整理额前碎发","priority_copy":"额前碎发会挡住眉形，先固定再看妆容层次。","findings":[{"key":"hair-fringe","category":"hair","label":"额前碎发","visible_observation":"额前碎发落到眉毛上方","recommendation":"用少量发蜡向后梳理并固定","priority":1,"position":1,"source_role":"face","anchor":{"x":0.2,"y":0.1,"w":0.4,"h":0.2}},{"key":"makeup-brow","category":"makeup","label":"眉形层次","visible_observation":"眉尾比眉头更淡，左右不对称","recommendation":"用眉笔补齐眉尾，保持自然过渡","priority":2,"position":2,"source_role":"face","anchor":{"x":0.25,"y":0.22,"w":0.5,"h":0.12}},{"key":"outfit-collar","category":"outfit","label":"领口位置","visible_observation":"领口偏松，肩线看起来往下滑","recommendation":"换成合肩的上衣，领口贴近锁骨","priority":3,"position":3,"source_role":"body","anchor":{"x":0.3,"y":0.18,"w":0.4,"h":0.16}}]}`)
	if err := validateReportPayload(raw); err != nil {
		t.Fatalf("snake_case anchor should be accepted: %v", err)
	}
}

// 草稿校验失败必须携带 ErrReportDraftContract 哨兵:assessment handler 据此
// 把采样方差(空文本、缺锚点)归类为消耗一次生成预算的内容违约,与传输/配额
// 故障区分;哨兵还需穿透路由器的多原因合并错误(errors.Is 沿 Unwrap 链可达)。
func TestReportSchemaViolationCarriesDraftContractSentinel(t *testing.T) {
	payload := validReportJSON()
	payload.Findings[0].Anchor.W = 0
	err := validateReportPayload(marshal(t, payload))
	if !errors.Is(err, ErrReportDraftContract) {
		t.Fatalf("got %v, want errors.Is ErrReportDraftContract", err)
	}
	payload = validReportJSON()
	payload.Findings[0].VisibleObservation = ""
	if err := validateReportPayload(marshal(t, payload)); !errors.Is(err, ErrReportDraftContract) {
		t.Fatalf("empty text: got %v, want errors.Is ErrReportDraftContract", err)
	}
	if err := validateReportPayload([]byte(`{broken`)); !errors.Is(err, ErrReportDraftContract) {
		t.Fatalf("bad JSON: got %v, want errors.Is ErrReportDraftContract", err)
	}
}

func TestReportSchemaRejectsVisualScoresAndMissingAnchor(t *testing.T) {
	payload := validReportJSON()
	payload.Findings[0].VisibleObservation = "身材评分 80"
	if err := validateReportPayload(marshal(t, payload)); err == nil {
		t.Fatal("expected score copy to be rejected")
	}
	payload = validReportJSON()
	payload.Findings[0].Anchor.W = 0
	if err := validateReportPayload(marshal(t, payload)); err == nil {
		t.Fatal("expected missing anchor to be rejected")
	}
}

func TestEvidenceVerifierRequiresOneDecisionPerFinding(t *testing.T) {
	if err := validateEvidencePayload(
		[]string{"f1", "f2"},
		[]byte(`{"findings":[{"key":"f1","supported":true,"confidence":0.96,"reason_code":""}]}`),
	); err == nil {
		t.Fatal("expected missing finding decision to be rejected")
	}
}

func TestPhotoQualitySchemaRejectsUnknownReasonCode(t *testing.T) {
	if err := validatePhotoQualityPayload([]byte(`{"photos":[{"role":"face","decision":"reject","reason_code":"ugly"},{"role":"side","decision":"pass","reason_code":""},{"role":"body","decision":"pass","reason_code":""}]}`)); err == nil {
		t.Fatal("expected unknown reason_code to be rejected")
	}
	if err := validatePhotoQualityPayload([]byte(`{"photos":[{"role":"face","decision":"reject","reason_code":"too_blurry"},{"role":"side","decision":"pass","reason_code":""},{"role":"body","decision":"pass","reason_code":""}]}`)); err != nil {
		t.Fatalf("allowed reason_code: %v", err)
	}
}

func TestIdentitySchemaRequiresDecisionAndConfidence(t *testing.T) {
	if err := validateIdentityPayload([]byte(`{"decision":"maybe","confidence":0.97}`)); err == nil {
		t.Fatal("expected invalid identity decision to be rejected")
	}
	if err := validateIdentityPayload([]byte(`{"decision":"uncertain","confidence":0.71}`)); err != nil {
		t.Fatalf("uncertain decision: %v", err)
	}
	if err := validateIdentityPayload([]byte(`{"decision":"pass","confidence":1.5}`)); err == nil {
		t.Fatal("expected out-of-range confidence to be rejected")
	}
}

func TestEvidenceVerifierRejectsExtraFinding(t *testing.T) {
	if err := validateEvidencePayload(
		[]string{"f1"},
		[]byte(`{"findings":[{"key":"f1","supported":true,"confidence":0.96,"reason_code":""},{"key":"f2","supported":false,"confidence":0.9,"reason_code":"observation_not_visible"}]}`),
	); err == nil {
		t.Fatal("expected extra finding key to be rejected")
	}
}

func TestReportAnalyzerForbidsScoringAndLimitsProfileToRecommendation(t *testing.T) {
	runtime := &runtimeSpy{responses: map[string][]byte{
		"appearance_analysis": marshal(t, validReportJSON()),
	}}
	providers := NewAssessmentProviders(runtime)
	_, err := providers.Analyzer.Analyze(context.Background(), ReportAnalysisInput{
		Images:  orderedImages(),
		Profile: json.RawMessage(`{"occupation":"设计师","budget":"medium","height_cm":170}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	req := runtime.request("appearance_analysis")
	text := req.Instructions + "\n" + req.Prompt
	for _, needle := range []string{"外貌", "身材", "年龄", "敏感", "职业", "预算", "身高", "recommendation"} {
		if !strings.Contains(text, needle) {
			t.Fatalf("appearance_analysis prompt missing %q\n%s", needle, text)
		}
	}
	if got := runtime.roles("appearance_analysis"); !equalStrings(got, []string{"face", "side", "body"}) {
		t.Fatalf("analysis roles = %v, want face/side/body", got)
	}
}

func TestEvidenceVerifierUsesOriginalImagesAndDraftFindings(t *testing.T) {
	draft := validReportJSON().toDraft()
	runtime := &runtimeSpy{responses: map[string][]byte{
		"report_evidence_verification": validEvidenceJSON(t, []string{"hair-fringe", "makeup-brow", "outfit-collar"}),
	}}
	providers := NewAssessmentProviders(runtime)
	result, err := providers.Evidence.Verify(context.Background(), orderedImages(), draft)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 3 {
		t.Fatalf("evidence findings = %d, want 3", len(result.Findings))
	}
	req := runtime.request("report_evidence_verification")
	text := req.Instructions + "\n" + req.Prompt
	if !strings.Contains(text, "不接收上一模型的自由文本解释") {
		t.Fatal("evidence prompt must refuse previous free-text rationale")
	}
	for _, key := range []string{"hair-fringe", "makeup-brow", "outfit-collar"} {
		if !strings.Contains(text, key) {
			t.Fatalf("evidence prompt missing draft finding %q", key)
		}
	}
	if !strings.Contains(text, "原图") {
		t.Fatal("evidence prompt must use the three original images")
	}
	if got := runtime.roles("report_evidence_verification"); !equalStrings(got, []string{"face", "side", "body"}) {
		t.Fatalf("evidence roles = %v, want face/side/body", got)
	}
}

type runtimeSpy struct {
	responses map[string][]byte
	calls     []StructuredRequest
}

func (s *runtimeSpy) Structured(_ context.Context, req StructuredRequest) (StructuredResult, error) {
	s.calls = append(s.calls, req)
	data := s.responses[req.Capability]
	if req.Validate != nil {
		if err := req.Validate(data); err != nil {
			return StructuredResult{}, err
		}
	}
	return StructuredResult{JSON: data, Meta: InvocationMeta{InvocationID: "inv-" + req.Capability}}, nil
}

func (s *runtimeSpy) roles(capability string) []string {
	req := s.request(capability)
	roles := make([]string, len(req.Images))
	for i, image := range req.Images {
		roles[i] = image.Role
	}
	return roles
}

func (s *runtimeSpy) request(capability string) StructuredRequest {
	for _, call := range s.calls {
		if call.Capability == capability {
			return call
		}
	}
	return StructuredRequest{}
}

func orderedImages() []ImageInput {
	return []ImageInput{
		{Role: "face", MIMEType: "image/jpeg", Data: []byte("face")},
		{Role: "side", MIMEType: "image/jpeg", Data: []byte("side")},
		{Role: "body", MIMEType: "image/jpeg", Data: []byte("body")},
	}
}

func validReportJSON() reportPayload {
	return reportPayload{
		ImpressionTags: []string{"利落", "干净"},
		PriorityTitle:  "先整理额前碎发",
		PriorityCopy:   "额前碎发会挡住眉形，先固定再看妆容层次。",
		Findings: []reportFindingPayload{
			{
				Key: "hair-fringe", Category: "hair", Label: "额前碎发",
				VisibleObservation: "额前碎发落到眉毛上方", Recommendation: "用少量发蜡向后梳理并固定",
				Priority: 1, Position: 1, SourceRole: "face",
				Anchor: domain.EvidenceAnchor{X: 0.20, Y: 0.10, W: 0.40, H: 0.20},
			},
			{
				Key: "makeup-brow", Category: "makeup", Label: "眉形层次",
				VisibleObservation: "眉尾比眉头更淡，左右不对称", Recommendation: "用眉笔补齐眉尾，保持自然过渡",
				Priority: 2, Position: 2, SourceRole: "face",
				Anchor: domain.EvidenceAnchor{X: 0.25, Y: 0.22, W: 0.50, H: 0.12},
			},
			{
				Key: "outfit-collar", Category: "outfit", Label: "领口位置",
				VisibleObservation: "领口偏松，肩线看起来往下滑", Recommendation: "换成合肩的上衣，领口贴近锁骨",
				Priority: 3, Position: 3, SourceRole: "body",
				Anchor: domain.EvidenceAnchor{X: 0.30, Y: 0.18, W: 0.40, H: 0.16},
			},
		},
	}
}

func validEvidenceJSON(t *testing.T, keys []string) []byte {
	t.Helper()
	findings := make([]EvidenceDecision, len(keys))
	for i, key := range keys {
		findings[i] = EvidenceDecision{Key: key, Supported: true, Confidence: 0.96}
	}
	return marshal(t, evidencePayload{Findings: findings})
}

func marshal(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
