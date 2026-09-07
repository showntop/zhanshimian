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
}

type AIRouteConfig struct {
	Policy     string   `json:"policy,omitempty"`
	Primary    string   `json:"primary"`
	Fallbacks  []string `json:"fallbacks,omitempty"`
	MaxCostCNY float64  `json:"max_cost_cny,omitempty"`
}

type AIRoutingConfig struct {
	Models map[string]AIModelConfig `json:"models"`
	Routes map[string]AIRouteConfig `json:"routes"`
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
	protocols := map[string]bool{
		"openai_responses":         true,
		"openai_chat_completions":  true,
		"openai_image_edit":        true,
		"dashscope_wan":            true,
		"dashscope_wanx_imageedit": true,
		"ark_image":                true,
	}
	structuredCapabilities := map[string]bool{"appearance_analysis": true, "photo_check": true, "outfit_diagnosis": true, "purchase_diagnosis": true, "advisor_chat": true, "today_plan": true}
	imageCapabilities := map[string]bool{"hair_edit": true, "makeup_edit": true, "full_look_edit": true}
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
		if !structuredCapabilities[capability] && !imageCapabilities[capability] {
			return fmt.Errorf("AI route %q is not a supported capability", capability)
		}
		if strings.TrimSpace(capability) == "" || route.Primary == "" {
			return fmt.Errorf("AI route %q requires a primary model", capability)
		}
		if _, ok := routing.Models[route.Primary]; !ok {
			return fmt.Errorf("AI route %q references unknown model %q", capability, route.Primary)
		}
		if route.Policy != "" && route.Policy != "quality_first" && route.Policy != "value_first" {
			return fmt.Errorf("AI route %q has unsupported policy %q", capability, route.Policy)
		}
		if route.MaxCostCNY < 0 {
			return fmt.Errorf("AI route %q contains a negative cost limit", capability)
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
