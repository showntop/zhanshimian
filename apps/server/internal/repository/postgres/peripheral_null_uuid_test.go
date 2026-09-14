package postgres

import (
	"context"
	"testing"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/service/diagnostic"
	"github.com/zhanshimian/server/internal/service/hair"
	"github.com/zhanshimian/server/internal/service/share"
	"github.com/zhanshimian/server/internal/service/wardrobe"
	"github.com/zhanshimian/server/internal/testutil"
)

// 可空 uuid 列的落库统一走 NULLIF($N,'')::uuid(先判空再转型)。第 12 轮全量
// E2E 实测:POST /v1/diagnostics 500 —— NULLIF($10::uuid,'') 里 '' 字面量在
// plan 期被强转成 uuid,任何输入都 22P02(today.go 同款,item 23 当时 grep
// 只查了单位数占位符,漏掉全部两位数与这些文件)。本文件钉住四个外围 writer
// 的空可选 uuid 插入路径。
func TestPeripheralInsertsPersistNullOptionalUUID(t *testing.T) {
	store := New(testutil.NewPostgres(t))
	ctx := context.Background()
	var userID string
	if err := store.pool.QueryRow(ctx, `INSERT INTO users(nickname) VALUES('null-uuid') RETURNING id::text`).Scan(&userID); err != nil {
		t.Fatal(err)
	}

	t.Run("diagnostic", func(t *testing.T) {
		d, err := store.InsertDiagnostic(ctx, userID, diagnostic.Diagnosis{
			Kind: "outfit", Scene: "日常", Conclusion: "暖调为主",
			PriorityTitle: "先定色温", PriorityCopy: "从暖色内搭开始。",
			MediaAssetID: "",
		})
		if err != nil {
			t.Fatalf("insert diagnostic without media asset: %v", err)
		}
		if d.ID == "" {
			t.Fatal("returned diagnosis must carry the inserted id")
		}
	})

	t.Run("wardrobe item", func(t *testing.T) {
		item, err := store.InsertWardrobeItem(ctx, userID, wardrobe.Item{
			MediaAssetID: "", Name: "白衬衫", Category: "上装", Scenes: []string{},
		})
		if err != nil {
			t.Fatalf("insert wardrobe item without media asset: %v", err)
		}
		if item.ID == "" {
			t.Fatal("returned item must carry the inserted id")
		}
	})

	t.Run("wardrobe outfit", func(t *testing.T) {
		outfit, err := store.InsertWardrobeOutfit(ctx, userID, wardrobe.Outfit{
			Title: "通勤组合", SelectedPlanID: "", ItemIDs: []string{},
		})
		if err != nil {
			t.Fatalf("insert wardrobe outfit without selected plan: %v", err)
		}
		if outfit.ID == "" {
			t.Fatal("returned outfit must carry the inserted id")
		}
	})

	t.Run("hair preview", func(t *testing.T) {
		preview, err := store.InsertHairPreview(ctx, userID, hair.Preview{
			StyleID: "style-texture-crop", State: "queued",
			SourceMedia: &domain.RenderMediaView{},
		})
		if err != nil {
			t.Fatalf("insert hair preview without source media: %v", err)
		}
		if preview.ID == "" {
			t.Fatal("returned preview must carry the inserted id")
		}
	})

	t.Run("share", func(t *testing.T) {
		card, err := store.InsertShare(ctx, userID, share.Source{
			SourceType: "today_plan", SourceID: userID, AssetID: "",
		}, share.Snapshot{}, false)
		if err != nil {
			t.Fatalf("insert share without asset: %v", err)
		}
		if card.ID == "" || card.Token == "" {
			t.Fatalf("returned card = %#v", card)
		}
	})
}
