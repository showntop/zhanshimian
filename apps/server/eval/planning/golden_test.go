package planning

import (
	"slices"
	"testing"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/service/planning"
)

func TestGoldenSetShapeAndPartitions(t *testing.T) {
	briefs := LoadBriefCases(t, "testdata/briefs.v1.jsonl")
	plans := LoadPlanSetCases(t, "testdata/plan_sets.v1.jsonl")
	if len(briefs) != 120 || len(plans) != 180 {
		t.Fatalf("briefs=%d plans=%d", len(briefs), len(plans))
	}
	assertGoldenSplit(t, briefs, map[Split]int{Development: 72, Validation: 24, Release: 24})
	assertGoldenSplit(t, plans, map[Split]int{Development: 108, Validation: 36, Release: 36})
	assertNoReportFixtureCrossesSplits(t, briefs, plans)
	assertPerScene(t, briefs, 20)
	assertPerScene(t, plans, 30)
}

func TestGoldenBriefNormalizationExpectations(t *testing.T) {
	for _, c := range LoadBriefCases(t, "testdata/briefs.v1.jsonl") {
		_, err := BriefInput(c)
		if c.Valid && err != nil {
			t.Fatalf("%s: expected valid brief, got %v", c.ID, err)
		}
		if !c.Valid && err == nil {
			t.Fatalf("%s: expected invalid brief to be rejected", c.ID)
		}
	}
}

func TestGoldenPlanSetGateExpectations(t *testing.T) {
	for _, tc := range LoadPlanSetCases(t, "testdata/plan_sets.v1.jsonl") {
		input := planning.ValidationInput{
			Report:    ReportFixtureSnapshot(tc.ReportFixtureID),
			Brief:     briefOf(t, tc),
			Candidate: tc.Candidate,
		}
		got := violationCodes(planning.ValidateCandidate(input))
		if !slices.Equal(got, tc.WantReasonCodes) {
			t.Fatalf("%s got=%v want=%v", tc.ID, got, tc.WantReasonCodes)
		}
	}
}

func briefOf(t *testing.T, tc PlanSetCase) domain.SceneBrief {
	t.Helper()
	for _, c := range LoadBriefCases(t, "testdata/briefs.v1.jsonl") {
		if c.ID == tc.BriefCaseID {
			brief, err := BriefInput(c)
			if err != nil {
				t.Fatalf("%s: linked brief %s is invalid: %v", tc.ID, c.ID, err)
			}
			return brief
		}
	}
	t.Fatalf("%s: linked brief %s not found", tc.ID, tc.BriefCaseID)
	return domain.SceneBrief{}
}

// violationCodes 由 run.go 提供(测试与 cmd/eval 共用同一份去重排序逻辑)。

func assertGoldenSplit(t *testing.T, cases any, want map[Split]int) {
	t.Helper()
	counts := map[Split]int{}
	switch typed := cases.(type) {
	case []BriefCase:
		for _, c := range typed {
			counts[c.Split]++
		}
	case []PlanSetCase:
		for _, c := range typed {
			counts[c.Split]++
		}
	default:
		t.Fatal("unsupported case type")
	}
	for split, wantCount := range want {
		if counts[split] != wantCount {
			t.Fatalf("split %s = %d, want %d", split, counts[split], wantCount)
		}
	}
}

func assertNoReportFixtureCrossesSplits(t *testing.T, briefs []BriefCase, plans []PlanSetCase) {
	t.Helper()
	fixtureSplit := map[string]Split{}
	for _, c := range briefs {
		if prev, ok := fixtureSplit[c.ReportFixtureID]; ok && prev != c.Split {
			t.Fatalf("%s: fixture %s appears in %s and %s", c.ID, c.ReportFixtureID, prev, c.Split)
		}
		fixtureSplit[c.ReportFixtureID] = c.Split
	}
	for _, c := range plans {
		if prev, ok := fixtureSplit[c.ReportFixtureID]; ok && prev != c.Split {
			t.Fatalf("%s: fixture %s appears in %s and %s", c.ID, c.ReportFixtureID, prev, c.Split)
		}
		fixtureSplit[c.ReportFixtureID] = c.Split
	}
}

func assertPerScene(t *testing.T, cases any, want int) {
	t.Helper()
	counts := map[string]int{}
	switch typed := cases.(type) {
	case []BriefCase:
		for _, c := range typed {
			counts[c.Scene]++
		}
	case []PlanSetCase:
		for _, c := range typed {
			counts[c.Scene]++
		}
	default:
		t.Fatal("unsupported case type")
	}
	for scene, count := range counts {
		if count != want {
			t.Fatalf("scene %s has %d cases, want %d", scene, count, want)
		}
	}
	if len(counts) != 6 {
		t.Fatalf("want all six scenes covered, got %d", len(counts))
	}
}
