package diagnostic

import (
	"context"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
)

type fakeReader struct{}

func (fakeReader) ReadDiagnosticGrounding(context.Context, string, string, string) (Grounding, error) {
	return Grounding{Profile: domain.ProfileSnapshot{}}, nil
}

type fakeAdvisor struct{}

func (fakeAdvisor) Diagnose(context.Context, DiagnosticRequest) (DiagnosticOutput, error) {
	return DiagnosticOutput{Conclusion: "可行"}, nil
}

type fakeWriter struct {
	inserted Diagnosis
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

// 诊断的源照片必须随结论落库（diagnostics.source_media_asset_id），
// 否则"离开同步诊断页后恢复结论"拿不到原图。
func TestRunPersistsSourceMediaAsset(t *testing.T) {
	writer := &fakeWriter{}
	svc := New(fakeReader{}, writer, fakeAdvisor{})
	if _, err := svc.Run(context.Background(), "user-1", RunInput{
		Kind: "outfit", Scene: "daily", MediaAssetID: "asset-1",
	}); err != nil {
		t.Fatal(err)
	}
	if writer.inserted.MediaAssetID != "asset-1" {
		t.Fatalf("source media asset dropped: %q", writer.inserted.MediaAssetID)
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
	svc := New(fakeReader{}, writer, fakeAdvisor{}).
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
	svc := New(fakeReader{}, writer, fakeAdvisor{})
	got, err := svc.Get(context.Background(), "user-1", "diag-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.SourceMedia == nil || got.SourceMedia.URL != "" {
		t.Fatalf("unsigned source_media = %#v", got.SourceMedia)
	}
}
