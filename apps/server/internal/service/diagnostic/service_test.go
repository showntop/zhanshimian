package diagnostic

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/service/account"
)

type fakeReader struct {
	grounding Grounding
	media     domain.MediaInput
	mediaErr  error
}

func (f fakeReader) ReadDiagnosticGrounding(context.Context, string, string, string) (Grounding, error) {
	return f.grounding, nil
}

func (f fakeReader) ReadDiagnosticMedia(context.Context, string, string) (domain.MediaInput, error) {
	if f.mediaErr != nil {
		return domain.MediaInput{}, f.mediaErr
	}
	if f.media.AssetID == "" {
		return domain.MediaInput{AssetID: "asset-1", ObjectKey: "users/u1/uploads/intent-1.jpg", MIMEType: "image/jpeg"}, nil
	}
	return f.media, nil
}

type fakeAdvisor struct {
	called  bool
	request DiagnosticRequest
	output  DiagnosticOutput
}

func (f *fakeAdvisor) Diagnose(_ context.Context, request DiagnosticRequest) (DiagnosticOutput, error) {
	f.called = true
	f.request = request
	if f.output.Conclusion != "" || f.output.Rejected {
		return f.output, nil
	}
	return DiagnosticOutput{Conclusion: "可行"}, nil
}

type fakeWriter struct {
	inserted Diagnosis
	count    int
	countErr error
}

func (f *fakeWriter) InsertDiagnostic(_ context.Context, _ string, d Diagnosis) (Diagnosis, error) {
	f.inserted = d
	d.ID = "diag-1"
	return d, nil
}

func (f *fakeWriter) GetDiagnosticByID(context.Context, string, string) (Diagnosis, error) {
	return f.inserted, nil
}

func (f *fakeWriter) GetLatestDiagnosticByKind(context.Context, string, string) (Diagnosis, error) {
	return f.inserted, nil
}

func (f *fakeWriter) UpdateDiagnosticSaved(_ context.Context, _ string, _ string, saved bool) (Diagnosis, error) {
	f.inserted.Saved = saved
	return f.inserted, nil
}

func (f *fakeWriter) CountDiagnosticsSince(context.Context, string, time.Time) (int, error) {
	return f.count, f.countErr
}

type fakeLoader struct {
	image Image
	err   error
	media domain.MediaInput
}

func (f *fakeLoader) Load(_ context.Context, media domain.MediaInput) (Image, error) {
	f.media = media
	if f.err != nil {
		return Image{}, f.err
	}
	if f.image.Data == nil {
		return Image{AssetID: media.AssetID, MIMEType: "image/jpeg", Data: []byte{1, 2, 3}}, nil
	}
	return f.image, nil
}

func newRunFixture() (*fakeReader, *fakeWriter, *fakeAdvisor, *fakeLoader) {
	return &fakeReader{}, &fakeWriter{}, &fakeAdvisor{}, &fakeLoader{}
}

// 诊断的源照片必须随结论落库（diagnostics.source_media_asset_id），
// 否则"离开同步诊断页后恢复结论"拿不到原图。
func TestRunPersistsSourceMediaAsset(t *testing.T) {
	reader, writer, advisor, loader := newRunFixture()
	svc := New(reader, writer, advisor, loader)
	if _, err := svc.Run(context.Background(), "user-1", RunInput{
		Kind: "outfit", Scene: "daily", MediaAssetID: "asset-1",
	}); err != nil {
		t.Fatal(err)
	}
	if writer.inserted.MediaAssetID != "asset-1" {
		t.Fatalf("source media asset dropped: %q", writer.inserted.MediaAssetID)
	}
}

// 照片门禁拒识：不产结论、不落库，错误必须带 ErrPhotoRejected 与用户可读
// 原因——客户端靠它给「换一张」空态，而不是把幻觉结果上屏。
func TestRunPhotoRejectedNeverPersisted(t *testing.T) {
	reader, writer, advisor, loader := newRunFixture()
	advisor.output = DiagnosticOutput{Rejected: true, ReasonCode: "illustration"}
	svc := New(reader, writer, advisor, loader)
	_, err := svc.Run(context.Background(), "user-1", RunInput{
		Kind: "outfit", Scene: "daily", MediaAssetID: "asset-1",
	})
	if !errors.Is(err, ErrPhotoRejected) {
		t.Fatalf("err = %v, want ErrPhotoRejected", err)
	}
	if writer.inserted.ID != "" || writer.inserted.Conclusion != "" {
		t.Fatalf("rejected diagnosis persisted: %+v", writer.inserted)
	}
}

// 输出 schema 要求 anchor_x/y，照片必须随请求发给视觉模型——无图时模型
// 只能凭空编造锚点（来源真实性红线）。
func TestRunSendsPhotoToAdvisor(t *testing.T) {
	reader, writer, advisor, loader := newRunFixture()
	svc := New(reader, writer, advisor, loader)
	if _, err := svc.Run(context.Background(), "user-1", RunInput{
		Kind: "outfit", Scene: "daily", MediaAssetID: "asset-1",
	}); err != nil {
		t.Fatal(err)
	}
	if len(advisor.request.Images) != 1 {
		t.Fatalf("advisor received %d images, want 1", len(advisor.request.Images))
	}
	image := advisor.request.Images[0]
	if len(image.Data) == 0 {
		t.Fatal("advisor image has no bytes")
	}
	if image.AssetID != "asset-1" || image.Role != "outfit" {
		t.Fatalf("outfit image identity = %q/%q", image.AssetID, image.Role)
	}
	if loader.media.ObjectKey == "" {
		t.Fatal("loader must receive the repository media location")
	}

	reader, writer, advisor, loader = newRunFixture()
	svc = New(reader, writer, advisor, loader)
	if _, err := svc.Run(context.Background(), "user-1", RunInput{
		Kind: "purchase", Scene: "daily", MediaAssetID: "asset-9",
	}); err != nil {
		t.Fatal(err)
	}
	if len(advisor.request.Images) != 1 || advisor.request.Images[0].Role != "product" {
		t.Fatalf("purchase image role = %#v", advisor.request.Images)
	}
}

// media_id 缺失时拒绝诊断（契约必填），绝不静默退化成无图诊断。
func TestRunRequiresPhoto(t *testing.T) {
	reader, writer, advisor, loader := newRunFixture()
	svc := New(reader, writer, advisor, loader)
	_, err := svc.Run(context.Background(), "user-1", RunInput{Kind: "outfit", Scene: "daily"})
	if !errors.Is(err, account.ErrValidation) {
		t.Fatalf("missing photo = %v, want ErrValidation", err)
	}
	if advisor.called {
		t.Fatal("advisor must not be called without a photo")
	}
}

// 照片越权/不存在按既有语义 404，绝不静默退化成无图诊断。
func TestRunPropagatesMissingPhoto(t *testing.T) {
	reader, writer, advisor, loader := newRunFixture()
	reader.mediaErr = repository.ErrNotFound
	svc := New(reader, writer, advisor, loader)
	_, err := svc.Run(context.Background(), "user-1", RunInput{
		Kind: "outfit", Scene: "daily", MediaAssetID: "asset-x",
	})
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("unknown photo = %v, want ErrNotFound", err)
	}
	if advisor.called {
		t.Fatal("advisor must not be called when the photo cannot be read")
	}
}

// 日限是诊断同步端点的成本防线：自然日（UTC）内第 9 次必须被拒，且
// 不触达 AI；8 次之内照常。
func TestRunRejectsNinthDiagnosticOfDay(t *testing.T) {
	reader, writer, advisor, loader := newRunFixture()
	writer.count = DailyLimitPerDay
	svc := New(reader, writer, advisor, loader)
	_, err := svc.Run(context.Background(), "user-1", RunInput{
		Kind: "outfit", Scene: "daily", MediaAssetID: "asset-1",
	})
	if !errors.Is(err, account.ErrRateLimited) {
		t.Fatalf("9th diagnostic = %v, want ErrRateLimited", err)
	}
	if advisor.called {
		t.Fatal("advisor must not be called over the daily limit")
	}

	reader, writer, advisor, loader = newRunFixture()
	writer.count = DailyLimitPerDay - 1
	svc = New(reader, writer, advisor, loader)
	if _, err := svc.Run(context.Background(), "user-1", RunInput{
		Kind: "outfit", Scene: "daily", MediaAssetID: "asset-1",
	}); err != nil {
		t.Fatalf("8th diagnostic must pass: %v", err)
	}
}

type signerFake struct {
	url       string
	expiresAt time.Time
	err       error
}

func (f signerFake) SignedURL(_ context.Context, objectKey string) (string, time.Time, error) {
	return f.url + objectKey, f.expiresAt, f.err
}

// 复访恢复（Get/Latest）必须给源照片补签名 URL 与 MIMEType：客户端投影对
// 空 url 一律拒渲染（outfit/purchase 复访照片不显示的根因）。
func TestReadPathsSignSourceMedia(t *testing.T) {
	expires := time.Now().Add(time.Hour).UTC()
	writer := &fakeWriter{inserted: Diagnosis{
		ID: "diag-1", Kind: "outfit",
		SourceMedia:          &domain.RenderMediaView{AssetID: "asset-1", SourceKind: "user_original", DisplayLabel: "原本"},
		SourceMediaObjectKey: "users/u1/uploads/intent-1.jpg",
		SourceMediaMIMEType:  "image/jpeg",
	}}
	svc := New(fakeReader{}, writer, &fakeAdvisor{}, &fakeLoader{}).
		WithMediaSigner(signerFake{url: "https://signed.example/", expiresAt: expires})

	got, err := svc.Get(context.Background(), "user-1", "diag-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.SourceMedia == nil {
		t.Fatal("source_media must be present")
	}
	if got.SourceMedia.URL != "https://signed.example/users/u1/uploads/intent-1.jpg" {
		t.Fatalf("source_media url = %q", got.SourceMedia.URL)
	}
	if got.SourceMedia.MIMEType != "image/jpeg" {
		t.Fatalf("source_media mime = %q", got.SourceMedia.MIMEType)
	}
	if !got.SourceMedia.URLExpiresAt.Equal(expires) {
		t.Fatalf("source_media expires = %v", got.SourceMedia.URLExpiresAt)
	}

	latest, err := svc.Latest(context.Background(), "user-1", "outfit")
	if err != nil {
		t.Fatal(err)
	}
	if latest.SourceMedia == nil || latest.SourceMedia.URL == "" {
		t.Fatalf("latest source_media = %#v", latest.SourceMedia)
	}

	saved, err := svc.SetSaved(context.Background(), "user-1", "diag-1", true)
	if err != nil {
		t.Fatal(err)
	}
	if saved.SourceMedia == nil || saved.SourceMedia.URL == "" {
		t.Fatalf("saved source_media = %#v", saved.SourceMedia)
	}
}

// 未装配签名器（单测/降级组装）保持无 URL，不报错。
func TestGetWithoutSignerLeavesSourceMediaUnsigned(t *testing.T) {
	writer := &fakeWriter{inserted: Diagnosis{
		ID: "diag-1", Kind: "outfit",
		SourceMedia:          &domain.RenderMediaView{AssetID: "asset-1"},
		SourceMediaObjectKey: "users/u1/uploads/intent-1.jpg",
	}}
	svc := New(fakeReader{}, writer, &fakeAdvisor{}, &fakeLoader{})
	got, err := svc.Get(context.Background(), "user-1", "diag-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.SourceMedia == nil || got.SourceMedia.URL != "" {
		t.Fatalf("unsigned source_media = %#v", got.SourceMedia)
	}
}
