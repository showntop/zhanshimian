package provider

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	providerai "github.com/zhanshimian/server/internal/provider/ai"
)

// invocationStoreFake 是 providerai.InvocationStore 的内存实现：记录每次
// start/finish，供断言台账行内容（不含提示词、图片字节或签名 URL）。
type invocationStoreFake struct {
	mu       sync.Mutex
	startID  string
	starts   []domain.StartInvocation
	finishes []domain.FinishInvocation
}

func (f *invocationStoreFake) StartInvocation(_ context.Context, in domain.StartInvocation) (domain.ProviderInvocation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts = append(f.starts, in)
	return domain.ProviderInvocation{ID: f.startID, UserID: in.UserID, TaskID: in.TaskID, AttemptNo: in.AttemptNo}, nil
}

func (f *invocationStoreFake) FinishInvocation(_ context.Context, in domain.FinishInvocation) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.finishes = append(f.finishes, in)
	return nil
}

func scopedInvocationCtx() context.Context {
	return domain.WithInvocationScope(context.Background(), domain.InvocationScope{
		UserID: "user-1", OperationID: "op-1", TaskID: "task-1", AttemptNo: 2,
	})
}

func newRecordingRuntime(t *testing.T, store *invocationStoreFake, server *httptest.Server) *AIRuntime {
	t.Helper()
	t.Setenv("AI_TEST_LEDGER_KEY", "ledger-key")
	runtime, err := NewAIRuntime([]AIModel{
		{ID: "vision", Vendor: "aliyun", Protocol: "openai_chat_completions", Model: "qwen-plus", BaseURL: server.URL, APIKeyEnv: "AI_TEST_LEDGER_KEY", Timeout: time.Second},
	}, []AIRoute{{Capability: CapabilityAppearanceAnalysis, Primary: "vision"}}, server.Client(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	runtime.SetInvocationRecorder(providerai.NewInvocationRecorder(store, nil), "ai-routing:test#v1")
	return runtime
}

func chatServer(t *testing.T, requestID string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      requestID,
			"choices": []map[string]any{{"finish_reason": "stop", "message": map[string]any{"content": `{"ok":true}`}}},
			"usage":   map[string]any{"prompt_tokens": 120, "completion_tokens": 12},
		})
	}))
}

// 台账存在的意义：reports/plan_sets/render_candidates 的 provider_invocation_id
// 是 NOT NULL 外键。scope 注入后，结构化调用必须先落台账行并把行 ID 返回给
// 业务 handler，否则发布事务在 baseline 上必然失败。
func TestAIRuntimeRecordsStructuredInvocationWhenScoped(t *testing.T) {
	store := &invocationStoreFake{startID: "inv-1"}
	server := chatServer(t, "req-ledger")
	defer server.Close()
	runtime := newRecordingRuntime(t, store, server)

	result, err := runtime.Structured(scopedInvocationCtx(), CapabilityAppearanceAnalysis,
		StructuredRequest{Prompt: "秘密提示词", Instructions: "规则", SchemaName: "report", Schema: map[string]any{"type": "object"}, MaxOutputTokens: 100,
			Images: []AnalysisImage{{Kind: "face", MIMEType: "image/jpeg", Data: []byte("bytes")}}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Meta.InvocationID != "inv-1" {
		t.Fatalf("result meta must carry the ledger row id, got %#v", result.Meta)
	}
	if result.Meta.RequestID != "req-ledger" {
		t.Fatalf("provider request id must stay separate from the ledger id: %#v", result.Meta)
	}
	if len(store.starts) != 1 || len(store.finishes) != 1 {
		t.Fatalf("expected one start and one finish, got %d/%d", len(store.starts), len(store.finishes))
	}
	start := store.starts[0]
	if start.UserID != "user-1" || start.OperationID != "op-1" || start.TaskID != "task-1" || start.AttemptNo != 2 {
		t.Fatalf("ledger row must link the exact operation/task/attempt: %#v", start)
	}
	if start.Capability != CapabilityAppearanceAnalysis || start.ModelKey != "vision" || start.ProviderKey != "aliyun" || start.RoutingConfigVersion != "ai-routing:test#v1" {
		t.Fatalf("ledger row must carry routing metadata: %#v", start)
	}
	if len(start.RequestHash) != 64 || strings.Contains(start.RequestHash, "秘密提示词") {
		t.Fatalf("request hash must be a redacted 64-hex digest, got %q", start.RequestHash)
	}
	finish := store.finishes[0]
	if finish.InvocationID != "inv-1" || finish.Status != domain.InvocationSucceeded || finish.ProviderRequestID != "req-ledger" {
		t.Fatalf("unexpected finish row: %#v", finish)
	}
	if finish.InputTokens == nil || *finish.InputTokens != 120 || finish.OutputTokens == nil || *finish.OutputTokens != 12 {
		t.Fatalf("token usage must be recorded: %#v", finish)
	}
}

// 无 scope 的调用方（如同步 API 路径）保持现状：不写台账、InvocationID 为空。
func TestAIRuntimeSkipsRecordingWithoutScope(t *testing.T) {
	store := &invocationStoreFake{startID: "inv-1"}
	server := chatServer(t, "req-plain")
	defer server.Close()
	runtime := newRecordingRuntime(t, store, server)

	result, err := runtime.Structured(context.Background(), CapabilityAppearanceAnalysis,
		StructuredRequest{Prompt: "x", SchemaName: "x", Schema: map[string]any{"type": "object"}, MaxOutputTokens: 50})
	if err != nil {
		t.Fatal(err)
	}
	if result.Meta.InvocationID != "" {
		t.Fatalf("scope-less calls must not fabricate a ledger id: %#v", result.Meta)
	}
	if len(store.starts) != 0 {
		t.Fatalf("scope-less calls must not write the ledger: %#v", store.starts)
	}
}

// fallback 链上每次模型尝试都是一行台账：失败行带 typed 状态，成功行的 ID
// 才是业务侧引用的 invocation。
func TestAIRuntimeRecordsEachAttemptAcrossFallback(t *testing.T) {
	store := &invocationStoreFake{startID: "inv-2"}
	t.Setenv("AI_TEST_LEDGER_KEY", "ledger-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "primary") {
			http.Error(w, "quota", http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "req-fallback",
			"choices": []map[string]any{{"finish_reason": "stop", "message": map[string]any{"content": `{"ok":true}`}}},
		})
	}))
	defer server.Close()
	runtime, err := NewAIRuntime([]AIModel{
		{ID: "primary", Vendor: "aliyun", Protocol: "openai_chat_completions", Model: "a", BaseURL: server.URL + "/primary", APIKeyEnv: "AI_TEST_LEDGER_KEY", Timeout: time.Second},
		{ID: "fallback", Vendor: "aliyun", Protocol: "openai_chat_completions", Model: "b", BaseURL: server.URL + "/fallback", APIKeyEnv: "AI_TEST_LEDGER_KEY", Timeout: time.Second},
	}, []AIRoute{{Capability: CapabilityAppearanceAnalysis, Primary: "primary", Fallbacks: []string{"fallback"}}}, server.Client(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	runtime.SetInvocationRecorder(providerai.NewInvocationRecorder(store, nil), "ai-routing:test#v1")

	result, err := runtime.Structured(scopedInvocationCtx(), CapabilityAppearanceAnalysis,
		StructuredRequest{Prompt: "x", SchemaName: "x", Schema: map[string]any{"type": "object"}, MaxOutputTokens: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(store.starts) != 2 || len(store.finishes) != 2 {
		t.Fatalf("each model attempt needs its own ledger row: %d/%d", len(store.starts), len(store.finishes))
	}
	if store.starts[0].ModelKey != "primary" || store.starts[1].ModelKey != "fallback" {
		t.Fatalf("attempt rows must name the attempted model: %#v", store.starts)
	}
	if store.finishes[0].Status != domain.InvocationFailed || store.finishes[0].ErrorClass == "" {
		t.Fatalf("failed attempt must record a typed error: %#v", store.finishes[0])
	}
	if store.finishes[1].Status != domain.InvocationSucceeded {
		t.Fatalf("fallback success must be recorded: %#v", store.finishes[1])
	}
	if result.Meta.InvocationID != "inv-2" || result.Meta.ModelID != "fallback" {
		t.Fatalf("result must reference the successful invocation: %#v", result.Meta)
	}
}

// 渲染生成走 EditImageOnModel，同样必须先落台账：render_candidates 与
// provider_output 媒体行都引用这次 invocation。
func TestEditImageOnModelRecordsInvocationWhenScoped(t *testing.T) {
	store := &invocationStoreFake{startID: "inv-3"}
	t.Setenv("AI_TEST_LEDGER_KEY", "ledger-key")
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/result.png" {
			_, _ = w.Write(png)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"request_id": "wan-ledger", "output": map[string]any{"choices": []map[string]any{{"message": map[string]any{"content": []map[string]string{{"image": server.URL + "/result.png"}}}}}}})
	}))
	defer server.Close()
	runtime, err := NewAIRuntime([]AIModel{
		{ID: "wan", Vendor: "aliyun", Protocol: "dashscope_wan", Model: "wan2.7-image-pro", BaseURL: server.URL + "/wan", APIKeyEnv: "AI_TEST_LEDGER_KEY", Timeout: time.Second},
	}, []AIRoute{{Capability: CapabilityFullLookEdit, Primary: "wan"}}, server.Client(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	runtime.SetInvocationRecorder(providerai.NewInvocationRecorder(store, nil), "ai-routing:test#v1")

	result, err := runtime.EditImageOnModel(scopedInvocationCtx(), "wan", CapabilityFullLookEdit,
		ImageEditRequest{Prompt: "渲染", Images: []AnalysisImage{{Kind: "body", MIMEType: "image/jpeg", Data: []byte("source")}}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Meta.InvocationID != "inv-3" {
		t.Fatalf("image result must carry the ledger row id: %#v", result.Meta)
	}
	if len(store.starts) != 1 || store.starts[0].Capability != CapabilityFullLookEdit || store.starts[0].ModelKey != "wan" {
		t.Fatalf("unexpected image ledger start: %#v", store.starts)
	}
	if len(store.finishes) != 1 || store.finishes[0].Status != domain.InvocationSucceeded {
		t.Fatalf("unexpected image ledger finish: %#v", store.finishes)
	}
}

// provider 返回 200 但载荷过不了域校验时，这次 invocation 必须记为失败——
// 否则“每阶段成功率”会把坏输出算成成功。
func TestAIRuntimeRecordsValidationFailureAsFailedInvocation(t *testing.T) {
	store := &invocationStoreFake{startID: "inv-4"}
	server := chatServer(t, "req-invalid")
	defer server.Close()
	runtime := newRecordingRuntime(t, store, server)

	_, err := runtime.Structured(scopedInvocationCtx(), CapabilityAppearanceAnalysis,
		StructuredRequest{Prompt: "x", SchemaName: "x", Schema: map[string]any{"type": "object"}, MaxOutputTokens: 50,
			Validate: func([]byte) error { return io.ErrUnexpectedEOF }})
	if err == nil {
		t.Fatal("validation failure must surface")
	}
	if len(store.finishes) != 1 || store.finishes[0].Status != domain.InvocationFailed {
		t.Fatalf("validation failure must finish the ledger row as failed: %#v", store.finishes)
	}
}
