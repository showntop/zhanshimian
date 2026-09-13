package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/planning"
)

// planSetService is the narrow Planning surface the transport layer may call.
type planSetService interface {
	CreatePlanSet(ctx context.Context, cmd planning.CreateCommand) (planning.CreateResult, error)
	GetPlanSet(ctx context.Context, userID, planSetID string) (domain.PlanSet, error)
	ListPlanSets(ctx context.Context, userID, reportID string, scene *domain.Scene) ([]domain.PlanSet, error)
}

type createPlanSetRequest struct {
	ReportID string            `json:"report_id"`
	Scene    domain.Scene      `json:"scene"`
	Brief    map[string]string `json:"brief"`
}

func (a *API) createPlanSet(w http.ResponseWriter, r *http.Request) {
	if a.planning == nil {
		a.internalError(w, r, errPlanningUnavailable)
		return
	}
	var input createPlanSetRequest
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", "请求内容格式不正确")
		return
	}
	result, err := a.planning.CreatePlanSet(r.Context(), planning.CreateCommand{
		UserID:         currentUser(r).ID,
		ReportID:       input.ReportID,
		Scene:          input.Scene,
		Answers:        input.Brief,
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
	})
	if err != nil {
		a.writePlanSetError(w, r, err)
		return
	}
	if !result.Accepted {
		// Semantic key already published: return the immutable plan set.
		writeData(w, http.StatusOK, publicPlanSet(*result.PlanSet))
		return
	}
	writeDataWithOperation(w, http.StatusAccepted, planSetAcceptedData{
		ID: result.PlanSetID, State: "planning",
	}, operationRefDTO{
		ID:     result.Operation.ID,
		Kind:   string(result.Operation.Kind),
		Status: string(result.Operation.Status),
	})
}

// writeDataWithOperation encodes a 202 envelope whose operation is already a
// public operation reference (no internal task fields exist yet).
func writeDataWithOperation(w http.ResponseWriter, status int, data any, ref operationRefDTO) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "operation": ref})
}

func (a *API) getPlanSet(w http.ResponseWriter, r *http.Request) {
	if a.planning == nil {
		a.internalError(w, r, errPlanningUnavailable)
		return
	}
	planSet, err := a.planning.GetPlanSet(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		a.writePlanSetError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, publicPlanSet(planSet))
}

func (a *API) listPlanSets(w http.ResponseWriter, r *http.Request) {
	if a.planning == nil {
		a.internalError(w, r, errPlanningUnavailable)
		return
	}
	query := r.URL.Query()
	reportID := strings.TrimSpace(query.Get("report_id"))
	if reportID == "" {
		writeError(w, r, http.StatusBadRequest, "validation_error", "report_id 为必填项")
		return
	}
	var scene *domain.Scene
	if raw := strings.TrimSpace(query.Get("scene")); raw != "" {
		value := domain.Scene(raw)
		scene = &value
	}
	sets, err := a.planning.ListPlanSets(r.Context(), currentUser(r).ID, reportID, scene)
	if err != nil {
		a.writePlanSetError(w, r, err)
		return
	}
	items := make([]planSetResponse, 0, len(sets))
	for _, planSet := range sets {
		items = append(items, publicPlanSet(planSet))
	}
	writeData(w, http.StatusOK, items)
}

func (a *API) writePlanSetError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, planning.ErrInvalidBrief) {
		writeError(w, r, http.StatusBadRequest, "invalid_brief", "场景答案不完整或有误，请检查后重试")
		return
	}
	if errors.Is(err, repository.ErrNotFound) {
		writeError(w, r, http.StatusNotFound, "not_found", "没有找到对应内容")
		return
	}
	a.internalError(w, r, err)
}

var errPlanningUnavailable = errors.New("planning service unavailable")

// ---- public DTOs (frozen contract shapes) ----

type planSetAcceptedData struct {
	ID    string `json:"id"`
	State string `json:"state"`
}

type operationRefDTO struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
}

type planSetResponse struct {
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
	State         string           `json:"state"`
	Retryable     bool             `json:"retryable"`
	OperationID   *string          `json:"operation_id"`
	Media         *displayMediaDTO `json:"media"`
	RenderRunID   *string          `json:"render_run_id"`
	PublicationID *string          `json:"publication_id"`
}

type displayMediaDTO struct {
	AssetID      string `json:"asset_id"`
	URL          string `json:"url"`
	URLExpiresAt string `json:"url_expires_at"`
	MIMEType     string `json:"mime_type"`
	SourceKind   string `json:"source_kind"`
	DisplayLabel string `json:"display_label"`
}

// planStepPayload is an interface so each category emits exactly its own
// details keys — the contract uses a oneOf with per-category detail shapes.
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
	Details hairStepDetailsDTO `json:"details"`
}

type makeupPlanStepDTO struct {
	planStepCommon
	Details makeupStepDetailsDTO `json:"details"`
}

type outfitPlanStepDTO struct {
	planStepCommon
	Details outfitStepDetailsDTO `json:"details"`
}

func (hairPlanStepDTO) planStepPayload()   {}
func (makeupPlanStepDTO) planStepPayload() {}
func (outfitPlanStepDTO) planStepPayload() {}

type hairStepDetailsDTO struct {
	Target    string `json:"target"`
	Intensity string `json:"intensity"`
}

type makeupStepDetailsDTO struct {
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

func publicPlanSet(planSet domain.PlanSet) planSetResponse {
	brief := make(map[string]string, len(planSet.SceneBrief.Answers))
	for name, value := range planSet.SceneBrief.Answers {
		brief[name] = value
	}
	variants := make([]planVariantDTO, 0, len(planSet.Variants))
	for _, variant := range planSet.Variants {
		steps := make([]planStepPayload, 0, len(variant.Steps))
		for _, step := range variant.Steps {
			steps = append(steps, publicPlanStep(step))
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
			Render:         planningRenderStatus(),
			CreatedAt:      formatPublicTime(variant.CreatedAt),
		})
	}
	return planSetResponse{
		ID:        planSet.ID,
		ReportID:  planSet.ReportID,
		Scene:     string(planSet.Scene),
		Brief:     brief,
		State:     planSetState(planSet),
		Variants:  variants,
		CreatedAt: formatPublicTime(planSet.CreatedAt),
	}
}

func publicPlanStep(step domain.PlanStep) planStepPayload {
	common := planStepCommon{
		ID:        step.ID,
		Category:  string(step.Category),
		Action:    string(step.Action),
		Title:     step.Title,
		Summary:   step.Summary,
		Position:  step.Position,
		CreatedAt: formatPublicTime(step.CreatedAt),
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
	case domain.CategoryMakeup:
		return makeupPlanStepDTO{planStepCommon: common, Details: makeupStepDetailsDTO{
			Target: step.Details.Target, Intensity: step.Details.Intensity,
		}}
	case domain.CategoryOutfit:
		return outfitPlanStepDTO{planStepCommon: common, Details: outfitStepDetailsDTO{
			Silhouette: step.Details.Silhouette,
			Palette:    nonNilStrings(step.Details.Palette),
			Layers:     nonNilStrings(step.Details.Layers),
			Avoid:      nonNilStrings(step.Details.Avoid),
		}}
	default:
		return hairPlanStepDTO{planStepCommon: common, Details: hairStepDetailsDTO{
			Target: step.Details.Target, Intensity: step.Details.Intensity,
		}}
	}
}

// planningRenderStatus reports the pre-rendering state: the Rendering plan
// fills operation/media/publication fields once it consumes the RenderSpec.
func planningRenderStatus() renderStatusDTO {
	return renderStatusDTO{State: "unavailable", Retryable: false}
}

// renderStatusOf projects the variant's current rendering state from the
// planning read model merged by the service layer.
func renderStatusOf(variant domain.PlanVariant) renderStatusDTO {
	if variant.RenderState == "" {
		return planningRenderStatus()
	}
	return renderStatusDTO{
		State:       variant.RenderState,
		Retryable:   false,
		OperationID: nilIfEmpty(variant.RenderOperationID),
		// media 由渲染媒体查询单独返回;方案列表内不嵌签名 URL。
	}
}

func planSetState(planSet domain.PlanSet) string {
	if planSet.RenderState != "" {
		return planSet.RenderState
	}
	// 渲染尚未开始:刚发布仍是 planning 阶段。
	return "planning"
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
