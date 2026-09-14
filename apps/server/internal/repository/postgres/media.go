package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
)

const uploadIntentSelect = `
	SELECT id::text, user_id::text, purpose, mime_type, byte_size, sha256, object_key,
	       status, expires_at, completed_media_asset_id::text, version, created_at, updated_at
	FROM upload_intents`

const mediaAssetSelect = `
	SELECT id::text, user_id::text, origin, purpose, object_key, sha256, mime_type, byte_size,
	       width, height, state, display_kind, provider_invocation_id::text, created_at, deleted_at
	FROM media_assets`

func (s *Store) CreateUploadIntent(ctx context.Context, in domain.CreateUploadIntent) (domain.UploadIntent, error) {
	id := in.ID
	if id == "" {
		id = uuid.NewString()
	}
	objectKey := in.ObjectKey
	if objectKey == "" {
		objectKey = fmt.Sprintf("users/%s/uploads/%s", in.UserID, id)
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO upload_intents(id, user_id, purpose, mime_type, byte_size, sha256, object_key, status, expires_at)
		VALUES($1::uuid, $2::uuid, $3, $4, $5, $6, $7, 'pending', $8)
		RETURNING id::text, user_id::text, purpose, mime_type, byte_size, sha256, object_key,
		          status, expires_at, completed_media_asset_id::text, version, created_at, updated_at`,
		id, in.UserID, in.Purpose, in.MIMEType, in.ByteSize, in.SHA256, objectKey, in.ExpiresAt)
	return scanUploadIntent(row)
}

func (s *Store) GetUploadIntent(ctx context.Context, userID, intentID string) (domain.UploadIntent, error) {
	intent, err := scanUploadIntent(s.pool.QueryRow(ctx, uploadIntentSelect+` WHERE user_id=$1::uuid AND id=$2::uuid`, userID, intentID))
	return intent, mapNotFound(err)
}

func (s *Store) CompleteUploadIntent(ctx context.Context, in domain.CompleteUploadIntent) (domain.MediaAsset, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.MediaAsset{}, false, err
	}
	defer tx.Rollback(ctx)

	intent, err := scanUploadIntent(tx.QueryRow(ctx, uploadIntentSelect+` WHERE user_id=$1::uuid AND id=$2::uuid FOR UPDATE`, in.UserID, in.IntentID))
	if err != nil {
		return domain.MediaAsset{}, false, mapNotFound(err)
	}
	if intent.Status == domain.UploadIntentCompleted && intent.CompletedMediaAssetID != "" {
		asset, err := scanMediaAsset(tx.QueryRow(ctx, mediaAssetSelect+` WHERE user_id=$1::uuid AND id=$2::uuid`, in.UserID, intent.CompletedMediaAssetID))
		if err != nil {
			return domain.MediaAsset{}, false, mapNotFound(err)
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.MediaAsset{}, false, err
		}
		return asset, false, nil
	}
	if intent.Status != domain.UploadIntentPending || !intent.ExpiresAt.After(time.Now()) {
		return domain.MediaAsset{}, false, repository.ErrConflict
	}

	asset, err := scanMediaAsset(tx.QueryRow(ctx, `
		INSERT INTO media_assets(user_id, origin, purpose, object_key, sha256, mime_type, byte_size, state, display_kind)
		VALUES($1::uuid, 'user_upload', $2, $3, $4, $5, $6, 'ready', 'original')
		RETURNING id::text, user_id::text, origin, purpose, object_key, sha256, mime_type, byte_size,
		          width, height, state, display_kind, provider_invocation_id::text, created_at, deleted_at`,
		in.UserID, intent.Purpose, in.Metadata.ObjectKey, in.Metadata.SHA256, in.Metadata.MIMEType, in.Metadata.ByteSize))
	if err != nil {
		return domain.MediaAsset{}, false, err
	}

	tag, err := tx.Exec(ctx, `
		UPDATE upload_intents
		SET status='completed', completed_media_asset_id=$3::uuid, updated_at=now(), version=version+1
		WHERE user_id=$1::uuid AND id=$2::uuid AND status='pending' AND version=$4`,
		in.UserID, in.IntentID, asset.ID, intent.Version)
	if err != nil {
		return domain.MediaAsset{}, false, err
	}
	if tag.RowsAffected() != 1 {
		return domain.MediaAsset{}, false, repository.ErrConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.MediaAsset{}, false, err
	}
	return asset, true, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUploadIntent(row rowScanner) (domain.UploadIntent, error) {
	var intent domain.UploadIntent
	var completedID *string
	err := row.Scan(
		&intent.ID, &intent.UserID, &intent.Purpose, &intent.MIMEType, &intent.ByteSize, &intent.SHA256,
		&intent.ObjectKey, &intent.Status, &intent.ExpiresAt, &completedID, &intent.Version, &intent.CreatedAt, &intent.UpdatedAt,
	)
	if completedID != nil {
		intent.CompletedMediaAssetID = *completedID
	}
	return intent, err
}

func scanMediaAsset(row rowScanner) (domain.MediaAsset, error) {
	var asset domain.MediaAsset
	var width, height *int
	var invocationID *string
	err := row.Scan(
		&asset.ID, &asset.UserID, &asset.Origin, &asset.Purpose, &asset.ObjectKey, &asset.SHA256,
		&asset.MIMEType, &asset.ByteSize, &width, &height, &asset.State, &asset.DisplayKind,
		&invocationID, &asset.CreatedAt, &asset.DeletedAt,
	)
	if width != nil {
		asset.Width = *width
	}
	if height != nil {
		asset.Height = *height
	}
	if invocationID != nil {
		asset.ProviderInvocationID = *invocationID
	}
	return asset, err
}

// demoMediaPurposes 把 demo 种类映射到 baseline 允许的 purpose：
// 三图就是三种 purpose；诊断/单品照没有专属 purpose，归入 feedback/wardrobe。
var demoMediaPurposes = map[string]domain.MediaPurpose{
	"face":     domain.MediaPurposeFace,
	"side":     domain.MediaPurposeSide,
	"body":     domain.MediaPurposeBody,
	"outfit":   domain.MediaPurposeFeedback,
	"product":  domain.MediaPurposeFeedback,
	"wardrobe": domain.MediaPurposeWardrobe,
}

// InsertDemoMedia 插入一条 demo 媒体资产（POST /v1/media/demo 的存储侧）。
// 对象字节由调用方（bootstrap demoMediaAdapter，内置 assets/looks/*.png）
// 先写入对象存储；这里只落满足全部 CHECK 的媒体行。
func (s *Store) InsertDemoMedia(ctx context.Context, userID, kind, objectKey, sha256Hex string, byteSize int64) (domain.MediaAsset, error) {
	purpose, ok := demoMediaPurposes[kind]
	if !ok {
		return domain.MediaAsset{}, fmt.Errorf("unsupported demo kind %q", kind)
	}
	return scanMediaAsset(s.pool.QueryRow(ctx, `
		INSERT INTO media_assets(user_id, origin, purpose, object_key, sha256, mime_type, byte_size, state, display_kind)
		VALUES($1::uuid, 'demo', $2, $3, $4, 'image/png', $5, 'ready', 'effect_example')
		RETURNING id::text, user_id::text, origin, purpose, object_key, sha256, mime_type, byte_size,
		          width, height, state, display_kind, provider_invocation_id::text, created_at, deleted_at`,
		userID, purpose, objectKey, sha256Hex, byteSize))
}
