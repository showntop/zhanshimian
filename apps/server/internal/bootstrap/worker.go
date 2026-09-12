package bootstrap

import (
	"context"
	"log/slog"

	"github.com/zhanshimian/server/internal/config"
	"github.com/zhanshimian/server/internal/database"
	"github.com/zhanshimian/server/internal/repository/postgres"
	"github.com/zhanshimian/server/internal/service/taskrunner"
)

type WorkerApp struct {
	run   func(context.Context)
	close func() error
}

func (a *WorkerApp) Run(ctx context.Context) error {
	if a == nil || a.run == nil {
		return nil
	}
	a.run(ctx)
	return nil
}

func (a *WorkerApp) Close() error {
	if a == nil || a.close == nil {
		return nil
	}
	return a.close()
}

func BuildWorker(cfg config.Config, logger *slog.Logger) (*WorkerApp, error) {
	if err := config.Validate(cfg); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}

	ctx := context.Background()
	pool, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	if err := database.Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	store := postgres.New(pool)
	registry, err := taskrunner.NewRegistry(nil, nil)
	if err != nil {
		pool.Close()
		return nil, err
	}
	opts := taskrunner.Options{
		Store:        store,
		Registry:     registry,
		WorkerID:     cfg.WorkerID,
		PollInterval: cfg.TaskPollInterval,
		Logger:       logger,
	}
	runner := taskrunner.NewRunner(opts.Store, opts.Registry, opts.WorkerID, opts.PollInterval, opts.Logger)
	return &WorkerApp{
		run: runner.Run,
		close: func() error {
			pool.Close()
			return nil
		},
	}, nil
}
