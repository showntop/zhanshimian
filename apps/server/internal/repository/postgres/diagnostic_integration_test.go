package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/diagnostic"
)

func insertDiagnosticMediaAsset(t *testing.T, store *Store, userID string) string {
	t.Helper()
	var assetID string
	err := store.pool.QueryRow(context.Background(), `
		INSERT INTO media_assets(user_id, origin, purpose, object_key, sha256, mime_type, byte_size, state, display_kind)
		VALUES ($1::uuid, 'user_upload', 'feedback', $2, $3, 'image/jpeg', 128, 'ready', 'original')
		RETURNING id::text`,
		userID, "users/"+userID+"/uploads/diag-"+uuid.NewString()+".jpg", strings.Repeat("ab", 32)).
		Scan(&assetID)
	if err != nil {
		t.Fatal(err)
	}
	return assetID
}

// 诊断源照片的对象定位必须带用户边界：越权/不存在/已删除一律 ErrNotFound，
// service 据此失败而绝不静默退化成无图诊断。
func TestReadDiagnosticMediaRequiresOwnedLiveAsset(t *testing.T) {
	store, userID := newMediaStore(t)
	otherID := insertUser(t, store)
	assetID := insertDiagnosticMediaAsset(t, store, userID)
	ctx := context.Background()

	media, err := store.ReadDiagnosticMedia(ctx, userID, assetID)
	if err != nil {
		t.Fatal(err)
	}
	if media.AssetID != assetID || media.ObjectKey == "" || media.MIMEType != "image/jpeg" {
		t.Fatalf("diagnostic media = %#v", media)
	}
	if _, err := store.ReadDiagnosticMedia(ctx, otherID, assetID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("cross-tenant media = %v, want ErrNotFound", err)
	}
	if _, err := store.ReadDiagnosticMedia(ctx, userID, uuid.NewString()); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("unknown media = %v, want ErrNotFound", err)
	}
	if _, err := store.pool.Exec(ctx, `
		UPDATE media_assets SET state='deleted', deleted_at=now()
		WHERE user_id=$1::uuid AND id=$2::uuid`, userID, assetID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadDiagnosticMedia(ctx, userID, assetID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("deleted media = %v, want ErrNotFound", err)
	}
}

// 日限自计数（不依赖 billing）：只计 since 之后本用户的诊断历史。
func TestCountDiagnosticsSinceCountsWithinWindow(t *testing.T) {
	store, userID := newMediaStore(t)
	otherID := insertUser(t, store)
	ctx := context.Background()

	insert := func(userID string) string {
		t.Helper()
		d, err := store.InsertDiagnostic(ctx, userID, diagnostic.Diagnosis{
			Kind: "outfit", Scene: "daily", Conclusion: "可行",
			CreatedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}
		return d.ID
	}

	now := time.Now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	firstID := insert(userID)
	insert(userID)
	insert(otherID)

	count, err := store.CountDiagnosticsSince(ctx, userID, dayStart)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("today count = %d, want 2", count)
	}
	otherCount, err := store.CountDiagnosticsSince(ctx, otherID, dayStart)
	if err != nil {
		t.Fatal(err)
	}
	if otherCount != 1 {
		t.Fatalf("other user count = %d, want 1", otherCount)
	}

	// 昨天落库的行不计入今日窗口。
	if _, err := store.pool.Exec(ctx, `
		UPDATE diagnostics SET created_at=$3
		WHERE user_id=$1::uuid AND id=$2::uuid`, userID, firstID, dayStart.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	count, err = store.CountDiagnosticsSince(ctx, userID, dayStart)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("count after backdating = %d, want 1", count)
	}
}
