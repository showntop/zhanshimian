package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/hair"
)

// HairService 是发型预览的最小依赖。
type HairService interface {
	CreatePreview(ctx context.Context, userID string, input hair.CreatePreviewInput) (hair.Preview, domain.OperationRef, error)
	Get(ctx context.Context, userID string, id string) (hair.Preview, error)
	Active(ctx context.Context, userID string) (hair.Preview, error)
	List(ctx context.Context, userID string, filter hair.ListFilter) ([]hair.Preview, error)
	Save(ctx context.Context, userID string, id string) (hair.Preview, error)
	Recommend(ctx context.Context, userID string, reportID string) ([]domain.HairStyle, error)
}

var errHairUnavailable = errors.New("hair service unavailable")

// POST /v1/hair-previews —— 202 {data, operation}：异步发型预览；media 恒为 null，
// 图像生成落库前不可见。
func (a *API) createHairPreview(w http.ResponseWriter, r *http.Request) {
	if a.hair == nil {
		a.internalError(w, r, errHairUnavailable)
		return
	}
	var input struct {
		MediaID  string `json:"media_id"`
		StyleID  string `json:"style_id"`
		ReportID string `json:"report_id"`
		Scene    string `json:"scene"` // 契约选填；当前不影响生成，收下即忽略
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	preview, operation, err := a.hair.CreatePreview(r.Context(), currentUser(r).ID, hair.CreatePreviewInput{
		ReportID: input.ReportID, MediaAssetID: input.MediaID, StyleID: input.StyleID,
	})
	if errors.Is(err, hair.ErrFaceMissing) {
		writeError(w, r, http.StatusBadRequest, "validation_error", "发型预览需要一张正脸照")
		return
	}
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeDataOperation(w, http.StatusAccepted, preview, operationDTO{
		ID: operation.ID, Kind: string(operation.Kind), Status: string(operation.Status),
	})
}

func (a *API) getHairPreview(w http.ResponseWriter, r *http.Request) {
	if a.hair == nil {
		a.internalError(w, r, errHairUnavailable)
		return
	}
	preview, err := a.hair.Get(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, preview)
}

// GET /v1/hair-previews/active —— 当前进行中的预览（无则 404）：
// 客户端本地引用丢失时据此恢复任务展示。
func (a *API) getActiveHairPreview(w http.ResponseWriter, r *http.Request) {
	if a.hair == nil {
		a.internalError(w, r, errHairUnavailable)
		return
	}
	preview, err := a.hair.Active(r.Context(), currentUser(r).ID)
	if errors.Is(err, repository.ErrNotFound) {
		writeError(w, r, http.StatusNotFound, "not_found", "没有进行中的发型预览")
		return
	}
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, preview)
}

// GET /v1/hair-previews —— 契约过滤：无参返回全部历史（含进行中），
// saved=true/false 按保存标记过滤，state=active 只返回进行中。
func (a *API) listHairPreviews(w http.ResponseWriter, r *http.Request) {
	if a.hair == nil {
		a.internalError(w, r, errHairUnavailable)
		return
	}
	filter, ok := parseHairListFilter(w, r)
	if !ok {
		return
	}
	items, err := a.hair.List(r.Context(), currentUser(r).ID, filter)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func parseHairListFilter(w http.ResponseWriter, r *http.Request) (hair.ListFilter, bool) {
	var filter hair.ListFilter
	query := r.URL.Query()
	if raw := query.Get("saved"); raw != "" {
		saved, err := strconv.ParseBool(raw)
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "validation_error", "saved 参数格式不正确")
			return hair.ListFilter{}, false
		}
		filter.Saved = &saved
	}
	if raw := query.Get("state"); raw != "" {
		if raw != "active" {
			writeError(w, r, http.StatusBadRequest, "validation_error", "state 参数只支持 active")
			return hair.ListFilter{}, false
		}
		filter.Active = true
	}
	return filter, true
}

func (a *API) saveHairPreview(w http.ResponseWriter, r *http.Request) {
	if a.hair == nil {
		a.internalError(w, r, errHairUnavailable)
		return
	}
	preview, err := a.hair.Save(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, preview)
}

// GET /v1/hairstyles —— 发型目录（纯读）。
func (a *API) listHairstyles(w http.ResponseWriter, r *http.Request) {
	if a.hair == nil {
		a.internalError(w, r, errHairUnavailable)
		return
	}
	items, err := a.hair.Recommend(r.Context(), currentUser(r).ID, r.URL.Query().Get("report_id"))
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}
