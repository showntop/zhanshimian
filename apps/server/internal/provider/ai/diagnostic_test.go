package ai

import (
	"context"
	"testing"

	"github.com/zhanshimian/server/internal/service/diagnostic"
)

func validDiagnosticJSON() []byte {
	return []byte(`{` +
		`"conclusion":"整体干净利落，适合日常",` +
		`"priority_title":"收紧下摆比例",` +
		`"priority_copy":"把上衣前摆轻塞进腰头，露出腰线",` +
		`"tags":["日常"],` +
		`"findings":[{"label":"配色协调","category":"color","tone":"positive","anchor_x":0.5,"anchor_y":0.4}],` +
		`"options":[{"name":"米色针织衫","note":"柔和过渡","reason":"与现有衣橱色系连续","tags":["日常"]}]` +
		`}`)
}

// 诊断输出 schema 要求 anchor_x/y：源照片必须作为视觉模型图片输入随请求
// 发出，否则锚点是无图编造的（来源真实性红线，旧线 Images: images 行为）。
func TestDiagnosticSendsPhotoAsVisionInput(t *testing.T) {
	runtime := &fakeStructuredRuntime{result: validDiagnosticJSON()}
	advisor := NewDiagnostic(runtime)
	_, err := advisor.Diagnose(context.Background(), diagnostic.DiagnosticRequest{
		Kind:         "outfit",
		Scene:        "daily",
		MediaAssetID: "asset-1",
		Images: []diagnostic.Image{
			{AssetID: "asset-1", Role: "outfit", MIMEType: "image/jpeg", Data: []byte{9, 8, 7}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.request.Capability != CapabilityOutfitDiagnosis {
		t.Fatalf("capability = %q", runtime.request.Capability)
	}
	if len(runtime.request.Images) != 1 {
		t.Fatalf("structured request images = %d, want 1", len(runtime.request.Images))
	}
	image := runtime.request.Images[0]
	if image.AssetID != "asset-1" || image.Role != "outfit" || image.MIMEType != "image/jpeg" || len(image.Data) != 3 {
		t.Fatalf("image input = %#v", image)
	}
}

// 购买诊断走 purchase_diagnosis 能力路由，照片角色为 product。
func TestPurchaseDiagnosticSendsProductImage(t *testing.T) {
	runtime := &fakeStructuredRuntime{result: validDiagnosticJSON()}
	advisor := NewDiagnostic(runtime)
	_, err := advisor.Diagnose(context.Background(), diagnostic.DiagnosticRequest{
		Kind:         "purchase",
		MediaAssetID: "asset-2",
		Images: []diagnostic.Image{
			{AssetID: "asset-2", Role: "product", MIMEType: "image/jpeg", Data: []byte{1}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.request.Capability != CapabilityPurchaseDiagnosis {
		t.Fatalf("capability = %q", runtime.request.Capability)
	}
	if len(runtime.request.Images) != 1 || runtime.request.Images[0].Role != "product" {
		t.Fatalf("purchase images = %#v", runtime.request.Images)
	}
}
