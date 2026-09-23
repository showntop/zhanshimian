package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/zhanshimian/server/internal/domain"
)

func (s *Store) GetReadyAssets(ctx context.Context, userID string, ids []string) ([]domain.MediaAsset, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, user_id::text, origin, purpose, object_key, sha256, mime_type,
		       byte_size, state, display_kind, created_at
		FROM media_assets
		WHERE user_id=$1::uuid AND id=ANY($2::uuid[]) AND state='ready' AND deleted_at IS NULL`,
		userID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.MediaAsset{}
	for rows.Next() {
		var asset domain.MediaAsset
		if err := rows.Scan(&asset.ID, &asset.UserID, &asset.Origin, &asset.Purpose, &asset.ObjectKey,
			&asset.SHA256, &asset.MIMEType, &asset.ByteSize, &asset.State, &asset.DisplayKind, &asset.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, asset)
	}
	return out, rows.Err()
}

// Snapshot 序列化当前 user_profiles 为建档时的档案快照；未建档时返回空对象。
func (s *Store) Snapshot(ctx context.Context, userID string) (json.RawMessage, error) {
	var role string
	var heightCM int
	var budget string
	err := s.pool.QueryRow(ctx, `
		SELECT role, COALESCE(height_cm,0), budget FROM user_profiles WHERE user_id=$1::uuid`, userID).
		Scan(&role, &heightCM, &budget)
	if errors.Is(err, pgx.ErrNoRows) {
		return json.RawMessage(`{}`), nil
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"role": role, "height_cm": heightCM, "budget": budget})
}

// SetOperationProgress 推进公开 Operation 的进度与阶段码（供 assessment handler 上报）。
func (s *Store) SetOperationProgress(ctx context.Context, operationID string, progressBPS int, stageCode string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE operations SET status='running', progress_bps=$2, stage_code=$3 WHERE id=$1::uuid`,
		operationID, progressBPS, stageCode)
	return err
}
