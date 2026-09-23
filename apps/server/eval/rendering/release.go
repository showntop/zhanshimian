// Package rendering evaluates the offline rendering gold set and enforces
// release gates. PR CI runs manifest/metrics/decision logic only; live
// locked-set evaluation runs from the unified eval command at Cutover.
package rendering

import (
	"encoding/json"
	"fmt"
	"time"
)

// ReleaseThresholds 是发布门禁阈值(排他上限用 >= 判停)。
type ReleaseThresholds struct {
	SourceMismatchMax                int
	WebPPublicationMax               int
	OldGenerationOverwriteMax        int
	SevereFailureFalseAcceptRateMax  float64
	HumanIdentityPassRateMin         float64
	SemanticComplianceRateMin        float64
	FirstCandidateP95Max             time.Duration
	SecondCandidateP95Max            time.Duration
}

// ProductionThresholds 是 v1 生产阈值。
var ProductionThresholds = ReleaseThresholds{
	SourceMismatchMax:               0,
	WebPPublicationMax:              0,
	OldGenerationOverwriteMax:       0,
	SevereFailureFalseAcceptRateMax: 0.01,
	HumanIdentityPassRateMin:        0.90,
	SemanticComplianceRateMin:       0.85,
	FirstCandidateP95Max:            120 * time.Second,
	SecondCandidateP95Max:           240 * time.Second,
}

// StopReason 是唯一停发原因码。
type StopReason string

const (
	StopSourceMismatch       StopReason = "source_mismatch"
	StopWebPPublication      StopReason = "webp_publication"
	StopOldGenerationWrite   StopReason = "old_generation_overwrite"
	StopSevereFalseAccept    StopReason = "severe_failure_false_accept_rate"
	StopIdentityPassRate     StopReason = "human_identity_pass_rate"
	StopSemanticCompliance   StopReason = "semantic_compliance_rate"
	StopFirstCandidateP95    StopReason = "first_candidate_p95"
	StopSecondCandidateP95   StopReason = "second_candidate_p95"
)

// EvalReport 是评测产出的报告(不携带任何图片/身份数据)。
type EvalReport struct {
	TotalCandidates                int             `json:"total_candidates"`
	PublishedCount                 int             `json:"published_count"`
	SourceMismatchCount            int             `json:"source_mismatch_count"`
	WebPPublicationCount           int             `json:"webp_publication_count"`
	OldGenerationOverwriteCount    int             `json:"old_generation_overwrite_count"`
	SevereFailures                 int             `json:"severe_failures"`
	SevereAccepted                 int             `json:"severe_accepted"`
	HumanIdentityPasses            int             `json:"human_identity_passes"`
	HumanIdentityTotal             int             `json:"human_identity_total"`
	SemanticCompliant              int             `json:"semantic_compliant"`
	SemanticTotal                  int             `json:"semantic_total"`
	FirstCandidateP95              time.Duration   `json:"first_candidate_p95_seconds"`
	SecondCandidateP95             time.Duration   `json:"second_candidate_p95_seconds"`
	PerCase                        []CaseDecision  `json:"per_case"`
}

type CaseDecision struct {
	CaseID   string   `json:"case_id"`
	Decision string   `json:"decision"`
	Reasons  []string `json:"reason_codes"`
	Latency  time.Duration `json:"latency_seconds"`
}

// DecideRelease 判定报告是否满足发布阈值;不满足时返回唯一 stop reason。
func DecideRelease(report EvalReport, thresholds ReleaseThresholds) (bool, StopReason) {
	if report.SourceMismatchCount > thresholds.SourceMismatchMax {
		return false, StopSourceMismatch
	}
	if report.WebPPublicationCount > thresholds.WebPPublicationMax {
		return false, StopWebPPublication
	}
	if report.OldGenerationOverwriteCount > thresholds.OldGenerationOverwriteMax {
		return false, StopOldGenerationWrite
	}
	if report.SevereFailures > 0 {
		if falseAcceptRate(report) >= thresholds.SevereFailureFalseAcceptRateMax {
			return false, StopSevereFalseAccept
		}
	}
	if passRate(report.HumanIdentityPasses, report.HumanIdentityTotal) < thresholds.HumanIdentityPassRateMin {
		return false, StopIdentityPassRate
	}
	if passRate(report.SemanticCompliant, report.SemanticTotal) < thresholds.SemanticComplianceRateMin {
		return false, StopSemanticCompliance
	}
	if report.FirstCandidateP95 > thresholds.FirstCandidateP95Max {
		return false, StopFirstCandidateP95
	}
	if report.SecondCandidateP95 > thresholds.SecondCandidateP95Max {
		return false, StopSecondCandidateP95
	}
	return true, ""
}

func falseAcceptRate(report EvalReport) float64 {
	if report.SevereFailures == 0 {
		return 0
	}
	return float64(report.SevereAccepted) / float64(report.SevereFailures)
}

func passRate(passes, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(passes) / float64(total)
}

// LoadReport 读取评测报告 JSON。
func LoadReport(data []byte) (EvalReport, error) {
	var report EvalReport
	if err := json.Unmarshal(data, &report); err != nil {
		return EvalReport{}, fmt.Errorf("decode eval report: %w", err)
	}
	return report, nil
}
