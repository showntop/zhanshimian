package postgres

import (
	"context"
	"testing"

	"github.com/zhanshimian/server/internal/domain"
)

// Demo 媒体是 E2E/体验路径的三图与诊断照片来源：baseline 没有 legacy
// multipart CreateMedia，demo 资产必须走专用插入并满足 media_assets 全部
// CHECK（purpose 枚举、sha256 形状、object_key 唯一）。
func TestInsertDemoMediaCreatesReadyAssessmentAssets(t *testing.T) {
	store, userID := newMediaStore(t)
	ctx := context.Background()

	ids := []string{}
	for _, kind := range []string{"face", "side", "body"} {
		asset, err := store.InsertDemoMedia(ctx, userID, kind)
		if err != nil {
			t.Fatalf("insert demo %s: %v", kind, err)
		}
		if asset.Origin != domain.MediaOriginDemo || asset.State != domain.MediaStateReady {
			t.Fatalf("demo %s origin/state = %s/%s", kind, asset.Origin, asset.State)
		}
		if asset.Purpose != domain.MediaPurpose(kind) {
			t.Fatalf("demo %s purpose = %s", kind, asset.Purpose)
		}
		if asset.DisplayKind != domain.DisplayKindEffectExample {
			t.Fatalf("demo %s display_kind = %s", kind, asset.DisplayKind)
		}
		ids = append(ids, asset.ID)
	}

	ready, err := store.GetReadyAssets(ctx, userID, ids)
	if err != nil || len(ready) != 3 {
		t.Fatalf("demo assets must be assessment-ready: n=%d err=%v", len(ready), err)
	}
}

func TestInsertDemoMediaMapsDiagnosticKindsToAllowedPurposes(t *testing.T) {
	store, userID := newMediaStore(t)
	ctx := context.Background()

	for kind, want := range map[string]domain.MediaPurpose{
		"outfit":   domain.MediaPurposeFeedback,
		"product":  domain.MediaPurposeFeedback,
		"wardrobe": domain.MediaPurposeWardrobe,
	} {
		asset, err := store.InsertDemoMedia(ctx, userID, kind)
		if err != nil {
			t.Fatalf("insert demo %s: %v", kind, err)
		}
		if asset.Purpose != want {
			t.Fatalf("demo %s purpose = %s, want %s", kind, asset.Purpose, want)
		}
	}
}

func TestInsertDemoMediaObjectKeysNeverCollide(t *testing.T) {
	store, userID := newMediaStore(t)
	ctx := context.Background()

	first, err := store.InsertDemoMedia(ctx, userID, "face")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.InsertDemoMedia(ctx, userID, "face")
	if err != nil {
		t.Fatalf("same-kind second demo must not hit object_key UNIQUE: %v", err)
	}
	if first.ObjectKey == second.ObjectKey {
		t.Fatal("object keys must differ per asset")
	}

	if _, err := store.InsertDemoMedia(ctx, userID, "pancake"); err == nil {
		t.Fatal("unsupported kind must be rejected")
	}
}
