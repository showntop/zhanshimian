package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// 渲染契约(2026-09-12-rendering-quality):full_look_edit 路由已删除且不得
// 保留别名;full_look_generation 只允许多图模型(wanx2.1 单图协议会丢掉人脸
// 参考帧,永不得入选);render_quality_evaluation 走结构化视觉模型。两份出货
// 配置都必须满足硬性入选条件。
func TestShippedConfigsCutOverToRenderingRoutes(t *testing.T) {
	t.Setenv("BAILIAN_WORKSPACE_ID", "workspace-test")
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
		if err := json.Unmarshal(expandAIRoutingEnv(raw), &routing); err != nil {
			t.Fatalf("decode %s: %v", name, err)
		}
		if _, ok := routing.Routes["full_look_edit"]; ok {
			t.Fatalf("%s must not keep the deleted full_look_edit route or an alias", name)
		}
		generation, ok := routing.Routes["full_look_generation"]
		if !ok {
			t.Fatalf("%s missing full_look_generation route", name)
		}
		for _, key := range append([]string{generation.Primary}, generation.Fallbacks...) {
			model, ok := routing.Models[key]
			if !ok {
				t.Fatalf("%s full_look_generation candidate %q is not in models", name, key)
			}
			if model.Protocol == "dashscope_wanx_imageedit" {
				t.Fatalf("%s full_look_generation candidate %q is single-image wanx (drops face ref)", name, key)
			}
		}
		if _, ok := routing.Routes["render_quality_evaluation"]; !ok {
			t.Fatalf("%s missing render_quality_evaluation route", name)
		}
		if err := validateAIRouting(routing); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}
