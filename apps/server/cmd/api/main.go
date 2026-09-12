package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

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
	if cfg.Environment == "production" {
		if _, err := exec.LookPath("ffmpeg"); err != nil {
			logger.Error("ffmpeg is required in production", "error", err)
			os.Exit(1)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	app, err := bootstrap.BuildAPI(cfg, logger)
	if err != nil {
		logger.Error("build api", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := app.Close(); err != nil {
			logger.Error("close api", "error", err)
		}
	}()

	writeTimeout := 30 * time.Second
	for _, model := range cfg.AIRouting.Models {
		modelTimeout := time.Duration(model.TimeoutSeconds) * time.Second
		if modelTimeout <= 0 {
			modelTimeout = 90 * time.Second
		}
		if modelTimeout+10*time.Second > writeTimeout {
			writeTimeout = modelTimeout + 10*time.Second
		}
	}
	server := &http.Server{Addr: cfg.Addr, Handler: app.Handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: writeTimeout, IdleTimeout: 90 * time.Second}
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
