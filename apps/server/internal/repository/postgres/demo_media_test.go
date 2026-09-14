package postgres

import (
	"context"
	"fmt"
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
	for i, kind := range []string{"face", "side", "body"} {
		asset, err := store.InsertDemoMedia(ctx, userID, kind,
			fmt.Sprintf("demo/%s/%s-%d.png", userID, kind, i), demoSHA(kind), 1234)
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

	purposes := map[string]domain.MediaPurpose{
		"outfit":   domain.MediaPurposeFeedback,
		"product":  domain.MediaPurposeFeedback,
		"wardrobe": domain.MediaPurposeWardrobe,
	}
	i := 0
	for kind, want := range purposes {
		i++
		asset, err := store.InsertDemoMedia(ctx, userID, kind,
			fmt.Sprintf("demo/%s/%s-%d.png", userID, kind, i), demoSHA(kind), 1234)
		if err != nil {
			t.Fatalf("insert demo %s: %v", kind, err)
		}
		if asset.Purpose != want {
			t.Fatalf("demo %s purpose = %s, want %s", kind, asset.Purpose, want)
		}
	}
}

func TestInsertDemoMediaRejectsBadInput(t *testing.T) {
	store, userID := newMediaStore(t)
	ctx := context.Background()

	if _, err := store.InsertDemoMedia(ctx, userID, "pancake", "demo/x/pancake.png", demoSHA("pancake"), 1); err == nil {
		t.Fatal("unsupported kind must be rejected")
	}
	if _, err := store.InsertDemoMedia(ctx, userID, "face", "demo/x/face.png", "not-hex", 1); err == nil {
		t.Fatal("malformed sha256 must be rejected by the CHECK constraint")
	}
	if _, err := store.InsertDemoMedia(ctx, userID, "face", "demo/x/face2.png", demoSHA("face"), 0); err == nil {
		t.Fatal("zero byte_size must be rejected by the CHECK constraint")
	}
}

// demoSHA 生成合法形状的 64-hex 测试哈希（内容无关，只满足 CHECK）。
func demoSHA(seed string) string {
	sum := [32]byte{}
	copy(sum[:], seed)
	return fmt.Sprintf("%x", sum)
}
