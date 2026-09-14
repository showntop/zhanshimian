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

	"github.com/zhanshimian/server/internal/domain"
	providerai "github.com/zhanshimian/server/internal/provider/ai"
)

const (
	CapabilityAppearanceAnalysis = "appearance_analysis"
	CapabilityPhotoCheck         = "photo_check"
	CapabilityOutfitDiagnosis    = "outfit_diagnosis"
	CapabilityPurchaseDiagnosis  = "purchase_diagnosis"
	CapabilityAdvisorChat        = "advisor_chat"
	CapabilityTodayPlan          = "today_plan"
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
	ImageDetail     string
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
	Source           string  `json:"source,omitempty"`
	InvocationID     string  `json:"invocation_id,omitempty"`
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
	models               map[string]AIModel
	routes               map[string]AIRoute
	client               *http.Client
	logger               *slog.Logger
	recorder             *providerai.InvocationRecorder
	routingConfigVersion string
	release              *providerai.ReleaseConfig
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

// SetInvocationRecorder 接上 provider_invocations 台账：装配后、且 ctx 携带
// domain.InvocationScope 时，每一次模型网络调用（含 fallback 的每次尝试）
// 都会落一行台账，返回的 Meta.InvocationID 是台账行 ID——reports、plan_sets、
// render_candidates 的 NOT NULL 外键都引用它。无 scope 的调用不写台账。
func (r *AIRuntime) SetInvocationRecorder(recorder *providerai.InvocationRecorder, routingConfigVersion string) {
	r.recorder = recorder
	r.routingConfigVersion = routingConfigVersion
}

// SetRelease 接上放量元数据：之后每条台账行都记录该用户的确定性分桶号
// （0-99，不含任何用户标识），放量观察窗据此按桶归因指标。
func (r *AIRuntime) SetRelease(release providerai.ReleaseConfig) {
	r.release = &release
}

// releaseBucketFor 计算当前任务用户的分桶；未配置 release 时不记录桶号。
func (r *AIRuntime) releaseBucketFor(userID string) *int {
	if r.release == nil || userID == "" {
		return nil
	}
	bucket := providerai.ReleaseBucket(userID, r.release.BucketSalt)
	return &bucket
}

// invocationRecorderFor 返回可用的台账记录器与任务身份；任一缺失则跳过记录。
func (r *AIRuntime) invocationRecorderFor(ctx context.Context) (*providerai.InvocationRecorder, domain.InvocationScope, bool) {
	if r.recorder == nil {
		return nil, domain.InvocationScope{}, false
	}
	scope, ok := domain.InvocationScopeFrom(ctx)
	if !ok || scope.UserID == "" || scope.OperationID == "" || scope.TaskID == "" {
		return nil, domain.InvocationScope{}, false
	}
	return r.recorder, scope, true
}

// recordStructuredCall 包一次结构化模型调用：台账先 start 后 finish，
// 成功时把台账行 ID 写回 result.Meta.InvocationID。
func (r *AIRuntime) recordStructuredCall(ctx context.Context, capability string, model AIModel, input StructuredRequest, call func(context.Context) (StructuredResult, error)) (StructuredResult, error) {
	recorder, scope, ok := r.invocationRecorderFor(ctx)
	if !ok {
		return call(ctx)
	}
	var result StructuredResult
	_, meta, err := recorder.Record(ctx, domain.StartInvocation{
		UserID: scope.UserID, OperationID: scope.OperationID, TaskID: scope.TaskID, AttemptNo: scope.AttemptNo,
		Capability: capability, RoutingConfigVersion: r.routingConfigVersion, ReleaseBucket: r.releaseBucketFor(scope.UserID),
		ProviderKey: model.Vendor, ModelKey: model.ID, Protocol: model.Protocol,
		RequestHash: structuredRequestHash(capability, input), InputImages: len(input.Images),
	}, func(callCtx context.Context) (providerai.CallResult, error) {
		var callErr error
		result, callErr = call(callCtx)
		if callErr != nil {
			return providerai.CallResult{}, callErr
		}
		return providerai.CallResult{
			ProviderRequestID: result.Meta.RequestID,
			InputTokens:       intPtr(result.Meta.InputTokens),
			OutputTokens:      intPtr(result.Meta.OutputTokens),
			InputImages:       intPtr(result.Meta.InputImages),
			OutputImages:      intPtr(result.Meta.OutputImages),
			EstimatedCostCNY:  floatPtr(result.Meta.EstimatedCostCNY),
		}, nil
	})
	if err == nil {
		result.Meta.InvocationID = meta.InvocationID
	}
	return result, err
}

// recordEditCall 与 recordStructuredCall 对称，包一次图片生成调用。
func (r *AIRuntime) recordEditCall(ctx context.Context, capability string, model AIModel, input ImageEditRequest, call func(context.Context) (ImageEditResult, error)) (ImageEditResult, error) {
	recorder, scope, ok := r.invocationRecorderFor(ctx)
	if !ok {
		return call(ctx)
	}
	var result ImageEditResult
	_, meta, err := recorder.Record(ctx, domain.StartInvocation{
		UserID: scope.UserID, OperationID: scope.OperationID, TaskID: scope.TaskID, AttemptNo: scope.AttemptNo,
		Capability: capability, RoutingConfigVersion: r.routingConfigVersion, ReleaseBucket: r.releaseBucketFor(scope.UserID),
		ProviderKey: model.Vendor, ModelKey: model.ID, Protocol: model.Protocol,
		RequestHash: editRequestHash(capability, input), InputImages: len(input.Images),
	}, func(callCtx context.Context) (providerai.CallResult, error) {
		var callErr error
		result, callErr = call(callCtx)
		if callErr != nil {
			return providerai.CallResult{}, callErr
		}
		return providerai.CallResult{
			ProviderRequestID: result.Meta.RequestID,
			InputTokens:       intPtr(result.Meta.InputTokens),
			OutputTokens:      intPtr(result.Meta.OutputTokens),
			InputImages:       intPtr(result.Meta.InputImages),
			OutputImages:      intPtr(result.Meta.OutputImages),
			EstimatedCostCNY:  floatPtr(result.Meta.EstimatedCostCNY),
		}, nil
	})
	if err == nil {
		result.Meta.InvocationID = meta.InvocationID
	}
	return result, err
}

// structuredRequestHash 只覆盖可脱敏的请求形状：能力、指令、提示词、schema
// 名与图片角色/类型序列。图片字节与 URL 一律不进哈希输入。
func structuredRequestHash(capability string, input StructuredRequest) string {
	var b strings.Builder
	b.WriteString(capability)
	b.WriteString("\ninstructions:" + input.Instructions)
	b.WriteString("\nprompt:" + input.Prompt)
	b.WriteString("\nschema:" + input.SchemaName)
	for _, image := range input.Images {
		b.WriteString("\nimage:" + image.Kind + ":" + image.MIMEType)
	}
	return b.String()
}

func editRequestHash(capability string, input ImageEditRequest) string {
	var b strings.Builder
	b.WriteString(capability)
	b.WriteString("\nprompt:" + input.Prompt)
	b.WriteString("\nsize:" + input.Size + "\nquality:" + input.Quality)
	for _, image := range input.Images {
		b.WriteString("\nimage:" + image.Kind + ":" + image.MIMEType)
	}
	return b.String()
}

func intPtr(v int) *int { return &v }

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

// StructuredCompatible 把 provider/ai 的结构化请求适配到 legacy 通道,
// 供渲染质量评估的 StructuredQualityEvaluator 使用。
func (r *AIRuntime) StructuredCompatible(ctx context.Context, request providerai.StructuredRequest) (providerai.StructuredResult, error) {
	images := make([]AnalysisImage, 0, len(request.Images))
	for _, image := range request.Images {
		images = append(images, AnalysisImage{ID: image.AssetID, Kind: image.Role, MIMEType: image.MIMEType, Data: image.Data})
	}
	result, err := r.Structured(ctx, request.Capability, StructuredRequest{
		Instructions: request.Instructions, Prompt: request.Prompt, Images: images,
		SchemaName: request.SchemaName, Schema: request.Schema,
		MaxOutputTokens: request.MaxOutputTokens, Validate: request.Validate,
	})
	if err != nil {
		return providerai.StructuredResult{}, err
	}
	return providerai.StructuredResult{JSON: result.JSON, Meta: providerai.InvocationMeta{
		InvocationID: result.Meta.InvocationID, ModelKey: result.Meta.ModelID, Protocol: result.Meta.Protocol,
		ProviderRequestID: result.Meta.RequestID, LatencyMS: int(result.Meta.LatencyMS),
		EstimatedCostCNY:  floatPtr(result.Meta.EstimatedCostCNY),
	}}, nil
}

func floatPtr(v float64) *float64 { return &v }

func (r *AIRuntime) Structured(ctx context.Context, capability string, input StructuredRequest) (StructuredResult, error) {
	route, ok := r.routes[capability]
	if !ok {
		return StructuredResult{}, fmt.Errorf("no AI route configured for %s", capability)
	}
	var causes []string
	var causeErrs []error
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
		started := time.Now()
		result, err := r.recordStructuredCall(ctx, capability, model, input, func(callCtx context.Context) (StructuredResult, error) {
			var res StructuredResult
			var callErr error
			if model.Protocol == "openai_chat_completions" {
				res, callErr = r.openAIChatCompletions(callCtx, capability, model, input)
			} else {
				res, callErr = r.openAIResponses(callCtx, capability, model, input)
			}
			// 域校验计入这次 invocation：provider 返回 200 但载荷不合契约，
			// 对“每阶段成功率/fallback 命中率”而言就是一次失败调用。
			if callErr == nil {
				res.JSON = normalizeStructuredJSON(res.JSON)
				if input.Validate != nil {
					callErr = input.Validate(res.JSON)
				}
			}
			return res, callErr
		})
		if err == nil {
			if index > 0 {
				result.Meta.FallbackReason = strings.Join(causes, "; ")
			}
			r.logInvocation(result.Meta, nil)
			return result, nil
		}
		causes = append(causes, modelID+": "+err.Error())
		causeErrs = append(causeErrs, err)
		failedMeta := result.Meta
		if failedMeta.ModelID == "" {
			failedMeta = InvocationMeta{Capability: capability, ModelID: model.ID, Vendor: model.Vendor, Protocol: model.Protocol, Model: model.Model, Source: InvocationSource(ctx)}
		}
		if failedMeta.LatencyMS == 0 {
			failedMeta.LatencyMS = time.Since(started).Milliseconds()
		}
		r.logInvocation(failedMeta, err)
	}
	return StructuredResult{}, &allModelsFailedError{
		text:   fmt.Sprintf("all AI models failed for %s: %s", capability, strings.Join(causes, "; ")),
		causes: causeErrs,
	}
}

// allModelsFailedError 在全候选失败时保留每条原因的错误链:调用方用
// errors.Is/As 区分内容类失败(契约违约,走内容重试预算)与传输类失败
// (基础设施重试),而不是只能解析拼接字符串。
type allModelsFailedError struct {
	text   string
	causes []error
}

func (e *allModelsFailedError) Error() string { return e.text }
func (e *allModelsFailedError) Unwrap() []error {
	return e.causes
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

// EditImageOnModel 在指定模型上执行一次图片编辑,不做任何 fallback:
// 渲染 Router 自管模型切换预算,不经过 runtime 的 route 循环。
func (r *AIRuntime) EditImageOnModel(ctx context.Context, modelID, capability string, input ImageEditRequest) (ImageEditResult, error) {
	model, ok := r.models[modelID]
	if !ok {
		return ImageEditResult{}, fmt.Errorf("unknown AI model %q", modelID)
	}
	var result ImageEditResult
	var err error
	dispatch := func(callCtx context.Context) (ImageEditResult, error) {
		switch model.Protocol {
		case "openai_image_edit":
			return r.openAIImageEdit(callCtx, capability, model, input)
		case "dashscope_wan":
			return r.dashScopeImageEdit(callCtx, capability, model, input)
		case "dashscope_wanx_imageedit":
			return r.dashScopeWanxImageEdit(callCtx, capability, model, input)
		case "ark_image":
			return r.arkImageEdit(callCtx, capability, model, input)
		default:
			return ImageEditResult{}, fmt.Errorf("incompatible image protocol %s", model.Protocol)
		}
	}
	result, err = r.recordEditCall(ctx, capability, model, input, dispatch)
	if err != nil {
		r.logInvocation(InvocationMeta{Capability: capability, ModelID: model.ID, Vendor: model.Vendor, Protocol: model.Protocol, Model: model.Model, Source: InvocationSource(ctx)}, err)
		return ImageEditResult{}, err
	}
	r.logInvocation(result.Meta, nil)
	return result, nil
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
		started := time.Now()
		result, err := r.recordEditCall(ctx, capability, model, input, func(callCtx context.Context) (ImageEditResult, error) {
			switch model.Protocol {
			case "openai_image_edit":
				return r.openAIImageEdit(callCtx, capability, model, input)
			case "dashscope_wan":
				return r.dashScopeImageEdit(callCtx, capability, model, input)
			case "dashscope_wanx_imageedit":
				return r.dashScopeWanxImageEdit(callCtx, capability, model, input)
			case "ark_image":
				return r.arkImageEdit(callCtx, capability, model, input)
			default:
				return ImageEditResult{}, fmt.Errorf("incompatible image protocol %s", model.Protocol)
			}
		})
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
			failedMeta = InvocationMeta{Capability: capability, ModelID: model.ID, Vendor: model.Vendor, Protocol: model.Protocol, Model: model.Model, Source: InvocationSource(ctx)}
		}
		if failedMeta.LatencyMS == 0 {
			failedMeta.LatencyMS = time.Since(started).Milliseconds()
		}
		r.logInvocation(failedMeta, err)
	}
	return ImageEditResult{}, fmt.Errorf("all AI models failed for %s: %s", capability, strings.Join(causes, "; "))
}

// dashScopeWanxImageEdit adapts the low-cost Wan 2.1 general image editor.
// Unlike wan2.7, this endpoint is asynchronous: submit a task, poll it, then
// download the short-lived result URL into our own storage.
//
// It only sends Images[0]. Do not use this protocol as the primary
// full_look_edit route — the face identity frame would be discarded.
func (r *AIRuntime) dashScopeWanxImageEdit(ctx context.Context, capability string, model AIModel, input ImageEditRequest) (ImageEditResult, error) {
	if len(input.Images) == 0 {
		return ImageEditResult{}, errors.New("image edit requires at least one source image")
	}
	if model.Timeout <= 0 {
		model.Timeout = 180 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, model.Timeout)
	defer cancel()
	base := input.Images[0]
	body := map[string]any{
		"model": model.Model,
		"input": map[string]any{
			"function":       "description_edit",
			"prompt":         input.Prompt,
			"base_image_url": dataURL(base.MIMEType, base.Data),
		},
		"parameters": map[string]any{"n": 1, "watermark": false},
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return ImageEditResult{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, model.BaseURL, bytes.NewReader(encoded))
	if err != nil {
		return ImageEditResult{}, err
	}
	request.Header.Set("Authorization", "Bearer "+os.Getenv(model.APIKeyEnv))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-DashScope-Async", "enable")
	var submitted struct {
		RequestID string `json:"request_id"`
		Output    struct {
			TaskID     string `json:"task_id"`
			TaskStatus string `json:"task_status"`
			Code       string `json:"code"`
			Message    string `json:"message"`
		} `json:"output"`
	}
	started := time.Now()
	if err := r.doRequest(model, request, &submitted, 2<<20); err != nil {
		return ImageEditResult{}, err
	}
	if submitted.Output.TaskID == "" {
		return ImageEditResult{}, fmt.Errorf("wanx image edit returned no task: %s", strings.TrimSpace(submitted.Output.Message))
	}

	taskURL, err := wanxTaskURL(model.BaseURL, submitted.Output.TaskID)
	if err != nil {
		return ImageEditResult{}, err
	}
	var completed struct {
		RequestID string `json:"request_id"`
		Output    struct {
			TaskStatus string `json:"task_status"`
			Code       string `json:"code"`
			Message    string `json:"message"`
			Results    []struct {
				URL     string `json:"url"`
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"results"`
		} `json:"output"`
	}
	pollEvery := time.NewTicker(2 * time.Second)
	defer pollEvery.Stop()
	for {
		poll, pollErr := http.NewRequestWithContext(ctx, http.MethodGet, taskURL, nil)
		if pollErr != nil {
			return ImageEditResult{}, pollErr
		}
		poll.Header.Set("Authorization", "Bearer "+os.Getenv(model.APIKeyEnv))
		if pollErr = r.doRequest(model, poll, &completed, 2<<20); pollErr != nil {
			return ImageEditResult{}, pollErr
		}
		switch completed.Output.TaskStatus {
		case "SUCCEEDED":
			if len(completed.Output.Results) == 0 || completed.Output.Results[0].URL == "" {
				return ImageEditResult{}, errors.New("wanx image edit succeeded without an image URL")
			}
			data, mimeType, resolveErr := r.resolveImage(ctx, model, "", completed.Output.Results[0].URL)
			if resolveErr != nil {
				return ImageEditResult{}, resolveErr
			}
			meta := invocationMeta(ctx, capability, model, started)
			meta.RequestID, meta.InputImages, meta.OutputImages = submitted.RequestID, 1, 1
			meta.EstimatedCostCNY = model.OutputImageCost
			return ImageEditResult{Data: data, MIMEType: mimeType, Meta: meta}, nil
		case "FAILED", "CANCELED", "UNKNOWN":
			return ImageEditResult{}, fmt.Errorf("wanx image edit task %s: %s", completed.Output.TaskStatus, strings.TrimSpace(completed.Output.Message))
		}
		select {
		case <-ctx.Done():
			return ImageEditResult{}, ctx.Err()
		case <-pollEvery.C:
		}
	}
}

func wanxTaskURL(baseURL, taskID string) (string, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("wanx image edit base URL is invalid")
	}
	marker := strings.Index(parsed.Path, "/services/")
	if marker < 0 {
		return "", errors.New("wanx image edit base URL must include /services/")
	}
	parsed.Path = strings.TrimRight(parsed.Path[:marker], "/") + "/tasks/" + url.PathEscape(taskID)
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func (r *AIRuntime) openAIResponses(ctx context.Context, capability string, model AIModel, input StructuredRequest) (StructuredResult, error) {
	content := []map[string]any{{"type": "input_text", "text": input.Prompt}}
	for _, image := range input.Images {
		content = append(content, map[string]any{"type": "input_text", "text": "照片类型：" + photoKindName(image.Kind)}, map[string]any{"type": "input_image", "detail": defaultString(input.ImageDetail, "high"), "image_url": dataURL(image.MIMEType, image.Data)})
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
	meta := invocationMeta(ctx, capability, model, started)
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
		content = append(content, map[string]any{"type": "text", "text": "照片类型：" + photoKindName(image.Kind)}, map[string]any{"type": "image_url", "image_url": map[string]any{"url": dataURL(image.MIMEType, image.Data), "detail": defaultString(input.ImageDetail, "high")}})
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
	meta := invocationMeta(ctx, capability, model, started)
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
	meta := invocationMeta(ctx, capability, model, started)
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
	meta := invocationMeta(ctx, capability, model, started)
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
	meta := invocationMeta(ctx, capability, model, started)
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
	if err := request.Context().Err(); err != nil {
		return fmt.Errorf("%s request context already done: %w", model.Vendor, err)
	}
	ctx, cancel := context.WithTimeout(request.Context(), model.Timeout)
	defer cancel()
	response, err := r.client.Do(request.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("%s request: %w", model.Vendor, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		message := strings.TrimSpace(string(snippet))
		if message == "" {
			return fmt.Errorf("%s returned status %d", model.Vendor, response.StatusCode)
		}
		return fmt.Errorf("%s returned status %d: %s", model.Vendor, response.StatusCode, message)
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

// invocationMeta builds the log/telemetry record for one AI call. The worker
// task identifier comes from ctx so failure WARNs can be traced to the job.
func invocationMeta(ctx context.Context, capability string, model AIModel, started time.Time) InvocationMeta {
	return InvocationMeta{Capability: capability, ModelID: model.ID, Vendor: model.Vendor, Protocol: model.Protocol, Model: model.Model, Source: InvocationSource(ctx), LatencyMS: time.Since(started).Milliseconds()}
}

func (r *AIRuntime) logInvocation(meta InvocationMeta, err error) {
	attributes := []any{
		"capability", meta.Capability, "model_id", meta.ModelID, "vendor", meta.Vendor,
		"protocol", meta.Protocol, "model", meta.Model, "task", meta.Source, "request_id", meta.RequestID,
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
