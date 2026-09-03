package bootstrap

import (
	"log/slog"
	"sort"
	"time"

	"github.com/example/jianwo/server/internal/config"
	"github.com/example/jianwo/server/internal/provider"
	"github.com/example/jianwo/server/internal/repository/postgres"
	"github.com/example/jianwo/server/internal/service"
	"github.com/example/jianwo/server/internal/storage"
)

type AIBundle struct {
	Analyzer provider.Analyzer
	Hair     provider.HairPreviewGenerator
	Look     provider.LookGenerator
	Outfit   provider.OutfitAdvisor
	Purchase provider.OutfitAdvisor
	Advisor  provider.AdvisorChat
	Routes   map[string]string
}

func BuildAI(cfg config.Config, repo *postgres.Store, objects storage.ObjectStorage, logger *slog.Logger) (AIBundle, error) {
	if cfg.AIRoutingSource == "" {
		return buildLegacyAI(cfg, repo, objects)
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
	return bundle, nil
}

func buildLegacyAI(cfg config.Config, repo *postgres.Store, objects storage.ObjectStorage) (AIBundle, error) {
	loader := service.NewAnalysisMediaLoader(repo, objects, cfg.PublicBaseURL, cfg.MaxUploadBytes, cfg.AssetDir)
	demoAnalyzer := provider.Analyzer(provider.NewDemoAnalyzer())
	analyzer := demoAnalyzer
	if cfg.AIProvider == "openai" {
		real, err := provider.NewOpenAIAnalyzer(provider.OpenAIConfig{APIKey: cfg.OpenAIAPIKey, BaseURL: cfg.OpenAIBaseURL, Model: cfg.OpenAIVisionModel, Timeout: cfg.AIRequestTimeout}, loader, nil)
		if err != nil {
			return AIBundle{}, err
		}
		analyzer = real
		if cfg.AIFallbackToDemo {
			analyzer = provider.NewFallbackAnalyzer(real, demoAnalyzer)
		}
	}
	demoHair := provider.HairPreviewGenerator(provider.NewDemoHairGenerator())
	hair := demoHair
	if cfg.HairPreviewProvider == "openai" {
		real, err := provider.NewOpenAIHairGenerator(provider.OpenAIHairConfig{APIKey: cfg.OpenAIAPIKey, BaseURL: cfg.OpenAIBaseURL, Model: cfg.OpenAIImageModel, Quality: cfg.OpenAIImageQuality, Timeout: cfg.HairPreviewTimeout}, loader, nil)
		if err != nil {
			return AIBundle{}, err
		}
		hair = real
		if cfg.HairPreviewFallbackToDemo {
			hair = provider.NewFallbackHairGenerator(real, demoHair)
		}
	}
	demoOutfit := provider.OutfitAdvisor(provider.NewDemoOutfitAdvisor())
	outfit := demoOutfit
	if cfg.OutfitDiagnosisProvider == "openai" {
		real, err := provider.NewOpenAIOutfitAdvisor(provider.OpenAIConfig{APIKey: cfg.OpenAIAPIKey, BaseURL: cfg.OpenAIBaseURL, Model: cfg.OpenAIOutfitModel, Timeout: cfg.OutfitDiagnosisTimeout}, loader, nil)
		if err != nil {
			return AIBundle{}, err
		}
		outfit = real
		if cfg.OutfitDiagnosisFallbackToDemo {
			outfit = provider.NewFallbackOutfitAdvisor(real, demoOutfit)
		}
	}
	return AIBundle{Analyzer: analyzer, Hair: hair, Look: provider.NewDemoLookGenerator(loader), Outfit: outfit, Routes: map[string]string{
		provider.CapabilityAppearanceAnalysis: cfg.AIProvider + "/" + cfg.OpenAIVisionModel,
		provider.CapabilityHairEdit:           cfg.HairPreviewProvider + "/" + cfg.OpenAIImageModel,
		provider.CapabilityFullLookEdit:       "demo/free-pass-through",
		provider.CapabilityOutfitDiagnosis:    cfg.OutfitDiagnosisProvider + "/" + cfg.OpenAIOutfitModel,
	}}, nil
}
