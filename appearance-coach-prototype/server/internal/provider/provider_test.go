package provider

import (
	"context"
	"testing"

	"github.com/example/jianwo/server/internal/domain"
)

func TestDemoAnalyzerReturnsRespectfulCompleteOutput(t *testing.T) {
	output, err := NewDemoAnalyzer().Analyze(context.Background(), domain.CreateAnalysisInput{Scene: "interview", MediaIDs: []string{"a", "b", "c"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(output.Findings) != 4 || len(output.Plans) != 3 {
		t.Fatalf("unexpected output sizes: findings=%d plans=%d", len(output.Findings), len(output.Plans))
	}
	if !output.Plans[0].Recommended || len(output.Plans[0].Steps) != 3 {
		t.Fatalf("first plan should be recommended and actionable: %#v", output.Plans[0])
	}
	for _, forbidden := range []string{"颜值", "丑", "身材分"} {
		if contains(output.PriorityCopy, forbidden) {
			t.Fatalf("output contains forbidden judgment %q", forbidden)
		}
	}
}

func contains(value, part string) bool {
	for i := 0; i+len(part) <= len(value); i++ {
		if value[i:i+len(part)] == part {
			return true
		}
	}
	return false
}
