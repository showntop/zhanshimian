package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// AIModelConfig describes one deployable model without embedding credentials.
// APIKeyEnv names the environment variable that contains the secret at runtime.
type AIModelConfig struct {
	Vendor               string         `json:"vendor"`
	Protocol             string         `json:"protocol"`
	Model                string         `json:"model"`
	BaseURL              string         `json:"base_url"`
	APIKeyEnv            string         `json:"api_key_env"`
	StructuredMode       string         `json:"structured_mode,omitempty"`
	Parameters           map[string]any `json:"parameters,omitempty"`
	TimeoutSeconds       int            `json:"timeout_seconds,omitempty"`
	InputCostPerMillion  float64        `json:"input_cost_per_million,omitempty"`
	OutputCostPerMillion float64        `json:"output_cost_per_million,omitempty"`
	InputImageCost       float64        `json:"input_image_cost,omitempty"`
	OutputImageCost      float64        `json:"output_image_cost,omitempty"`

	// 渲染能力元数据:full_look_generation / render_quality_evaluation 的
	// 硬性入选条件,校验失败即启动失败,不允许单图降级。
	InputModalities                 []string `json:"input_modalities,omitempty"`
	MaxInputImages                  int      `json:"max_input_images,omitempty"`
	SupportsMultipleReferenceImages bool     `json:"supports_multiple_reference_images,omitempty"`
	IdentityPreservation            bool     `json:"identity_preservation,omitempty"`
	IdentityComparison              bool     `json:"identity_comparison,omitempty"`
	OutputMIMETypes                 []string `json:"output_mime_types,omitempty"`
	DataRetention                   string   `json:"data_retention,omitempty"`
}

type AIRouteConfig struct {
	Policy         string   `json:"policy,omitempty"`
	Primary        string   `json:"primary"`
	Fallbacks      []string `json:"fallbacks,omitempty"`
	MaxCostCNY     float64  `json:"max_cost_cny,omitempty"`
	MaxInputImages int      `json:"max_input_images,omitempty"`
	MaxSwitches    int      `json:"max_switches,omitempty"`

	Requirements RouteRequirements `json:"requirements,omitempty"`
}

// RouteRequirements 声明一个渲染 route 对全部候选模型的硬性要求。
type RouteRequirements struct {
	MinInputImages       int      `json:"min_input_images,omitempty"`
	MultipleReferences   bool     `json:"multiple_references,omitempty"`
	IdentityPreservation bool     `json:"identity_preservation,omitempty"`
	IdentityComparison   bool     `json:"identity_comparison,omitempty"`
	OutputMIMETypes      []string `json:"output_mime_types,omitempty"`
	DataRetention        string   `json:"data_retention,omitempty"`
}

type AIRoutingConfig struct {
	Models  map[string]AIModelConfig `json:"models"`
	Routes  map[string]AIRouteConfig `json:"routes"`
	Release *AIReleaseConfig         `json:"release,omitempty"`
}

// AIReleaseConfig 是放量元数据:candidate_percent 只允许 0/5/25/50/100
// 五档(与 provider/ai.router_rollout 的白名单一致,config 不反向 import
// provider,这里保留同语义校验),bucket_salt 决定用户分桶。
type AIReleaseConfig struct {
	PreviousVersion  string `json:"previous_version"`
	CandidateVersion string `json:"candidate_version"`
	CandidatePercent int    `json:"candidate_percent"`
	BucketSalt       string `json:"bucket_salt"`
}

func loadAIRouting() (AIRoutingConfig, string, error) {
	path := strings.TrimSpace(os.Getenv("AI_ROUTING_FILE"))
	raw := strings.TrimSpace(os.Getenv("AI_ROUTING_JSON"))
	if path == "" && raw == "" {
		return AIRoutingConfig{}, "", nil
	}
	if path != "" && raw != "" {
		return AIRoutingConfig{}, "", fmt.Errorf("set only one of AI_ROUTING_FILE or AI_ROUTING_JSON")
	}
	var data []byte
	var err error
	source := "AI_ROUTING_JSON"
	if path != "" {
		data, err = os.ReadFile(path)
		source = path
	} else {
		data = []byte(raw)
	}
	if err != nil {
		return AIRoutingConfig{}, "", fmt.Errorf("read AI routing config: %w", err)
	}
	data = expandAIRoutingEnv(data)
	var routing AIRoutingConfig
	if err := json.Unmarshal(data, &routing); err != nil {
		return AIRoutingConfig{}, "", fmt.Errorf("decode AI routing config: %w", err)
	}
	if err := validateAIRouting(routing); err != nil {
		return AIRoutingConfig{}, "", err
	}
	return routing, source, nil
}

// expandAIRoutingEnv resolves ${VAR} placeholders in the non-secret routing
// catalog. Unset variables remain visible so validation can report the exact
// missing placeholder instead of silently producing a malformed URL.
func expandAIRoutingEnv(data []byte) []byte {
	return []byte(os.Expand(string(data), func(key string) string {
		value, ok := os.LookupEnv(key)
		if !ok || strings.TrimSpace(value) == "" {
			return "${" + key + "}"
		}
		return value
	}))
}

func validateAIRouting(routing AIRoutingConfig) error {
	if len(routing.Models) == 0 || len(routing.Routes) == 0 {
		return fmt.Errorf("AI routing config requires models and routes")
	}
	if err := validateAIRelease(routing.Release); err != nil {
		return err
	}
	protocols := map[string]bool{
		"openai_responses":         true,
		"openai_chat_completions":  true,
		"openai_image_edit":        true,
		"dashscope_wan":            true,
		"dashscope_wanx_imageedit": true,
		"ark_image":                true,
	}
	structuredCapabilities := map[string]bool{
		"appearance_analysis": true, "photo_check": true, "outfit_diagnosis": true,
		"purchase_diagnosis": true, "advisor_chat": true, "today_plan": true,
		"photo_quality_check": true, "photo_identity_consistency": true, "report_evidence_verification": true,
		"plan_set_generation": true, "plan_grounding_verification": true,
	}
	assessmentMultiImageCapabilities := map[string]bool{
		"photo_quality_check": true, "photo_identity_consistency": true,
		"appearance_analysis": true, "report_evidence_verification": true,
	}
	imageCapabilities := map[string]bool{"hair_edit": true, "makeup_edit": true, "full_look_edit": true}
	renderingCapabilities := map[string]bool{"full_look_generation": true, "render_quality_evaluation": true}
	for id, model := range routing.Models {
		if strings.TrimSpace(id) == "" || strings.TrimSpace(model.Vendor) == "" || strings.TrimSpace(model.Model) == "" {
			return fmt.Errorf("AI model %q requires vendor and model", id)
		}
		if !protocols[model.Protocol] {
			return fmt.Errorf("AI model %q uses unsupported protocol %q", id, model.Protocol)
		}
		if model.BaseURL == "" || model.APIKeyEnv == "" {
			return fmt.Errorf("AI model %q requires base_url and api_key_env", id)
		}
		if strings.Contains(model.BaseURL, "${") {
			return fmt.Errorf("AI model %q base_url contains an unset environment placeholder", id)
		}
		if model.StructuredMode != "" && model.StructuredMode != "json_schema" && model.StructuredMode != "json_object" {
			return fmt.Errorf("AI model %q has unsupported structured_mode %q", id, model.StructuredMode)
		}
		if model.InputCostPerMillion < 0 || model.OutputCostPerMillion < 0 || model.InputImageCost < 0 || model.OutputImageCost < 0 {
			return fmt.Errorf("AI model %q contains a negative cost", id)
		}
	}
	for capability, route := range routing.Routes {
		if !structuredCapabilities[capability] && !imageCapabilities[capability] && !renderingCapabilities[capability] {
			return fmt.Errorf("AI route %q is not a supported capability", capability)
		}
		if strings.TrimSpace(capability) == "" || route.Primary == "" {
			return fmt.Errorf("AI route %q requires a primary model", capability)
		}
		if _, ok := routing.Models[route.Primary]; !ok {
			return fmt.Errorf("AI route %q references unknown model %q", capability, route.Primary)
		}
		if route.Policy != "" && route.Policy != "quality_first" && route.Policy != "value_first" && route.Policy != "balanced" {
			return fmt.Errorf("AI route %q has unsupported policy %q", capability, route.Policy)
		}
		if err := validateRenderingRoute(capability, route, routing.Models); err != nil {
			return err
		}
		if route.MaxCostCNY < 0 {
			return fmt.Errorf("AI route %q contains a negative cost limit", capability)
		}
		if assessmentMultiImageCapabilities[capability] && route.MaxInputImages < 3 {
			return fmt.Errorf("AI route %q requires max_input_images >= 3", capability)
		}
		seen := map[string]bool{route.Primary: true}
		for _, fallback := range route.Fallbacks {
			if _, ok := routing.Models[fallback]; !ok {
				return fmt.Errorf("AI route %q references unknown fallback %q", capability, fallback)
			}
			if seen[fallback] {
				return fmt.Errorf("AI route %q repeats model %q", capability, fallback)
			}
			seen[fallback] = true
		}
		for modelID := range seen {
			protocol := routing.Models[modelID].Protocol
			if structuredCapabilities[capability] && protocol != "openai_responses" && protocol != "openai_chat_completions" {
				return fmt.Errorf("AI route %q uses image protocol %q", capability, protocol)
			}
			if imageCapabilities[capability] && protocol != "openai_image_edit" && protocol != "dashscope_wan" && protocol != "dashscope_wanx_imageedit" && protocol != "ark_image" {
				return fmt.Errorf("AI route %q uses structured protocol %q", capability, protocol)
			}
		}
	}
	return nil
}

// renderingRouteRequirements 是两个渲染能力对候选模型的硬性入选条件;
// 任一不满足即整体校验失败,绝不静默降级到弱模型。
var renderingRouteRequirements = map[string]RouteRequirements{
	"full_look_generation": {
		MinInputImages: 2, MultipleReferences: true, IdentityPreservation: true,
		OutputMIMETypes: []string{"image/jpeg"}, DataRetention: "zero",
	},
	"render_quality_evaluation": {
		MinInputImages: 3, MultipleReferences: true, IdentityComparison: true,
		OutputMIMETypes: []string{"image/jpeg"}, DataRetention: "zero",
	},
}

func validateRenderingRoute(capability string, route AIRouteConfig, models map[string]AIModelConfig) error {
	requirements, ok := renderingRouteRequirements[capability]
	if !ok {
		if route.MaxSwitches > 1 {
			return fmt.Errorf("AI route %q allows at most one model switch", capability)
		}
		return nil
	}
	if route.MaxSwitches > 1 {
		return fmt.Errorf("AI route %q allows at most one model switch", capability)
	}
	if route.Policy != "" && route.Policy != "balanced" {
		return fmt.Errorf("AI route %q requires balanced policy, got %q", capability, route.Policy)
	}
	modelKeys := append([]string{route.Primary}, route.Fallbacks...)
	for _, key := range modelKeys {
		model, ok := models[key]
		if !ok {
			return fmt.Errorf("AI route %q references unknown model %q", capability, key)
		}
		if model.MaxInputImages < requirements.MinInputImages {
			return fmt.Errorf("AI route %q model %q requires at least %d input images", capability, key, requirements.MinInputImages)
		}
		if requirements.MultipleReferences && !model.SupportsMultipleReferenceImages {
			return fmt.Errorf("AI route %q model %q must support multiple reference images", capability, key)
		}
		if requirements.IdentityPreservation && !model.IdentityPreservation {
			return fmt.Errorf("AI route %q model %q must preserve identity", capability, key)
		}
		if requirements.IdentityComparison && !model.IdentityComparison {
			return fmt.Errorf("AI route %q model %q must support identity comparison", capability, key)
		}
		if requirements.DataRetention != "" && model.DataRetention != requirements.DataRetention {
			return fmt.Errorf("AI route %q model %q requires data_retention=%q", capability, key, requirements.DataRetention)
		}
		if len(requirements.OutputMIMETypes) > 0 && !containsMIME(model.OutputMIMETypes, requirements.OutputMIMETypes) {
			return fmt.Errorf("AI route %q model %q must deliver %v", capability, key, requirements.OutputMIMETypes)
		}
		if strings.HasPrefix(key, "demo") {
			return fmt.Errorf("AI route %q must not reference demo model %q", capability, key)
		}
	}
	return nil
}

// validateAIRelease 校验放量元数据:出现 release 块时四个字段必须齐全,
// candidate_percent 只接受 0/5/25/50/100 五档——与 set-rollout.mjs 和
// provider/ai 的 validateCandidatePercent 同一份阶梯。
func validateAIRelease(release *AIReleaseConfig) error {
	if release == nil {
		return nil
	}
	if strings.TrimSpace(release.PreviousVersion) == "" || strings.TrimSpace(release.CandidateVersion) == "" {
		return fmt.Errorf("AI release config requires previous_version and candidate_version")
	}
	if strings.TrimSpace(release.BucketSalt) == "" {
		return fmt.Errorf("AI release config requires bucket_salt")
	}
	switch release.CandidatePercent {
	case 0, 5, 25, 50, 100:
		return nil
	default:
		return fmt.Errorf("candidate_percent must be one of 0, 5, 25, 50, 100")
	}
}

func containsMIME(values, required []string) bool {
	for _, want := range required {
		found := false
		for _, got := range values {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
