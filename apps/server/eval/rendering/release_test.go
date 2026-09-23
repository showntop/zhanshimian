package rendering

import (
	"testing"
	"time"
)

func passingReport() EvalReport {
	return EvalReport{
		TotalCandidates: 300, PublishedCount: 260,
		HumanIdentityPasses: 95, HumanIdentityTotal: 100,
		SemanticCompliant: 90, SemanticTotal: 100,
		FirstCandidateP95: 60 * time.Second, SecondCandidateP95: 120 * time.Second,
	}
}

func TestReleaseDecisionEligibleBaseline(t *testing.T) {
	eligible, reason := DecideRelease(passingReport(), ProductionThresholds)
	if !eligible {
		t.Fatalf("baseline report must be eligible, stop=%s", reason)
	}
}

func TestReleaseDecisionStopConditions(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(EvalReport) EvalReport
		wantStop StopReason
	}{
		{"source mismatch", func(r EvalReport) EvalReport { r.SourceMismatchCount = 1; return r }, StopSourceMismatch},
		{"webp publication", func(r EvalReport) EvalReport { r.WebPPublicationCount = 1; return r }, StopWebPPublication},
		{"old generation overwrite", func(r EvalReport) EvalReport { r.OldGenerationOverwriteCount = 1; return r }, StopOldGenerationWrite},
		{"severe false accept at exclusive cap", func(r EvalReport) EvalReport {
			r.SevereFailures, r.SevereAccepted = 100, 1
			return r
		}, StopSevereFalseAccept},
		{"first candidate p95 over", func(r EvalReport) EvalReport {
			r.FirstCandidateP95 = 240*time.Second + time.Nanosecond
			return r
		}, StopFirstCandidateP95},
		{"identity pass rate under", func(r EvalReport) EvalReport {
			r.HumanIdentityPasses, r.HumanIdentityTotal = 89, 100
			return r
		}, StopIdentityPassRate},
		{"semantic compliance under", func(r EvalReport) EvalReport {
			r.SemanticCompliant, r.SemanticTotal = 84, 100
			return r
		}, StopSemanticCompliance},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eligible, reason := DecideRelease(tc.mutate(passingReport()), ProductionThresholds)
			if eligible || reason != tc.wantStop {
				t.Fatalf("eligible=%v stop=%s, want stop=%s", eligible, reason, tc.wantStop)
			}
		})
	}
}
