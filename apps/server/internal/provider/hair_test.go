package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/zhanshimian/server/internal/domain"
)

func TestDemoHairGeneratorReturnsBundledStyle(t *testing.T) {
	output, err := NewDemoHairGenerator().Generate(context.Background(), domain.HairPreviewInput{StyleID: "warm"})
	if err != nil || output.ProviderVersion != "demo-hair-v1" || output.ImageURL == "" {
		t.Fatalf("unexpected demo hair output: %#v err=%v", output, err)
	}
	if _, err := NewDemoHairGenerator().Generate(context.Background(), domain.HairPreviewInput{StyleID: "unknown"}); err == nil {
		t.Fatal("unknown style should be rejected")
	}
}

func TestHairPromptLocksIdentity(t *testing.T) {
	prompt := hairPrompt("sharp")
	for _, required := range []string{"仅编辑人物发型", "禁止换脸", "妆容、身体、服装"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("hair prompt must contain %q: %s", required, prompt)
		}
	}
}
