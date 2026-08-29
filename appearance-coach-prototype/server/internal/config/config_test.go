package config

import "testing"

func TestLoadAIProviderDefaults(t *testing.T) {
	t.Setenv("AI_PROVIDER", "")
	t.Setenv("AI_FALLBACK_TO_DEMO", "")
	t.Setenv("AI_REQUEST_TIMEOUT_SECONDS", "")
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_VISION_MODEL", "")
	t.Setenv("HAIR_PREVIEW_PROVIDER", "")
	t.Setenv("HAIR_PREVIEW_FALLBACK_TO_DEMO", "")
	t.Setenv("OPENAI_IMAGE_MODEL", "")
	t.Setenv("OPENAI_IMAGE_QUALITY", "")
	t.Setenv("OUTFIT_DIAGNOSIS_PROVIDER", "")
	t.Setenv("OUTFIT_DIAGNOSIS_FALLBACK_TO_DEMO", "")
	t.Setenv("OPENAI_OUTFIT_MODEL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AIProvider != "demo" || !cfg.AIFallbackToDemo || cfg.OpenAIVisionModel != "gpt-5-mini" || cfg.HairPreviewProvider != "demo" || cfg.OpenAIImageModel != "gpt-image-2" || cfg.OutfitDiagnosisProvider != "demo" {
		t.Fatalf("unexpected AI defaults: %#v", cfg)
	}
}

func TestLoadRejectsInvalidImageQuality(t *testing.T) {
	t.Setenv("OPENAI_IMAGE_QUALITY", "ultra")
	if _, err := Load(); err == nil {
		t.Fatal("expected invalid image quality to be rejected")
	}
}

func TestLoadRejectsUnknownAIProvider(t *testing.T) {
	t.Setenv("AI_PROVIDER", "unknown")
	if _, err := Load(); err == nil {
		t.Fatal("expected unknown AI provider to be rejected")
	}
}
