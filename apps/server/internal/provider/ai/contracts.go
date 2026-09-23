package ai

import "context"

const (
	CapabilityPhotoQualityCheck          = "photo_quality_check"
	CapabilityPhotoIdentityConsistency   = "photo_identity_consistency"
	CapabilityAppearanceAnalysis         = "appearance_analysis"
	CapabilityReportEvidenceVerification = "report_evidence_verification"
)

type ImageInput struct {
	AssetID  string
	Role     string
	MIMEType string
	Width    int
	Height   int
	Data     []byte
}

type StructuredRequest struct {
	Capability      string
	Instructions    string
	Prompt          string
	Images          []ImageInput
	SchemaName      string
	Schema          map[string]any
	MaxOutputTokens int
	Validate        func([]byte) error
}

type StructuredResult struct {
	JSON []byte
	Meta InvocationMeta
}

type Runtime interface {
	Structured(context.Context, StructuredRequest) (StructuredResult, error)
}
