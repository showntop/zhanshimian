// Package assessment evaluates the offline golden set for the report quality
// gate. Cases carry de-identified provider samples; the runner reuses the
// exact draft validation and evidence policy the online handler enforces, so
// a case result always predicts production behaviour.
package assessment

import (
	"fmt"
)

// ManifestSchemaVersion pins both the checked-in synthetic fixture and the
// authorized golden manifests mounted from the private object store.
const ManifestSchemaVersion = "assessment-golden.v1"

const (
	SplitDevelopment = "development"
	SplitValidation  = "validation"
	SplitRelease     = "release"
)

// Dispositions mirror the handler outcomes a case can expect.
const (
	OutcomePublish = "publish"
	OutcomeRetry   = "retry"
	OutcomeReject  = "reject"
)

// Manifest is one golden set: ordered cases with stable ids.
type Manifest struct {
	SchemaVersion string `json:"schema_version"`
	Cases         []Case `json:"cases"`
}

// CaseInput references programmatic synthetic photos. Release manifests mount
// authorized photos from the private object store under the same keys.
type CaseInput struct {
	Face string `json:"face"`
	Side string `json:"side"`
	Body string `json:"body"`
}

// CaseExpectation states what the gate must do with the sample.
type CaseExpectation struct {
	PhotoRolesValid          bool     `json:"photo_roles_valid"`
	IdentityConsistent       bool     `json:"identity_consistent"`
	AllowedObservations      []string `json:"allowed_observations"`
	ForbiddenObservations    []string `json:"forbidden_observations"`
	MinimumSupportedFindings int      `json:"minimum_supported_findings"`
	Outcome                  string   `json:"outcome"`
}

// Anchor is the normalized evidence rectangle of one finding.
type Anchor struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// DraftFinding is one de-identified analyzer finding sample.
type DraftFinding struct {
	Key                string  `json:"key"`
	Category           string  `json:"category"`
	Label              string  `json:"label"`
	VisibleObservation string  `json:"visible_observation"`
	Recommendation     string  `json:"recommendation"`
	Priority           int     `json:"priority"`
	Position           int     `json:"position"`
	SourceRole         string  `json:"source_role"`
	Anchor             Anchor  `json:"anchor"`
	Confidence         float64 `json:"confidence"`
}

// Draft mirrors the analyzer output consumed by ValidateReportDraft.
type Draft struct {
	ImpressionTags []string       `json:"impression_tags"`
	PriorityTitle  string         `json:"priority_title"`
	PriorityCopy   string         `json:"priority_copy"`
	Findings       []DraftFinding `json:"findings"`
}

// EvidenceDecision is the verifier verdict for one finding key.
type EvidenceDecision struct {
	Key        string  `json:"key"`
	Supported  bool    `json:"supported"`
	Confidence float64 `json:"confidence"`
	ReasonCode string  `json:"reason_code"`
}

// Evidence mirrors the verifier output consumed by Policy.SupportedFindings.
type Evidence struct {
	Findings []EvidenceDecision `json:"findings"`
}

// ProviderSample is the de-identified provider JSON pair of one case.
type ProviderSample struct {
	Draft    Draft    `json:"draft"`
	Evidence Evidence `json:"evidence"`
}

// Case is one golden evaluation unit.
type Case struct {
	ID         string          `json:"id"`
	IdentityID string          `json:"identity_id"`
	Authorized bool            `json:"authorized"`
	Split      string          `json:"split"`
	Input      CaseInput       `json:"input"`
	Expected   CaseExpectation `json:"expected"`
	Provider   ProviderSample  `json:"provider"`
}

// ValidateManifest enforces schema, split discipline and — in release mode —
// explicit authorization for every identity.
func ValidateManifest(m Manifest, release bool) error {
	if m.SchemaVersion != ManifestSchemaVersion {
		return fmt.Errorf("unexpected manifest schema %q, want %q", m.SchemaVersion, ManifestSchemaVersion)
	}
	seenCase := make(map[string]bool, len(m.Cases))
	identitySplit := make(map[string]string, len(m.Cases))
	for _, c := range m.Cases {
		if c.ID == "" {
			return fmt.Errorf("case %d is missing an id", len(seenCase))
		}
		if seenCase[c.ID] {
			return fmt.Errorf("duplicate case id %q", c.ID)
		}
		seenCase[c.ID] = true
		switch c.Split {
		case SplitDevelopment, SplitValidation, SplitRelease:
		default:
			return fmt.Errorf("case %q has invalid split %q", c.ID, c.Split)
		}
		if c.IdentityID == "" {
			return fmt.Errorf("case %q is missing an identity", c.ID)
		}
		if prev, ok := identitySplit[c.IdentityID]; ok && prev != c.Split {
			return fmt.Errorf("identity appears in multiple splits: %s (%s, %s)", c.IdentityID, prev, c.Split)
		}
		identitySplit[c.IdentityID] = c.Split
		if release && !c.Authorized {
			return fmt.Errorf("case %q is not authorized for release evaluation", c.ID)
		}
	}
	return nil
}
