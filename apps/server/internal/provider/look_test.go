package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/zhanshimian/server/internal/domain"
)

type lookMediaLoaderStub struct {
	images []AnalysisImage
}

func (s lookMediaLoaderStub) Load(context.Context, []string) ([]AnalysisImage, error) {
	return s.images, nil
}

func TestLookPromptRequiresFaceAndFullBodyComposition(t *testing.T) {
	prompt := lookPrompt(LookInput{
		Name: "都市利落风",
		Steps: []domain.PlanStep{
			{Category: "hair", Title: "侧分层次", Summary: "露出面部轮廓"},
			{Category: "makeup", Title: "清透底妆", Summary: "增强眉眼对比"},
			{Category: "outfit", Title: "利落套装", Summary: "保留纵向线条"},
		},
	}, true)

	for _, required := range []string{"完整脸部", "发型", "妆面", "上衣和下装", "从头到脚", "绝不能裁掉头部", "第二张是同一人物的面部特写", "禁止换脸"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("look prompt must contain %q: %s", required, prompt)
		}
	}
}

func TestLookPromptWithoutFaceDoesNotClaimSecondPhoto(t *testing.T) {
	prompt := lookPrompt(LookInput{Name: "都市利落风"}, false)
	if strings.Contains(prompt, "第二张") {
		t.Fatalf("single-image look prompt must not claim a face reference: %s", prompt)
	}
	if !strings.Contains(prompt, "唯一身份来源") || !strings.Contains(prompt, "禁止换脸") {
		t.Fatalf("single-image look prompt must still lock identity: %s", prompt)
	}
}

func TestDemoLookGeneratorReturnsUsersBodyPhoto(t *testing.T) {
	generator := NewDemoLookGenerator(lookMediaLoaderStub{images: []AnalysisImage{
		{Kind: "face", MIMEType: "image/jpeg", Data: []byte("face")},
		{Kind: "body", MIMEType: "image/png", Data: []byte("body")},
	}})

	result, err := generator.Generate(context.Background(), LookInput{MediaIDs: []string{"face", "body"}})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if string(result.ImageData) != "body" || result.MIMEType != "image/png" {
		t.Fatalf("unexpected demo look result: %#v", result)
	}
	if result.ProviderVersion != "demo-look-pass-through-v1" {
		t.Fatalf("unexpected provider version: %q", result.ProviderVersion)
	}
}
