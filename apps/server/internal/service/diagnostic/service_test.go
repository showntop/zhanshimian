package diagnostic

import (
	"context"
	"testing"

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
