package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Addr                          string
	DatabaseURL                   string
	PublicBaseURL                 string
	UploadDir                     string
	AssetDir                      string
	DevLoginEnabled               bool
	RunWorker                     bool
	SessionTTL                    time.Duration
	MaxUploadBytes                int64
	AnalysisPollTime              time.Duration
	AIProvider                    string
	AIFallbackToDemo              bool
	AIRequestTimeout              time.Duration
	OpenAIAPIKey                  string
	OpenAIBaseURL                 string
	OpenAIVisionModel             string
	HairPreviewProvider           string
	HairPreviewFallbackToDemo     bool
	HairPreviewTimeout            time.Duration
	OpenAIImageModel              string
	OpenAIImageQuality            string
	OutfitDiagnosisProvider       string
	OutfitDiagnosisFallbackToDemo bool
	OutfitDiagnosisTimeout        time.Duration
	OpenAIOutfitModel             string
}

func Load() (Config, error) {
	aiProvider := env("AI_PROVIDER", "demo")
	cfg := Config{
		Addr:                          env("ADDR", ":58000"),
		DatabaseURL:                   env("DATABASE_URL", "postgres://jianwo:jianwo@localhost:55432/jianwo?sslmode=disable"),
		PublicBaseURL:                 env("PUBLIC_BASE_URL", "http://localhost:58000"),
		UploadDir:                     env("UPLOAD_DIR", "data/uploads"),
		AssetDir:                      env("ASSET_DIR", "assets"),
		DevLoginEnabled:               envBool("DEV_LOGIN_ENABLED", true),
		RunWorker:                     envBool("RUN_WORKER", true),
		SessionTTL:                    30 * 24 * time.Hour,
		MaxUploadBytes:                10 << 20,
		AnalysisPollTime:              700 * time.Millisecond,
		AIProvider:                    aiProvider,
		AIFallbackToDemo:              envBool("AI_FALLBACK_TO_DEMO", true),
		AIRequestTimeout:              time.Duration(envInt("AI_REQUEST_TIMEOUT_SECONDS", 90)) * time.Second,
		OpenAIAPIKey:                  os.Getenv("OPENAI_API_KEY"),
		OpenAIBaseURL:                 env("OPENAI_BASE_URL", "https://api.openai.com/v1"),
		OpenAIVisionModel:             env("OPENAI_VISION_MODEL", "gpt-5-mini"),
		HairPreviewProvider:           env("HAIR_PREVIEW_PROVIDER", aiProvider),
		HairPreviewFallbackToDemo:     envBool("HAIR_PREVIEW_FALLBACK_TO_DEMO", true),
		HairPreviewTimeout:            time.Duration(envInt("HAIR_PREVIEW_TIMEOUT_SECONDS", 150)) * time.Second,
		OpenAIImageModel:              env("OPENAI_IMAGE_MODEL", "gpt-image-2"),
		OpenAIImageQuality:            env("OPENAI_IMAGE_QUALITY", "medium"),
		OutfitDiagnosisProvider:       env("OUTFIT_DIAGNOSIS_PROVIDER", aiProvider),
		OutfitDiagnosisFallbackToDemo: envBool("OUTFIT_DIAGNOSIS_FALLBACK_TO_DEMO", true),
		OutfitDiagnosisTimeout:        time.Duration(envInt("OUTFIT_DIAGNOSIS_TIMEOUT_SECONDS", 60)) * time.Second,
		OpenAIOutfitModel:             env("OPENAI_OUTFIT_MODEL", "gpt-5-mini"),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.AIProvider != "demo" && cfg.AIProvider != "openai" {
		return Config{}, fmt.Errorf("AI_PROVIDER must be demo or openai")
	}
	if cfg.HairPreviewProvider != "demo" && cfg.HairPreviewProvider != "openai" {
		return Config{}, fmt.Errorf("HAIR_PREVIEW_PROVIDER must be demo or openai")
	}
	if cfg.OutfitDiagnosisProvider != "demo" && cfg.OutfitDiagnosisProvider != "openai" {
		return Config{}, fmt.Errorf("OUTFIT_DIAGNOSIS_PROVIDER must be demo or openai")
	}
	if cfg.OpenAIImageQuality != "low" && cfg.OpenAIImageQuality != "medium" && cfg.OpenAIImageQuality != "high" {
		return Config{}, fmt.Errorf("OPENAI_IMAGE_QUALITY must be low, medium or high")
	}
	return cfg, nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
