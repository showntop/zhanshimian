package assessment

import (
	"errors"
	"maps"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/zhanshimian/server/internal/domain"
)

func TestValidateSlotsRequiresThreeDistinctOwnedReadyAssets(t *testing.T) {
	userID := uuid.NewString()
	assets := validAssets(userID)
	tests := []struct {
		name   string
		slots  domain.PhotoSlots
		mutate func(map[string]domain.MediaAsset)
		code   string
	}{
		{"missing side", domain.PhotoSlots{FaceAssetID: "face", BodyAssetID: "body"}, nil, "photo_slots_invalid"},
		{"duplicate asset", domain.PhotoSlots{FaceAssetID: "face", SideAssetID: "face", BodyAssetID: "body"}, nil, "photo_slots_invalid"},
		{"foreign owner", validSlots(), func(v map[string]domain.MediaAsset) { a := v["side"]; a.UserID = uuid.NewString(); v["side"] = a }, "photo_asset_not_found"},
		{"wrong purpose", validSlots(), func(v map[string]domain.MediaAsset) {
			a := v["body"]
			a.Purpose = domain.MediaPurposeFace
			v["body"] = a
		}, "photo_role_mismatch"},
		{"not ready", validSlots(), func(v map[string]domain.MediaAsset) {
			a := v["face"]
			a.State = domain.MediaStateQuarantined
			v["face"] = a
		}, "photo_asset_not_ready"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			copy := maps.Clone(assets)
			if tc.mutate != nil {
				tc.mutate(copy)
			}
			err := ValidateSlots(userID, tc.slots, copy)
			var rejected *ValidationError
			if !errors.As(err, &rejected) || rejected.Code != tc.code {
				t.Fatalf("got %v, want %s", err, tc.code)
			}
		})
	}
}

func TestPhotoSetContentHashIsRoleOrdered(t *testing.T) {
	first := PhotoSetContentHash("photo-set.v1", map[domain.PhotoRole]string{
		domain.PhotoRoleFace: "face-sha", domain.PhotoRoleSide: "side-sha", domain.PhotoRoleBody: "body-sha",
	})
	second := PhotoSetContentHash("photo-set.v1", map[domain.PhotoRole]string{
		domain.PhotoRoleBody: "body-sha", domain.PhotoRoleFace: "face-sha", domain.PhotoRoleSide: "side-sha",
	})
	if first != second {
		t.Fatalf("map order changed hash")
	}
}

func TestValidateReportDraftRejectsScoresAndIncompleteEvidence(t *testing.T) {
	draft := validDraft()
	draft.Findings[0].VisibleObservation = "颜值 90 分"
	if err := ValidateReportDraft(draft); !errors.Is(err, ErrCopyPolicy) {
		t.Fatalf("got %v", err)
	}
	draft = validDraft()
	draft.Findings[0].Anchor.W = 0
	if err := ValidateReportDraft(draft); !errors.Is(err, ErrEvidenceMissing) {
		t.Fatalf("got %v", err)
	}
}

func TestValidateSlotsAcceptsThreeDistinctOwnedReadyAssets(t *testing.T) {
	userID := uuid.NewString()
	if err := ValidateSlots(userID, validSlots(), validAssets(userID)); err != nil {
		t.Fatalf("valid slots: %v", err)
	}
}

func TestValidateSlotsMissingAssetIsNotFound(t *testing.T) {
	userID := uuid.NewString()
	assets := validAssets(userID)
	delete(assets, "side")
	err := ValidateSlots(userID, validSlots(), assets)
	var rejected *ValidationError
	if !errors.As(err, &rejected) || rejected.Code != "photo_asset_not_found" {
		t.Fatalf("got %v, want photo_asset_not_found", err)
	}
}

func TestAnalysisInputHashIsOrderIndependentForEquivalentParts(t *testing.T) {
	profile := []byte(`{"role":"designer","height_cm":172}`)
	first := AnalysisInputHash("content-a", profile, "analyzer.v1", "quality.v1")
	second := AnalysisInputHash("content-a", append([]byte(nil), profile...), "analyzer.v1", "quality.v1")
	if first != second {
		t.Fatalf("equivalent inputs changed hash")
	}
	if !hexSHA256(first) {
		t.Fatalf("hash %q is not lowercase hex sha256", first)
	}
	if first == AnalysisInputHash("content-b", profile, "analyzer.v1", "quality.v1") {
		t.Fatal("different content hash produced the same input hash")
	}
}

func TestReportContentHashIsStableAcrossTagAndFindingOrder(t *testing.T) {
	draft := validDraft()
	first := ReportContentHash("report.v1", draft)
	draft.ImpressionTags = []string{"干净", "利落"}
	draft.Findings[0], draft.Findings[2] = draft.Findings[2], draft.Findings[0]
	second := ReportContentHash("report.v1", draft)
	if first != second {
		t.Fatalf("tag or finding order changed hash")
	}
	if !hexSHA256(first) {
		t.Fatalf("hash %q is not lowercase hex sha256", first)
	}
}

func TestValidateReportDraftAcceptsValidDraft(t *testing.T) {
	if err := ValidateReportDraft(validDraft()); err != nil {
		t.Fatalf("valid draft: %v", err)
	}
}

func TestValidateReportDraftRejectsStructureAndBounds(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*domain.ReportDraft)
		want   error
	}{
		{"too few findings", func(d *domain.ReportDraft) { d.Findings = d.Findings[:2] }, ErrDraftInvalid},
		{"too many findings", func(d *domain.ReportDraft) {
			extra := d.Findings[2]
			extra.Key = "color-extra"
			extra.Category = "color"
			extra.Position = 4
			d.Findings = append(d.Findings, extra, extra, extra, extra)
			d.Findings[4].Key = "color-5"
			d.Findings[4].Position = 5
			d.Findings[5].Key = "color-6"
			d.Findings[5].Position = 6
			d.Findings[6].Key = "color-7"
			d.Findings[6].Position = 7
		}, ErrDraftInvalid},
		{"gap in positions", func(d *domain.ReportDraft) { d.Findings[2].Position = 4 }, ErrDraftInvalid},
		{"priority out of range", func(d *domain.ReportDraft) { d.Findings[1].Priority = 4 }, ErrDraftInvalid},
		{"unknown category", func(d *domain.ReportDraft) { d.Findings[1].Category = "proportion" }, ErrDraftInvalid},
		{"missing source role", func(d *domain.ReportDraft) { d.Findings[0].SourceRole = "" }, ErrDraftInvalid},
		{"blank label", func(d *domain.ReportDraft) { d.Findings[0].Label = "   " }, ErrDraftInvalid},
		{"title too long", func(d *domain.ReportDraft) { d.PriorityTitle = strings.Repeat("题", 81) }, ErrDraftInvalid},
		{"observation too long", func(d *domain.ReportDraft) { d.Findings[0].VisibleObservation = strings.Repeat("看", 241) }, ErrDraftInvalid},
		{"anchor overflows", func(d *domain.ReportDraft) {
			d.Findings[0].Anchor = domain.EvidenceAnchor{X: 0.8, Y: 0.1, W: 0.3, H: 0.2}
		}, ErrEvidenceMissing},
		{"banned diagnosis", func(d *domain.ReportDraft) { d.PriorityCopy = "这不是诊断结论" }, ErrCopyPolicy},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			draft := validDraft()
			tc.mutate(&draft)
			if err := ValidateReportDraft(draft); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func hexSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' && r < 'a' || r > 'f' {
			return false
		}
	}
	return true
}

func validSlots() domain.PhotoSlots {
	return domain.PhotoSlots{
		FaceAssetID: "face",
		SideAssetID: "side",
		BodyAssetID: "body",
	}
}

func validAssets(userID string) map[string]domain.MediaAsset {
	return map[string]domain.MediaAsset{
		"face": {
			ID:      "face",
			UserID:  userID,
			Purpose: domain.MediaPurposeFace,
			State:   domain.MediaStateReady,
			SHA256:  "face-sha",
		},
		"side": {
			ID:      "side",
			UserID:  userID,
			Purpose: domain.MediaPurposeSide,
			State:   domain.MediaStateReady,
			SHA256:  "side-sha",
		},
		"body": {
			ID:      "body",
			UserID:  userID,
			Purpose: domain.MediaPurposeBody,
			State:   domain.MediaStateReady,
			SHA256:  "body-sha",
		},
	}
}

func validDraft() domain.ReportDraft {
	return domain.ReportDraft{
		ImpressionTags: []string{"利落", "干净"},
		PriorityTitle:  "先整理额前碎发",
		PriorityCopy:   "额前碎发会挡住眉形，先固定再看妆容层次。",
		Findings: []domain.DraftFinding{
			{
				Key:                "hair-fringe",
				Category:           "hair",
				Label:              "额前碎发",
				VisibleObservation: "额前碎发落到眉毛上方",
				Recommendation:     "用少量发蜡向后梳理并固定",
				Priority:           1,
				Position:           1,
				SourceRole:         domain.PhotoRoleFace,
				Anchor:             domain.EvidenceAnchor{X: 0.20, Y: 0.10, W: 0.40, H: 0.20},
				Confidence:         0.9,
			},
			{
				Key:                "makeup-brow",
				Category:           "makeup",
				Label:              "眉形层次",
				VisibleObservation: "眉尾比眉头更淡，左右不对称",
				Recommendation:     "用眉笔补齐眉尾，保持自然过渡",
				Priority:           2,
				Position:           2,
				SourceRole:         domain.PhotoRoleFace,
				Anchor:             domain.EvidenceAnchor{X: 0.25, Y: 0.22, W: 0.50, H: 0.12},
				Confidence:         0.8,
			},
			{
				Key:                "outfit-collar",
				Category:           "outfit",
				Label:              "领口位置",
				VisibleObservation: "领口偏松，肩线看起来往下滑",
				Recommendation:     "换成合肩的上衣，领口贴近锁骨",
				Priority:           3,
				Position:           3,
				SourceRole:         domain.PhotoRoleBody,
				Anchor:             domain.EvidenceAnchor{X: 0.30, Y: 0.18, W: 0.40, H: 0.16},
				Confidence:         0.85,
			},
		},
	}
}
