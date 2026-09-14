// Package planning evaluates the static planning golden set: brief
// normalization cases and plan set gate expectations. No live model runs
// here; PR CI only checks dataset shape, partition discipline and the
// deterministic gate outcomes.
package planning

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/service/planning"
)

// testingTB keeps the loader usable from tests without importing testing in
// the production surface.
type testingTB interface {
	Helper()
	Fatal(args ...any)
	Fatalf(format string, args ...any)
}

// Split is one golden partition.
type Split string

const (
	Development Split = "development"
	Validation  Split = "validation"
	Release     Split = "release"
)

// BriefCase is one normalization expectation.
type BriefCase struct {
	ID              string            `json:"id"`
	Split           Split             `json:"split"`
	ReportFixtureID string            `json:"report_fixture_id"`
	Scene           string            `json:"scene"`
	Answers         map[string]string `json:"answers"`
	Valid           bool              `json:"valid"`
	WantErrorCode   string            `json:"want_error_code"`
}

// PlanSetCase is one deterministic gate expectation.
type PlanSetCase struct {
	ID              string                     `json:"id"`
	Split           Split                      `json:"split"`
	ReportFixtureID string                     `json:"report_fixture_id"`
	Scene           string                     `json:"scene"`
	BriefCaseID     string                     `json:"brief_case_id"`
	Candidate       planning.GeneratedPlanSet  `json:"candidate"`
	WantReasonCodes []string                   `json:"want_reason_codes"`
}

var knownScenes = map[string]bool{
	"general": true, "interview": true, "wedding": true,
	"date": true, "daily": true, "gathering": true,
}

var knownReasonCodes = map[string]bool{
	planning.ReasonVariantCount:             true,
	planning.ReasonVariantKeySet:            true,
	planning.ReasonVariantSlotSet:           true,
	planning.ReasonRecommendedCount:         true,
	planning.ReasonStepCategorySet:          true,
	planning.ReasonStepActionInvalid:        true,
	planning.ReasonStepDetailsInvalid:       true,
	planning.ReasonStepGroundingMissing:     true,
	planning.ReasonGroundingUnknownSrc:      true,
	planning.ReasonGroundingUnknownID:       true,
	planning.ReasonReportPriorityUncovered:  true,
	planning.ReasonSceneConstraintUncovered: true,
	planning.ReasonDifferenceInsufficient:   true,
	planning.ReasonCopyPolicyViolation:      true,
}

// LoadBriefCases reads briefs.v1.jsonl strictly, reporting file:line on any
// violation.
func LoadBriefCases(t testingTB, path string) []BriefCase {
	t.Helper()
	cases, err := LoadBriefCasesStrict(path)
	if err != nil {
		t.Fatal(err)
	}
	return cases
}

// LoadBriefCasesStrict 是可被 cmd/eval 复用的错误返回版加载器,校验规则与
// LoadBriefCases 完全一致。
func LoadBriefCasesStrict(path string) ([]BriefCase, error) {
	lines, err := readLinesErr(path)
	if err != nil {
		return nil, err
	}
	cases := make([]BriefCase, 0, len(lines))
	seen := map[string]bool{}
	for index, line := range lines {
		var c BriefCase
		decode := json.NewDecoder(strings.NewReader(line))
		decode.DisallowUnknownFields()
		if err := decode.Decode(&c); err != nil {
			return nil, fmt.Errorf("%s:%d: decode brief case: %v", path, index+1, err)
		}
		if c.ID == "" || seen[c.ID] {
			return nil, fmt.Errorf("%s:%d: missing or duplicate case id %q", path, index+1, c.ID)
		}
		seen[c.ID] = true
		if err := checkSplit(c.Split); err != nil {
			return nil, fmt.Errorf("%s:%d: %v", path, index+1, err)
		}
		if !knownScenes[c.Scene] {
			return nil, fmt.Errorf("%s:%d: unknown scene %q", path, index+1, c.Scene)
		}
		if c.Valid == (c.WantErrorCode != "") {
			return nil, fmt.Errorf("%s:%d: valid flag and want_error_code disagree", path, index+1)
		}
		cases = append(cases, c)
	}
	return cases, nil
}

// LoadPlanSetCases reads plan_sets.v1.jsonl strictly.
func LoadPlanSetCases(t testingTB, path string) []PlanSetCase {
	t.Helper()
	cases, err := LoadPlanSetCasesStrict(path)
	if err != nil {
		t.Fatal(err)
	}
	return cases
}

// LoadPlanSetCasesStrict 是可被 cmd/eval 复用的错误返回版加载器。
func LoadPlanSetCasesStrict(path string) ([]PlanSetCase, error) {
	lines, err := readLinesErr(path)
	if err != nil {
		return nil, err
	}
	cases := make([]PlanSetCase, 0, len(lines))
	seen := map[string]bool{}
	for index, line := range lines {
		var c PlanSetCase
		decode := json.NewDecoder(strings.NewReader(line))
		decode.DisallowUnknownFields()
		if err := decode.Decode(&c); err != nil {
			return nil, fmt.Errorf("%s:%d: decode plan set case: %v", path, index+1, err)
		}
		if c.ID == "" || seen[c.ID] {
			return nil, fmt.Errorf("%s:%d: missing or duplicate case id %q", path, index+1, c.ID)
		}
		seen[c.ID] = true
		if err := checkSplit(c.Split); err != nil {
			return nil, fmt.Errorf("%s:%d: %v", path, index+1, err)
		}
		if !knownScenes[c.Scene] {
			return nil, fmt.Errorf("%s:%d: unknown scene %q", path, index+1, c.Scene)
		}
		if c.BriefCaseID == "" {
			return nil, fmt.Errorf("%s:%d: missing brief_case_id", path, index+1)
		}
		for _, code := range c.WantReasonCodes {
			if !knownReasonCodes[code] {
				return nil, fmt.Errorf("%s:%d: unknown reason code %q", path, index+1, code)
			}
		}
		cases = append(cases, c)
	}
	return cases, nil
}

func checkSplit(split Split) error {
	switch split {
	case Development, Validation, Release:
		return nil
	default:
		return fmt.Errorf("unknown split %q", split)
	}
}

func readLinesErr(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			return nil, fmt.Errorf("%s:%d: blank lines are not allowed", path, len(lines)+1)
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

// BriefInput normalizes the recorded answers into the canonical brief.
func BriefInput(c BriefCase) (domain.SceneBrief, error) {
	return planning.NormalizeBrief(domain.Scene(c.Scene), c.Answers)
}

// ReportFixtureSnapshot builds the deterministic report the fixture id
// stands for: three findings whose ids derive from the fixture id and the
// first finding is the priority finding.
func ReportFixtureSnapshot(fixtureID string) planning.ReportSnapshot {
	return planning.ReportSnapshot{
		ID:                fixtureID,
		UserID:            "fixture-user",
		PhotoSetID:        fixtureID + "-photoset",
		FaceAssetID:       fixtureID + "-face",
		BodyAssetID:       fixtureID + "-body",
		ProfileSnapshot:   []byte(`{"role":"designer"}`),
		ImpressionTags:    []string{"利落"},
		PriorityTitle:     "先整理额前碎发",
		PriorityCopy:      "额前碎发落到眉毛上方，先固定发根。",
		PriorityFindingID: fixtureID + "-f1",
		Findings: []planning.FindingSnapshot{
			{ID: fixtureID + "-f1", Category: "hair", Priority: 1, Label: "额前碎发", VisibleObservation: "额前碎发落到眉毛上方", Recommendation: "向后梳理并固定"},
			{ID: fixtureID + "-f2", Category: "outfit", Priority: 2, Label: "肩线偏塌", VisibleObservation: "上衣肩线低于自然肩点", Recommendation: "换成合肩线的上装"},
			{ID: fixtureID + "-f3", Category: "color", Priority: 3, Label: "整体色偏灰", VisibleObservation: "上装与背景同为灰色系", Recommendation: "用亮色内搭拉开层次"},
		},
	}
}
