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

	"github.com/example/jianwo/server/internal/bootstrap"
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
	objects, err := storage.New(storage.Config{
		Provider: cfg.StorageProvider, LocalRoot: cfg.UploadDir,
		COS: storage.COSConfig{BucketURL: cfg.COSBucketURL, SecretID: cfg.COSSecretID, SecretKey: cfg.COSSecretKey, KeyPrefix: cfg.COSKeyPrefix},
	})
	if err != nil {
		logger.Error("create storage", "error", err)
		os.Exit(1)
	}
	repo := postgres.New(pool)
	ai, err := bootstrap.BuildAI(cfg, repo, objects, logger)
	if err != nil {
		logger.Error("create AI capability providers", "routing_source", cfg.AIRoutingSource, "error", err)
		os.Exit(1)
	}
	weather, err := buildWeather(cfg)
	if err != nil {
		logger.Error("create weather provider", "provider", cfg.WeatherProvider, "error", err)
		os.Exit(1)
	}
	wechat, err := buildWeChat(cfg)
	if err != nil {
		logger.Error("create wechat login provider", "error", err)
		os.Exit(1)
	}
	logger.Info("AI capability routes configured", "source", cfg.AIRoutingSource, "routes", ai.Routes)
	svc := service.New(repo, objects, ai.Analyzer, cfg.PublicBaseURL, cfg.SessionTTL, cfg.MaxUploadBytes, logger, service.ProviderOptions{
		Hair: ai.Hair, Outfit: ai.Outfit, Purchase: ai.Purchase, Advisor: ai.Advisor, Weather: weather, WeChat: wechat, AssetURLTTL: cfg.AssetURLTTL,
	})
	if cfg.RunWorker {
		go svc.RunWorker(ctx, cfg.AnalysisPollTime)
	}

	root := http.NewServeMux()
	root.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.Dir(cfg.AssetDir))))
	root.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir(cfg.UploadDir))))
	root.Handle("/", httpapi.New(svc, logger, cfg.DevLoginEnabled, httpapi.RuntimeInfo{
		Environment:             cfg.Environment,
		StorageProvider:         cfg.StorageProvider,
		WeatherProvider:         cfg.WeatherProvider,
		WeChatLoginConfigured:   wechat != nil,
		AnalysisProvider:        cfg.AIProvider,
		FallbackEnabled:         cfg.AIProvider == "openai" && cfg.AIFallbackToDemo,
		HairPreviewProvider:     cfg.HairPreviewProvider,
		OutfitDiagnosisProvider: cfg.OutfitDiagnosisProvider,
		AIRoutes:                ai.Routes,
	}))
	writeTimeout := 30 * time.Second
	if cfg.AIRoutingSource != "" {
		writeTimeout = 30 * time.Second
		for _, model := range cfg.AIRouting.Models {
			modelTimeout := time.Duration(model.TimeoutSeconds) * time.Second
			if modelTimeout <= 0 {
				modelTimeout = 90 * time.Second
			}
			if modelTimeout+10*time.Second > writeTimeout {
				writeTimeout = modelTimeout + 10*time.Second
			}
		}
	}
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

func buildWeChat(cfg config.Config) (provider.WeChatAuthenticator, error) {
	if cfg.WeChatAppID == "" || cfg.WeChatAppSecret == "" {
		if cfg.DevLoginEnabled {
			return nil, nil
		}
		return nil, errors.New("wechat credentials are required when development login is disabled")
	}
	return provider.NewWeChatCodeExchanger(provider.WeChatConfig{
		AppID: cfg.WeChatAppID, AppSecret: cfg.WeChatAppSecret,
		BaseURL: cfg.WeChatAPIBaseURL, Timeout: cfg.WeChatRequestTimeout,
	}, nil)
}

func buildWeather(cfg config.Config) (provider.WeatherProvider, error) {
	if cfg.WeatherProvider == "demo" {
		return provider.NewDemoWeatherProvider(), nil
	}
	return provider.NewAMapWeatherProvider(provider.AMapWeatherConfig{
		APIKey: cfg.AMapWebServiceKey, BaseURL: cfg.AMapAPIBaseURL,
		DefaultCity: cfg.AMapDefaultCity, DefaultCode: cfg.AMapDefaultAdcode,
		Timeout: cfg.WeatherRequestTimeout,
	}, nil)
}
