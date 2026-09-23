package postgres

import (
	"context"
	"testing"

	"github.com/zhanshimian/server/internal/testutil"
)

// 账户展示头像列必须折进 baseline——第 17 轮全量 E2E 实测:GET /v1/me 500
// (42703 column u.avatar_media_id does not exist),legacy 020_user_avatar.sql
// 的列与 001_init 的 users.updated_at 都没折进来(UpdateUserAvatar 同时写
// 两列)。钉住设置头像→读回头像的完整往返。
func TestUserAvatarRoundTrip(t *testing.T) {
	store := New(testutil.NewPostgres(t))
	ctx := context.Background()
	var userID, mediaID string
	if err := store.pool.QueryRow(ctx, `INSERT INTO users(nickname) VALUES('avatar') RETURNING id::text`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `
		INSERT INTO media_assets(user_id, origin, purpose, object_key, sha256, mime_type, byte_size, state, display_kind)
		VALUES($1::uuid, 'user_upload', 'feedback', $2,
		       '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef',
		       'image/jpeg', 1024, 'ready', 'original')
		RETURNING id::text`, userID, "avatar-"+userID).Scan(&mediaID); err != nil {
		t.Fatal(err)
	}

	if err := store.UpdateUserAvatar(ctx, userID, mediaID); err != nil {
		t.Fatalf("update avatar: %v", err)
	}
	asset, err := store.GetUserAvatar(ctx, userID)
	if err != nil {
		t.Fatalf("get avatar: %v", err)
	}
	if asset.ID != mediaID {
		t.Fatalf("avatar = %s, want %s", asset.ID, mediaID)
	}
}
