package share_test

import (
	"context"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/service/share"
)

// 编译期契约：Reader 的签名是本计划冻结的边界。
type shareReaderFake struct {
	source share.Source
	err    error
}

func (f shareReaderFake) ReadShareSource(context.Context, string, string, string) (share.Source, error) {
	return f.source, f.err
}

var _ share.Reader = shareReaderFake{}

type writerFake struct {
	card share.Card
	err  error
}

func (f writerFake) InsertShare(_ context.Context, _ string, source share.Source, snapshot share.Snapshot, includePhoto bool) (share.Card, error) {
	return share.Card{
		ID: "card-1", Token: "token-1", SourceType: source.SourceType, SourceID: source.SourceID,
		Snapshot: snapshot, IncludePhoto: includePhoto, ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
	}, f.err
}

func (f writerFake) GetPublicShareByToken(context.Context, string) (share.Card, error) {
	return f.card, f.err
}

func (f writerFake) RevokeShareByID(context.Context, string, string) error { return f.err }

type signerFake struct {
	url       string
	expiresAt time.Time
	err       error
}

func (f signerFake) SignedURL(_ context.Context, objectKey string, _ time.Duration) (string, time.Time, error) {
	return f.url + objectKey, f.expiresAt, f.err
}

var publishedSource = share.Source{
	SourceType: "plan_variant", SourceID: "variant-1", Title: "利落", Summary: "干练",
	AssetID: "asset-1", ObjectKey: "users/u1/render-published/pub-1.jpg", MIMEType: "image/jpeg",
	SourceKind: domain.MediaSourceGeneratedPreview, DisplayLabel: "风格参考",
}

// 创建响应必须回填签名媒体（含 MIMEType）：客户端投影对 generated 类强制
// image/jpeg、对空 url 一律拒渲染——缺任一字段创建者的分享卡都无图。
func TestCreateBackfillsSignedMedia(t *testing.T) {
	expires := time.Now().Add(time.Hour).UTC()
	svc := share.New(shareReaderFake{source: publishedSource}, writerFake{},
		signerFake{url: "https://signed.example/", expiresAt: expires}, time.Hour)

	card, err := svc.Create(context.Background(), "user-1", share.CreateInput{SourceType: "plan_variant", SourceID: "variant-1"})
	if err != nil {
		t.Fatal(err)
	}
	if card.Media == nil {
		t.Fatal("creator card must carry media")
	}
	if card.Media.URL != "https://signed.example/"+publishedSource.ObjectKey {
		t.Fatalf("media url = %q", card.Media.URL)
	}
	if card.Media.MIMEType != "image/jpeg" {
		t.Fatalf("media mime = %q", card.Media.MIMEType)
	}
	if card.Media.SourceKind != "generated_preview" || card.Media.DisplayLabel != "风格参考" {
		t.Fatalf("media source = %q/%q", card.Media.SourceKind, card.Media.DisplayLabel)
	}
	if !card.Media.URLExpiresAt.Equal(expires) {
		t.Fatalf("media expires = %v", card.Media.URLExpiresAt)
	}
	if card.Snapshot.AssetID != "asset-1" {
		t.Fatalf("snapshot asset = %q", card.Snapshot.AssetID)
	}
}

// 无发布媒体的来源（如今日方案渲染未回填）创建为纯文本卡：不报错、不带媒体。
func TestCreateWithoutPublishedAssetYieldsTextOnlyCard(t *testing.T) {
	svc := share.New(shareReaderFake{source: share.Source{
		SourceType: "today_plan", SourceID: "today-1", Title: "今日利落通勤", Summary: "浅色提亮",
	}}, writerFake{}, signerFake{url: "https://signed.example/", expiresAt: time.Now().Add(time.Hour)}, time.Hour)

	card, err := svc.Create(context.Background(), "user-1", share.CreateInput{SourceType: "today_plan", SourceID: "today-1"})
	if err != nil {
		t.Fatal(err)
	}
	if card.Media != nil {
		t.Fatalf("text-only share must not carry media: %#v", card.Media)
	}
	if card.Snapshot.Title != "今日利落通勤" {
		t.Fatalf("snapshot = %#v", card.Snapshot)
	}
}

// 公开读取的媒体必须带 MIMEType（接收方 ShareView 的客户端投影同样强制）。
func TestGetPublicSignsMediaWithMIMEType(t *testing.T) {
	expires := time.Now().Add(time.Hour).UTC()
	svc := share.New(shareReaderFake{}, writerFake{card: share.Card{
		SourceType: "plan_variant",
		Snapshot: share.Snapshot{
			Title: "利落", Summary: "干练", AssetID: "asset-1",
			SourceKind: "generated_preview", DisplayLabel: "风格参考",
		},
		ObjectKey: "users/u1/render-published/pub-1.jpg", MIMEType: "image/jpeg",
		ExpiresAt: time.Now().Add(time.Hour),
	}}, signerFake{url: "https://signed.example/", expiresAt: expires}, time.Hour)

	view, err := svc.GetPublic(context.Background(), "token-1")
	if err != nil {
		t.Fatal(err)
	}
	if view.Media == nil {
		t.Fatal("public view must carry media")
	}
	if view.Media.URL == "" || view.Media.MIMEType != "image/jpeg" {
		t.Fatalf("media = %#v", view.Media)
	}
}

// 撤销/过期的分享一律 ErrShareGone。
func TestGetPublicRevokedIsGone(t *testing.T) {
	svc := share.New(shareReaderFake{}, writerFake{card: share.Card{
		Revoked: true, ExpiresAt: time.Now().Add(time.Hour),
	}}, signerFake{}, time.Hour)
	if _, err := svc.GetPublic(context.Background(), "token-1"); err != share.ErrShareGone {
		t.Fatalf("err = %v, want ErrShareGone", err)
	}
}
