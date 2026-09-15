package postgres

import (
	"context"
	"encoding/json"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/wardrobe"
)

var _ wardrobe.Writer = (*Store)(nil)

func (s *Store) GetWardrobeItems(ctx context.Context, userID string) ([]wardrobe.Item, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT wi.id::text, wi.name, wi.category, wi.color, wi.season, wi.formality,
		       wi.scenes, wi.favorite, wi.wear_count, wi.created_at, wi.updated_at,
		       ma.id::text, ma.mime_type, COALESCE(ma.object_key, ''),
		       CASE ma.origin
		         WHEN 'user_upload' THEN 'user_original'
		         WHEN 'provider_output' THEN 'generated_preview'
		         WHEN 'bundled_reference' THEN 'bundled_reference'
		         WHEN 'demo' THEN 'demo_example'
		         ELSE ''
		       END,
		       CASE ma.display_kind
		         WHEN 'original' THEN '原本'
		         WHEN 'generated_reference' THEN '风格参考'
		         WHEN 'effect_example' THEN '效果示例'
		         WHEN 'style_reference' THEN '风格参考'
		         ELSE ''
		       END
		FROM wardrobe_items wi
		LEFT JOIN media_assets ma ON ma.user_id=wi.user_id AND ma.id=wi.media_asset_id
		WHERE wi.user_id=$1::uuid
		ORDER BY wi.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []wardrobe.Item{}
	for rows.Next() {
		var item wardrobe.Item
		var mediaAssetID, mediaMIME, sourceKind, displayLabel *string
		if err := rows.Scan(&item.ID, &item.Name, &item.Category, &item.Color, &item.Season, &item.Formality,
			&item.Scenes, &item.Favorite, &item.WearCount, &item.CreatedAt, &item.UpdatedAt,
			&mediaAssetID, &mediaMIME, &item.MediaObjectKey, &sourceKind, &displayLabel); err != nil {
			return nil, err
		}
		if mediaAssetID != nil && *mediaAssetID != "" {
			item.Media = &domain.RenderMediaView{
				AssetID: *mediaAssetID, MIMEType: deref(mediaMIME), SourceKind: deref(sourceKind), DisplayLabel: deref(displayLabel),
			}
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) InsertWardrobeItem(ctx context.Context, userID string, item wardrobe.Item) (wardrobe.Item, error) {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO wardrobe_items(user_id, media_asset_id, name, category, color, season, formality, scenes)
		VALUES ($1::uuid, NULLIF($2,'')::uuid, $3, $4, $5, $6, $7, $8)
		RETURNING id::text, created_at, updated_at`,
		userID, item.MediaAssetID, item.Name, item.Category, item.Color, item.Season, item.Formality, item.Scenes).
		Scan(&item.ID, &item.CreatedAt, &item.UpdatedAt)
	return item, mapNotFound(err)
}

func (s *Store) RemoveWardrobeItem(ctx context.Context, userID string, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM wardrobe_items WHERE user_id=$1::uuid AND id=$2::uuid`, userID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return repository.ErrNotFound
	}
	return nil
}

func (s *Store) InsertWardrobeOutfit(ctx context.Context, userID string, outfit wardrobe.Outfit) (wardrobe.Outfit, error) {
	contextJSON, err := json.Marshal(outfit.Context)
	if err != nil {
		return outfit, err
	}
	err = s.pool.QueryRow(ctx, `
		INSERT INTO wardrobe_outfits(user_id, title, note, context, item_ids, selected_plan_id)
		VALUES ($1::uuid, $2, $3, $4, $5::uuid[], NULLIF($6,'')::uuid)
		RETURNING id::text, created_at`,
		userID, outfit.Title, outfit.Note, contextJSON, outfit.ItemIDs, outfit.SelectedPlanID).
		Scan(&outfit.ID, &outfit.CreatedAt)
	return outfit, mapNotFound(err)
}

func (s *Store) SetWardrobeOutfitWorn(ctx context.Context, userID string, id string) (wardrobe.Outfit, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE wardrobe_outfits SET worn=true WHERE user_id=$1::uuid AND id=$2::uuid`, userID, id)
	if err != nil {
		return wardrobe.Outfit{}, err
	}
	if tag.RowsAffected() == 0 {
		return wardrobe.Outfit{}, repository.ErrNotFound
	}
	var outfit wardrobe.Outfit
	if err := s.pool.QueryRow(ctx, `
		SELECT id::text, title, note, context, item_ids, worn, created_at
		FROM wardrobe_outfits WHERE user_id=$1::uuid AND id=$2::uuid`, userID, id).
		Scan(&outfit.ID, &outfit.Title, &outfit.Note, &outfit.Context, &outfit.ItemIDs, &outfit.Worn, &outfit.CreatedAt); err != nil {
		return wardrobe.Outfit{}, err
	}
	return outfit, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
