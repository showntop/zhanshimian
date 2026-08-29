package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/jianwo/server/internal/config"
	"github.com/example/jianwo/server/internal/database"
	"github.com/example/jianwo/server/internal/httpapi"
	"github.com/example/jianwo/server/internal/provider"
	"github.com/example/jianwo/server/internal/repository/postgres"
	"github.com/example/jianwo/server/internal/service"
	"github.com/example/jianwo/server/internal/storage"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	pool, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err := database.Migrate(ctx, pool); err != nil {
		logger.Error("migrate database", "error", err)
		os.Exit(1)
	}
	objects, err := storage.NewLocal(cfg.UploadDir)
	if err != nil {
		logger.Error("create storage", "error", err)
		os.Exit(1)
	}
	repo := postgres.New(pool)
	analyzer, err := buildAnalyzer(cfg, repo, objects)
	if err != nil {
		logger.Error("create analysis provider", "provider", cfg.AIProvider, "error", err)
		os.Exit(1)
	}
	hairGenerator, err := buildHairGenerator(cfg, repo, objects)
	if err != nil {
		logger.Error("create hair preview provider", "provider", cfg.HairPreviewProvider, "error", err)
		os.Exit(1)
	}
	outfitAdvisor, err := buildOutfitAdvisor(cfg, repo, objects)
	if err != nil {
		logger.Error("create outfit diagnosis provider", "provider", cfg.OutfitDiagnosisProvider, "error", err)
		os.Exit(1)
	}
	logger.Info("analysis provider configured", "provider", cfg.AIProvider, "fallback_to_demo", cfg.AIProvider == "openai" && cfg.AIFallbackToDemo)
	logger.Info("hair preview provider configured", "provider", cfg.HairPreviewProvider, "fallback_to_demo", cfg.HairPreviewProvider == "openai" && cfg.HairPreviewFallbackToDemo)
	logger.Info("outfit diagnosis provider configured", "provider", cfg.OutfitDiagnosisProvider, "fallback_to_demo", cfg.OutfitDiagnosisProvider == "openai" && cfg.OutfitDiagnosisFallbackToDemo)
	svc := service.New(repo, objects, analyzer, cfg.PublicBaseURL, cfg.SessionTTL, cfg.MaxUploadBytes, logger, service.ProviderOptions{Hair: hairGenerator, Outfit: outfitAdvisor})
	if cfg.RunWorker {
		go svc.RunWorker(ctx, cfg.AnalysisPollTime)
	}

	root := http.NewServeMux()
	root.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.Dir(cfg.AssetDir))))
	root.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir(cfg.UploadDir))))
	root.Handle("/", httpapi.New(svc, logger, cfg.DevLoginEnabled, httpapi.RuntimeInfo{
		AnalysisProvider:        cfg.AIProvider,
		FallbackEnabled:         cfg.AIProvider == "openai" && cfg.AIFallbackToDemo,
		HairPreviewProvider:     cfg.HairPreviewProvider,
		OutfitDiagnosisProvider: cfg.OutfitDiagnosisProvider,
	}))
	writeTimeout := 30 * time.Second
	if cfg.OutfitDiagnosisProvider == "openai" && cfg.OutfitDiagnosisTimeout+10*time.Second > writeTimeout {
		writeTimeout = cfg.OutfitDiagnosisTimeout + 10*time.Second
	}
	server := &http.Server{Addr: cfg.Addr, Handler: root, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: writeTimeout, IdleTimeout: 90 * time.Second}
	go func() {
		logger.Info("api started", "addr", cfg.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("serve", "error", err)
			stop()
		}
	}()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil {
		logger.Error("shutdown", "error", err)
	}
}

func buildOutfitAdvisor(cfg config.Config, repo *postgres.Store, objects storage.ObjectStorage) (provider.OutfitAdvisor, error) {
	demo := provider.OutfitAdvisor(provider.NewDemoOutfitAdvisor())
	if cfg.OutfitDiagnosisProvider == "demo" {
		return demo, nil
	}
	loader := service.NewAnalysisMediaLoader(repo, objects, cfg.PublicBaseURL, cfg.MaxUploadBytes)
	realAdvisor, err := provider.NewOpenAIOutfitAdvisor(provider.OpenAIConfig{APIKey: cfg.OpenAIAPIKey, BaseURL: cfg.OpenAIBaseURL, Model: cfg.OpenAIOutfitModel, Timeout: cfg.OutfitDiagnosisTimeout}, loader, nil)
	if err != nil {
		return nil, err
	}
	if cfg.OutfitDiagnosisFallbackToDemo {
		return provider.NewFallbackOutfitAdvisor(realAdvisor, demo), nil
	}
	return realAdvisor, nil
}

func buildHairGenerator(cfg config.Config, repo *postgres.Store, objects storage.ObjectStorage) (provider.HairPreviewGenerator, error) {
	demo := provider.HairPreviewGenerator(provider.NewDemoHairGenerator())
	if cfg.HairPreviewProvider == "demo" {
		return demo, nil
	}
	loader := service.NewAnalysisMediaLoader(repo, objects, cfg.PublicBaseURL, cfg.MaxUploadBytes)
	realGenerator, err := provider.NewOpenAIHairGenerator(provider.OpenAIHairConfig{
		APIKey: cfg.OpenAIAPIKey, BaseURL: cfg.OpenAIBaseURL, Model: cfg.OpenAIImageModel,
		Quality: cfg.OpenAIImageQuality, Timeout: cfg.HairPreviewTimeout,
	}, loader, nil)
	if err != nil {
		return nil, err
	}
	if cfg.HairPreviewFallbackToDemo {
		return provider.NewFallbackHairGenerator(realGenerator, demo), nil
	}
	return realGenerator, nil
}

func buildAnalyzer(cfg config.Config, repo *postgres.Store, objects storage.ObjectStorage) (provider.Analyzer, error) {
	demo := provider.Analyzer(provider.NewDemoAnalyzer())
	if cfg.AIProvider == "demo" {
		return demo, nil
	}
	loader := service.NewAnalysisMediaLoader(repo, objects, cfg.PublicBaseURL, cfg.MaxUploadBytes)
	realAnalyzer, err := provider.NewOpenAIAnalyzer(provider.OpenAIConfig{
		APIKey: cfg.OpenAIAPIKey, BaseURL: cfg.OpenAIBaseURL, Model: cfg.OpenAIVisionModel, Timeout: cfg.AIRequestTimeout,
	}, loader, nil)
	if err != nil {
		return nil, err
	}
	if cfg.AIFallbackToDemo {
		return provider.NewFallbackAnalyzer(realAnalyzer, demo), nil
	}
	return realAnalyzer, nil
}
