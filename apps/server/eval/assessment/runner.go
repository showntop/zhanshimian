package assessment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/provider/ai"
	core "github.com/zhanshimian/server/internal/service/assessment"
)

const (
	// photosPerCase is the fixed face/side/body triple of one case.
	photosPerCase = 3

	// Release floors for the mounted authorized golden set.
	minReleaseIdentities = 60
	minReleasePhotos     = 180
	minReleaseReports    = 120

	// minEvidenceSupportRate is the release precision floor for supported
	// findings; the policy threshold itself stays calibrated in
	// service/assessment and is read from there.
	minEvidenceSupportRate = 0.95
)

// Summary aggregates one manifest run.
type Summary struct {
	AuthorizedIdentities                             int
	Photos, Reports                                  int
	RoleChecks, RoleChecksCorrect                    int
	EvidenceTotal, EvidenceLinked, EvidenceSupported int
	SensitiveViolations                              int
}

// CaseOutcome is the gate verdict for one case.
type CaseOutcome struct {
	Err                 error
	Disposition         string
	RoleChecks          int
	RoleChecksCorrect   int
	EvidenceTotal       int
	EvidenceLinked      int
	EvidenceSupported   int
	SensitiveViolations int
}

// EvaluateCase runs one case through the production draft validation and
// evidence policy. A hard Err means the case itself contradicts the gate
// (for example a sensitive draft that should have been rejected).
func EvaluateCase(c Case) CaseOutcome {
	outcome := CaseOutcome{Disposition: OutcomeReject}
	outcome.RoleChecks = photosPerCase
	if c.Expected.PhotoRolesValid {
		outcome.RoleChecksCorrect = photosPerCase
	}
	draft := draftFromFixture(c.Provider.Draft)
	if err := core.ValidateReportDraft(draft); err != nil {
		outcome.Err = err
		return outcome
	}
	observations := draftObservations(draft)
	for _, observation := range observations {
		for _, forbidden := range c.Expected.ForbiddenObservations {
			if strings.Contains(observation, forbidden) {
				outcome.SensitiveViolations++
			}
		}
	}
	for _, allowed := range c.Expected.AllowedObservations {
		if !containsAny(observations, allowed) {
			outcome.Err = fmt.Errorf("case %q: expected observation %q missing from draft", c.ID, allowed)
			return outcome
		}
	}
	evidence := evidenceFromFixture(c.Provider.Evidence)
	policy := core.Policy{}
	supported := policy.SupportedFindings(draft.Findings, evidence, core.QualityPolicyVersion)
	outcome.EvidenceTotal = len(draft.Findings)
	outcome.EvidenceLinked = linkedFindings(draft.Findings)
	outcome.EvidenceSupported = len(supported)
	switch {
	case len(supported) >= c.Expected.MinimumSupportedFindings && c.Expected.MinimumSupportedFindings > 0:
		outcome.Disposition = OutcomePublish
	default:
		outcome.Disposition = OutcomeRetry
	}
	return outcome
}

// Run evaluates the whole manifest. In release mode every case must be
// authorized and the summary must reach the release floors.
func Run(_ context.Context, manifest Manifest, release bool) (Summary, error) {
	if err := ValidateManifest(manifest, release); err != nil {
		return Summary{}, err
	}
	summary := Summary{}
	identities := make(map[string]bool, len(manifest.Cases))
	for _, c := range manifest.Cases {
		if c.Authorized {
			identities[c.IdentityID] = true
		}
		summary.Photos += photosPerCase
		outcome := EvaluateCase(c)
		if c.Expected.Outcome == OutcomeReject {
			// A rejected sample must be blocked by the gate; reaching any
			// later disposition means the sample would leak to users.
			if outcome.Disposition != OutcomeReject {
				return summary, fmt.Errorf("case %q: gate outcome %q, want reject — sensitive sample leaked", c.ID, outcome.Disposition)
			}
		} else {
			if outcome.Err != nil {
				return summary, outcome.Err
			}
			if outcome.Disposition != c.Expected.Outcome {
				return summary, fmt.Errorf("case %q: gate outcome %q, want %q", c.ID, outcome.Disposition, c.Expected.Outcome)
			}
		}
		summary.RoleChecks += outcome.RoleChecks
		summary.RoleChecksCorrect += outcome.RoleChecksCorrect
		if outcome.Disposition == OutcomePublish {
			summary.Reports++
			summary.EvidenceTotal += outcome.EvidenceTotal
			summary.EvidenceLinked += outcome.EvidenceLinked
			summary.EvidenceSupported += outcome.EvidenceSupported
			summary.SensitiveViolations += outcome.SensitiveViolations
		}
	}
	summary.AuthorizedIdentities = len(identities)
	if err := CheckRelease(summary); err != nil {
		return summary, err
	}
	if release {
		if err := checkReleaseScale(summary); err != nil {
			return summary, err
		}
	}
	return summary, nil
}

// CheckRelease enforces the hard quality gates: 100% role accuracy, 100%
// evidence linkage, at least 95% evidence support on published findings and
// zero sensitive violations.
func CheckRelease(s Summary) error {
	if s.RoleChecks == 0 {
		return errors.New("no role checks were evaluated")
	}
	if s.RoleChecksCorrect != s.RoleChecks {
		return fmt.Errorf("role accuracy %.4f is below 1", rate(s.RoleChecksCorrect, s.RoleChecks))
	}
	if s.EvidenceTotal == 0 {
		return errors.New("no evidence checks were evaluated")
	}
	if s.EvidenceLinked != s.EvidenceTotal {
		return fmt.Errorf("evidence link rate %.4f is below 1", rate(s.EvidenceLinked, s.EvidenceTotal))
	}
	if support := rate(s.EvidenceSupported, s.EvidenceTotal); support < minEvidenceSupportRate {
		return fmt.Errorf("evidence support rate %.4f is below %.2f", support, minEvidenceSupportRate)
	}
	if s.SensitiveViolations > 0 {
		return fmt.Errorf("sensitive content violations: %d", s.SensitiveViolations)
	}
	return nil
}

func checkReleaseScale(s Summary) error {
	switch {
	case s.AuthorizedIdentities < minReleaseIdentities:
		return fmt.Errorf("release scale: %d authorized identities, want >= %d", s.AuthorizedIdentities, minReleaseIdentities)
	case s.Photos < minReleasePhotos:
		return fmt.Errorf("release scale: %d photos, want >= %d", s.Photos, minReleasePhotos)
	case s.Reports < minReleaseReports:
		return fmt.Errorf("release scale: %d reports, want >= %d", s.Reports, minReleaseReports)
	}
	return nil
}

// JSON renders the fixed CLI summary line consumed by cmd/eval.
func (s Summary) JSON(passed bool) ([]byte, error) {
	return json.Marshal(struct {
		RoleAccuracy            float64 `json:"role_accuracy"`
		EvidenceLinkRate        float64 `json:"evidence_link_rate"`
		EvidenceSupportRate     float64 `json:"evidence_support_rate"`
		SensitiveViolationCount int     `json:"sensitive_violation_count"`
		Passed                  bool    `json:"passed"`
	}{
		RoleAccuracy:            rate(s.RoleChecksCorrect, s.RoleChecks),
		EvidenceLinkRate:        rate(s.EvidenceLinked, s.EvidenceTotal),
		EvidenceSupportRate:     rate(s.EvidenceSupported, s.EvidenceTotal),
		SensitiveViolationCount: s.SensitiveViolations,
		Passed:                  passed,
	})
}

func rate(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(n) / float64(total)
}

func draftFromFixture(d Draft) domain.ReportDraft {
	findings := make([]domain.DraftFinding, 0, len(d.Findings))
	for _, f := range d.Findings {
		findings = append(findings, domain.DraftFinding{
			Key:                f.Key,
			Category:           f.Category,
			Label:              f.Label,
			VisibleObservation: f.VisibleObservation,
			Recommendation:     f.Recommendation,
			Priority:           f.Priority,
			Position:           f.Position,
			SourceRole:         domain.PhotoRole(f.SourceRole),
			Anchor:             domain.EvidenceAnchor{X: f.Anchor.X, Y: f.Anchor.Y, W: f.Anchor.W, H: f.Anchor.H},
			Confidence:         f.Confidence,
		})
	}
	return domain.ReportDraft{
		ImpressionTags: d.ImpressionTags,
		PriorityTitle:  d.PriorityTitle,
		PriorityCopy:   d.PriorityCopy,
		Findings:       findings,
	}
}

func evidenceFromFixture(e Evidence) ai.EvidenceResult {
	decisions := make([]ai.EvidenceDecision, 0, len(e.Findings))
	for _, d := range e.Findings {
		decisions = append(decisions, ai.EvidenceDecision{
			Key: d.Key, Supported: d.Supported, Confidence: d.Confidence, ReasonCode: d.ReasonCode,
		})
	}
	return ai.EvidenceResult{Findings: decisions}
}

func draftObservations(draft domain.ReportDraft) []string {
	out := make([]string, 0, len(draft.Findings))
	for _, finding := range draft.Findings {
		out = append(out, finding.VisibleObservation)
	}
	return out
}

func containsAny(values []string, needle string) bool {
	for _, value := range values {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

// linkedFindings counts findings whose evidence anchor and source photo role
// are fully populated — the invariant the schema and DTO enforce in
// production.
func linkedFindings(findings []domain.DraftFinding) int {
	linked := 0
	for _, finding := range findings {
		if finding.SourceRole == "" {
			continue
		}
		a := finding.Anchor
		if a.X < 0 || a.Y < 0 || a.W <= 0 || a.H <= 0 || a.X+a.W > 1 || a.Y+a.H > 1 {
			continue
		}
		linked++
	}
	return linked
}
