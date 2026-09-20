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

// 收藏：内容引用 + 完整副本（jsonb）+ 多态素材 + 生命周期。
// 检索一律走 category / saved_at / (user_id, content_key)，不解析副本。

var _ daily.CollectionStore = (*Store)(nil)

const dailyCollectionSelectSQL = `
	SELECT id::text, user_id::text, content_id, content_key, content_snapshot,
	       title, summary, category, assets, context, gene_snapshot, status, note, saved_at, updated_at
	FROM daily_collection`

func (s *Store) CreateCollection(ctx context.Context, item domain.DailyCollection) (domain.DailyCollection, error) {
	snapshot, err := json.Marshal(item.Snapshot)
	if err != nil {
		return item, err
	}
	assets, err := json.Marshal(item.Assets)
	if err != nil {
		return item, err
	}
	if item.Assets == nil {
		assets = []byte(`[]`)
	}
	contextJSON, err := json.Marshal(item.Context)
	if err != nil {
		return item, err
	}
	geneJSON, err := marshalJSONMap(item.Gene)
	if err != nil {
		return item, err
	}
	var id string
	var savedAt, updatedAt time.Time
	err = s.pool.QueryRow(ctx, `
		INSERT INTO daily_collection(user_id, content_id, content_key, content_snapshot,
		                             title, summary, category, assets, context, gene_snapshot, status, note)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (user_id, content_key) DO NOTHING
		RETURNING id::text, saved_at, updated_at`,
		item.UserID, item.ContentID, item.ContentKey, snapshot, item.Title, item.Summary,
		item.Category, assets, contextJSON, geneJSON, item.Status, item.Note,
	).Scan(&id, &savedAt, &updatedAt)
	if err == nil {
		item.ID = id
		item.SavedAt = savedAt
		item.UpdatedAt = updatedAt
		return item, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return item, err
	}
	existing, readErr := s.collectionByKey(ctx, item.UserID, item.ContentKey)
	if readErr != nil {
		return item, repository.ErrConflict
	}
	return existing, nil
}

func (s *Store) ListCollections(ctx context.Context, userID string, category string, limit int) ([]domain.DailyCollection, error) {
	rows, err := s.pool.Query(ctx, dailyCollectionSelectSQL+`
		WHERE user_id=$1::uuid AND ($2 = '' OR category = $2)
		ORDER BY saved_at DESC, id DESC
		LIMIT $3`, userID, category, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.DailyCollection{}
	for rows.Next() {
		item, err := scanDailyCollection(rows)
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

func (s *Store) CollectionByID(ctx context.Context, userID string, id string) (domain.DailyCollection, error) {
	item, err := scanDailyCollection(s.pool.QueryRow(ctx, dailyCollectionSelectSQL+`
		WHERE user_id=$1::uuid AND id=$2::uuid`, userID, id))
	if err != nil {
		return item, mapNotFound(err)
	}
	return item, nil
}

func (s *Store) collectionByKey(ctx context.Context, userID string, contentKey string) (domain.DailyCollection, error) {
	item, err := scanDailyCollection(s.pool.QueryRow(ctx, dailyCollectionSelectSQL+`
		WHERE user_id=$1::uuid AND content_key=$2`, userID, contentKey))
	if err != nil {
		return item, mapNotFound(err)
	}
	return item, nil
}

func (s *Store) UpdateCollection(ctx context.Context, userID string, id string, status string, note string) (domain.DailyCollection, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE daily_collection SET
			status = CASE WHEN $3 = '' THEN status ELSE $3 END,
			note   = CASE WHEN $4 = '' THEN note ELSE $4 END,
			updated_at = now()
		WHERE user_id=$1::uuid AND id=$2::uuid`, userID, id, status, note)
	if err != nil {
		return domain.DailyCollection{}, err
	}
	if tag.RowsAffected() == 0 {
		return domain.DailyCollection{}, repository.ErrNotFound
	}
	return s.CollectionByID(ctx, userID, id)
}

func (s *Store) DeleteCollection(ctx context.Context, userID string, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM daily_collection WHERE user_id=$1::uuid AND id=$2::uuid`, userID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return repository.ErrNotFound
	}
	return nil
}

func (s *Store) CountCollections(ctx context.Context, userID string) (map[string]int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT category, count(*) FROM daily_collection WHERE user_id=$1::uuid GROUP BY category`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var category string
		var count int
		if err := rows.Scan(&category, &count); err != nil {
			return nil, err
		}
		counts[category] = count
	}
	return counts, rows.Err()
}

func scanDailyCollection(row dailyCollectionRow) (domain.DailyCollection, error) {
	var item domain.DailyCollection
	var snapshotRaw, assetsRaw, contextRaw, geneRaw []byte
	err := row.Scan(
		&item.ID, &item.UserID, &item.ContentID, &item.ContentKey, &snapshotRaw,
		&item.Title, &item.Summary, &item.Category, &assetsRaw, &contextRaw, &geneRaw,
		&item.Status, &item.Note, &item.SavedAt, &item.UpdatedAt,
	)
	if err != nil {
		return item, err
	}
	if len(snapshotRaw) > 0 {
		if err := json.Unmarshal(snapshotRaw, &item.Snapshot); err != nil {
			return item, err
		}
	}
	if len(assetsRaw) > 0 {
		if err := json.Unmarshal(assetsRaw, &item.Assets); err != nil {
			return item, err
		}
	}
	if len(contextRaw) > 0 {
		if err := json.Unmarshal(contextRaw, &item.Context); err != nil {
			return item, err
		}
	}
	if len(geneRaw) > 0 {
		gene := map[string]string{}
		if err := json.Unmarshal(geneRaw, &gene); err == nil {
			item.Gene = gene
		}
	}
	if item.Assets == nil {
		item.Assets = []domain.CollectionAsset{}
	}
	if item.Snapshot.Visual.Spec == nil {
		item.Snapshot.Visual.Spec = map[string]any{}
	}
	return item, nil
}

type dailyCollectionRow interface {
	Scan(dest ...any) error
}

func marshalJSONMap(value map[string]string) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	return json.Marshal(value)
}
