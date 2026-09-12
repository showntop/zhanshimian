package ai

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
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
	copyPolicyBanned = regexp.MustCompile(`颜值|身材分|评分|百分位|缺陷严重|诊断|疾病|族裔|性格`)

	photoQualityReasons = map[string]bool{
		"multiple_people":       true,
		"no_person":             true,
		"screenshot":            true,
		"illustration":          true,
		"pet":                   true,
		"face_not_frontal":      true,
		"face_occluded":         true,
		"side_not_profile":      true,
		"body_not_head_to_calf": true,
		"too_blurry":            true,
		"too_dark":              true,
	}
)

type reportPayload struct {
	ImpressionTags []string               `json:"impression_tags"`
	PriorityTitle  string                 `json:"priority_title"`
	PriorityCopy   string                 `json:"priority_copy"`
	Findings       []reportFindingPayload `json:"findings"`
}

type reportFindingPayload struct {
	Key                string                `json:"key"`
	Category           string                `json:"category"`
	Label              string                `json:"label"`
	VisibleObservation string                `json:"visible_observation"`
	Recommendation     string                `json:"recommendation"`
	Priority           int                   `json:"priority"`
	Position           int                   `json:"position"`
	SourceRole         string                `json:"source_role"`
	Anchor             domain.EvidenceAnchor `json:"anchor"`
	Confidence         float64               `json:"confidence"`
}

func (p reportPayload) toDraft() domain.ReportDraft {
	findings := make([]domain.DraftFinding, len(p.Findings))
	for i, finding := range p.Findings {
		findings[i] = domain.DraftFinding{
			Key:                finding.Key,
			Category:           finding.Category,
			Label:              finding.Label,
			VisibleObservation: finding.VisibleObservation,
			Recommendation:     finding.Recommendation,
			Priority:           finding.Priority,
			Position:           finding.Position,
			SourceRole:         domain.PhotoRole(finding.SourceRole),
			Anchor:             finding.Anchor,
			Confidence:         finding.Confidence,
		}
	}
	return domain.ReportDraft{
		ImpressionTags: p.ImpressionTags,
		PriorityTitle:  p.PriorityTitle,
		PriorityCopy:   p.PriorityCopy,
		Findings:       findings,
	}
}

type photoQualityPayload struct {
	Photos []PhotoQualityItem `json:"photos"`
}

type identityPayload struct {
	Decision   string  `json:"decision"`
	Confidence float64 `json:"confidence"`
}

type evidencePayload struct {
	Findings []EvidenceDecision `json:"findings"`
}

func validatePhotoQualityPayload(data []byte) error {
	var payload photoQualityPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	if len(payload.Photos) != 3 {
		return fmt.Errorf("photo quality output has %d photos, want 3", len(payload.Photos))
	}
	seen := map[domain.PhotoRole]bool{}
	for _, item := range payload.Photos {
		switch item.Role {
		case domain.PhotoRoleFace, domain.PhotoRoleSide, domain.PhotoRoleBody:
		default:
			return fmt.Errorf("photo quality output has unknown role %q", item.Role)
		}
		if seen[item.Role] {
			return fmt.Errorf("photo quality output repeats role %q", item.Role)
		}
		seen[item.Role] = true
		switch item.Decision {
		case "pass":
			if item.ReasonCode != "" {
				return fmt.Errorf("photo quality output for %q has a reason despite pass", item.Role)
			}
		case "reject":
			if !photoQualityReasons[item.ReasonCode] {
				return fmt.Errorf("photo quality output for %q has unknown reason_code %q", item.Role, item.ReasonCode)
			}
		default:
			return fmt.Errorf("photo quality output for %q has invalid decision %q", item.Role, item.Decision)
		}
	}
	return nil
}

func validateIdentityPayload(data []byte) error {
	var payload identityPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	switch payload.Decision {
	case "pass", "reject", "uncertain":
	default:
		return fmt.Errorf("identity output has invalid decision %q", payload.Decision)
	}
	if !validUnitInterval(payload.Confidence) {
		return fmt.Errorf("identity output has invalid confidence %v", payload.Confidence)
	}
	return nil
}

func validateReportPayload(data []byte) error {
	var payload reportPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	return validateReportDraft(payload.toDraft())
}

func validateReportDraft(draft domain.ReportDraft) error {
	if err := validateDraftCopy(draft); err != nil {
		return err
	}
	if len(draft.Findings) < minFindings || len(draft.Findings) > maxFindings {
		return fmt.Errorf("report draft has %d findings, want %d-%d", len(draft.Findings), minFindings, maxFindings)
	}
	seen := make([]bool, len(draft.Findings)+1)
	for _, finding := range draft.Findings {
		if err := validateDraftFinding(finding, len(draft.Findings), seen); err != nil {
			return err
		}
	}
	for position := 1; position <= len(draft.Findings); position++ {
		if !seen[position] {
			return fmt.Errorf("report draft missing position %d", position)
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
		return fmt.Errorf("report draft copy policy violation")
	}
	if len(draft.ImpressionTags) == 0 {
		return fmt.Errorf("report draft missing impression tags")
	}
	for _, tag := range draft.ImpressionTags {
		trimmed, err := requireText(tag, maxTagLen)
		if err != nil {
			return err
		}
		if copyPolicyBanned.MatchString(trimmed) {
			return fmt.Errorf("report draft copy policy violation")
		}
	}
	return nil
}

func validateDraftFinding(finding domain.DraftFinding, count int, seen []bool) error {
	if finding.Position < 1 || finding.Position > count || seen[finding.Position] {
		return fmt.Errorf("report draft has invalid finding position %d", finding.Position)
	}
	seen[finding.Position] = true
	if finding.Priority < 1 || finding.Priority > 3 {
		return fmt.Errorf("report draft has invalid priority %d", finding.Priority)
	}
	switch finding.Category {
	case "hair", "makeup", "outfit", "color":
	default:
		return fmt.Errorf("report draft has invalid category %q", finding.Category)
	}
	switch finding.SourceRole {
	case domain.PhotoRoleFace, domain.PhotoRoleSide, domain.PhotoRoleBody:
	default:
		return fmt.Errorf("report draft has invalid source role %q", finding.SourceRole)
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
		return fmt.Errorf("report draft copy policy violation")
	}
	if !validAnchor(finding.Anchor) {
		return fmt.Errorf("report draft finding %q is missing a valid evidence anchor", finding.Key)
	}
	return nil
}

func validateEvidencePayload(keys []string, data []byte) error {
	var payload evidencePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	if len(payload.Findings) != len(keys) {
		return fmt.Errorf("evidence output has %d findings, want %d", len(payload.Findings), len(keys))
	}
	required := make(map[string]bool, len(keys))
	for _, key := range keys {
		required[key] = true
	}
	seen := make(map[string]bool, len(payload.Findings))
	for _, item := range payload.Findings {
		if !required[item.Key] {
			return fmt.Errorf("evidence output has unexpected key %q", item.Key)
		}
		if seen[item.Key] {
			return fmt.Errorf("evidence output repeats key %q", item.Key)
		}
		seen[item.Key] = true
		if !validUnitInterval(item.Confidence) {
			return fmt.Errorf("evidence output for %q has invalid confidence %v", item.Key, item.Confidence)
		}
	}
	for _, key := range keys {
		if !seen[key] {
			return fmt.Errorf("evidence output missing key %q", key)
		}
	}
	return nil
}

func requireText(value string, maxLen int) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || len([]rune(trimmed)) > maxLen {
		return "", fmt.Errorf("report draft text is empty or too long")
	}
	return trimmed, nil
}

func validAnchor(anchor domain.EvidenceAnchor) bool {
	if !validClosedUnit(anchor.X) || !validClosedUnit(anchor.Y) {
		return false
	}
	if !validPositiveUnit(anchor.W) || !validPositiveUnit(anchor.H) {
		return false
	}
	return anchor.X+anchor.W <= 1 && anchor.Y+anchor.H <= 1
}

func validUnitInterval(value float64) bool {
	return validClosedUnit(value)
}

func validClosedUnit(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1
}

func validPositiveUnit(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value > 0 && value <= 1
}

func photoQualitySchema() map[string]any {
	return objectSchema(map[string]any{
		"photos": map[string]any{
			"type":     "array",
			"minItems": 3,
			"maxItems": 3,
			"items": objectSchema(map[string]any{
				"role":        map[string]any{"type": "string", "enum": []string{"face", "side", "body"}},
				"decision":    map[string]any{"type": "string", "enum": []string{"pass", "reject"}},
				"reason_code": map[string]any{"type": "string"},
			}, "role", "decision", "reason_code"),
		},
	}, "photos")
}

func identitySchema() map[string]any {
	return objectSchema(map[string]any{
		"decision":   map[string]any{"type": "string", "enum": []string{"pass", "reject", "uncertain"}},
		"confidence": map[string]any{"type": "number", "minimum": 0, "maximum": 1},
	}, "decision", "confidence")
}

func reportSchema() map[string]any {
	finding := objectSchema(map[string]any{
		"key":                 map[string]any{"type": "string"},
		"category":            map[string]any{"type": "string", "enum": []string{"hair", "makeup", "outfit", "color"}},
		"label":               map[string]any{"type": "string"},
		"visible_observation": map[string]any{"type": "string"},
		"recommendation":      map[string]any{"type": "string"},
		"priority":            map[string]any{"type": "integer", "minimum": 1, "maximum": 3},
		"position":            map[string]any{"type": "integer", "minimum": 1},
		"source_role":         map[string]any{"type": "string", "enum": []string{"face", "side", "body"}},
		"anchor": objectSchema(map[string]any{
			"x": map[string]any{"type": "number"},
			"y": map[string]any{"type": "number"},
			"w": map[string]any{"type": "number"},
			"h": map[string]any{"type": "number"},
		}, "x", "y", "w", "h"),
		"confidence": map[string]any{"type": "number"},
	}, "key", "category", "label", "visible_observation", "recommendation", "priority", "position", "source_role", "anchor")
	return objectSchema(map[string]any{
		"impression_tags": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 1},
		"priority_title":  map[string]any{"type": "string"},
		"priority_copy":   map[string]any{"type": "string"},
		"findings":        map[string]any{"type": "array", "items": finding, "minItems": minFindings, "maxItems": maxFindings},
	}, "impression_tags", "priority_title", "priority_copy", "findings")
}

func evidenceSchema() map[string]any {
	return objectSchema(map[string]any{
		"findings": map[string]any{
			"type": "array",
			"items": objectSchema(map[string]any{
				"key":         map[string]any{"type": "string"},
				"supported":   map[string]any{"type": "boolean"},
				"confidence":  map[string]any{"type": "number", "minimum": 0, "maximum": 1},
				"reason_code": map[string]any{"type": "string"},
			}, "key", "supported", "confidence", "reason_code"),
		},
	}, "findings")
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           properties,
		"required":             required,
	}
}
