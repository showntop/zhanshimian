package planning

import (
	"fmt"
	"slices"
	"sort"

	"github.com/zhanshimian/server/internal/service/planning"
)

// Summary 聚合一次 planning 金集运行:案例数与两条硬指标的达成率。
// GroundingCompleteness 是接地裁决与金集期望一致的案例占比;
// PairDifferencePassRate 是两两差异裁决一致的案例占比。
type Summary struct {
	Briefs                 int
	PlanSets               int
	GroundingCompleteness  float64
	PairDifferencePassRate float64
}

// Run 加载金集文件、校验分区纪律(报告 fixture 不得跨分区),再把每个案例
// 跑过生产环境的 brief 归一化与 PlanSet Gate:任何与金集期望不一致都是硬
// 门禁失败。split 为空时评估全部案例。
func Run(briefsPath, plansPath string, split Split) (Summary, error) {
	briefs, err := LoadBriefCasesStrict(briefsPath)
	if err != nil {
		return Summary{}, err
	}
	plans, err := LoadPlanSetCasesStrict(plansPath)
	if err != nil {
		return Summary{}, err
	}
	if err := checkFixturePartition(briefs, plans); err != nil {
		return Summary{}, err
	}
	briefByID := make(map[string]BriefCase, len(briefs))
	summary := Summary{}
	for _, c := range briefs {
		briefByID[c.ID] = c
		if split != "" && c.Split != split {
			continue
		}
		summary.Briefs++
		_, err := BriefInput(c)
		if c.Valid && err != nil {
			return summary, fmt.Errorf("brief %s: expected valid brief, got %v", c.ID, err)
		}
		if !c.Valid && err == nil {
			return summary, fmt.Errorf("brief %s: expected invalid brief to be rejected", c.ID)
		}
	}
	groundingMatches, differenceMatches := 0, 0
	for _, tc := range plans {
		if split != "" && tc.Split != split {
			continue
		}
		summary.PlanSets++
		briefCase, ok := briefByID[tc.BriefCaseID]
		if !ok {
			return summary, fmt.Errorf("plan %s: linked brief %s not found", tc.ID, tc.BriefCaseID)
		}
		brief, err := BriefInput(briefCase)
		if err != nil {
			return summary, fmt.Errorf("plan %s: linked brief %s is invalid: %v", tc.ID, tc.BriefCaseID, err)
		}
		got := violationCodes(planning.ValidateCandidate(planning.ValidationInput{
			Report:    ReportFixtureSnapshot(tc.ReportFixtureID),
			Brief:     brief,
			Candidate: tc.Candidate,
		}))
		if !slices.Equal(got, tc.WantReasonCodes) {
			return summary, fmt.Errorf("plan %s got=%v want=%v", tc.ID, got, tc.WantReasonCodes)
		}
		if slices.Contains(got, planning.ReasonStepGroundingMissing) == slices.Contains(tc.WantReasonCodes, planning.ReasonStepGroundingMissing) {
			groundingMatches++
		}
		if slices.Contains(got, planning.ReasonDifferenceInsufficient) == slices.Contains(tc.WantReasonCodes, planning.ReasonDifferenceInsufficient) {
			differenceMatches++
		}
	}
	if summary.PlanSets > 0 {
		summary.GroundingCompleteness = float64(groundingMatches) / float64(summary.PlanSets)
		summary.PairDifferencePassRate = float64(differenceMatches) / float64(summary.PlanSets)
	}
	return summary, nil
}

// checkFixturePartition 校验报告 fixture 不跨分区(与金集 shape 测试同规则)。
func checkFixturePartition(briefs []BriefCase, plans []PlanSetCase) error {
	fixtureSplit := map[string]Split{}
	for _, c := range briefs {
		if prev, ok := fixtureSplit[c.ReportFixtureID]; ok && prev != c.Split {
			return fmt.Errorf("%s: fixture %s appears in %s and %s", c.ID, c.ReportFixtureID, prev, c.Split)
		}
		fixtureSplit[c.ReportFixtureID] = c.Split
	}
	for _, c := range plans {
		if prev, ok := fixtureSplit[c.ReportFixtureID]; ok && prev != c.Split {
			return fmt.Errorf("%s: fixture %s appears in %s and %s", c.ID, c.ReportFixtureID, prev, c.Split)
		}
		fixtureSplit[c.ReportFixtureID] = c.Split
	}
	return nil
}

// violationCodes returns the deduplicated, sorted reason code set — golden
// records pin the minimal expected code set, not per-detail duplicates.
func violationCodes(violations []planning.Violation) []string {
	seen := map[string]bool{}
	codes := make([]string, 0, len(violations))
	for _, violation := range violations {
		if !seen[violation.Code] {
			seen[violation.Code] = true
			codes = append(codes, violation.Code)
		}
	}
	sort.Strings(codes)
	return codes
}
