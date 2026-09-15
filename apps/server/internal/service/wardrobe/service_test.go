package wardrobe

import (
	"context"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
)

type writerFake struct {
	items []Item
	err   error
}

func (f writerFake) GetWardrobeItems(context.Context, string) ([]Item, error) {
	return f.items, f.err
}

func (f writerFake) InsertWardrobeItem(_ context.Context, _ string, item Item) (Item, error) {
	return item, f.err
}

func (f writerFake) RemoveWardrobeItem(context.Context, string, string) error { return f.err }

func (f writerFake) InsertWardrobeOutfit(_ context.Context, _ string, outfit Outfit) (Outfit, error) {
	return outfit, f.err
}

func (f writerFake) SetWardrobeOutfitWorn(_ context.Context, _ string, _ string) (Outfit, error) {
	return Outfit{}, f.err
}

type signerFake struct {
	url       string
	expiresAt time.Time
	err       error
}

func (f signerFake) SignedURL(_ context.Context, objectKey string) (string, time.Time, error) {
	return f.url + objectKey, f.expiresAt, f.err
}

// 列表读路径必须给单品照片补签名 URL：客户端投影对空 url 一律拒渲染
// （衣橱单品照片不显示的根因）。无照片的单品不受影响。
func TestListItemsSignsItemMedia(t *testing.T) {
	expires := time.Now().Add(time.Hour).UTC()
	writer := writerFake{items: []Item{
		{
			ID: "item-1", Name: "白衬衫",
			Media:          &domain.RenderMediaView{AssetID: "asset-1", MIMEType: "image/jpeg", SourceKind: "user_original", DisplayLabel: "原本"},
			MediaObjectKey: "users/u1/uploads/intent-1.jpg",
		},
		{ID: "item-2", Name: "黑西裤"},
	}}
	svc := New(nil, writer).WithMediaSigner(signerFake{url: "https://signed.example/", expiresAt: expires})

	items, err := svc.ListItems(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d", len(items))
	}
	media := items[0].Media
	if media == nil {
		t.Fatal("item media must be present")
	}
	if media.URL != "https://signed.example/users/u1/uploads/intent-1.jpg" {
		t.Fatalf("media url = %q", media.URL)
	}
	if !media.URLExpiresAt.Equal(expires) {
		t.Fatalf("media expires = %v", media.URLExpiresAt)
	}
	if media.MIMEType != "image/jpeg" || media.SourceKind != "user_original" {
		t.Fatalf("media = %#v", media)
	}
	if items[1].Media != nil {
		t.Fatalf("photo-less item must stay media-less: %#v", items[1].Media)
	}
}

// 未装配签名器（单测/降级组装）保持无 URL，不报错。
func TestListItemsWithoutSignerLeavesMediaUnsigned(t *testing.T) {
	writer := writerFake{items: []Item{{
		ID:             "item-1",
		Media:          &domain.RenderMediaView{AssetID: "asset-1"},
		MediaObjectKey: "users/u1/uploads/intent-1.jpg",
	}}}
	svc := New(nil, writer)
	items, err := svc.ListItems(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if items[0].Media == nil || items[0].Media.URL != "" {
		t.Fatalf("unsigned media = %#v", items[0].Media)
	}
}
