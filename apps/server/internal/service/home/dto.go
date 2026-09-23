package home

import (
	"time"

	"github.com/zhanshimian/server/internal/domain"
)

// 本文件的 DTO 与 GET /v1/reports/{id}、GET /v1/plan-sets/{id} 的响应
// 形状一一对应（冻结契约 Report / PlanSet）。httpapi 对 /v1/home/bootstrap
// 只透传，因此契约形状由 home 自持；两份 DTO 是有意的镜像，改契约时同步改。

const publicTimeLayout = "2006-01-02T15:04:05Z"

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(publicTimeLayout)
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func nilIfEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// ---- report（契约 Report）----

type mediaDTO struct {
	AssetID      string `json:"asset_id"`
	URL          string `json:"url"`
	URLExpiresAt string `json:"url_expires_at"`
	MIMEType     string `json:"mime_type"`
	SourceKind   string `json:"source_kind"`
	DisplayLabel string `json:"display_label"`
}

type reportPhotoDTO struct {
	ItemID string           `json:"item_id"`
	Role   domain.PhotoRole `json:"role"`
	Media  mediaDTO         `json:"media"`
}

type reportSourceMediaDTO struct {
	Face reportPhotoDTO `json:"face"`
	Side reportPhotoDTO `json:"side"`
	Body reportPhotoDTO `json:"body"`
}

type findingDTO struct {
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
	Anchor anchorDTO `json:"anchor"`
}

type anchorDTO struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

type reportDTO struct {
	ID             string               `json:"id"`
	PhotoSetID     string               `json:"photo_set_id"`
	HeroAssetID    string               `json:"hero_asset_id"`
	PriorityTitle  string               `json:"priority_title"`
	PriorityCopy   string               `json:"priority_copy"`
	SchemaVersion  string               `json:"schema_version"`
	ImpressionTags []string             `json:"impression_tags"`
	SourceMedia    reportSourceMediaDTO `json:"source_media"`
	Findings       []findingDTO         `json:"findings"`
	CreatedAt      string               `json:"created_at"`
}

func newReportDTO(view ReportView) reportDTO {
	findings := make([]findingDTO, 0, len(view.Findings))
	for _, finding := range view.Findings {
		item := findingDTO{
			ID:                 finding.ID,
			Category:           finding.Category,
			Label:              finding.Label,
			VisibleObservation: finding.VisibleObservation,
			Recommendation:     finding.Recommendation,
			Priority:           finding.Priority,
			Position:           finding.Position,
			Anchor: anchorDTO{
				X: finding.Anchor.X, Y: finding.Anchor.Y, W: finding.Anchor.W, H: finding.Anchor.H,
			},
		}
		item.SourcePhoto.ItemID = finding.SourcePhoto.ItemID
		item.SourcePhoto.Role = finding.SourcePhoto.Role
		findings = append(findings, item)
	}
	return reportDTO{
		ID:             view.ID,
		PhotoSetID:     view.PhotoSetID,
		HeroAssetID:    view.HeroAssetID,
		PriorityTitle:  view.PriorityTitle,
		PriorityCopy:   view.PriorityCopy,
		SchemaVersion:  view.SchemaVersion,
		ImpressionTags: nonNilStrings(view.ImpressionTags),
		SourceMedia: reportSourceMediaDTO{
			Face: newReportPhotoDTO(view.SourceMedia.Face),
			Side: newReportPhotoDTO(view.SourceMedia.Side),
			Body: newReportPhotoDTO(view.SourceMedia.Body),
		},
		Findings:  findings,
		CreatedAt: formatTime(view.CreatedAt),
	}
}

func newReportPhotoDTO(photo ReportPhoto) reportPhotoDTO {
	return reportPhotoDTO{
		ItemID: photo.ItemID,
		Role:   photo.Role,
		Media:  newMediaDTO(photo.Media),
	}
}

func newMediaDTO(media PresentedMedia) mediaDTO {
	return mediaDTO{
		AssetID:      media.AssetID,
		URL:          media.URL,
		URLExpiresAt: formatTime(media.URLExpiresAt),
		MIMEType:     media.MIMEType,
		SourceKind:   media.SourceKind,
		DisplayLabel: media.DisplayLabel,
	}
}

// ---- plan_set（契约 PlanSet）----

type planSetDTO struct {
	ID        string            `json:"id"`
	ReportID  string            `json:"report_id"`
	Scene     string            `json:"scene"`
	Brief     map[string]string `json:"brief"`
	State     string            `json:"state"`
	Variants  []planVariantDTO  `json:"variants"`
	CreatedAt string            `json:"created_at"`
}

type planVariantDTO struct {
	ID             string            `json:"id"`
	Slot           int               `json:"slot"`
	Key            string            `json:"key"`
	Name           string            `json:"name"`
	Descriptor     string            `json:"descriptor"`
	Rationale      string            `json:"rationale"`
	Recommended    bool              `json:"recommended"`
	OutcomeTags    []string          `json:"outcome_tags"`
	DifferenceTags []string          `json:"difference_tags"`
	Steps          []planStepPayload `json:"steps"`
	Render         renderStatusDTO   `json:"render"`
	CreatedAt      string            `json:"created_at"`
}

type renderStatusDTO struct {
	State         string    `json:"state"`
	Retryable     bool      `json:"retryable"`
	OperationID   *string   `json:"operation_id"`
	Media         *mediaDTO `json:"media"`
	RenderRunID   *string   `json:"render_run_id"`
	PublicationID *string   `json:"publication_id"`
}

// planStepPayload 是接口类型，让每个 category 恰好发出自己的 details 键
// （契约是按 category 分的 oneOf）。
type planStepPayload interface{ planStepPayload() }

type planStepCommon struct {
	ID         string             `json:"id"`
	Category   string             `json:"category"`
	Action     string             `json:"action"`
	Title      string             `json:"title"`
	Summary    string             `json:"summary"`
	Position   int                `json:"position"`
	Groundings []planGroundingDTO `json:"groundings"`
	CreatedAt  string             `json:"created_at"`
}

type hairPlanStepDTO struct {
	planStepCommon
	Details appearanceStepDetailsDTO `json:"details"`
}

type makeupPlanStepDTO struct {
	planStepCommon
	Details appearanceStepDetailsDTO `json:"details"`
}

type outfitPlanStepDTO struct {
	planStepCommon
	Details outfitStepDetailsDTO `json:"details"`
}

func (hairPlanStepDTO) planStepPayload()   {}
func (makeupPlanStepDTO) planStepPayload() {}
func (outfitPlanStepDTO) planStepPayload() {}

type appearanceStepDetailsDTO struct {
	Target    string `json:"target"`
	Intensity string `json:"intensity"`
}

type outfitStepDetailsDTO struct {
	Silhouette string   `json:"silhouette"`
	Palette    []string `json:"palette"`
	Layers     []string `json:"layers"`
	Avoid      []string `json:"avoid"`
}

type planGroundingDTO struct {
	SourceType string `json:"source_type"`
	SourceID   string `json:"source_id"`
	Reason     string `json:"reason"`
}

// newPlanSetDTO 把 planning 读模型（渲染视图已合并进各 variant）映射为契约
// PlanSet。无渲染头的 variant 投影为 unavailable 占位，与 plan-sets 端点一致。
func newPlanSetDTO(planSet domain.PlanSet) planSetDTO {
	brief := make(map[string]string, len(planSet.SceneBrief.Answers))
	for name, value := range planSet.SceneBrief.Answers {
		brief[name] = value
	}
	variants := make([]planVariantDTO, 0, len(planSet.Variants))
	for _, variant := range planSet.Variants {
		steps := make([]planStepPayload, 0, len(variant.Steps))
		for _, step := range variant.Steps {
			steps = append(steps, newPlanStepPayload(step))
		}
		variants = append(variants, planVariantDTO{
			ID:             variant.ID,
			Slot:           variant.Slot,
			Key:            string(variant.Key),
			Name:           variant.Name,
			Descriptor:     variant.Descriptor,
			Rationale:      variant.Rationale,
			Recommended:    variant.Recommended,
			OutcomeTags:    nonNilStrings(variant.OutcomeTags),
			DifferenceTags: nonNilStrings(variant.DifferenceTags),
			Steps:          steps,
			Render:         renderStatusOf(variant),
			CreatedAt:      formatTime(variant.CreatedAt),
		})
	}
	return planSetDTO{
		ID:        planSet.ID,
		ReportID:  planSet.ReportID,
		Scene:     string(planSet.Scene),
		Brief:     brief,
		State:     planSetState(planSet),
		Variants:  variants,
		CreatedAt: formatTime(planSet.CreatedAt),
	}
}

func newPlanStepPayload(step domain.PlanStep) planStepPayload {
	common := planStepCommon{
		ID:        step.ID,
		Category:  string(step.Category),
		Action:    string(step.Action),
		Title:     step.Title,
		Summary:   step.Summary,
		Position:  step.Position,
		CreatedAt: formatTime(step.CreatedAt),
	}
	common.Groundings = make([]planGroundingDTO, 0, len(step.Groundings))
	for _, grounding := range step.Groundings {
		common.Groundings = append(common.Groundings, planGroundingDTO{
			SourceType: string(grounding.SourceType),
			SourceID:   grounding.SourceID,
			Reason:     grounding.Reason,
		})
	}
	switch step.Category {
	case domain.StepCategoryMakeup:
		return makeupPlanStepDTO{planStepCommon: common, Details: appearanceStepDetailsDTO{
			Target: step.Details.Target, Intensity: step.Details.Intensity,
		}}
	case domain.StepCategoryOutfit:
		return outfitPlanStepDTO{planStepCommon: common, Details: outfitStepDetailsDTO{
			Silhouette: step.Details.Silhouette,
			Palette:    nonNilStrings(step.Details.Palette),
			Layers:     nonNilStrings(step.Details.Layers),
			Avoid:      nonNilStrings(step.Details.Avoid),
		}}
	default:
		return hairPlanStepDTO{planStepCommon: common, Details: appearanceStepDetailsDTO{
			Target: step.Details.Target, Intensity: step.Details.Intensity,
		}}
	}
}

// renderStatusOf 投影 variant 的当前渲染状态：渲染视图已由 planning 读模型
// 合并（state/operation_id/retryable/render_run_id/publication_id 透传，
// ready 时嵌出已签名媒体）；nil 表示从未触发渲染。
func renderStatusOf(variant domain.PlanVariant) renderStatusDTO {
	if variant.Render == nil {
		return renderStatusDTO{State: "unavailable", Retryable: false}
	}
	return renderStatusDTO{
		State:         variant.Render.State,
		Retryable:     variant.Render.Retryable,
		OperationID:   nilIfEmpty(variant.Render.OperationID),
		RenderRunID:   variant.Render.RenderRunID,
		PublicationID: variant.Render.PublicationID,
		Media:         renderMediaDTO(variant.Render.Media),
	}
}

func renderMediaDTO(media *domain.RenderMediaView) *mediaDTO {
	if media == nil {
		return nil
	}
	return &mediaDTO{
		AssetID:      media.AssetID,
		URL:          media.URL,
		URLExpiresAt: formatTime(media.URLExpiresAt),
		MIMEType:     media.MIMEType,
		SourceKind:   media.SourceKind,
		DisplayLabel: media.DisplayLabel,
	}
}

func planSetState(planSet domain.PlanSet) string {
	if planSet.RenderState != "" {
		return planSet.RenderState
	}
	// 渲染只读端口未装配（如单测）时读模型不带聚合状态，退回 planning。
	return "planning"
}
