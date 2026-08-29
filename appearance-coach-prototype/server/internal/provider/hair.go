package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	"github.com/example/jianwo/server/internal/domain"
)

type HairPreviewOutput struct {
	ImageData       []byte
	MIMEType        string
	ImageURL        string
	ProviderVersion string
}

type HairPreviewGenerator interface {
	Generate(context.Context, domain.HairPreviewInput) (HairPreviewOutput, error)
}

type DemoHairGenerator struct{}

func NewDemoHairGenerator() *DemoHairGenerator { return &DemoHairGenerator{} }

func (*DemoHairGenerator) Generate(_ context.Context, input domain.HairPreviewInput) (HairPreviewOutput, error) {
	paths := map[string]string{"sharp": "/assets/looks/sharp.png", "warm": "/assets/looks/warm.png", "natural": "/assets/looks/natural.png"}
	path := paths[input.StyleID]
	if path == "" {
		return HairPreviewOutput{}, fmt.Errorf("unsupported hair style")
	}
	return HairPreviewOutput{ImageURL: path, ProviderVersion: "demo-hair-v1"}, nil
}

type OpenAIHairConfig struct {
	APIKey  string
	BaseURL string
	Model   string
	Quality string
	Timeout time.Duration
}

type OpenAIHairGenerator struct {
	config OpenAIHairConfig
	loader MediaLoader
	client *http.Client
}

func NewOpenAIHairGenerator(config OpenAIHairConfig, loader MediaLoader, client *http.Client) (*OpenAIHairGenerator, error) {
	if strings.TrimSpace(config.APIKey) == "" {
		return nil, errors.New("OPENAI_API_KEY is required for openai hair preview provider")
	}
	if loader == nil {
		return nil, errors.New("media loader is required for hair preview provider")
	}
	if config.BaseURL == "" {
		config.BaseURL = "https://api.openai.com/v1"
	}
	if config.Model == "" {
		config.Model = "gpt-image-2"
	}
	if config.Quality == "" {
		config.Quality = "medium"
	}
	if config.Timeout <= 0 {
		config.Timeout = 150 * time.Second
	}
	if client == nil {
		client = &http.Client{Timeout: config.Timeout}
	}
	return &OpenAIHairGenerator{config: config, loader: loader, client: client}, nil
}

func (g *OpenAIHairGenerator) Generate(ctx context.Context, input domain.HairPreviewInput) (HairPreviewOutput, error) {
	images, err := g.loader.Load(ctx, []string{input.MediaID})
	if err != nil {
		return HairPreviewOutput{}, fmt.Errorf("load hair source photo: %w", err)
	}
	if len(images) != 1 {
		return HairPreviewOutput{}, fmt.Errorf("expected one hair source photo")
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writeImagePart(writer, images[0]); err != nil {
		return HairPreviewOutput{}, err
	}
	fields := map[string]string{
		"model": g.config.Model, "prompt": hairPrompt(input.StyleID), "size": "1024x1536",
		"quality": g.config.Quality, "input_fidelity": "high", "output_format": "png",
	}
	for name, value := range fields {
		if err := writer.WriteField(name, value); err != nil {
			return HairPreviewOutput{}, err
		}
	}
	if err := writer.Close(); err != nil {
		return HairPreviewOutput{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(g.config.BaseURL, "/")+"/images/edits", &body)
	if err != nil {
		return HairPreviewOutput{}, err
	}
	request.Header.Set("Authorization", "Bearer "+g.config.APIKey)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := g.client.Do(request)
	if err != nil {
		return HairPreviewOutput{}, fmt.Errorf("hair preview request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return HairPreviewOutput{}, fmt.Errorf("hair preview request returned status %d", response.StatusCode)
	}
	var payload struct {
		Data []struct {
			B64JSON string `json:"b64_json"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 32<<20)).Decode(&payload); err != nil {
		return HairPreviewOutput{}, fmt.Errorf("decode hair preview response: %w", err)
	}
	if len(payload.Data) != 1 || payload.Data[0].B64JSON == "" {
		return HairPreviewOutput{}, fmt.Errorf("hair preview response contained no image")
	}
	data, err := base64.StdEncoding.DecodeString(payload.Data[0].B64JSON)
	if err != nil {
		return HairPreviewOutput{}, fmt.Errorf("decode generated hair image: %w", err)
	}
	if len(data) == 0 || len(data) > 20<<20 {
		return HairPreviewOutput{}, fmt.Errorf("generated hair image has invalid size")
	}
	if http.DetectContentType(data) != "image/png" {
		return HairPreviewOutput{}, fmt.Errorf("generated hair image has invalid format")
	}
	return HairPreviewOutput{ImageData: data, MIMEType: "image/png", ProviderVersion: "openai-image-edit:" + g.config.Model}, nil
}

func writeImagePart(writer *multipart.Writer, image AnalysisImage) error {
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="image"; filename="source.jpg"`)
	header.Set("Content-Type", image.MIMEType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return err
	}
	_, err = part.Write(image.Data)
	return err
}

func hairPrompt(styleID string) string {
	styles := map[string]string{
		"sharp":   "锁骨长度的轻盈层次发，颅顶自然蓬松，6:4 侧分，脸侧有少量空气感碎发，发尾干净利落",
		"warm":    "锁骨长度的空气微卷，柔和的大弧度发尾，颅顶自然蓬松，整体温柔明亮",
		"natural": "保持当前发长和发色，只调整为自然偏分并整理耳侧线条，低维护、真实日常",
	}
	return "仅编辑人物发型为：" + styles[styleID] + "。必须严格保留同一个人的身份、脸型、五官、表情、肤色、妆容、身体、服装、姿势、背景、光线和镜头构图；不要改变年龄感，不磨皮，不添加首饰或文字。输出写实摄影效果，不是插画。"
}

type FallbackHairGenerator struct {
	primary  HairPreviewGenerator
	fallback HairPreviewGenerator
}

func NewFallbackHairGenerator(primary, fallback HairPreviewGenerator) *FallbackHairGenerator {
	return &FallbackHairGenerator{primary: primary, fallback: fallback}
}

func (g *FallbackHairGenerator) Generate(ctx context.Context, input domain.HairPreviewInput) (HairPreviewOutput, error) {
	output, err := g.primary.Generate(ctx, input)
	if err == nil {
		return output, nil
	}
	return g.fallback.Generate(ctx, input)
}
