package assessment

import (
	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/provider/ai"
)

const (
	StagePhotoTechnicalCheck = "photo.technical_check"
	StagePhotoContentCheck   = "photo.content_check"
	StagePhotoIdentityCheck  = "photo.identity_check"
	StageReportGenerating    = "report.generating"
	StageEvidenceVerifying   = "report.evidence_check"
	StageReportPublishing    = "report.publishing"

	codePhotoIdentityUncertain     = "photo_identity_uncertain"
	codeReportEvidenceInsufficient = "report_evidence_insufficient"
	codePhotoContentRejected       = "photo_content_rejected"
	codeQualityPolicyUnsupported   = "quality_policy_unsupported"
)

type Policy struct{}

type PublicFailure struct {
	Code      string
	Message   string
	Retryable bool
}

func (e *PublicFailure) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

func NewPublicFailure(code, message string, retryable bool) *PublicFailure {
	return &PublicFailure{Code: code, Message: message, Retryable: retryable}
}

var publicFailures = map[string]PublicFailure{
	codePhotoIdentityUncertain: {
		Code: codePhotoIdentityUncertain, Message: "照片差异较大，请重新拍摄确认", Retryable: false,
	},
	codeReportEvidenceInsufficient: {
		Code: codeReportEvidenceInsufficient, Message: "这次未能形成可靠报告，请重新拍摄后再试", Retryable: true,
	},
	codePhotoContentRejected: {
		Code: codePhotoContentRejected, Message: "照片不符合拍摄要求，请按提示重新拍摄", Retryable: false,
	},
	codeQualityPolicyUnsupported: {
		Code: codeQualityPolicyUnsupported, Message: "这次未能形成可靠报告，请重新拍摄后再试", Retryable: false,
	},
}

var evidenceConfidenceByPolicy = map[string]float64{
	QualityPolicyVersion: 0.95,
}

var identityConfidenceByPolicy = map[string]float64{
	QualityPolicyVersion: 0.92,
}

func LookupPublicFailure(code string) (PublicFailure, bool) {
	if failure, ok := publicFailures[code]; ok {
		return failure, true
	}
	if message, ok := photoPublicMessage[code]; ok {
		return PublicFailure{Code: code, Message: message, Retryable: false}, true
	}
	return PublicFailure{}, false
}

func (Policy) EvidenceThreshold(policyVersion string) (float64, error) {
	threshold, ok := evidenceConfidenceByPolicy[policyVersion]
	if !ok {
		return 0, catalogFailure(codeQualityPolicyUnsupported)
	}
	return threshold, nil
}

func (Policy) IdentityThreshold(policyVersion string) (float64, error) {
	threshold, ok := identityConfidenceByPolicy[policyVersion]
	if !ok {
		return 0, catalogFailure(codeQualityPolicyUnsupported)
	}
	return threshold, nil
}

func (Policy) AcceptContent(content ai.PhotoQualityResult) error {
	if len(content.Photos) == 0 {
		return catalogFailure(codePhotoContentRejected)
	}
	for _, photo := range content.Photos {
		if photo.Decision != "pass" {
			return catalogFailure(codePhotoContentRejected)
		}
	}
	return nil
}

func (p Policy) AcceptIdentity(identity ai.IdentityResult, policyVersion string) error {
	threshold, err := p.IdentityThreshold(policyVersion)
	if err != nil {
		return err
	}
	if identity.Decision != "pass" || identity.Confidence < threshold {
		return catalogFailure(codePhotoIdentityUncertain)
	}
	return nil
}

func (p Policy) SupportedFindings(findings []domain.DraftFinding, evidence ai.EvidenceResult, policyVersion string) []domain.DraftFinding {
	threshold, err := p.EvidenceThreshold(policyVersion)
	if err != nil {
		return nil
	}
	byKey := make(map[string]ai.EvidenceDecision, len(evidence.Findings))
	for _, decision := range evidence.Findings {
		byKey[decision.Key] = decision
	}
	supported := make([]domain.DraftFinding, 0, len(findings))
	for _, finding := range findings {
		decision, ok := byKey[finding.Key]
		if !ok || !decision.Supported || decision.Confidence < threshold {
			continue
		}
		supported = append(supported, finding)
	}
	return supported
}

func catalogFailure(code string) *PublicFailure {
	if failure, ok := publicFailures[code]; ok {
		copy := failure
		return &copy
	}
	return NewPublicFailure(code, code, false)
}
