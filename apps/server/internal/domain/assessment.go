package domain

import (
	"encoding/json"
	"time"
)

type PhotoRole string

const (
	PhotoRoleFace PhotoRole = "face"
	PhotoRoleSide PhotoRole = "side"
	PhotoRoleBody PhotoRole = "body"
)

type PhotoSlots struct {
	FaceAssetID string `json:"face_asset_id"`
	SideAssetID string `json:"side_asset_id"`
	BodyAssetID string `json:"body_asset_id"`
}

type PhotoSet struct {
	ID, UserID, ContentHash, SchemaVersion string
	ProfileSnapshot                        json.RawMessage
	Items                                  []PhotoSetItem
	CreatedAt                              time.Time
}

type PhotoSetItem struct {
	ID, UserID, PhotoSetID, MediaAssetID string
	Role                                 PhotoRole
	Asset                                MediaAsset
}

type AnalysisOutcome string

const (
	AnalysisOutcomePublished AnalysisOutcome = "published"
	AnalysisOutcomeRejected  AnalysisOutcome = "rejected"
	AnalysisOutcomeFailed    AnalysisOutcome = "failed"
)

type AnalysisRun struct {
	ID, UserID, PhotoSetID, OperationID, InputHash string
	AnalyzerSchemaVersion, QualityPolicyVersion    string
	ProfileSnapshot                                json.RawMessage
	ProviderInvocationID, ReportID                 string
	Outcome                                        *AnalysisOutcome
	CreatedAt                                      time.Time
	FinishedAt                                     *time.Time
}

type EvidenceAnchor struct{ X, Y, W, H float64 }

type ReportFinding struct {
	ID, UserID, ReportID, Label, VisibleObservation, Recommendation string
	SourcePhotoItemID                                               string
	Category                                                        string
	Priority                                                        int
	Anchor                                                          EvidenceAnchor
	Confidence                                                      float64
	Position                                                        int
}

type Report struct {
	ID, UserID, PhotoSetID, HeroAssetID, SchemaVersion, ContentHash string
	PriorityTitle, PriorityCopy                                     string
	ImpressionTags                                                  []string
	ProfileSnapshot                                                 json.RawMessage
	ProviderInvocationID, QualityEvaluationID                       string
	Findings                                                        []ReportFinding
	CreatedAt                                                       time.Time
}

type DraftFinding struct {
	Key, Category, Label, VisibleObservation, Recommendation string
	Priority, Position                                       int
	SourceRole                                               PhotoRole
	Anchor                                                   EvidenceAnchor
	Confidence                                               float64
}

type ReportDraft struct {
	ImpressionTags              []string
	PriorityTitle, PriorityCopy string
	Findings                    []DraftFinding
}

type ReportDetail struct {
	Report   Report
	PhotoSet PhotoSet
}
