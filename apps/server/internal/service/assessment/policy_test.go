package assessment

import (
	"errors"
	"testing"

	"github.com/zhanshimian/server/internal/provider/ai"
)

func TestPolicyThresholdsFailClosedForUnknownVersion(t *testing.T) {
	var policy Policy
	if _, err := policy.EvidenceThreshold("quality.unknown"); err == nil {
		t.Fatal("EvidenceThreshold(unknown) = nil, want error")
	} else {
		assertUnsupportedPolicy(t, err)
	}
	if _, err := policy.IdentityThreshold("quality.unknown"); err == nil {
		t.Fatal("IdentityThreshold(unknown) = nil, want error")
	} else {
		assertUnsupportedPolicy(t, err)
	}

	got, err := policy.EvidenceThreshold(QualityPolicyVersion)
	if err != nil || got != 0.95 {
		t.Fatalf("EvidenceThreshold(quality.v1) = %v, %v, want 0.95, nil", got, err)
	}
	got, err = policy.IdentityThreshold(QualityPolicyVersion)
	if err != nil || got != 0.92 {
		t.Fatalf("IdentityThreshold(quality.v1) = %v, %v, want 0.92, nil", got, err)
	}
}

func TestAcceptIdentityRejectsUnknownPolicyVersion(t *testing.T) {
	err := Policy{}.AcceptIdentity(ai.IdentityResult{Decision: "pass", Confidence: 0.99}, "quality.unknown")
	assertUnsupportedPolicy(t, err)
}

func TestSupportedFindingsUnknownPolicyPublishesNothing(t *testing.T) {
	supported := Policy{}.SupportedFindings(fixtureDraft().Findings, threeSupported(), "quality.unknown")
	if len(supported) != 0 {
		t.Fatalf("SupportedFindings(unknown) = %d, want 0", len(supported))
	}
}

func assertUnsupportedPolicy(t *testing.T, err error) {
	t.Helper()
	var public *PublicFailure
	if !errors.As(err, &public) || public == nil || public.Code != codeQualityPolicyUnsupported {
		t.Fatalf("error = %v, want PublicFailure %s", err, codeQualityPolicyUnsupported)
	}
}

func TestSupportedFindingsKnownPolicyKeepsSupported(t *testing.T) {
	supported := Policy{}.SupportedFindings(fixtureDraft().Findings, threeSupported(), QualityPolicyVersion)
	if len(supported) != 3 {
		t.Fatalf("SupportedFindings(quality.v1) = %d, want 3", len(supported))
	}
}
