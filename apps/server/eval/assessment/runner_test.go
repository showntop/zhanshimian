package assessment

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	core "github.com/zhanshimian/server/internal/service/assessment"
)

func loadManifest(t *testing.T) Manifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func loadCase(t *testing.T, name string) Case {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "cases", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var c Case
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestValidateManifestPreventsIdentityLeakageAcrossSplits(t *testing.T) {
	m := loadManifest(t)
	if err := ValidateManifest(m, false); err != nil {
		t.Fatalf("checked-in manifest must validate: %v", err)
	}
	m.Cases[1].IdentityID = m.Cases[0].IdentityID
	m.Cases[1].Split = SplitValidation
	err := ValidateManifest(m, false)
	if err == nil || !strings.Contains(err.Error(), "identity appears in multiple splits") {
		t.Fatalf("got %v, want identity split leakage error", err)
	}
}

func TestReleaseThresholds(t *testing.T) {
	summary := Summary{
		AuthorizedIdentities: 60, Photos: 180, Reports: 120,
		EvidenceLinked: 360, EvidenceTotal: 360,
		EvidenceSupported: 343, SensitiveViolations: 0,
		RoleChecks: 180, RoleChecksCorrect: 180,
	}
	if err := CheckRelease(summary); err != nil { // 343/360 = 95.27%
		t.Fatalf("expected release summary to pass, got %v", err)
	}
	summary.EvidenceSupported = 341
	err := CheckRelease(summary)
	if err == nil || !strings.Contains(err.Error(), "evidence support") {
		t.Fatalf("got %v, want evidence support failure", err)
	}
}

func TestFixtureRejectsSensitiveCopy(t *testing.T) {
	c := loadCase(t, "reject-sensitive")
	outcome := EvaluateCase(c)
	if !errors.Is(outcome.Err, core.ErrCopyPolicy) {
		t.Fatalf("got %v, want %v", outcome.Err, core.ErrCopyPolicy)
	}
	if outcome.Disposition != OutcomeReject {
		t.Fatalf("disposition = %q, want reject", outcome.Disposition)
	}
}

func TestCheckedInFixtureRunsClean(t *testing.T) {
	manifest := loadManifest(t)
	for _, c := range manifest.Cases {
		standalone := loadCase(t, c.ID)
		want, _ := json.Marshal(c)
		got, _ := json.Marshal(standalone)
		if string(want) != string(got) {
			t.Fatalf("case %q drifted between manifest.json and cases/%s.json", c.ID, c.ID)
		}
	}
	summary, err := Run(context.Background(), manifest, false)
	if err != nil {
		t.Fatalf("fixture run must pass: %v", err)
	}
	line, err := summary.JSON(true)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		RoleAccuracy        float64 `json:"role_accuracy"`
		EvidenceLinkRate    float64 `json:"evidence_link_rate"`
		EvidenceSupportRate float64 `json:"evidence_support_rate"`
		Passed              bool    `json:"passed"`
	}
	if err := json.Unmarshal(line, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.RoleAccuracy != 1 || decoded.EvidenceLinkRate != 1 || !decoded.Passed {
		t.Fatalf("unexpected fixture summary: %s", line)
	}
	if decoded.EvidenceSupportRate < 0.95 {
		t.Fatalf("fixture support rate %.4f below release floor", decoded.EvidenceSupportRate)
	}
}

func TestCheckedInFixtureFailsReleaseScale(t *testing.T) {
	manifest := loadManifest(t)
	_, err := Run(context.Background(), manifest, true)
	if err == nil || !strings.Contains(err.Error(), "release scale") {
		t.Fatalf("got %v, want release scale failure for the synthetic fixture", err)
	}
}
