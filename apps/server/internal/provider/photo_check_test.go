package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/example/jianwo/server/internal/domain"
)

func TestValidatePhotoCheckPayload(t *testing.T) {
	valid := photoCheckPayload{Results: []photoCheckItem{{Kind: "face", OK: true}, {Kind: "side", OK: true}, {Kind: "body", OK: true}}}
	if err := validatePhotoCheckPayload(valid); err != nil {
		t.Fatalf("valid payload rejected: %v", err)
	}
	cases := []photoCheckPayload{
		{Results: []photoCheckItem{{Kind: "face", OK: true}, {Kind: "side", OK: true}}},
		{Results: []photoCheckItem{{Kind: "face", OK: true}, {Kind: "face", OK: true}, {Kind: "body", OK: true}}},
		{Results: []photoCheckItem{{Kind: "face", OK: true}, {Kind: "pet", OK: true}, {Kind: "body", OK: true}}},
		{Results: []photoCheckItem{{Kind: "face", OK: true, Reason: "矛盾"}, {Kind: "side", OK: true}, {Kind: "body", OK: true}}},
		{Results: []photoCheckItem{{Kind: "face", OK: false}, {Kind: "side", OK: true}, {Kind: "body", OK: true}}},
	}
	for index, payload := range cases {
		if err := validatePhotoCheckPayload(payload); err == nil {
			t.Fatalf("case %d should have been rejected", index)
		}
	}
}

func photoCheckTestImages() []AnalysisImage {
	return []AnalysisImage{
		{ID: "1", Kind: "face", MIMEType: "image/jpeg", Data: []byte("face")},
		{ID: "2", Kind: "side", MIMEType: "image/jpeg", Data: []byte("side")},
		{ID: "3", Kind: "body", MIMEType: "image/jpeg", Data: []byte("body")},
	}
}

func chatServer(t *testing.T, content func(r *http.Request) string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "req",
			"choices": []map[string]any{{"finish_reason": "stop", "message": map[string]any{"content": content(r)}}},
			"usage":   map[string]any{"prompt_tokens": 10, "completion_tokens": 10},
		})
	}))
}

func TestRoutedAnalyzerRejectsNonConformingPhotos(t *testing.T) {
	t.Setenv("AI_TEST_CHECK_KEY", "key")
	analysisCalls := 0
	analysis := chatServer(t, func(_ *http.Request) string {
		analysisCalls++
		payload, _ := json.Marshal(validAnalysisPayload())
		return string(payload)
	})
	defer analysis.Close()
	check := chatServer(t, func(_ *http.Request) string {
		return `{"results":[{"kind":"face","ok":false,"reason":"未检测到清晰人脸"},{"kind":"side","ok":true,"reason":""},{"kind":"body","ok":true,"reason":""}]}`
	})
	defer check.Close()
	runtime, err := NewAIRuntime([]AIModel{
		{ID: "flash", Vendor: "aliyun", Protocol: "openai_chat_completions", Model: "qwen-flash", BaseURL: check.URL, APIKeyEnv: "AI_TEST_CHECK_KEY", Timeout: time.Second},
		{ID: "plus", Vendor: "aliyun", Protocol: "openai_chat_completions", Model: "qwen-plus", BaseURL: analysis.URL, APIKeyEnv: "AI_TEST_CHECK_KEY", Timeout: time.Second},
	}, []AIRoute{
		{Capability: CapabilityPhotoCheck, Primary: "flash"},
		{Capability: CapabilityAppearanceAnalysis, Primary: "plus"},
	}, check.Client(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	analyzer, err := NewRoutedAnalyzer(runtime, stubMediaLoader{images: photoCheckTestImages()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = analyzer.Analyze(context.Background(), domain.CreateAnalysisInput{MediaIDs: []string{"1", "2", "3"}})
	var rejected *PhotoRejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("expected PhotoRejectedError, got %v", err)
	}
	if len(rejected.Rejections) != 1 || rejected.Rejections[0].Kind != "face" || !strings.Contains(rejected.UserMessage(), "正脸照片") {
		t.Fatalf("unexpected rejection: %#v", rejected)
	}
	if analysisCalls != 0 {
		t.Fatalf("analysis model was called %d times despite rejected photos", analysisCalls)
	}
}

func TestRoutedAnalyzerRunsCheckBeforeAnalysis(t *testing.T) {
	t.Setenv("AI_TEST_CHECK_KEY", "key")
	analysisCalls := 0
	analysis := chatServer(t, func(_ *http.Request) string {
		analysisCalls++
		payload, _ := json.Marshal(validAnalysisPayload())
		return string(payload)
	})
	defer analysis.Close()
	check := chatServer(t, func(_ *http.Request) string {
		return `{"results":[{"kind":"face","ok":true,"reason":""},{"kind":"side","ok":true,"reason":""},{"kind":"body","ok":true,"reason":""}]}`
	})
	defer check.Close()
	runtime, err := NewAIRuntime([]AIModel{
		{ID: "flash", Vendor: "aliyun", Protocol: "openai_chat_completions", Model: "qwen-flash", BaseURL: check.URL, APIKeyEnv: "AI_TEST_CHECK_KEY", Timeout: time.Second},
		{ID: "plus", Vendor: "aliyun", Protocol: "openai_chat_completions", Model: "qwen-plus", BaseURL: analysis.URL, APIKeyEnv: "AI_TEST_CHECK_KEY", Timeout: time.Second},
	}, []AIRoute{
		{Capability: CapabilityPhotoCheck, Primary: "flash"},
		{Capability: CapabilityAppearanceAnalysis, Primary: "plus"},
	}, check.Client(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	analyzer, err := NewRoutedAnalyzer(runtime, stubMediaLoader{images: photoCheckTestImages()})
	if err != nil {
		t.Fatal(err)
	}
	output, err := analyzer.Analyze(context.Background(), domain.CreateAnalysisInput{MediaIDs: []string{"1", "2", "3"}})
	if err != nil {
		t.Fatal(err)
	}
	if analysisCalls != 1 || len(output.Plans) != 3 {
		t.Fatalf("analysis did not run after a passing check: calls=%d output=%#v", analysisCalls, output)
	}
}

func TestRoutedAnalyzerSkipsCheckWithoutRoute(t *testing.T) {
	t.Setenv("AI_TEST_CHECK_KEY", "key")
	analysis := chatServer(t, func(_ *http.Request) string {
		payload, _ := json.Marshal(validAnalysisPayload())
		return string(payload)
	})
	defer analysis.Close()
	runtime, err := NewAIRuntime([]AIModel{
		{ID: "plus", Vendor: "aliyun", Protocol: "openai_chat_completions", Model: "qwen-plus", BaseURL: analysis.URL, APIKeyEnv: "AI_TEST_CHECK_KEY", Timeout: time.Second},
	}, []AIRoute{
		{Capability: CapabilityAppearanceAnalysis, Primary: "plus"},
	}, analysis.Client(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	analyzer, err := NewRoutedAnalyzer(runtime, stubMediaLoader{images: photoCheckTestImages()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := analyzer.Analyze(context.Background(), domain.CreateAnalysisInput{MediaIDs: []string{"1", "2", "3"}}); err != nil {
		t.Fatalf("analysis without photo_check route should not be gated: %v", err)
	}
}
