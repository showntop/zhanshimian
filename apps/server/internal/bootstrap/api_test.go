package bootstrap

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/config"
	"github.com/zhanshimian/server/internal/service/taskrunner"
)

func testConfig() config.Config {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://jianwo:jianwo@localhost:55432/jianwo?sslmode=disable"
	}
	return config.Config{
		Environment:        "test",
		Addr:               "127.0.0.1:0",
		DatabaseURL:        databaseURL,
		PublicBaseURL:      "http://localhost:58000",
		UploadDir:          os.TempDir(),
		AssetDir:           "assets",
		StorageProvider:    "local",
		DevLoginEnabled:    true,
		WeatherProvider:    "demo",
		SmsProvider:        "console",
		SessionTTL:         time.Hour,
		MaxUploadBytes:     10 << 20,
		WorkerID:           "worker-test",
		TaskPollInterval:   500 * time.Millisecond,
		TaskLeaseDuration:  30 * time.Second,
		TaskHeartbeatEvery: 10 * time.Second,
		AIRoutingSource:    "test",
		AIRouting: config.AIRoutingConfig{
			Models: map[string]config.AIModelConfig{
				"demo": {
					Vendor:    "demo",
					Protocol:  "openai_chat_completions",
					Model:     "demo",
					BaseURL:   "https://example.com/v1",
					APIKeyEnv: "BOOTSTRAP_TEST_AI_KEY",
				},
			},
			Routes: map[string]config.AIRouteConfig{
				"appearance_analysis": {Primary: "demo"},
				"outfit_diagnosis":    {Primary: "demo"},
				"hair_edit":           {Primary: "demo"},
			},
		},
	}
}

func TestBuildAPIDoesNotConstructRunner(t *testing.T) {
	t.Setenv("BOOTSTRAP_TEST_AI_KEY", "test-key")
	deps := Dependencies{RunnerFactory: func(taskrunner.Options) (*taskrunner.Runner, error) {
		t.Fatal("API must not construct worker runner")
		return nil, nil
	}}
	app, err := BuildAPIWithDependencies(testConfig(), slog.Default(), deps)
	if err != nil {
		t.Fatalf("BuildAPIWithDependencies: %v", err)
	}
	if app == nil || app.Handler == nil {
		t.Fatal("expected API handler")
	}
	if err := app.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
