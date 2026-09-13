package planning

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/zhanshimian/server/internal/domain"
)

const (
	// BriefSchemaVersion pins the canonical brief encoding.
	BriefSchemaVersion = "brief.v1"
	// PlannerSchemaVersion pins the plan set generator contract.
	PlannerSchemaVersion = "plan-set.v1"
	// StyleRuleVersion pins the semantics of the stable style rule IDs below.
	StyleRuleVersion = "style-rules.v1"
)

// ErrInvalidBrief marks a brief that is missing fields, carries unknown
// fields, or uses values outside the scene catalog.
var ErrInvalidBrief = errors.New("invalid scene brief")

// Stable style rule grounding IDs. The semantics of these IDs are frozen by
// StyleRuleVersion; a semantic change must ship a new version instead of
// rewriting them.
const (
	StyleRuleKeepWithoutEvidence = "style.keep_without_evidence"
	StyleRuleLowIntensityHair    = "style.low_intensity_hair"
	StyleRuleLowIntensityMakeup  = "style.low_intensity_makeup"
	StyleRuleCoherentPalette     = "style.coherent_palette"
	StyleRuleSceneFormality      = "style.scene_formality"
)

// StyleRuleIDs is the exhaustive set a candidate may reference.
var StyleRuleIDs = []string{
	StyleRuleKeepWithoutEvidence,
	StyleRuleLowIntensityHair,
	StyleRuleLowIntensityMakeup,
	StyleRuleCoherentPalette,
	StyleRuleSceneFormality,
}

var briefFields = map[domain.Scene]map[string][]string{
	domain.SceneGeneral: {
		"focus":       {"balanced", "hair_first", "makeup_first", "outfit_first"},
		"preparation": {"closet", "key_piece", "complete"},
		"impression":  {"energetic", "reliable", "natural", "memorable"},
	},
	domain.SceneInterview: {
		"when":        {"today", "three_days", "week", "later"},
		"format":      {"onsite", "video", "final"},
		"preparation": {"closet", "key_piece", "complete"},
		"impression":  {"energetic", "reliable", "natural", "memorable"},
	},
	domain.SceneWedding: {
		"role":       {"guest", "bridal_party", "family", "speaker"},
		"timing":     {"lunch", "afternoon", "dinner", "unknown"},
		"dress_code": {"relaxed", "elegant", "formal"},
		"impression": {"energetic", "reliable", "natural", "memorable"},
	},
	domain.SceneDate: {
		"activity":    {"coffee", "dinner", "exhibition", "outdoor"},
		"timing":      {"afternoon", "evening", "night", "unknown"},
		"preparation": {"closet", "key_piece", "complete"},
		"impression":  {"energetic", "reliable", "natural", "memorable"},
	},
	domain.SceneDaily: {
		"activity":    {"office", "weekend", "friends", "city_walk"},
		"weather":     {"air_conditioned", "walking", "rain", "mild"},
		"preparation": {"closet", "key_piece", "complete"},
		"impression":  {"energetic", "reliable", "natural", "memorable"},
	},
	domain.SceneGathering: {
		"activity":    {"friends", "dinner", "birthday", "drinks"},
		"timing":      {"afternoon", "evening", "night", "unknown"},
		"preparation": {"closet", "key_piece", "complete"},
		"impression":  {"energetic", "reliable", "natural", "memorable"},
	},
}

// NormalizeBrief validates the raw scene answers against the scene catalog
// and returns the canonical brief snapshot. Missing fields, unknown fields,
// unknown scenes and unlisted values are all rejected.
func NormalizeBrief(scene domain.Scene, answers map[string]string) (domain.SceneBrief, error) {
	fields, ok := briefFields[scene]
	if !ok {
		return domain.SceneBrief{}, fmt.Errorf("%w: unknown scene %q", ErrInvalidBrief, scene)
	}
	if len(answers) != len(fields) {
		return domain.SceneBrief{}, fmt.Errorf("%w: scene %q wants exactly %d answers, got %d", ErrInvalidBrief, scene, len(fields), len(answers))
	}
	cleaned := make(map[string]string, len(answers))
	for name, allowed := range fields {
		value, ok := answers[name]
		if !ok {
			return domain.SceneBrief{}, fmt.Errorf("%w: scene %q misses answer %q", ErrInvalidBrief, scene, name)
		}
		value = strings.TrimSpace(value)
		if !contains(allowed, value) {
			return domain.SceneBrief{}, fmt.Errorf("%w: answer %q=%q is not allowed in scene %q", ErrInvalidBrief, name, value, scene)
		}
		cleaned[name] = value
	}
	return domain.SceneBrief{SchemaVersion: BriefSchemaVersion, Scene: scene, Answers: cleaned}, nil
}

// BriefHash canonicalizes the brief (map order independent) and returns the
// SHA-256 digest as 64 lowercase hex characters.
func BriefHash(brief domain.SceneBrief) string {
	names := make([]string, 0, len(brief.Answers))
	for name := range brief.Answers {
		names = append(names, name)
	}
	sort.Strings(names)
	ordered := make(map[string]string, len(names))
	for _, name := range names {
		ordered[name] = brief.Answers[name]
	}
	canonical := domain.SceneBrief{SchemaVersion: brief.SchemaVersion, Scene: brief.Scene, Answers: ordered}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		// The canonical shape is plain strings; marshal cannot fail.
		return ""
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

// planningInputFingerprint is the deterministic structure PlanningInputHash
// serializes. Memory items are sorted by ID so the digest is order independent.
type planningInputFingerprint struct {
	ReportID             string                      `json:"report_id"`
	ProfileSnapshot      json.RawMessage             `json:"profile_snapshot"`
	BriefHash            string                      `json:"brief_hash"`
	Memories             []domain.FeedbackMemoryItem `json:"memories"`
	PlannerSchemaVersion string                      `json:"planner_schema_version"`
	StyleRuleVersion     string                      `json:"style_rule_version"`
}

// PlanningInputHash folds the report identity, profile snapshot, brief hash,
// recent preference memories and both schema versions into one digest. Unlike
// BriefHash (which only canonicalizes the SceneBrief), a new preference memory
// changes this hash, so the same report/scene/answers yields a new PlanSet
// identity instead of reusing stale published content.
func PlanningInputHash(reportID string, profileSnapshot json.RawMessage, briefHash string, memories []domain.PreferenceMemory) string {
	items := make([]domain.FeedbackMemoryItem, 0, len(memories))
	for _, memory := range memories {
		items = append(items, domain.FeedbackMemoryItem{
			ID: memory.ID, Key: memory.Key, Category: string(memory.Category), Value: memory.Value,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	raw, err := json.Marshal(planningInputFingerprint{
		ReportID: reportID, ProfileSnapshot: profileSnapshot, BriefHash: briefHash,
		Memories: items, PlannerSchemaVersion: PlannerSchemaVersion, StyleRuleVersion: StyleRuleVersion,
	})
	if err != nil {
		// The fingerprint shape is plain strings and raw JSON; marshal cannot fail.
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// IsStyleRuleID reports whether sourceID is one of the frozen style rules.
func IsStyleRuleID(sourceID string) bool {
	return contains(StyleRuleIDs, sourceID)
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
