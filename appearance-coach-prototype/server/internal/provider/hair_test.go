package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/jianwo/server/internal/domain"
)

type hairGeneratorFunc func(context.Context, domain.HairPreviewInput) (HairPreviewOutput, error)

func (fn hairGeneratorFunc) Generate(ctx context.Context, input domain.HairPreviewInput) (HairPreviewOutput, error) {
	return fn(ctx, input)
}

func TestOpenAIHairGeneratorUsesHighFidelityImageEdit(t *testing.T) {
	generatedPNG := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/images/edits" || request.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected request target or authorization")
		}
		if err := request.ParseMultipartForm(2 << 20); err != nil {
			t.Errorf("parse multipart request: %v", err)
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		if request.FormValue("model") != "gpt-image-2" || request.FormValue("input_fidelity") != "high" || request.FormValue("size") != "1024x1536" {
			t.Errorf("missing image edit controls: %#v", request.MultipartForm.Value)
		}
		if !strings.Contains(request.FormValue("prompt"), "严格保留同一个人的身份") {
			t.Errorf("identity preservation is missing from prompt")
		}
		file, _, err := request.FormFile("image")
		if err != nil {
			t.Errorf("missing input image: %v", err)
		} else {
			data, _ := io.ReadAll(file)
			_ = file.Close()
			if string(data) != "source-photo" {
				t.Errorf("unexpected source image")
			}
		}
		_ = json.NewEncoder(response).Encode(map[string]any{"data": []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString(generatedPNG)}}})
	}))
	defer server.Close()

	generator, err := NewOpenAIHairGenerator(OpenAIHairConfig{APIKey: "test-key", BaseURL: server.URL, Model: "gpt-image-2", Quality: "medium"}, stubMediaLoader{images: []AnalysisImage{{ID: "face", Kind: "face", MIMEType: "image/jpeg", Data: []byte("source-photo")}}}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	output, err := generator.Generate(context.Background(), domain.HairPreviewInput{MediaID: "face", StyleID: "sharp", Scene: "daily"})
	if err != nil {
		t.Fatal(err)
	}
	if string(output.ImageData) != string(generatedPNG) || output.ProviderVersion != "openai-image-edit:gpt-image-2" {
		t.Fatalf("unexpected output: %#v", output)
	}
}

func TestFallbackHairGeneratorUsesDemo(t *testing.T) {
	primary := hairGeneratorFunc(func(context.Context, domain.HairPreviewInput) (HairPreviewOutput, error) {
		return HairPreviewOutput{}, errors.New("upstream unavailable")
	})
	output, err := NewFallbackHairGenerator(primary, NewDemoHairGenerator()).Generate(context.Background(), domain.HairPreviewInput{StyleID: "warm"})
	if err != nil || output.ProviderVersion != "demo-hair-v1" || output.ImageURL == "" {
		t.Fatalf("fallback failed: output=%#v err=%v", output, err)
	}
}
