package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// wanx2.1 description_edit only accepts one base image and drops the face
// reference that look.go sends as the second frame. full_look_edit must
// therefore start on a multi-image protocol (wan2.7 / Seedream).
func TestFullLookEditPrimaryAcceptsFaceReference(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	configDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "config")
	for _, name := range []string{"ai-routing.example.json", "ai-routing.production.json"} {
		raw, err := os.ReadFile(filepath.Join(configDir, name))
		if err != nil {
			t.Fatal(err)
		}
		var routing AIRoutingConfig
		if err := json.Unmarshal(raw, &routing); err != nil {
			t.Fatalf("decode %s: %v", name, err)
		}
		route, ok := routing.Routes["full_look_edit"]
		if !ok {
			t.Fatalf("%s missing full_look_edit route", name)
		}
		model, ok := routing.Models[route.Primary]
		if !ok {
			t.Fatalf("%s full_look_edit primary %q is not in models", name, route.Primary)
		}
		if model.Protocol == "dashscope_wanx_imageedit" {
			t.Fatalf("%s full_look_edit primary must not be wanx (single-image, drops face ref), got %s / %s", name, route.Primary, model.Protocol)
		}
	}
}
