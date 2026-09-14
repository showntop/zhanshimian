package bootstrap

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/zhanshimian/server/internal/config"
	"github.com/zhanshimian/server/internal/provider"
	providerai "github.com/zhanshimian/server/internal/provider/ai"
	"github.com/zhanshimian/server/internal/repository/postgres"
	"github.com/zhanshimian/server/internal/storage"
)

type AIBundle struct {
	Routes map[string]string
	// Runtime 是能力路由运行时：provider/ai 的全部能力（assessment/planning/
	// rendering/today/advisor/diagnostic）都经它调用，bootstrap 用
	// structuredRuntimeAdapter 把它适配成 ai.StructuredRuntime。
	Runtime *provider.AIRuntime
}

// BuildAI 一律走能力路由（AGENTS.md 红线：AI 只经 ai-routing.*.json）。
// 未配置路由表直接报错启动失败，不再回退遗留的 AI_PROVIDER/OPENAI_* 环境变量路径。
func BuildAI(cfg config.Config, repo *postgres.Store, objects storage.ObjectStorage, logger *slog.Logger) (AIBundle, error) {
	if cfg.AIRoutingSource == "" {
		return AIBundle{}, fmt.Errorf("AI_ROUTING_FILE or AI_ROUTING_JSON is required")
	}
	models := make([]provider.AIModel, 0, len(cfg.AIRouting.Models))
	modelIDs := make([]string, 0, len(cfg.AIRouting.Models))
	for id := range cfg.AIRouting.Models {
		modelIDs = append(modelIDs, id)
	}
	sort.Strings(modelIDs)
	for _, id := range modelIDs {
		item := cfg.AIRouting.Models[id]
		models = append(models, provider.AIModel{
			ID: id, Vendor: item.Vendor, Protocol: item.Protocol, Model: item.Model,
			BaseURL: item.BaseURL, APIKeyEnv: item.APIKeyEnv,
			StructuredMode: item.StructuredMode, Parameters: item.Parameters,
			Timeout:             time.Duration(item.TimeoutSeconds) * time.Second,
			InputCostPerMillion: item.InputCostPerMillion, OutputCostPerMillion: item.OutputCostPerMillion,
			InputImageCost: item.InputImageCost, OutputImageCost: item.OutputImageCost,
		})
	}
	routes := make([]provider.AIRoute, 0, len(cfg.AIRouting.Routes))
	capabilities := make([]string, 0, len(cfg.AIRouting.Routes))
	for capability := range cfg.AIRouting.Routes {
		capabilities = append(capabilities, capability)
	}
	sort.Strings(capabilities)
	for _, capability := range capabilities {
		item := cfg.AIRouting.Routes[capability]
		routes = append(routes, provider.AIRoute{Capability: capability, Policy: item.Policy, Primary: item.Primary, Fallbacks: item.Fallbacks, MaxCostCNY: item.MaxCostCNY})
	}
	runtime, err := provider.NewAIRuntime(models, routes, nil, logger)
	if err != nil {
		return AIBundle{}, err
	}
	// 台账：worker 任务内（ctx 带 InvocationScope）的每次模型调用都落
	// provider_invocations，行 ID 供 reports/plan_sets/render_candidates 外键引用。
	runtime.SetInvocationRecorder(providerai.NewInvocationRecorder(repo, nil), aiRoutingConfigVersion(cfg))
	return AIBundle{Routes: runtime.RouteSummary(), Runtime: runtime}, nil
}

// aiRoutingConfigVersion 用路由表内容哈希标识台账里的 routing_config_version：
// 路由文件无独立 version 字段，内容变化即版本变化。
func aiRoutingConfigVersion(cfg config.Config) string {
	canonical, err := json.Marshal(cfg.AIRouting)
	if err != nil {
		return "ai-routing:unknown"
	}
	sum := sha256.Sum256(canonical)
	return fmt.Sprintf("%s#%s", cfg.AIRoutingSource, hex.EncodeToString(sum[:])[:12])
}
