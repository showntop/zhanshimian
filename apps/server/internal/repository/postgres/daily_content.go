package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/daily"
)

// 每日内容产物：每天每用户一条（(user_id, gen_date) 唯一），
// user_id 为空的行是公共兜底池——降级链的最后一级就靠它。
//
// 越权口径：所有读写都带 user_id；别人的行与不存在的行一律 ErrNotFound。

var _ daily.ContentStore = (*Store)(nil)

const dailyContentSelectSQL = `
	SELECT id::text, COALESCE(user_id::text,''), gen_date::text, category, topic, lead, fit_text, why,
	       visual, COALESCE(lock, '{}'::jsonb), fact_ids, source, model_key, dedupe_key, created_at
	FROM daily_content`

func (s *Store) TodayContent(ctx context.Context, userID string, genDate string) (domain.DailyContent, error) {
	return s.scanDailyContent(s.pool.QueryRow(ctx, dailyContentSelectSQL+`
		WHERE user_id=$1::uuid AND gen_date=$2::date`, userID, genDate))
}

// ContentByID 收藏时取内容：本人生成的行，或公共兜底池的行。
func (s *Store) ContentByID(ctx context.Context, userID string, id string) (domain.DailyContent, error) {
	return s.scanDailyContent(s.pool.QueryRow(ctx, dailyContentSelectSQL+`
		WHERE id=$1::uuid AND (user_id=$2::uuid OR user_id IS NULL)`, id, userID))
}

func (s *Store) FallbackPool(ctx context.Context) ([]domain.DailyContent, error) {
	rows, err := s.pool.Query(ctx, dailyContentSelectSQL+`
		WHERE user_id IS NULL AND source='fallback'
		ORDER BY category, dedupe_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.DailyContent{}
	for rows.Next() {
		item, err := scanDailyContentRows(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

// RecentContents 近 N 天已生成的内容（新到旧，至多 limit 条）：
// prompt 历史 + topic 去重闸的数据源。
func (s *Store) RecentContents(ctx context.Context, userID string, since time.Time, limit int) ([]domain.DailyContent, error) {
	rows, err := s.pool.Query(ctx, dailyContentSelectSQL+`
		WHERE user_id=$1::uuid AND created_at >= $2
		ORDER BY created_at DESC LIMIT $3`, userID, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.DailyContent{}
	for rows.Next() {
		item, err := scanDailyContentRows(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// HistoryContents 该用户生成过的每日内容（按 gen_date 倒序，至多 limit 条）。
// 走 daily_content_user_idx (user_id, gen_date DESC)；公共兜底池行
// （user_id IS NULL）不属于任何用户，天然被 WHERE 挡掉。
func (s *Store) HistoryContents(ctx context.Context, userID string, limit int) ([]domain.DailyContent, error) {
	rows, err := s.pool.Query(ctx, dailyContentSelectSQL+`
		WHERE user_id=$1::uuid
		ORDER BY gen_date DESC, created_at DESC
		LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.DailyContent{}
	for rows.Next() {
		item, err := scanDailyContentRows(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// RecentContentKeys 近 N 天已推过的内容 key（兜底池去重也用它）。
func (s *Store) RecentContentKeys(ctx context.Context, userID string, since time.Time) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT dedupe_key FROM daily_content
		WHERE user_id=$1::uuid AND created_at >= $2`, userID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := []string{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

// SaveContent 落库当日内容；撞 (user_id, gen_date) 时返回已存在的那条
// （幂等：同一用户同一天只生成一次）。
func (s *Store) SaveContent(ctx context.Context, content domain.DailyContent) (domain.DailyContent, error) {
	visual, err := json.Marshal(content.Visual)
	if err != nil {
		return content, err
	}
	if content.Visual.Spec == nil {
		visual = []byte(`{}`)
	}
	lock, err := marshalLock(content.Lock)
	if err != nil {
		return content, err
	}
	var id string
	var createdAt time.Time
	err = s.pool.QueryRow(ctx, `
		INSERT INTO daily_content(user_id, gen_date, category, topic, lead, fit_text, why, visual, lock, fact_ids, source, model_key, dedupe_key)
		VALUES (NULLIF($1,'')::uuid, $2::date, $3, $4, $5, $6, $7, $8, $9, $10::uuid[], $11, $12, $13)
		ON CONFLICT (user_id, gen_date) WHERE user_id IS NOT NULL DO NOTHING
		RETURNING id::text, created_at`,
		content.UserID, content.GenDate, content.Category, content.Topic, content.Lead, content.FitText,
		content.Why, visual, lock, normalizeIDs(content.FactIDs), content.Source, content.ModelKey, content.DedupeKey,
	).Scan(&id, &createdAt)
	if err == nil {
		content.ID = id
		content.CreatedAt = createdAt
		return content, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return content, err
	}
	// 已存在（并发或当天重复调用）：读回那条，不把它当失败。
	existing, readErr := s.TodayContent(ctx, content.UserID, content.GenDate)
	if readErr != nil {
		return content, repository.ErrConflict
	}
	return existing, nil
}

// ReplaceContent 覆盖当天内容（upsert）：调试开关 DAILY_FORCE_REGEN 专用，
// 让同一天可以反复重生成并看到新内容。正常链路走 SaveContent（DO NOTHING）。
func (s *Store) ReplaceContent(ctx context.Context, content domain.DailyContent) (domain.DailyContent, error) {
	visual, err := json.Marshal(content.Visual)
	if err != nil {
		return content, err
	}
	if content.Visual.Spec == nil {
		visual = []byte(`{}`)
	}
	lock, err := marshalLock(content.Lock)
	if err != nil {
		return content, err
	}
	var id string
	var createdAt time.Time
	err = s.pool.QueryRow(ctx, `
		INSERT INTO daily_content(user_id, gen_date, category, topic, lead, fit_text, why, visual, lock, fact_ids, source, model_key, dedupe_key)
		VALUES (NULLIF($1,'')::uuid, $2::date, $3, $4, $5, $6, $7, $8, $9, $10::uuid[], $11, $12, $13)
		ON CONFLICT (user_id, gen_date) WHERE user_id IS NOT NULL DO UPDATE SET
			category=EXCLUDED.category, topic=EXCLUDED.topic, lead=EXCLUDED.lead,
			fit_text=EXCLUDED.fit_text, why=EXCLUDED.why, visual=EXCLUDED.visual, lock=EXCLUDED.lock,
			fact_ids=EXCLUDED.fact_ids, source=EXCLUDED.source,
			model_key=EXCLUDED.model_key, dedupe_key=EXCLUDED.dedupe_key
		RETURNING id::text, created_at`,
		content.UserID, content.GenDate, content.Category, content.Topic, content.Lead, content.FitText,
		content.Why, visual, lock, normalizeIDs(content.FactIDs), content.Source, content.ModelKey, content.DedupeKey,
	).Scan(&id, &createdAt)
	if err != nil {
		return content, mapNotFound(err)
	}
	content.ID = id
	content.CreatedAt = createdAt
	return content, nil
}

func (s *Store) scanDailyContent(row pgx.Row) (domain.DailyContent, error) {
	content, err := scanDailyContentRows(row)
	if err != nil {
		return content, mapNotFound(err)
	}
	return content, nil
}

type dailyContentRow interface {
	Scan(dest ...any) error
}

// marshalLock 定格参数落库：全空时存 NULL（区分「没有」与「全回退」）。
func marshalLock(lock *domain.ContentLock) ([]byte, error) {
	if lock == nil || (lock.Look == "" && lock.Color == "" && lock.Waist == "" && lock.Hair == "") {
		return nil, nil
	}
	return json.Marshal(lock)
}

func scanDailyContentRows(row dailyContentRow) (domain.DailyContent, error) {
	var content domain.DailyContent
	var visualRaw []byte
	var lockRaw []byte
	err := row.Scan(
		&content.ID, &content.UserID, &content.GenDate, &content.Category, &content.Topic,
		&content.Lead, &content.FitText, &content.Why, &visualRaw, &lockRaw, &content.FactIDs,
		&content.Source, &content.ModelKey, &content.DedupeKey, &content.CreatedAt,
	)
	if err != nil {
		return content, err
	}
	if len(lockRaw) > 0 {
		var lock domain.ContentLock
		if err := json.Unmarshal(lockRaw, &lock); err != nil {
			return content, err
		}
		if lock.Look != "" || lock.Color != "" || lock.Waist != "" || lock.Hair != "" {
			content.Lock = &lock
		}
	}
	spec := map[string]any{}
	if len(visualRaw) > 0 {
		visual := struct {
			Modality string         `json:"modality"`
			Spec     map[string]any `json:"spec"`
			Alt      string         `json:"alt"`
		}{}
		if err := json.Unmarshal(visualRaw, &visual); err != nil {
			return content, err
		}
		if visual.Spec != nil {
			spec = visual.Spec
		}
		content.Visual = domain.ContentVisual{Modality: visual.Modality, Spec: spec, Alt: visual.Alt}
	}
	if content.Visual.Spec == nil {
		content.Visual.Spec = map[string]any{}
	}
	if content.FactIDs == nil {
		content.FactIDs = []string{}
	}
	return content, nil
}
