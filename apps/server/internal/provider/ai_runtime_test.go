package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAIRuntimeChatStructuredFallsBackAndTracksModel(t *testing.T) {
	t.Setenv("AI_TEST_PRIMARY_KEY", "primary")
	t.Setenv("AI_TEST_FALLBACK_KEY", "fallback")
	var fallbackBody map[string]any
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer fallback" {
			t.Fatalf("unexpected fallback request: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&fallbackBody); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "req-fallback", "choices": []map[string]any{{"finish_reason": "stop", "message": map[string]any{"content": `{"reply":"可以"}`}}},
			"usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 10},
		})
	}))
	defer fallback.Close()
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "temporary", http.StatusServiceUnavailable)
	}))
	defer primary.Close()
	runtime, err := NewAIRuntime([]AIModel{
		{ID: "primary", Vendor: "aliyun", Protocol: "openai_chat_completions", Model: "qwen-plus", BaseURL: primary.URL, APIKeyEnv: "AI_TEST_PRIMARY_KEY", Timeout: time.Second},
		{ID: "fallback", Vendor: "aliyun", Protocol: "openai_chat_completions", Model: "qwen-flash", BaseURL: fallback.URL, APIKeyEnv: "AI_TEST_FALLBACK_KEY", StructuredMode: "json_object", Timeout: time.Second, InputCostPerMillion: .2, OutputCostPerMillion: .8, Parameters: map[string]any{"enable_thinking": false}},
	}, []AIRoute{{Capability: CapabilityAdvisorChat, Primary: "primary", Fallbacks: []string{"fallback"}}}, fallback.Client(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Structured(context.Background(), CapabilityAdvisorChat, StructuredRequest{Prompt: "问题", Instructions: "规则", SchemaName: "advisor", Schema: map[string]any{"type": "object"}, MaxOutputTokens: 100})
	if err != nil {
		t.Fatal(err)
	}
	if result.Meta.ModelID != "fallback" || result.Meta.FallbackReason == "" || result.Meta.RequestID != "req-fallback" || result.Meta.EstimatedCostCNY <= 0 {
		t.Fatalf("unexpected invocation metadata: %#v", result.Meta)
	}
	if fallbackBody["enable_thinking"] != false {
		t.Fatalf("model parameters were not forwarded: %#v", fallbackBody)
	}
	format := fallbackBody["response_format"].(map[string]any)
	if format["type"] != "json_object" {
		t.Fatalf("unexpected response format: %#v", format)
	}
}

func TestAIRuntimeDashScopeImageEditPersistsDownloadedBytes(t *testing.T) {
	t.Setenv("AI_TEST_WAN_KEY", "wan-key")
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/wan":
			if r.Header.Get("Authorization") != "Bearer wan-key" {
				t.Fatal("missing bearer token")
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			parameters := body["parameters"].(map[string]any)
			if parameters["size"] != "2K" || parameters["prompt_extend"] != nil {
				t.Fatalf("unexpected Wan parameters: %#v", parameters)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"request_id": "wan-1", "output": map[string]any{"choices": []map[string]any{{"message": map[string]any{"content": []map[string]string{{"image": server.URL + "/result.png"}}}}}}})
		case "/result.png":
			_, _ = w.Write(png)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	runtime, err := NewAIRuntime([]AIModel{{ID: "wan", Vendor: "aliyun", Protocol: "dashscope_wan", Model: "wan2.7-image-pro", BaseURL: server.URL + "/wan", APIKeyEnv: "AI_TEST_WAN_KEY", Timeout: time.Second, OutputImageCost: .5}}, []AIRoute{{Capability: CapabilityHairEdit, Primary: "wan", MaxCostCNY: .6}}, server.Client(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.EditImage(context.Background(), CapabilityHairEdit, ImageEditRequest{Prompt: "改发型", Images: []AnalysisImage{{MIMEType: "image/jpeg", Data: []byte("source")}}, Size: "2K"})
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Data) != string(png) || result.MIMEType != "image/png" || result.Meta.ProviderVersion() != "aliyun:dashscope_wan:wan2.7-image-pro" {
		t.Fatalf("unexpected image result: %#v", result)
	}
}

// 硅基流动：JSON 单端点，参考图放 image（data URL）、生成参数用 image_size、
// 结果容器是 images[0].url。
func TestAIRuntimeSiliconFlowImageEditSendsReferenceImage(t *testing.T) {
	t.Setenv("AI_TEST_SF_KEY", "sf-key")
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00}
	var body map[string]any
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/images/generations":
			if r.Header.Get("Authorization") != "Bearer sf-key" {
				t.Fatal("missing bearer token")
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"images": []map[string]string{{"url": server.URL + "/result.png"}}})
		case "/result.png":
			_, _ = w.Write(png)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	runtime, err := NewAIRuntime(
		[]AIModel{{
			ID: "kolors", Vendor: "siliconflow", Protocol: "siliconflow_image_edit",
			Model: "Kwai-Kolors/Kolors", BaseURL: server.URL + "/v1", APIKeyEnv: "AI_TEST_SF_KEY",
			Timeout: time.Second, Parameters: map[string]any{"image_size": "1024x768"},
		}},
		[]AIRoute{{Capability: CapabilityHairEdit, Primary: "kolors"}},
		server.Client(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.EditImage(context.Background(), CapabilityHairEdit, ImageEditRequest{
		Prompt: "换发型", Images: []AnalysisImage{{MIMEType: "image/jpeg", Data: []byte("source")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Data) != string(png) || result.MIMEType != "image/png" {
		t.Fatalf("unexpected image result: %#v", result)
	}
	if result.Meta.ProviderVersion() != "siliconflow:siliconflow_image_edit:Kwai-Kolors/Kolors" {
		t.Fatalf("unexpected provider version: %s", result.Meta.ProviderVersion())
	}
	if body["model"] != "Kwai-Kolors/Kolors" || body["prompt"] != "换发型" {
		t.Fatalf("unexpected request body: %#v", body)
	}
	if image, _ := body["image"].(string); !strings.HasPrefix(image, "data:image/jpeg;base64,") {
		t.Fatalf("reference image must be inlined as a data URL: %#v", body["image"])
	}
	if body["image_size"] != "1024x768" {
		t.Fatalf("model parameters were not forwarded: %#v", body)
	}
}

func TestAIRuntimeWanxImageEditSubmitsAndPollsTask(t *testing.T) {
	t.Setenv("AI_TEST_WANX_KEY", "wanx-key")
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/services/aigc/image2image/image-synthesis":
			if r.Header.Get("X-DashScope-Async") != "enable" || r.Header.Get("Authorization") != "Bearer wanx-key" {
				t.Fatal("missing Wanx async/auth headers")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"request_id": "submit-1", "output": map[string]any{"task_id": "task-1", "task_status": "PENDING"}})
		case "/api/v1/tasks/task-1":
			_ = json.NewEncoder(w).Encode(map[string]any{"request_id": "poll-1", "output": map[string]any{"task_status": "SUCCEEDED", "results": []map[string]string{{"url": server.URL + "/result.png"}}}})
		case "/result.png":
			_, _ = w.Write(png)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	runtime, err := NewAIRuntime([]AIModel{{ID: "wanx", Vendor: "aliyun", Protocol: "dashscope_wanx_imageedit", Model: "wanx2.1-imageedit", BaseURL: server.URL + "/api/v1/services/aigc/image2image/image-synthesis", APIKeyEnv: "AI_TEST_WANX_KEY", Timeout: 2 * time.Second, OutputImageCost: .14}}, []AIRoute{{Capability: CapabilityFullLookEdit, Primary: "wanx"}}, server.Client(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.EditImage(context.Background(), CapabilityFullLookEdit, ImageEditRequest{Prompt: "完整造型", Images: []AnalysisImage{{MIMEType: "image/jpeg", Data: []byte("source")}}})
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Data) != string(png) || result.MIMEType != "image/png" || result.Meta.ProviderVersion() != "aliyun:dashscope_wanx_imageedit:wanx2.1-imageedit" {
		t.Fatalf("unexpected Wanx result: %#v", result)
	}
}

func TestAIRuntimeFallsBackWhenDomainValidationRejectsOutput(t *testing.T) {
	t.Setenv("AI_TEST_VALIDATE_KEY", "key")
	primaryCalls, fallbackCalls := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		content := `{"value":"unsafe"}`
		if strings.Contains(r.URL.Path, "fallback") {
			fallbackCalls++
			content = `{"value":"safe"}`
		} else {
			primaryCalls++
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "request", "choices": []map[string]any{{"finish_reason": "stop", "message": map[string]any{"content": content}}}})
	}))
	defer server.Close()
	runtime, err := NewAIRuntime([]AIModel{
		{ID: "primary", Vendor: "a", Protocol: "openai_chat_completions", Model: "a", BaseURL: server.URL + "/primary", APIKeyEnv: "AI_TEST_VALIDATE_KEY", Timeout: time.Second},
		{ID: "fallback", Vendor: "b", Protocol: "openai_chat_completions", Model: "b", BaseURL: server.URL + "/fallback", APIKeyEnv: "AI_TEST_VALIDATE_KEY", Timeout: time.Second},
	}, []AIRoute{{Capability: CapabilityAdvisorChat, Primary: "primary", Fallbacks: []string{"fallback"}}}, server.Client(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Structured(context.Background(), CapabilityAdvisorChat, StructuredRequest{Prompt: "x", SchemaName: "x", Schema: map[string]any{"type": "object"}, MaxOutputTokens: 50, Validate: func(data []byte) error {
		if strings.Contains(string(data), "unsafe") {
			return io.ErrUnexpectedEOF
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if primaryCalls != 1 || fallbackCalls != 1 || result.Meta.ModelID != "fallback" || result.Meta.FallbackReason == "" {
		t.Fatalf("validation fallback failed: primary=%d fallback=%d meta=%#v", primaryCalls, fallbackCalls, result.Meta)
	}
}

func TestAIRuntimeCombinedErrorPreservesCauseChain(t *testing.T) {
	t.Setenv("AI_TEST_CHAIN_KEY", "key")
	sentinel := errors.New("generator contract violation")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "fallback") {
			// fallback 传输失败(403 配额),primary 域校验违约:两种失败都要留在错误链里。
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"message":"quota exhausted"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "request", "choices": []map[string]any{{"finish_reason": "stop", "message": map[string]any{"content": `{"value":"bad"}`}}}})
	}))
	defer server.Close()
	runtime, err := NewAIRuntime([]AIModel{
		{ID: "primary", Vendor: "a", Protocol: "openai_chat_completions", Model: "a", BaseURL: server.URL + "/primary", APIKeyEnv: "AI_TEST_CHAIN_KEY", Timeout: time.Second},
		{ID: "fallback", Vendor: "b", Protocol: "openai_chat_completions", Model: "b", BaseURL: server.URL + "/fallback", APIKeyEnv: "AI_TEST_CHAIN_KEY", Timeout: time.Second},
	}, []AIRoute{{Capability: CapabilityAdvisorChat, Primary: "primary", Fallbacks: []string{"fallback"}}}, server.Client(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Structured(context.Background(), CapabilityAdvisorChat, StructuredRequest{Prompt: "x", SchemaName: "x", Schema: map[string]any{"type": "object"}, MaxOutputTokens: 50, Validate: func(data []byte) error {
		return fmt.Errorf("%w: bad output", sentinel)
	}})
	if err == nil || !errors.Is(err, sentinel) {
		t.Fatalf("combined error must preserve the cause chain: %v", err)
	}
	if !strings.Contains(err.Error(), "all AI models failed") || !strings.Contains(err.Error(), "fallback") {
		t.Fatalf("message must stay human-readable: %v", err)
	}
}

func TestNormalizeStructuredJSONOnlyUnwrapsSingletonObjectArray(t *testing.T) {
	if got := string(normalizeStructuredJSON([]byte(`[{"ok":true}]`))); got != `{"ok":true}` {
		t.Fatalf("singleton object array was not normalized: %s", got)
	}
	for _, input := range []string{`[{"ok":true},{"ok":false}]`, `[1]`, `{"ok":true}`, `not-json`} {
		if got := string(normalizeStructuredJSON([]byte(input))); got != input {
			t.Fatalf("unexpected normalization of %s: %s", input, got)
		}
	}
}

func TestAIRuntimeArkImageEditAcceptsBase64AndBudgetSkipsExpensivePrimary(t *testing.T) {
	t.Setenv("AI_TEST_ARK_KEY", "ark-key")
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00}
	called := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		if r.URL.Path != "/images/generations" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "ark-1", "data": []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString(png)}}})
	}))
	defer server.Close()
	runtime, err := NewAIRuntime([]AIModel{
		{ID: "too-expensive", Vendor: "aliyun", Protocol: "dashscope_wan", Model: "wan", BaseURL: server.URL + "/never", APIKeyEnv: "AI_TEST_ARK_KEY", Timeout: time.Second, OutputImageCost: .7},
		{ID: "ark", Vendor: "volcengine", Protocol: "ark_image", Model: "seedream", BaseURL: server.URL, APIKeyEnv: "AI_TEST_ARK_KEY", Timeout: time.Second, OutputImageCost: .25},
	}, []AIRoute{{Capability: CapabilityFullLookEdit, Primary: "too-expensive", Fallbacks: []string{"ark"}, MaxCostCNY: .5}}, server.Client(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.EditImage(context.Background(), CapabilityFullLookEdit, ImageEditRequest{Prompt: "完整造型", Images: []AnalysisImage{{MIMEType: "image/jpeg", Data: []byte("source")}}})
	if err != nil {
		t.Fatal(err)
	}
	if called != 1 || result.Meta.ModelID != "ark" || !strings.Contains(result.Meta.FallbackReason, "exceeds route limit") {
		t.Fatalf("budget routing failed: called=%d meta=%#v", called, result.Meta)
	}
}

func TestNewAIRuntimeRequiresNamedSecret(t *testing.T) {
	const key = "AI_TEST_MISSING_KEY"
	_ = os.Unsetenv(key)
	_, err := NewAIRuntime([]AIModel{{ID: "x", Vendor: "x", Protocol: "openai_responses", Model: "x", BaseURL: "https://example.com", APIKeyEnv: key}}, []AIRoute{{Capability: CapabilityAppearanceAnalysis, Primary: "x"}}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), key) {
		t.Fatalf("expected missing secret error, got %v", err)
	}
}

// 失败与成功的 AI invocation 日志都必须携带任务标识,否则 worker 故障时
// 无法把零散的 WARN 关联回具体任务。
func TestAIRuntimeInvocationLogsCarryTaskSource(t *testing.T) {
	t.Setenv("AI_TEST_PRIMARY_KEY", "primary")
	t.Setenv("AI_TEST_FALLBACK_KEY", "fallback")
	var logs bytes.Buffer
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "req-fallback", "choices": []map[string]any{{"finish_reason": "stop", "message": map[string]any{"content": `{"reply":"可以"}`}}},
			"usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 10},
		})
	}))
	defer fallback.Close()
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "temporary", http.StatusServiceUnavailable)
	}))
	defer primary.Close()
	runtime, err := NewAIRuntime([]AIModel{
		{ID: "primary", Vendor: "aliyun", Protocol: "openai_chat_completions", Model: "qwen-plus", BaseURL: primary.URL, APIKeyEnv: "AI_TEST_PRIMARY_KEY", Timeout: time.Second},
		{ID: "fallback", Vendor: "aliyun", Protocol: "openai_chat_completions", Model: "qwen-flash", BaseURL: fallback.URL, APIKeyEnv: "AI_TEST_FALLBACK_KEY", StructuredMode: "json_object", Timeout: time.Second},
	}, []AIRoute{{Capability: CapabilityAdvisorChat, Primary: "primary", Fallbacks: []string{"fallback"}}}, fallback.Client(), slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithInvocationSource(context.Background(), "analysis:analysis-1")
	result, err := runtime.Structured(ctx, CapabilityAdvisorChat, StructuredRequest{Prompt: "问题", Instructions: "规则", SchemaName: "advisor", Schema: map[string]any{"type": "object"}, MaxOutputTokens: 100})
	if err != nil {
		t.Fatal(err)
	}
	if result.Meta.Source != "analysis:analysis-1" {
		t.Fatalf("result meta missing task source: %#v", result.Meta)
	}
	if got := strings.Count(logs.String(), "task=analysis:analysis-1"); got != 2 {
		t.Fatalf("expected task source on both failure and success log lines, got %d:\n%s", got, logs.String())
	}
}
