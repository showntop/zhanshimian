package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
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
