package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/zhanshimian/server/internal/bootstrap"
	"github.com/zhanshimian/server/internal/config"
	"github.com/zhanshimian/server/internal/database"
	"github.com/zhanshimian/server/internal/repository/postgres"
	"github.com/zhanshimian/server/internal/service"
	"github.com/zhanshimian/server/internal/storage"
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
	logger.Info("AI capability routes configured", "source", cfg.AIRoutingSource, "routes", ai.Routes)
	svc := service.New(repo, objects, ai.Analyzer, cfg.PublicBaseURL, cfg.SessionTTL, cfg.MaxUploadBytes, logger, service.ProviderOptions{
		Hair: ai.Hair, Look: ai.Look, Orbit: ai.Orbit, PlanGroup: ai.PlanGroup, Outfit: ai.Outfit, Purchase: ai.Purchase, Advisor: ai.Advisor, AssetURLTTL: cfg.AssetURLTTL,
		AssetDir: cfg.AssetDir,
	})
	svc.RunWorker(ctx, cfg.AnalysisPollTime)
}
