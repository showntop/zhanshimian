package wardrobe

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
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

// ---- CreateItem 校验/默认值/media 归属（旧线 CreateWardrobeItem 恢复） ----

type mediaCheckerFake struct{ err error }

func (f mediaCheckerFake) CheckWardrobeMedia(context.Context, string, string) error { return f.err }

type readerFake struct{ grounding Grounding }

func (f readerFake) ReadWardrobeGrounding(context.Context, string) (Grounding, error) {
	return f.grounding, nil
}

func TestCreateItemValidatesRequiredFields(t *testing.T) {
	svc := New(nil, writerFake{})
	cases := []CreateItemInput{
		{Name: "", Category: "top", Color: "白"},
		{Name: "白衬衫", Category: "top", Color: ""},
		{Name: "白衬衫", Category: "dress", Color: "白"},
		{Name: "  ", Category: "top", Color: "白"},
	}
	for _, input := range cases {
		if _, err := svc.CreateItem(context.Background(), "user-1", input); !errors.Is(err, ErrValidation) {
			t.Fatalf("input %+v error = %v, want ErrValidation", input, err)
		}
	}
}

func TestCreateItemAppliesDefaults(t *testing.T) {
	writer := writerFake{}
	svc := New(nil, writer)
	item, err := svc.CreateItem(context.Background(), "user-1", CreateItemInput{
		Name: "白衬衫", Category: "top", Color: "白",
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.Season != "all" || item.Formality != "proper" {
		t.Fatalf("defaults = %q/%q, want all/proper", item.Season, item.Formality)
	}
	if len(item.Scenes) != 1 || item.Scenes[0] != "daily" {
		t.Fatalf("scenes = %v, want [daily]", item.Scenes)
	}
}

func TestCreateItemKeepsExplicitAttributes(t *testing.T) {
	svc := New(nil, writerFake{})
	item, err := svc.CreateItem(context.Background(), "user-1", CreateItemInput{
		Name: "羊绒大衣", Category: "outer", Color: "驼", Season: "winter", Formality: "formal", Scenes: []string{"interview"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.Season != "winter" || item.Formality != "formal" || item.Scenes[0] != "interview" {
		t.Fatalf("explicit attributes lost: %#v", item)
	}
}

// media 归属/用途不符一律 404（越权不泄露存在性）；未装配校验器时拒绝带图建档。
func TestCreateItemForeignMediaIsNotFound(t *testing.T) {
	svc := New(nil, writerFake{}).WithMediaChecker(mediaCheckerFake{err: repository.ErrNotFound})
	_, err := svc.CreateItem(context.Background(), "user-1", CreateItemInput{
		Name: "白衬衫", Category: "top", Color: "白", MediaAssetID: "foreign-asset",
	})
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}

	unchecked := New(nil, writerFake{})
	if _, err := unchecked.CreateItem(context.Background(), "user-1", CreateItemInput{
		Name: "白衬衫", Category: "top", Color: "白", MediaAssetID: "asset-1",
	}); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("unchecked media error = %v, want ErrNotFound", err)
	}
}

func TestCreateItemWithOwnedMedia(t *testing.T) {
	svc := New(nil, writerFake{}).WithMediaChecker(mediaCheckerFake{})
	item, err := svc.CreateItem(context.Background(), "user-1", CreateItemInput{
		Name: "白衬衫", Category: "top", Color: "白", MediaAssetID: "asset-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.MediaAssetID != "asset-1" {
		t.Fatalf("media asset = %q", item.MediaAssetID)
	}
}

// ---- CreateOutfit 校验/默认值/归属（旧线 CreateWardrobeOutfit 恢复） ----

func outfitWriter() writerFake {
	return writerFake{items: []Item{
		{ID: "item-1", Name: "白衬衫", Category: "top", Color: "白"},
		{ID: "item-2", Name: "黑西裤", Category: "bottom", Color: "黑"},
	}}
}

func TestCreateOutfitValidatesTitle(t *testing.T) {
	svc := New(readerFake{}, outfitWriter())
	for _, title := range []string{"", "   ", strings.Repeat("字", 61)} {
		if _, err := svc.CreateOutfit(context.Background(), "user-1", CreateOutfitInput{
			Title: title, ItemIDs: []string{"item-1"},
		}); !errors.Is(err, ErrValidation) {
			t.Fatalf("title %q error = %v, want ErrValidation", title, err)
		}
	}
}

func TestCreateOutfitValidatesItemCount(t *testing.T) {
	svc := New(readerFake{}, outfitWriter())
	if _, err := svc.CreateOutfit(context.Background(), "user-1", CreateOutfitInput{Title: "周一"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty items error = %v, want ErrValidation", err)
	}
	tooMany := make([]string, 13)
	for i := range tooMany {
		tooMany[i] = "item-1"
	}
	if _, err := svc.CreateOutfit(context.Background(), "user-1", CreateOutfitInput{Title: "周一", ItemIDs: tooMany}); !errors.Is(err, ErrValidation) {
		t.Fatalf("13 items error = %v, want ErrValidation", err)
	}
}

func TestCreateOutfitForeignItemIsNotFound(t *testing.T) {
	svc := New(readerFake{}, outfitWriter())
	_, err := svc.CreateOutfit(context.Background(), "user-1", CreateOutfitInput{
		Title: "周一通勤", ItemIDs: []string{"item-1", "foreign-item"},
	})
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestCreateOutfitAppliesDefaultsAndEmbedsItems(t *testing.T) {
	svc := New(readerFake{}, outfitWriter())
	outfit, err := svc.CreateOutfit(context.Background(), "user-1", CreateOutfitInput{
		Title: "  周一搭配  ", ItemIDs: []string{"item-1", "item-2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if outfit.Title != "周一搭配" {
		t.Fatalf("title = %q, want trimmed", outfit.Title)
	}
	if outfit.Note != "优先复用你常穿的单品，用颜色与比例完成这套表达。" {
		t.Fatalf("note = %q", outfit.Note)
	}
	if outfit.Context.Date == "" || outfit.Context.Schedule == "" || outfit.Context.DayType == "" {
		t.Fatalf("context snapshot = %#v", outfit.Context)
	}
	if len(outfit.Items) != 2 || outfit.Items[0].ID != "item-1" || outfit.Items[1].ID != "item-2" {
		t.Fatalf("embedded items = %#v", outfit.Items)
	}
}
