package assessment

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/zhanshimian/server/internal/domain"
)

const (
	maxTitleLen          = 80
	maxLabelLen          = 80
	maxTagLen            = 80
	maxKeyLen            = 80
	maxCopyLen           = 240
	maxObservationLen    = 240
	maxRecommendationLen = 240
	minFindings          = 3
	maxFindings          = 6
)

var (
	ErrCopyPolicy      = errors.New("copy_policy_violation")
	ErrEvidenceMissing = errors.New("evidence_missing")
	ErrDraftInvalid    = errors.New("report_draft_invalid")

	copyPolicyBanned = regexp.MustCompile(`颜值|身材分|评分|百分位|缺陷严重|诊断|疾病|族裔|性格`)
)

type ValidationError struct {
	Code string
}

func (e *ValidationError) Error() string {
	if e == nil {
		return ""
	}
	return e.Code
}

func ValidateSlots(userID string, slots domain.PhotoSlots, assets map[string]domain.MediaAsset) error {
	if slots.FaceAssetID == "" || slots.SideAssetID == "" || slots.BodyAssetID == "" ||
		slots.FaceAssetID == slots.SideAssetID || slots.FaceAssetID == slots.BodyAssetID || slots.SideAssetID == slots.BodyAssetID {
		return &ValidationError{Code: "photo_slots_invalid"}
	}
	for _, slot := range []struct {
		id      string
		purpose domain.MediaPurpose
	}{
		{slots.FaceAssetID, domain.MediaPurposeFace},
		{slots.SideAssetID, domain.MediaPurposeSide},
		{slots.BodyAssetID, domain.MediaPurposeBody},
	} {
		asset, ok := assets[slot.id]
		if !ok || asset.UserID != userID {
			return &ValidationError{Code: "photo_asset_not_found"}
		}
		if asset.Purpose != slot.purpose {
			return &ValidationError{Code: "photo_role_mismatch"}
		}
		if asset.State != domain.MediaStateReady {
			return &ValidationError{Code: "photo_asset_not_ready"}
		}
	}
	return nil
}

func PhotoSetContentHash(schemaVersion string, hashes map[domain.PhotoRole]string) string {
	return hashParts(schemaVersion, hashes[domain.PhotoRoleFace], hashes[domain.PhotoRoleSide], hashes[domain.PhotoRoleBody])
}

func AnalysisInputHash(contentHash string, profile json.RawMessage, analyzerSchemaVersion, qualityPolicyVersion string) string {
	return hashParts(contentHash, string(profile), analyzerSchemaVersion, qualityPolicyVersion)
}

func ReportContentHash(schemaVersion string, draft domain.ReportDraft) string {
	tags := append([]string(nil), draft.ImpressionTags...)
	for i := range tags {
		tags[i] = strings.TrimSpace(tags[i])
	}
	sort.Strings(tags)
	parts := []string{
		schemaVersion,
		strings.TrimSpace(draft.PriorityTitle),
		strings.TrimSpace(draft.PriorityCopy),
		strings.Join(tags, "\n"),
	}
	findings := append([]domain.DraftFinding(nil), draft.Findings...)
	sort.SliceStable(findings, func(i, j int) bool { return findings[i].Position < findings[j].Position })
	for _, finding := range findings {
		parts = append(parts,
			strconv.Itoa(finding.Position),
			strings.TrimSpace(finding.Category),
			strconv.Itoa(finding.Priority),
			strings.TrimSpace(finding.Label),
			strings.TrimSpace(finding.VisibleObservation),
			strings.TrimSpace(finding.Recommendation),
			string(finding.SourceRole),
			formatAnchor(finding.Anchor),
		)
	}
	return hashParts(parts...)
}

func ValidateReportDraft(draft domain.ReportDraft) error {
	if err := validateDraftCopy(draft); err != nil {
		return err
	}
	if len(draft.Findings) < minFindings || len(draft.Findings) > maxFindings {
		return ErrDraftInvalid
	}
	seen := make([]bool, len(draft.Findings)+1)
	for _, finding := range draft.Findings {
		if err := validateDraftFinding(finding, len(draft.Findings), seen); err != nil {
			return err
		}
	}
	for position := 1; position <= len(draft.Findings); position++ {
		if !seen[position] {
			return ErrDraftInvalid
		}
	}
	return nil
}

func validateDraftCopy(draft domain.ReportDraft) error {
	title, err := requireText(draft.PriorityTitle, maxTitleLen)
	if err != nil {
		return err
	}
	copy, err := requireText(draft.PriorityCopy, maxCopyLen)
	if err != nil {
		return err
	}
	if copyPolicyBanned.MatchString(title) || copyPolicyBanned.MatchString(copy) {
		return ErrCopyPolicy
	}
	if len(draft.ImpressionTags) == 0 {
		return ErrDraftInvalid
	}
	for _, tag := range draft.ImpressionTags {
		trimmed, err := requireText(tag, maxTagLen)
		if err != nil {
			return err
		}
		if copyPolicyBanned.MatchString(trimmed) {
			return ErrCopyPolicy
		}
	}
	return nil
}

func validateDraftFinding(finding domain.DraftFinding, count int, seen []bool) error {
	if finding.Position < 1 || finding.Position > count || seen[finding.Position] {
		return ErrDraftInvalid
	}
	seen[finding.Position] = true
	if finding.Priority < 1 || finding.Priority > 3 {
		return ErrDraftInvalid
	}
	switch finding.Category {
	case "hair", "makeup", "outfit", "color":
	default:
		return ErrDraftInvalid
	}
	switch finding.SourceRole {
	case domain.PhotoRoleFace, domain.PhotoRoleSide, domain.PhotoRoleBody:
	default:
		return ErrDraftInvalid
	}
	if _, err := requireText(finding.Key, maxKeyLen); err != nil {
		return err
	}
	label, err := requireText(finding.Label, maxLabelLen)
	if err != nil {
		return err
	}
	observation, err := requireText(finding.VisibleObservation, maxObservationLen)
	if err != nil {
		return err
	}
	recommendation, err := requireText(finding.Recommendation, maxRecommendationLen)
	if err != nil {
		return err
	}
	if copyPolicyBanned.MatchString(label) || copyPolicyBanned.MatchString(observation) || copyPolicyBanned.MatchString(recommendation) {
		return ErrCopyPolicy
	}
	if !validAnchor(finding.Anchor) {
		return ErrEvidenceMissing
	}
	return nil
}

func requireText(value string, maxLen int) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || len([]rune(trimmed)) > maxLen {
		return "", ErrDraftInvalid
	}
	return trimmed, nil
}

func validAnchor(anchor domain.EvidenceAnchor) bool {
	if math.IsNaN(anchor.X) || math.IsNaN(anchor.Y) || math.IsNaN(anchor.W) || math.IsNaN(anchor.H) {
		return false
	}
	if math.IsInf(anchor.X, 0) || math.IsInf(anchor.Y, 0) || math.IsInf(anchor.W, 0) || math.IsInf(anchor.H, 0) {
		return false
	}
	if anchor.X < 0 || anchor.X > 1 || anchor.Y < 0 || anchor.Y > 1 {
		return false
	}
	if anchor.W <= 0 || anchor.W > 1 || anchor.H <= 0 || anchor.H > 1 {
		return false
	}
	if anchor.X+anchor.W > 1 || anchor.Y+anchor.H > 1 {
		return false
	}
	return true
}

func hashParts(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:])
}

func formatAnchor(anchor domain.EvidenceAnchor) string {
	return fmt.Sprintf("%.8f,%.8f,%.8f,%.8f", anchor.X, anchor.Y, anchor.W, anchor.H)
}
