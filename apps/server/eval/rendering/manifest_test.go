package rendering

import (
	"fmt"
	"strings"
	"testing"
)

func syntheticCase(id string, identity string, split Split) GoldCase {
	return GoldCase{
		CaseID: id, IdentityHash: identity, Split: split,
		ConsentRecordID: "consent-" + id,
		SHA256:          map[string]string{"body": strings.Repeat("a", 64)},
	}
}

func syntheticValidManifest() Manifest {
	// 60 身份 × 5 候选 = 300 候选;dev 36 身份 / validation 12 / locked 12。
	splits := []Split{SplitDevelopment, SplitValidation, SplitLocked}
	counts := map[Split]int{SplitDevelopment: 36, SplitValidation: 12, SplitLocked: 12}
	manifest := Manifest{}
	index := 0
	for _, split := range splits {
		for i := 0; i < counts[split]; i++ {
			identity := fmt.Sprintf("%064x", index)
			index++
			for c := 0; c < 5; c++ {
				manifest.Cases = append(manifest.Cases,
					syntheticCase(strings.ToLower(string(split))+"-"+itoa(i)+"-"+itoa(c), identity, split))
			}
		}
	}
	return manifest
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	out := ""
	for n > 0 {
		out = string(rune('0'+n%10)) + out
		n /= 10
	}
	return out
}

func TestManifestRequiresMinimumAuthorizedCoverage(t *testing.T) {
	manifest := syntheticValidManifest()
	if err := ValidateManifest(manifest); err != nil {
		t.Fatal(err)
	}
	if got := manifest.IdentityCount(); got != 60 {
		t.Fatalf("identities = %d", got)
	}
	if got := manifest.CandidateCount(); got < 300 {
		t.Fatalf("candidates = %d", got)
	}
}

func TestManifestRejectsIdentitySplitLeak(t *testing.T) {
	manifest := syntheticValidManifest()
	manifest.Cases[0].IdentityHash = manifest.Cases[len(manifest.Cases)-1].IdentityHash
	err := ValidateManifest(manifest)
	if err == nil || !strings.Contains(err.Error(), "identity_split_leak") {
		t.Fatalf("got %v", err)
	}
}

func TestManifestRejectsInvalidRatioAndPII(t *testing.T) {
	manifest := syntheticValidManifest()
	manifest.Cases = manifest.Cases[:299]
	if err := ValidateManifest(manifest); err == nil || !strings.Contains(err.Error(), "candidates") {
		t.Fatalf("expected candidate shortfall, got %v", err)
	}
	manifest = syntheticValidManifest()
	manifest.Cases[0].CaseID = "case-with-name-inside"
	if err := ValidateManifest(manifest); err == nil || !strings.Contains(err.Error(), "pii_field_forbidden") {
		t.Fatalf("expected pii rejection, got %v", err)
	}
	manifest = syntheticValidManifest()
	manifest.Cases[0].ConsentRecordID = ""
	if err := ValidateManifest(manifest); err == nil || !strings.Contains(err.Error(), "missing_consent") {
		t.Fatalf("expected missing consent, got %v", err)
	}
	manifest = syntheticValidManifest()
	manifest.Cases[0].SHA256["body"] = "nothex"
	if err := ValidateManifest(manifest); err == nil || !strings.Contains(err.Error(), "invalid_sha256") {
		t.Fatalf("expected invalid sha, got %v", err)
	}
}
