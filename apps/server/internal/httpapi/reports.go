package httpapi

import (
	"net/http"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/service/assessment"
)

type MediaResponse struct {
	AssetID      string `json:"asset_id"`
	URL          string `json:"url"`
	URLExpiresAt string `json:"url_expires_at"`
	MIMEType     string `json:"mime_type"`
	SourceKind   string `json:"source_kind"`
	DisplayLabel string `json:"display_label"`
}

type ReportPhotoResponse struct {
	ItemID string           `json:"item_id"`
	Role   domain.PhotoRole `json:"role"`
	Media  MediaResponse    `json:"media"`
}

type ReportSourceMediaResponse struct {
	Face ReportPhotoResponse `json:"face"`
	Side ReportPhotoResponse `json:"side"`
	Body ReportPhotoResponse `json:"body"`
}

type FindingResponse struct {
	ID                 string `json:"id"`
	Category           string `json:"category"`
	Label              string `json:"label"`
	VisibleObservation string `json:"visible_observation"`
	Recommendation     string `json:"recommendation"`
	Priority           int    `json:"priority"`
	Position           int    `json:"position"`
	SourcePhoto        struct {
		ItemID string           `json:"item_id"`
		Role   domain.PhotoRole `json:"role"`
	} `json:"source_photo"`
	Anchor evidenceAnchorResponse `json:"anchor"`
}

type evidenceAnchorResponse struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

type ReportResponse struct {
	ID             string                    `json:"id"`
	PhotoSetID     string                    `json:"photo_set_id"`
	HeroAssetID    string                    `json:"hero_asset_id"`
	PriorityTitle  string                    `json:"priority_title"`
	PriorityCopy   string                    `json:"priority_copy"`
	SchemaVersion  string                    `json:"schema_version"`
	ImpressionTags []string                  `json:"impression_tags"`
	SourceMedia    ReportSourceMediaResponse `json:"source_media"`
	Findings       []FindingResponse         `json:"findings"`
	CreatedAt      string                    `json:"created_at"`
}

func (a *API) getPublishedReport(w http.ResponseWriter, r *http.Request) {
	if a.assessment == nil {
		a.internalError(w, r, errAssessmentUnavailable)
		return
	}
	view, err := a.assessment.GetReport(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		a.writeAssessmentError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, publicReport(view))
}

func (a *API) getCurrentPublishedReport(w http.ResponseWriter, r *http.Request) {
	if a.assessment == nil {
		a.internalError(w, r, errAssessmentUnavailable)
		return
	}
	view, err := a.assessment.GetCurrentReport(r.Context(), currentUser(r).ID)
	if err != nil {
		a.writeAssessmentError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, publicReport(view))
}

func publicReport(view assessment.ReportView) ReportResponse {
	tags := view.ImpressionTags
	if tags == nil {
		tags = []string{}
	}
	findings := make([]FindingResponse, 0, len(view.Findings))
	for _, finding := range view.Findings {
		item := FindingResponse{
			ID:                 finding.ID,
			Category:           finding.Category,
			Label:              finding.Label,
			VisibleObservation: finding.VisibleObservation,
			Recommendation:     finding.Recommendation,
			Priority:           finding.Priority,
			Position:           finding.Position,
			Anchor: evidenceAnchorResponse{
				X: finding.Anchor.X, Y: finding.Anchor.Y, W: finding.Anchor.W, H: finding.Anchor.H,
			},
		}
		item.SourcePhoto.ItemID = finding.SourcePhoto.ItemID
		item.SourcePhoto.Role = finding.SourcePhoto.Role
		findings = append(findings, item)
	}
	return ReportResponse{
		ID:             view.ID,
		PhotoSetID:     view.PhotoSetID,
		HeroAssetID:    view.HeroAssetID,
		PriorityTitle:  view.PriorityTitle,
		PriorityCopy:   view.PriorityCopy,
		SchemaVersion:  view.SchemaVersion,
		ImpressionTags: tags,
		SourceMedia: ReportSourceMediaResponse{
			Face: publicReportPhoto(view.SourceMedia.Face),
			Side: publicReportPhoto(view.SourceMedia.Side),
			Body: publicReportPhoto(view.SourceMedia.Body),
		},
		Findings:  findings,
		CreatedAt: formatPublicTime(view.CreatedAt),
	}
}

func publicReportPhoto(photo assessment.ReportPhoto) ReportPhotoResponse {
	return ReportPhotoResponse{
		ItemID: photo.ItemID,
		Role:   photo.Role,
		Media:  publicMedia(photo.Media),
	}
}

func publicMedia(media assessment.PresentedMedia) MediaResponse {
	return MediaResponse{
		AssetID:      media.AssetID,
		URL:          media.URL,
		URLExpiresAt: formatPublicTime(media.URLExpiresAt),
		MIMEType:     media.MIMEType,
		SourceKind:   media.SourceKind,
		DisplayLabel: media.DisplayLabel,
	}
}
