package bootstrap

import (
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/zhanshimian/server/internal/config"
	"github.com/zhanshimian/server/internal/provider"
	"github.com/zhanshimian/server/internal/repository/postgres"
	"github.com/zhanshimian/server/internal/service"
	"github.com/zhanshimian/server/internal/storage"
)

type AIBundle struct {
	Analyzer provider.Analyzer
	Hair     provider.HairPreviewGenerator
	Look     provider.LookGenerator
	Outfit   provider.OutfitAdvisor
	Purchase provider.OutfitAdvisor
	Advisor  provider.AdvisorChat
	Today    provider.TodayPlanner
	Routes   map[string]string
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
	loader := service.NewAnalysisMediaLoader(repo, objects, cfg.PublicBaseURL, cfg.MaxUploadBytes, cfg.AssetDir)
	analyzer, err := provider.NewRoutedAnalyzer(runtime, loader)
	if err != nil {
		return AIBundle{}, err
	}
	hair, err := provider.NewRoutedHairGenerator(runtime, loader)
	if err != nil {
		return AIBundle{}, err
	}
	outfit, err := provider.NewRoutedOutfitAdvisor(runtime, loader)
	if err != nil {
		return AIBundle{}, err
	}
	bundle := AIBundle{Analyzer: analyzer, Hair: hair, Outfit: outfit, Routes: runtime.RouteSummary()}
	if runtime.HasRoute(provider.CapabilityFullLookEdit) {
		look, err := provider.NewRoutedLookGenerator(runtime, loader)
		if err != nil {
			return AIBundle{}, err
		}
		bundle.Look = look
	}
	if runtime.HasRoute(provider.CapabilityPurchaseDiagnosis) {
		bundle.Purchase, err = provider.NewRoutedPurchaseAdvisor(runtime, loader)
		if err != nil {
			return AIBundle{}, err
		}
	}
	if runtime.HasRoute(provider.CapabilityAdvisorChat) {
		bundle.Advisor, err = provider.NewRoutedAdvisorChat(runtime)
		if err != nil {
			return AIBundle{}, err
		}
	}
	if runtime.HasRoute(provider.CapabilityTodayPlan) {
		bundle.Today, err = provider.NewRoutedTodayPlanner(runtime)
		if err != nil {
			return AIBundle{}, err
		}
	}
	return bundle, nil
}

