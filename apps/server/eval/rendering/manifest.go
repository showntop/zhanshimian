package rendering

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Split 是金集分区:development/validation/locked。
type Split string

const (
	SplitDevelopment Split = "development"
	SplitValidation  Split = "validation"
	SplitLocked      Split = "locked"
)

// GoldLabels 是金集标注。
type GoldLabels struct {
	IdentityPass      bool     `json:"identity_pass"`
	AnatomyPass       bool     `json:"anatomy_pass"`
	CompositionPass   bool     `json:"composition_pass"`
	SemanticPass      bool     `json:"semantic_pass"`
	SevereReasonCodes []string `json:"severe_reason_codes"`
}

// GoldCase 是 manifest 中的一行。
type GoldCase struct {
	CaseID             string            `json:"case_id"`
	IdentityHash       string            `json:"identity_hash"`
	Split              Split             `json:"split"`
	ConsentRecordID    string            `json:"consent_record_id"`
	BodyObjectKey      string            `json:"body_object_key"`
	FaceObjectKey      string            `json:"face_object_key"`
	CandidateObjectKey string            `json:"candidate_object_key"`
	SHA256             map[string]string `json:"sha256"`
	Expected           GoldLabels        `json:"expected"`
}

// Manifest 是整个金集清单。
type Manifest struct {
	Cases []GoldCase
}

// IdentityCount 返回去重后的身份数量。
func (m Manifest) IdentityCount() int {
	seen := map[string]bool{}
	for _, c := range m.Cases {
		seen[c.IdentityHash] = true
	}
	return len(seen)
}

// CandidateCount 返回候选总数。
func (m Manifest) CandidateCount() int { return len(m.Cases) }

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

var forbiddenPIIFields = []string{"name", "phone", "open_id", "url"}

// ValidateManifest 校验授权覆盖、分区比例、consent/hash 格式与 PII 红线。
func ValidateManifest(m Manifest) error {
	const (
		minIdentities = 60
		minCandidates = 300
	)
	if got := m.IdentityCount(); got < minIdentities {
		return fmt.Errorf("manifest has %d identities, want >= %d", got, minIdentities)
	}
	if got := m.CandidateCount(); got < minCandidates {
		return fmt.Errorf("manifest has %d candidates, want >= %d", got, minCandidates)
	}
	identitySplits := map[string]Split{}
	splitCounts := map[Split]int{}
	for _, c := range m.Cases {
		switch c.Split {
		case SplitDevelopment, SplitValidation, SplitLocked:
		default:
			return fmt.Errorf("case %s has invalid split %q", c.CaseID, c.Split)
		}
		if prev, ok := identitySplits[c.IdentityHash]; ok && prev != c.Split {
			return fmt.Errorf("%s: identity_split_leak (%s in %s and %s)", c.CaseID, c.IdentityHash, prev, c.Split)
		}
		identitySplits[c.IdentityHash] = c.Split
		splitCounts[c.Split]++
		if c.ConsentRecordID == "" {
			return fmt.Errorf("%s: missing_consent", c.CaseID)
		}
		if !sha256Pattern.MatchString(c.IdentityHash) {
			return fmt.Errorf("%s: invalid_sha256 identity hash", c.CaseID)
		}
		for key, hash := range c.SHA256 {
			if !sha256Pattern.MatchString(hash) {
				return fmt.Errorf("%s: invalid_sha256 for %s", c.CaseID, key)
			}
		}
		for _, field := range forbiddenPIIFields {
			if strings.Contains(strings.ToLower(c.CaseID), field) {
				return fmt.Errorf("%s: pii_field_forbidden %s", c.CaseID, field)
			}
		}
	}
	// 60/20/20 → development/validation/locked = 3/1/1。
	dev := splitCounts[SplitDevelopment]
	if dev == 0 || splitCounts[SplitValidation] == 0 || splitCounts[SplitLocked] == 0 {
		return fmt.Errorf("invalid_split_ratio: all splits must be present")
	}
	if dev != splitCounts[SplitValidation]*3 || dev != splitCounts[SplitLocked]*3 {
		return fmt.Errorf("invalid_split_ratio: dev=%d validation=%d locked=%d, want 3:1:1",
			dev, splitCounts[SplitValidation], splitCounts[SplitLocked])
	}
	return nil
}

// LoadManifest 从 JSONL 文件读取清单(严格模式)。
func LoadManifest(path string) (Manifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return Manifest{}, err
	}
	defer file.Close()
	manifest := Manifest{}
	scanner := bufio.NewScanner(file)
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			return Manifest{}, fmt.Errorf("%s:%d: blank line", path, line)
		}
		var c GoldCase
		decode := json.NewDecoder(strings.NewReader(text))
		decode.DisallowUnknownFields()
		if err := decode.Decode(&c); err != nil {
			return Manifest{}, fmt.Errorf("%s:%d: %w", path, line, err)
		}
		manifest.Cases = append(manifest.Cases, c)
	}
	return manifest, scanner.Err()
}
