package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/zhanshimian/server/internal/repository"
)

func insertWardrobeMediaAsset(t *testing.T, store *Store, userID, purpose string) string {
	t.Helper()
	var assetID string
	err := store.pool.QueryRow(context.Background(), `
		INSERT INTO media_assets(user_id, origin, purpose, object_key, sha256, mime_type, byte_size, state, display_kind)
		VALUES ($1::uuid, 'user_upload', $2, $3, $4, 'image/jpeg', 128, 'ready', 'original')
		RETURNING id::text`,
		userID, purpose, "users/"+userID+"/uploads/wardrobe-"+uuid.NewString()+".jpg", strings.Repeat("cd", 32)).
		Scan(&assetID)
	if err != nil {
		t.Fatal(err)
	}
	return assetID
}

// 单品照片媒体的归属/用途 guard（旧线 CreateWardrobeItem 的 media 校验恢复）：
// 越权、不存在、已删除、用途非 wardrobe 一律 ErrNotFound。
func TestCheckWardrobeMediaRequiresOwnedWardrobeAsset(t *testing.T) {
	store, userID := newMediaStore(t)
	otherID := insertUser(t, store)

	owned := insertWardrobeMediaAsset(t, store, userID, "wardrobe")
	if err := store.CheckWardrobeMedia(context.Background(), userID, owned); err != nil {
		t.Fatalf("owned wardrobe asset must pass: %v", err)
	}
	if err := store.CheckWardrobeMedia(context.Background(), otherID, owned); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("cross-user error = %v, want ErrNotFound", err)
	}
	if err := store.CheckWardrobeMedia(context.Background(), userID, uuid.NewString()); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("missing error = %v, want ErrNotFound", err)
	}

	wrongPurpose := insertWardrobeMediaAsset(t, store, userID, "face")
	if err := store.CheckWardrobeMedia(context.Background(), userID, wrongPurpose); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("wrong purpose error = %v, want ErrNotFound", err)
	}

	deleted := insertWardrobeMediaAsset(t, store, userID, "wardrobe")
	if _, err := store.pool.Exec(context.Background(),
		`UPDATE media_assets SET state='deleted' WHERE user_id=$1::uuid AND id=$2::uuid`, userID, deleted); err != nil {
		t.Fatal(err)
	}
	if err := store.CheckWardrobeMedia(context.Background(), userID, deleted); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("deleted error = %v, want ErrNotFound", err)
	}
}
