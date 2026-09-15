// Package limits 提供用量限额的环境变量覆盖：默认生产值，0 表示不限。
// 仅用于内测/运营调节，正式环境不设置即保持默认防线。
package limits

import (
	"os"
	"strconv"
	"strings"
)

const (
	EnvAnalysisPerDay    = "USAGE_ANALYSIS_PER_DAY"
	EnvRenderRunsPerDay  = "USAGE_RENDER_RUNS_PER_DAY"
	EnvRenderConcurrency = "USAGE_RENDER_CONCURRENCY"
	EnvDiagnosticPerDay  = "USAGE_DIAGNOSTIC_PER_DAY"
	EnvAdvisorPerDay     = "USAGE_ADVISOR_PER_DAY"
	EnvAdvisorPerHour    = "USAGE_ADVISOR_PER_HOUR"
)

// FromEnv 读取限额覆盖；未设置/非法值回退默认。0 = 不限。
func FromEnv(key string, def int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return def
	}
	return value
}
