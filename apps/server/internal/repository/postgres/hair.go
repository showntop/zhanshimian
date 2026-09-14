package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/hair"
)

var (
	_ hair.Writer     = (*Store)(nil)
	_ hair.Hairstyler = (*Store)(nil)
)

const hairPreviewSelectSQL = `
	SELECT hp.id::text, hp.style_id, hp.style_name, hp.state, hp.retryable,
	       COALESCE(op.id::text,''), COALESCE(op.kind::text,''), COALESCE(op.status::text,''),
	       src_ma.id::text,
	       CASE src_ma.origin
	         WHEN 'user_upload' THEN 'user_original'
	         WHEN 'provider_output' THEN 'generated_preview'
	         WHEN 'bundled_reference' THEN 'bundled_reference'
	         WHEN 'demo' THEN 'demo_example'
	         ELSE ''
	       END,
	       CASE src_ma.display_kind
	         WHEN 'original' THEN '原本'
	         WHEN 'generated_reference' THEN '风格参考'
	         WHEN 'effect_example' THEN '效果示例'
	         WHEN 'style_reference' THEN '风格参考'
	         ELSE ''
	       END,
	       pub_ma.id::text,
	       CASE pub_ma.origin
	         WHEN 'user_upload' THEN 'user_original'
	         WHEN 'provider_output' THEN 'generated_preview'
	         WHEN 'bundled_reference' THEN 'bundled_reference'
	         WHEN 'demo' THEN 'demo_example'
	         ELSE ''
	       END,
	       CASE pub_ma.display_kind
	         WHEN 'original' THEN '原本'
	         WHEN 'generated_reference' THEN '风格参考'
	         WHEN 'effect_example' THEN '效果示例'
	         WHEN 'style_reference' THEN '风格参考'
	         ELSE ''
	       END,
	       hp.saved, hp.created_at, hp.updated_at
	FROM hair_previews hp
	LEFT JOIN operations op ON op.user_id=hp.user_id AND op.id=hp.operation_id
	LEFT JOIN media_assets src_ma ON src_ma.user_id=hp.user_id AND src_ma.id=hp.source_media_asset_id
	LEFT JOIN render_publications rp ON rp.user_id=hp.user_id AND rp.id=hp.render_publication_id
	LEFT JOIN render_candidates rc ON rc.user_id=rp.user_id AND rc.id=rp.candidate_id
	LEFT JOIN media_assets pub_ma ON pub_ma.user_id=rc.user_id AND pub_ma.id=rc.asset_id`

func (s *Store) InsertHairPreview(ctx context.Context, userID string, preview hair.Preview) (hair.Preview, error) {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO hair_previews(user_id, style_id, source_media_asset_id, state)
		VALUES ($1::uuid, $2, NULLIF($3,'')::uuid, $4)
		RETURNING id::text, created_at, updated_at`,
		userID, preview.StyleID, preview.SourceMedia.AssetID, preview.State).
		Scan(&preview.ID, &preview.CreatedAt, &preview.UpdatedAt)
	return preview, mapNotFound(err)
}

func (s *Store) GetHairPreviewRow(ctx context.Context, userID string, id string) (hair.Preview, error) {
	return s.scanHairPreview(s.pool.QueryRow(ctx, hairPreviewSelectSQL+` WHERE hp.user_id=$1::uuid AND hp.id=$2::uuid`, userID, id))
}

func (s *Store) GetInFlightHairPreview(ctx context.Context, userID string) (hair.Preview, error) {
	return s.scanHairPreview(s.pool.QueryRow(ctx, hairPreviewSelectSQL+`
		WHERE hp.user_id=$1::uuid AND hp.state IN ('queued','generating','checking')
		ORDER BY hp.created_at DESC LIMIT 1`, userID))
}

func (s *Store) ListSavedHairPreviewRows(ctx context.Context, userID string) ([]hair.Preview, error) {
	rows, err := s.pool.Query(ctx, hairPreviewSelectSQL+`
		WHERE hp.user_id=$1::uuid AND hp.saved
		ORDER BY hp.updated_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []hair.Preview{}
	for rows.Next() {
		preview, err := s.scanHairPreview(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, preview)
	}
	return out, rows.Err()
}

func (s *Store) MarkHairPreviewSavedByID(ctx context.Context, userID string, id string) (hair.Preview, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE hair_previews SET saved=true, updated_at=now() WHERE user_id=$1::uuid AND id=$2::uuid`, userID, id)
	if err != nil {
		return hair.Preview{}, err
	}
	if tag.RowsAffected() == 0 {
		return hair.Preview{}, repository.ErrNotFound
	}
	return s.GetHairPreviewRow(ctx, userID, id)
}

func (s *Store) scanHairPreview(row rowScanner) (hair.Preview, error) {
	var preview hair.Preview
	var opID, opKind, opStatus *string
	var srcAssetID, srcSourceKind, srcDisplayLabel *string
	var pubAssetID, pubSourceKind, pubDisplayLabel *string
	err := row.Scan(&preview.ID, &preview.StyleID, &preview.StyleName, &preview.State, &preview.Retryable,
		&opID, &opKind, &opStatus,
		&srcAssetID, &srcSourceKind, &srcDisplayLabel,
		&pubAssetID, &pubSourceKind, &pubDisplayLabel,
		&preview.Saved, &preview.CreatedAt, &preview.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return preview, repository.ErrNotFound
	}
	if err != nil {
		return preview, err
	}
	if opID != nil && *opID != "" {
		preview.Operation = domain.OperationRef{ID: *opID, Kind: domain.OperationKind(*opKind), Status: domain.OperationStatus(*opStatus)}
	}
	if srcAssetID != nil && *srcAssetID != "" {
		preview.SourceMedia = &domain.RenderMediaView{
			AssetID: *srcAssetID, SourceKind: deref(srcSourceKind), DisplayLabel: deref(srcDisplayLabel),
		}
	}
	if pubAssetID != nil && *pubAssetID != "" {
		preview.Media = &domain.RenderMediaView{
			AssetID: *pubAssetID, SourceKind: deref(pubSourceKind), DisplayLabel: deref(pubDisplayLabel),
		}
	}
	return preview, nil
}

// ListHairstyles 返回内置发型目录（稳定的 id/name/media；reason 供文案展示）。
// 推荐 AI 能力接入前的确定性实现：目录本身即产品事实，不做个性化排序伪装。
func (s *Store) RecommendHairstyles(ctx context.Context, userID string) ([]domain.HairStyle, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT ma.id::text, ma.mime_type,
		       CASE ma.origin
		         WHEN 'user_upload' THEN 'user_original'
		         WHEN 'provider_output' THEN 'generated_preview'
		         WHEN 'bundled_reference' THEN 'bundled_reference'
		         WHEN 'demo' THEN 'demo_example'
		       END,
		       CASE ma.display_kind
		         WHEN 'original' THEN '原本'
		         WHEN 'generated_reference' THEN '风格参考'
		         WHEN 'effect_example' THEN '效果示例'
		         WHEN 'style_reference' THEN '风格参考'
		       END,
		       h.style_key, h.style_name, COALESCE(h.reason,'')
		FROM hairstyles h
		JOIN media_assets ma ON ma.id = h.media_asset_id
		WHERE ma.deleted_at IS NULL
		ORDER BY h.display_order`)
	if errors.Is(err, pgx.ErrNoRows) {
		return []domain.HairStyle{}, nil
	}
	if err != nil {
		// hairstyles 目录表在当前 baseline 中不存在：返回空目录而非失败。
		return []domain.HairStyle{}, nil
	}
	defer rows.Close()
	out := []domain.HairStyle{}
	for rows.Next() {
		var style domain.HairStyle
		if err := rows.Scan(&style.Media.AssetID, &style.Media.MIMEType, &style.Media.SourceKind,
			&style.Media.DisplayLabel, &style.ID, &style.Name, &style.Reason); err != nil {
			return nil, err
		}
		out = append(out, style)
	}
	return out, rows.Err()
}

// StartPreviewOperation 创建 hair_preview 的公开 Operation（kind=render）。
// Worker 侧的 hair 生成 handler 接入渲染管线后，任务创建由该 handler 的
// registry 驱动；当前先保证 Operation 可轮询（状态机与渲染一致）。
func (s *Store) StartPreviewOperation(ctx context.Context, userID string, previewID string) (domain.OperationRef, error) {
	var ref domain.OperationRef
	err := s.pool.QueryRow(ctx, `
		INSERT INTO operations(user_id, kind, subject_type, subject_id, status)
		VALUES ($1::uuid, 'render', 'hair_preview', $2::uuid, 'accepted')
		RETURNING id::text, kind, status`, userID, previewID).
		Scan(&ref.ID, &ref.Kind, &ref.Status)
	if err != nil {
		return domain.OperationRef{}, err
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE hair_previews SET operation_id=$3::uuid, state='queued', updated_at=now()
		WHERE user_id=$1::uuid AND id=$2::uuid`, userID, previewID, ref.ID); err != nil {
		return domain.OperationRef{}, err
	}
	return ref, nil
}
