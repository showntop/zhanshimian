package main

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/zhanshimian/server/internal/bootstrap"
	"github.com/zhanshimian/server/internal/config"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config", "error", err)
		os.Exit(1)
	}
	if err := config.Validate(cfg); err != nil {
		logger.Error("validate config", "error", err)
		os.Exit(1)
	}
	// worker 要用 ffmpeg 给 body_orbit 视频抽帧；生产镜像缺了直接拒绝启动，
	// 不让任务跑到一半才域失败。
	if cfg.Environment == "production" {
		if _, err := exec.LookPath("ffmpeg"); err != nil {
			logger.Error("ffmpeg is required in production", "error", err)
			os.Exit(1)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	app, err := bootstrap.BuildWorker(cfg, logger)
	if err != nil {
		logger.Error("build worker", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := app.Close(); err != nil {
			logger.Error("close worker", "error", err)
		}
	}()

	if err := app.Run(ctx); err != nil {
		logger.Error("worker stopped", "error", err)
		os.Exit(1)
	}
}
