package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/daily"
)

// DailyService 是每日内容的最小依赖：两段式生成（prepare/generate）+ 收藏。
//
// 协议纪律：generate 永远返回 200 + 内容（source=generated/fallback），
// 降级对客户端透明——客户端没有「生成失败」分支，只有「内容来源」字段。
type DailyService interface {
	Prepare(ctx context.Context, userID string, city string) (daily.PrepareResult, error)
	Generate(ctx context.Context, userID string, pickToken string) (daily.GenerateResult, error)
	CreateCollection(ctx context.Context, userID string, contentID string, note string) (domain.DailyCollection, error)
	ListCollections(ctx context.Context, userID string, category string, limit int) ([]domain.DailyCollection, error)
	UpdateCollection(ctx context.Context, userID string, id string, status string, note string) (domain.DailyCollection, error)
	DeleteCollection(ctx context.Context, userID string, id string) error
	CollectionStats(ctx context.Context, userID string) (daily.CollectionStats, error)
}

var errDailyUnavailable = errors.New("daily service unavailable")

// ---- DTO（与 contracts/openapi.yaml 的 Daily* schema 一一对应） ----

type dailyPrepareResponse struct {
	GenDate   string `json:"gen_date"`
	PickToken string `json:"pick_token"`
	Scenario  string `json:"scenario"`
	CacheHit  bool   `json:"cache_hit"`
}

type dailyGenerateResponse struct {
	Source  string          `json:"source"`
	Content dailyContentDTO `json:"content"`
}

type dailyContentDTO struct {
	ID        string               `json:"id"`
	Type      string               `json:"type"`
	Topic     string               `json:"topic"`
	Lead      string               `json:"lead"`
	FitText   string               `json:"fit_text"`
	Why       string               `json:"why"`
	Visual    domain.ContentVisual `json:"visual"`
	Asset     string               `json:"asset"`
	DedupeKey string               `json:"dedupe_key"`
}

type dailyCollectionDTO struct {
	ID         string                   `json:"id"`
	ContentID  string                   `json:"content_id"`
	ContentKey string                   `json:"content_key"`
	Category   string                   `json:"category"`
	Status     string                   `json:"status"`
	Note       string                   `json:"note"`
	Title      string                   `json:"title"`
	Summary    string                   `json:"summary"`
	Snapshot   domain.ContentSnapshot   `json:"content_snapshot"`
	Assets     []domain.CollectionAsset `json:"assets"`
	Context    domain.SavedContext      `json:"context"`
	SavedAt    string                   `json:"saved_at"`
	UpdatedAt  string                   `json:"updated_at"`
}

type dailyCollectionStatsDTO struct {
	Counts map[string]int `json:"counts"`
	Total  int            `json:"total"`
}

func toDailyContentDTO(content domain.DailyContent) dailyContentDTO {
	return dailyContentDTO{
		ID:        content.ID,
		Type:      domain.CategoryToContentType(content.Category),
		Topic:     content.Topic,
		Lead:      content.Lead,
		FitText:   content.FitText,
		Why:       content.Why,
		Visual:    content.Visual,
		Asset:     content.Category,
		DedupeKey: content.DedupeKey,
	}
}

func toDailyCollectionDTO(item domain.DailyCollection) dailyCollectionDTO {
	return dailyCollectionDTO{
		ID:         item.ID,
		ContentID:  item.ContentID,
		ContentKey: item.ContentKey,
		Category:   item.Category,
		Status:     item.Status,
		Note:       item.Note,
		Title:      item.Title,
		Summary:    item.Summary,
		Snapshot:   item.Snapshot,
		Assets:     item.Assets,
		Context:    item.Context,
		SavedAt:    item.SavedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:  item.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

func toDailyCollectionDTOs(items []domain.DailyCollection) []dailyCollectionDTO {
	out := make([]dailyCollectionDTO, 0, len(items))
	for _, item := range items {
		out = append(out, toDailyCollectionDTO(item))
	}
	return out
}

// ---- handlers ----

func (a *API) prepareDaily(w http.ResponseWriter, r *http.Request) {
	if a.daily == nil {
		a.internalError(w, r, errDailyUnavailable)
		return
	}
	var input struct {
		City string `json:"city"`
	}
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &input); err != nil {
			writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
			return
		}
	}
	result, err := a.daily.Prepare(r.Context(), currentUser(r).ID, input.City)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, dailyPrepareResponse{
		GenDate:   result.GenDate,
		PickToken: result.PickToken,
		Scenario:  result.Scenario,
		CacheHit:  result.CacheHit,
	})
}

func (a *API) generateDaily(w http.ResponseWriter, r *http.Request) {
	if a.daily == nil {
		a.internalError(w, r, errDailyUnavailable)
		return
	}
	var input struct {
		PickToken string `json:"pick_token"`
	}
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &input); err != nil {
			writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
			return
		}
	}
	result, err := a.daily.Generate(r.Context(), currentUser(r).ID, input.PickToken)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, dailyGenerateResponse{
		Source:  result.Source,
		Content: toDailyContentDTO(result.Content),
	})
}

func (a *API) createDailyCollection(w http.ResponseWriter, r *http.Request) {
	if a.daily == nil {
		a.internalError(w, r, errDailyUnavailable)
		return
	}
	var input struct {
		ContentID string `json:"content_id"`
		Note      string `json:"note"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	item, err := a.daily.CreateCollection(r.Context(), currentUser(r).ID, input.ContentID, input.Note)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, toDailyCollectionDTO(item))
}

func (a *API) listDailyCollection(w http.ResponseWriter, r *http.Request) {
	if a.daily == nil {
		a.internalError(w, r, errDailyUnavailable)
		return
	}
	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			writeError(w, r, http.StatusBadRequest, "validation_error", "limit 不是合法数字")
			return
		}
		limit = parsed
	}
	items, err := a.daily.ListCollections(r.Context(), currentUser(r).ID, r.URL.Query().Get("category"), limit)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, toDailyCollectionDTOs(items))
}

func (a *API) patchDailyCollection(w http.ResponseWriter, r *http.Request) {
	if a.daily == nil {
		a.internalError(w, r, errDailyUnavailable)
		return
	}
	var input struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	item, err := a.daily.UpdateCollection(r.Context(), currentUser(r).ID, r.PathValue("id"), input.Status, input.Note)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, toDailyCollectionDTO(item))
}

func (a *API) deleteDailyCollection(w http.ResponseWriter, r *http.Request) {
	if a.daily == nil {
		a.internalError(w, r, errDailyUnavailable)
		return
	}
	if err := a.daily.DeleteCollection(r.Context(), currentUser(r).ID, r.PathValue("id")); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			// 已删/不存在视为成功：客户端「移出」是幂等动作。
			w.WriteHeader(http.StatusNoContent)
			return
		}
		a.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) getDailyCollectionStats(w http.ResponseWriter, r *http.Request) {
	if a.daily == nil {
		a.internalError(w, r, errDailyUnavailable)
		return
	}
	stats, err := a.daily.CollectionStats(r.Context(), currentUser(r).ID)
	if err != nil {
		a.writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, dailyCollectionStatsDTO{Counts: stats.Counts, Total: stats.Total})
}
