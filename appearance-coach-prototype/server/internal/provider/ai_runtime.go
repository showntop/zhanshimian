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
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	CapabilityAppearanceAnalysis = "appearance_analysis"
	CapabilityPhotoCheck         = "photo_check"
	CapabilityOutfitDiagnosis    = "outfit_diagnosis"
	CapabilityPurchaseDiagnosis  = "purchase_diagnosis"
	CapabilityAdvisorChat        = "advisor_chat"
	CapabilityHairEdit           = "hair_edit"
	CapabilityMakeupEdit         = "makeup_edit"
	CapabilityFullLookEdit       = "full_look_edit"
)

type AIModel struct {
	ID                   string
	Vendor               string
	Protocol             string
	Model                string
	BaseURL              string
	APIKeyEnv            string
	StructuredMode       string
	Parameters           map[string]any
	Timeout              time.Duration
	InputCostPerMillion  float64
	OutputCostPerMillion float64
	InputImageCost       float64
	OutputImageCost      float64
}

type AIRoute struct {
	Capability string
	Policy     string
	Primary    string
	Fallbacks  []string
	MaxCostCNY float64
}

type StructuredRequest struct {
	Instructions    string
	Prompt          string
	Images          []AnalysisImage
	SchemaName      string
	Schema          map[string]any
	MaxOutputTokens int
	Validate        func([]byte) error
}

type ImageEditRequest struct {
	Prompt  string
	Images  []AnalysisImage
	Size    string
	Quality string
}

type InvocationMeta struct {
	Capability       string  `json:"capability"`
	ModelID          string  `json:"model_id"`
	Vendor           string  `json:"vendor"`
	Protocol         string  `json:"protocol"`
	Model            string  `json:"model"`
	RequestID        string  `json:"request_id,omitempty"`
	LatencyMS        int64   `json:"latency_ms"`
	InputTokens      int     `json:"input_tokens,omitempty"`
	OutputTokens     int     `json:"output_tokens,omitempty"`
	InputImages      int     `json:"input_images,omitempty"`
	OutputImages     int     `json:"output_images,omitempty"`
	EstimatedCostCNY float64 `json:"estimated_cost_cny,omitempty"`
	FallbackReason   string  `json:"fallback_reason,omitempty"`
}

func (m InvocationMeta) ProviderVersion() string {
	return m.Vendor + ":" + m.Protocol + ":" + m.Model
}

type StructuredResult struct {
	JSON []byte
	Meta InvocationMeta
}

type ImageEditResult struct {
	Data     []byte
	MIMEType string
	Meta     InvocationMeta
}

type AIRuntime struct {
	models map[string]AIModel
	routes map[string]AIRoute
	client *http.Client
	logger *slog.Logger
}

func NewAIRuntime(models []AIModel, routes []AIRoute, client *http.Client, logger *slog.Logger) (*AIRuntime, error) {
	if client == nil {
		client = &http.Client{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	runtime := &AIRuntime{models: map[string]AIModel{}, routes: map[string]AIRoute{}, client: client, logger: logger}
	for _, model := range models {
		if model.ID == "" || model.Model == "" || model.Vendor == "" || model.Protocol == "" || model.BaseURL == "" || model.APIKeyEnv == "" {
			return nil, errors.New("AI model configuration is incomplete")
		}
		if strings.TrimSpace(os.Getenv(model.APIKeyEnv)) == "" {
			return nil, fmt.Errorf("%s is required by AI model %q", model.APIKeyEnv, model.ID)
		}
		if model.Timeout <= 0 {
			model.Timeout = 90 * time.Second
		}
		runtime.models[model.ID] = model
	}
	for _, route := range routes {
		if _, ok := runtime.models[route.Primary]; !ok {
			return nil, fmt.Errorf("AI route %q has unknown primary %q", route.Capability, route.Primary)
		}
		for _, fallback := range route.Fallbacks {
			if _, ok := runtime.models[fallback]; !ok {
				return nil, fmt.Errorf("AI route %q has unknown fallback %q", route.Capability, fallback)
			}
		}
		runtime.routes[route.Capability] = route
	}
	return runtime, nil
}

func (r *AIRuntime) HasRoute(capability string) bool {
	_, ok := r.routes[capability]
	return ok
}

func (r *AIRuntime) RouteSummary() map[string]string {
	result := make(map[string]string, len(r.routes))
	for capability, route := range r.routes {
		result[capability] = route.Primary
	}
	return result
}

func (r *AIRuntime) Structured(ctx context.Context, capability string, input StructuredRequest) (StructuredResult, error) {
	route, ok := r.routes[capability]
	if !ok {
		return StructuredResult{}, fmt.Errorf("no AI route configured for %s", capability)
	}
	var causes []string
	for index, modelID := range append([]string{route.Primary}, route.Fallbacks...) {
		model := r.models[modelID]
		worstCaseOutputCost := float64(input.MaxOutputTokens)*model.OutputCostPerMillion/1_000_000 + float64(len(input.Images))*model.InputImageCost
		if route.MaxCostCNY > 0 && worstCaseOutputCost > route.MaxCostCNY {
			causes = append(causes, fmt.Sprintf("%s: output budget %.4f exceeds route limit %.4f", modelID, worstCaseOutputCost, route.MaxCostCNY))
			continue
		}
		if model.Protocol != "openai_responses" && model.Protocol != "openai_chat_completions" {
			causes = append(causes, modelID+": incompatible structured protocol")
			continue
		}
		var result StructuredResult
		var err error
		if model.Protocol == "openai_chat_completions" {
			result, err = r.openAIChatCompletions(ctx, capability, model, input)
		} else {
			result, err = r.openAIResponses(ctx, capability, model, input)
		}
		if err == nil {
			result.JSON = normalizeStructuredJSON(result.JSON)
			if input.Validate != nil {
				err = input.Validate(result.JSON)
			}
		}
		if err == nil {
			if index > 0 {
				result.Meta.FallbackReason = strings.Join(causes, "; ")
			}
			r.logInvocation(result.Meta, nil)
			return result, nil
		}
		causes = append(causes, modelID+": "+err.Error())
		failedMeta := result.Meta
		if failedMeta.ModelID == "" {
			failedMeta = InvocationMeta{Capability: capability, ModelID: model.ID, Vendor: model.Vendor, Protocol: model.Protocol, Model: model.Model}
		}
		r.logInvocation(failedMeta, err)
	}
	return StructuredResult{}, fmt.Errorf("all AI models failed for %s: %s", capability, strings.Join(causes, "; "))
}

// normalizeStructuredJSON handles a provider quirk seen in some compatible
// endpoints where a single JSON object is wrapped in an array. It is
// deliberately narrow: multi-item arrays are not unwrapped, and the domain
// validator still enforces the complete output contract afterwards.
func normalizeStructuredJSON(data []byte) []byte {
	var value any
	if json.Unmarshal(data, &value) != nil {
		return data
	}
	items, ok := value.([]any)
	if !ok || len(items) != 1 {
		return data
	}
	object, ok := items[0].(map[string]any)
	if !ok {
		return data
	}
	normalized, err := json.Marshal(object)
	if err != nil {
		return data
	}
	return normalized
}

func (r *AIRuntime) EditImage(ctx context.Context, capability string, input ImageEditRequest) (ImageEditResult, error) {
	route, ok := r.routes[capability]
	if !ok {
		return ImageEditResult{}, fmt.Errorf("no AI route configured for %s", capability)
	}
	var causes []string
	for index, modelID := range append([]string{route.Primary}, route.Fallbacks...) {
		model := r.models[modelID]
		estimatedCost := float64(len(input.Images))*model.InputImageCost + model.OutputImageCost
		if route.MaxCostCNY > 0 && estimatedCost > route.MaxCostCNY {
			causes = append(causes, fmt.Sprintf("%s: estimated cost %.4f exceeds route limit %.4f", modelID, estimatedCost, route.MaxCostCNY))
			continue
		}
		var result ImageEditResult
		var err error
		switch model.Protocol {
		case "openai_image_edit":
			result, err = r.openAIImageEdit(ctx, capability, model, input)
		case "dashscope_wan":
			result, err = r.dashScopeImageEdit(ctx, capability, model, input)
		case "ark_image":
			result, err = r.arkImageEdit(ctx, capability, model, input)
		default:
			err = fmt.Errorf("incompatible image protocol %s", model.Protocol)
		}
		if err == nil {
			if index > 0 {
				result.Meta.FallbackReason = strings.Join(causes, "; ")
			}
			r.logInvocation(result.Meta, nil)
			return result, nil
		}
		causes = append(causes, modelID+": "+err.Error())
		r.logInvocation(InvocationMeta{Capability: capability, ModelID: model.ID, Vendor: model.Vendor, Protocol: model.Protocol, Model: model.Model}, err)
	}
	return ImageEditResult{}, fmt.Errorf("all AI models failed for %s: %s", capability, strings.Join(causes, "; "))
}

func (r *AIRuntime) openAIResponses(ctx context.Context, capability string, model AIModel, input StructuredRequest) (StructuredResult, error) {
	content := []map[string]any{{"type": "input_text", "text": input.Prompt}}
	for _, image := range input.Images {
		content = append(content, map[string]any{"type": "input_text", "text": "照片类型：" + photoKindName(image.Kind)}, map[string]any{"type": "input_image", "detail": "high", "image_url": dataURL(image.MIMEType, image.Data)})
	}
	body := map[string]any{
		"model": model.Model, "store": false, "instructions": input.Instructions,
		"input":             []map[string]any{{"role": "user", "content": content}},
		"text":              map[string]any{"format": map[string]any{"type": "json_schema", "name": input.SchemaName, "strict": true, "schema": input.Schema}},
		"max_output_tokens": input.MaxOutputTokens,
	}
	mergeModelParameters(body, model.Parameters, map[string]bool{"model": true, "input": true, "instructions": true, "text": true})
	var response struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
		Output []struct {
			Type    string            `json:"type"`
			Content []responseContent `json:"content"`
		} `json:"output"`
	}
	started := time.Now()
	if err := r.doJSON(ctx, model, http.MethodPost, strings.TrimSuffix(model.BaseURL, "/")+"/responses", body, &response, 4<<20); err != nil {
		return StructuredResult{}, err
	}
	if response.Error != nil {
		return StructuredResult{}, errors.New("structured model response failed")
	}
	if response.Status != "" && response.Status != "completed" {
		return StructuredResult{}, fmt.Errorf("structured model status is %s", response.Status)
	}
	compat := responsesAPIResponse{Status: response.Status, Output: response.Output}
	text, refusal := responseText(compat)
	if refusal != "" {
		return StructuredResult{}, errors.New("structured model refused the request")
	}
	if !json.Valid([]byte(text)) {
		return StructuredResult{}, errors.New("structured model returned invalid JSON")
	}
	meta := invocationMeta(capability, model, started)
	meta.RequestID = response.ID
	meta.InputTokens = response.Usage.InputTokens
	meta.OutputTokens = response.Usage.OutputTokens
	meta.InputImages = len(input.Images)
	meta.EstimatedCostCNY = float64(meta.InputTokens)*model.InputCostPerMillion/1_000_000 + float64(meta.OutputTokens)*model.OutputCostPerMillion/1_000_000 + float64(meta.InputImages)*model.InputImageCost
	return StructuredResult{JSON: []byte(text), Meta: meta}, nil
}

func (r *AIRuntime) openAIChatCompletions(ctx context.Context, capability string, model AIModel, input StructuredRequest) (StructuredResult, error) {
	content := []map[string]any{{"type": "text", "text": input.Prompt}}
	for _, image := range input.Images {
		content = append(content, map[string]any{"type": "text", "text": "照片类型：" + photoKindName(image.Kind)}, map[string]any{"type": "image_url", "image_url": map[string]any{"url": dataURL(image.MIMEType, image.Data), "detail": "high"}})
	}
	mode := defaultString(model.StructuredMode, "json_schema")
	responseFormat := map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": input.SchemaName, "strict": true, "schema": input.Schema}}
	if mode == "json_object" {
		schemaJSON, _ := json.Marshal(input.Schema)
		content[0]["text"] = input.Prompt + "\n请只输出符合以下 JSON Schema 的 JSON 对象，不要输出 Markdown：" + string(schemaJSON)
		responseFormat = map[string]any{"type": "json_object"}
	}
	body := map[string]any{
		"model": model.Model, "messages": []map[string]any{{"role": "system", "content": input.Instructions}, {"role": "user", "content": content}},
		"response_format": responseFormat, "max_completion_tokens": input.MaxOutputTokens,
	}
	mergeModelParameters(body, model.Parameters, map[string]bool{"model": true, "messages": true, "response_format": true})
	var response struct {
		ID      string `json:"id"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	started := time.Now()
	if err := r.doJSON(ctx, model, http.MethodPost, strings.TrimSuffix(model.BaseURL, "/")+"/chat/completions", body, &response, 4<<20); err != nil {
		return StructuredResult{}, err
	}
	if len(response.Choices) == 0 || !json.Valid([]byte(response.Choices[0].Message.Content)) {
		return StructuredResult{}, errors.New("chat model returned invalid structured JSON")
	}
	if response.Choices[0].FinishReason == "length" {
		return StructuredResult{}, errors.New("chat model output was truncated")
	}
	meta := invocationMeta(capability, model, started)
	meta.RequestID, meta.InputTokens, meta.OutputTokens, meta.InputImages = response.ID, response.Usage.PromptTokens, response.Usage.CompletionTokens, len(input.Images)
	meta.EstimatedCostCNY = float64(meta.InputTokens)*model.InputCostPerMillion/1_000_000 + float64(meta.OutputTokens)*model.OutputCostPerMillion/1_000_000 + float64(meta.InputImages)*model.InputImageCost
	return StructuredResult{JSON: []byte(response.Choices[0].Message.Content), Meta: meta}, nil
}

func (r *AIRuntime) openAIImageEdit(ctx context.Context, capability string, model AIModel, input ImageEditRequest) (ImageEditResult, error) {
	if len(input.Images) == 0 {
		return ImageEditResult{}, errors.New("image edit requires at least one source image")
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, image := range input.Images {
		if err := writeImagePart(writer, image); err != nil {
			return ImageEditResult{}, err
		}
	}
	fields := map[string]string{"model": model.Model, "prompt": input.Prompt, "size": defaultString(input.Size, "1024x1536"), "quality": defaultString(input.Quality, "medium"), "input_fidelity": "high", "output_format": "png"}
	for name, value := range fields {
		if err := writer.WriteField(name, value); err != nil {
			return ImageEditResult{}, err
		}
	}
	if err := writer.Close(); err != nil {
		return ImageEditResult{}, err
	}
	started := time.Now()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(model.BaseURL, "/")+"/images/edits", &body)
	if err != nil {
		return ImageEditResult{}, err
	}
	request.Header.Set("Authorization", "Bearer "+os.Getenv(model.APIKeyEnv))
	request.Header.Set("Content-Type", writer.FormDataContentType())
	var payload struct {
		Data []struct {
			B64JSON string `json:"b64_json"`
			URL     string `json:"url"`
		} `json:"data"`
	}
	if err := r.doRequest(model, request, &payload, 32<<20); err != nil {
		return ImageEditResult{}, err
	}
	if len(payload.Data) == 0 {
		return ImageEditResult{}, errors.New("image response contained no output")
	}
	data, mimeType, err := r.resolveImage(ctx, model, payload.Data[0].B64JSON, payload.Data[0].URL)
	if err != nil {
		return ImageEditResult{}, err
	}
	meta := invocationMeta(capability, model, started)
	meta.InputImages, meta.OutputImages = len(input.Images), 1
	meta.EstimatedCostCNY = float64(meta.InputImages)*model.InputImageCost + model.OutputImageCost
	return ImageEditResult{Data: data, MIMEType: mimeType, Meta: meta}, nil
}

func (r *AIRuntime) dashScopeImageEdit(ctx context.Context, capability string, model AIModel, input ImageEditRequest) (ImageEditResult, error) {
	content := make([]map[string]any, 0, len(input.Images)+1)
	for _, image := range input.Images {
		content = append(content, map[string]any{"image": dataURL(image.MIMEType, image.Data)})
	}
	content = append(content, map[string]any{"text": input.Prompt})
	parameters := map[string]any{"n": 1, "size": defaultString(input.Size, "2K"), "watermark": false, "thinking_mode": true}
	for key, value := range model.Parameters {
		parameters[key] = value
	}
	body := map[string]any{"model": model.Model, "input": map[string]any{"messages": []map[string]any{{"role": "user", "content": content}}}, "parameters": parameters}
	var response struct {
		RequestID string `json:"request_id"`
		Output    struct {
			Choices []struct {
				Message struct {
					Content []struct {
						Image string `json:"image"`
						URL   string `json:"url"`
					} `json:"content"`
				} `json:"message"`
			} `json:"choices"`
			Results []struct {
				URL string `json:"url"`
			} `json:"results"`
		} `json:"output"`
	}
	started := time.Now()
	if err := r.doJSON(ctx, model, http.MethodPost, model.BaseURL, body, &response, 4<<20); err != nil {
		return ImageEditResult{}, err
	}
	imageURL := ""
	if len(response.Output.Choices) > 0 && len(response.Output.Choices[0].Message.Content) > 0 {
		imageURL = response.Output.Choices[0].Message.Content[0].Image
		if imageURL == "" {
			imageURL = response.Output.Choices[0].Message.Content[0].URL
		}
	}
	if imageURL == "" && len(response.Output.Results) > 0 {
		imageURL = response.Output.Results[0].URL
	}
	data, mimeType, err := r.resolveImage(ctx, model, "", imageURL)
	if err != nil {
		return ImageEditResult{}, err
	}
	meta := invocationMeta(capability, model, started)
	meta.RequestID, meta.InputImages, meta.OutputImages = response.RequestID, len(input.Images), 1
	meta.EstimatedCostCNY = float64(meta.InputImages)*model.InputImageCost + model.OutputImageCost
	return ImageEditResult{Data: data, MIMEType: mimeType, Meta: meta}, nil
}

func (r *AIRuntime) arkImageEdit(ctx context.Context, capability string, model AIModel, input ImageEditRequest) (ImageEditResult, error) {
	images := make([]string, 0, len(input.Images))
	for _, image := range input.Images {
		images = append(images, dataURL(image.MIMEType, image.Data))
	}
	body := map[string]any{"model": model.Model, "prompt": input.Prompt, "image": images, "size": defaultString(input.Size, "2K"), "sequential_image_generation": "disabled", "response_format": "url", "watermark": false}
	mergeModelParameters(body, model.Parameters, map[string]bool{"model": true, "prompt": true, "image": true})
	var response struct {
		ID   string `json:"id"`
		Data []struct {
			URL     string `json:"url"`
			B64JSON string `json:"b64_json"`
		} `json:"data"`
	}
	started := time.Now()
	if err := r.doJSON(ctx, model, http.MethodPost, strings.TrimSuffix(model.BaseURL, "/")+"/images/generations", body, &response, 4<<20); err != nil {
		return ImageEditResult{}, err
	}
	if len(response.Data) == 0 {
		return ImageEditResult{}, errors.New("image response contained no output")
	}
	data, mimeType, err := r.resolveImage(ctx, model, response.Data[0].B64JSON, response.Data[0].URL)
	if err != nil {
		return ImageEditResult{}, err
	}
	meta := invocationMeta(capability, model, started)
	meta.RequestID, meta.InputImages, meta.OutputImages = response.ID, len(input.Images), 1
	meta.EstimatedCostCNY = float64(meta.InputImages)*model.InputImageCost + model.OutputImageCost
	return ImageEditResult{Data: data, MIMEType: mimeType, Meta: meta}, nil
}

func (r *AIRuntime) doJSON(ctx context.Context, model AIModel, method, endpoint string, body any, output any, limit int64) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+os.Getenv(model.APIKeyEnv))
	request.Header.Set("Content-Type", "application/json")
	return r.doRequest(model, request, output, limit)
}

func (r *AIRuntime) doRequest(model AIModel, request *http.Request, output any, limit int64) error {
	ctx, cancel := context.WithTimeout(request.Context(), model.Timeout)
	defer cancel()
	response, err := r.client.Do(request.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("%s request: %w", model.Vendor, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 8<<10))
		return fmt.Errorf("%s returned status %d", model.Vendor, response.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, limit)).Decode(output); err != nil {
		return fmt.Errorf("decode %s response: %w", model.Vendor, err)
	}
	return nil
}

func (r *AIRuntime) resolveImage(ctx context.Context, model AIModel, encoded, imageURL string) ([]byte, string, error) {
	var data []byte
	var err error
	if encoded != "" {
		data, err = base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, "", fmt.Errorf("decode generated image: %w", err)
		}
	} else {
		if imageURL == "" {
			return nil, "", errors.New("image response contained no usable image")
		}
		if err := validateGeneratedImageURL(imageURL, strings.HasPrefix(model.BaseURL, "http://")); err != nil {
			return nil, "", err
		}
		downloadCtx, cancel := context.WithTimeout(ctx, model.Timeout)
		defer cancel()
		request, err := http.NewRequestWithContext(downloadCtx, http.MethodGet, imageURL, nil)
		if err != nil {
			return nil, "", err
		}
		response, err := r.client.Do(request)
		if err != nil {
			return nil, "", fmt.Errorf("download generated image: %w", err)
		}
		defer response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, "", fmt.Errorf("download generated image returned status %d", response.StatusCode)
		}
		data, err = io.ReadAll(io.LimitReader(response.Body, (20<<20)+1))
		if err != nil {
			return nil, "", err
		}
	}
	if len(data) == 0 || len(data) > 20<<20 {
		return nil, "", errors.New("generated image has invalid size")
	}
	mimeType := http.DetectContentType(data)
	if mimeType != "image/png" && mimeType != "image/jpeg" && mimeType != "image/webp" {
		return nil, "", fmt.Errorf("generated image has unsupported format %s", mimeType)
	}
	return data, mimeType, nil
}

func invocationMeta(capability string, model AIModel, started time.Time) InvocationMeta {
	return InvocationMeta{Capability: capability, ModelID: model.ID, Vendor: model.Vendor, Protocol: model.Protocol, Model: model.Model, LatencyMS: time.Since(started).Milliseconds()}
}

func (r *AIRuntime) logInvocation(meta InvocationMeta, err error) {
	attributes := []any{
		"capability", meta.Capability, "model_id", meta.ModelID, "vendor", meta.Vendor,
		"protocol", meta.Protocol, "model", meta.Model, "request_id", meta.RequestID,
		"latency_ms", meta.LatencyMS, "input_tokens", meta.InputTokens, "output_tokens", meta.OutputTokens,
		"input_images", meta.InputImages, "output_images", meta.OutputImages,
		"estimated_cost_cny", meta.EstimatedCostCNY, "fallback_reason", meta.FallbackReason,
	}
	if err != nil {
		r.logger.Warn("AI invocation failed", append(attributes, "error", err)...)
		return
	}
	r.logger.Info("AI invocation completed", attributes...)
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func mergeModelParameters(target, parameters map[string]any, reserved map[string]bool) {
	for key, value := range parameters {
		if !reserved[key] {
			target[key] = value
		}
	}
}

func validateGeneratedImageURL(value string, allowHTTP bool) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "https" && !(allowHTTP && parsed.Scheme == "http")) {
		return errors.New("generated image URL is not an allowed absolute URL")
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		if allowHTTP {
			return nil
		}
		return errors.New("generated image URL uses a blocked host")
	}
	if address := net.ParseIP(host); address != nil && (address.IsLoopback() || address.IsPrivate() || address.IsLinkLocalUnicast() || address.IsUnspecified()) && !allowHTTP {
		return errors.New("generated image URL uses a blocked network address")
	}
	return nil
}
