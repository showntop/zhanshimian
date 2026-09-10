package provider

import (
	"context"
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
