package provider

import (
	"context"

	"github.com/example/jianwo/server/internal/domain"
)

type FallbackAnalyzer struct {
	primary  Analyzer
	fallback Analyzer
}

func NewFallbackAnalyzer(primary, fallback Analyzer) *FallbackAnalyzer {
	return &FallbackAnalyzer{primary: primary, fallback: fallback}
}

func (a *FallbackAnalyzer) Analyze(ctx context.Context, input domain.CreateAnalysisInput) (domain.AnalysisOutput, error) {
	output, err := a.primary.Analyze(ctx, input)
	if err == nil || a.fallback == nil {
		return output, err
	}
	return a.fallback.Analyze(ctx, input)
}
