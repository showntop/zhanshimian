package provider

import (
	"context"
	"testing"

	"github.com/zhanshimian/server/internal/domain"
)

func TestDemoAnalyzerReturnsRespectfulCompleteOutput(t *testing.T) {
	output, err := NewDemoAnalyzer().Analyze(context.Background(), domain.CreateAnalysisInput{Scene: "interview", MediaIDs: []string{"a", "b", "c"}})
	if err != nil {
		t.Fatal(err)
	}
	// 报告与方案解耦：分析只产出 findings/标签/优先建议，方案由
	// plan_group 生成器负责（见 DemoPlanGroupGenerator 测试）。
	if len(output.Findings) != 4 || len(output.ImpressionTags) != 3 {
		t.Fatalf("unexpected output sizes: findings=%d tags=%d", len(output.Findings), len(output.ImpressionTags))
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
