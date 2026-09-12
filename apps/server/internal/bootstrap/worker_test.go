package bootstrap

import (
	"strings"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/config"
)

func TestWorkerConfigRequiresStableOwnerAndHeartbeatRatio(t *testing.T) {
	cfg := testConfig()
	cfg.WorkerID = ""
	err := config.Validate(cfg)
	if err == nil || !strings.Contains(err.Error(), "WORKER_ID") {
		t.Fatalf("expected WORKER_ID error, got %v", err)
	}
	cfg.WorkerID = "worker-a"
	cfg.TaskLeaseDuration = 20 * time.Second
	cfg.TaskHeartbeatEvery = 15 * time.Second
	err = config.Validate(cfg)
	if err == nil || !strings.Contains(err.Error(), "lease duration") {
		t.Fatalf("expected lease duration error, got %v", err)
	}
}
