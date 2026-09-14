package config

import (
	"encoding/json"
	"os"
	"testing"
)

// release 块是放量门禁的载体:四个字段必须齐全,candidate_percent 只接受
// 0/5/25/50/100——写错的配置必须在启动时失败,不能带病上线。
func TestAIReleaseValidation(t *testing.T) {
	routing := minimalStructuredRouting("photo_check")
	valid := &AIReleaseConfig{
		PreviousVersion: "quality-core-r1", CandidateVersion: "quality-core-r2",
		CandidatePercent: 5, BucketSalt: "quality-core-2026-09-12",
	}
	routing.Release = valid
	if err := validateAIRouting(routing); err != nil {
		t.Fatal(err)
	}
	for _, percent := range []int{0, 5, 25, 50, 100} {
		release := *valid
		release.CandidatePercent = percent
		routing.Release = &release
		if err := validateAIRouting(routing); err != nil {
			t.Fatalf("percent %d: %v", percent, err)
		}
	}
	for _, percent := range []int{-1, 1, 10, 99, 101} {
		release := *valid
		release.CandidatePercent = percent
		routing.Release = &release
		if err := validateAIRouting(routing); err == nil {
			t.Fatalf("percent %d must be rejected", percent)
		}
	}
	release := *valid
	release.BucketSalt = " "
	routing.Release = &release
	if err := validateAIRouting(routing); err == nil {
		t.Fatal("empty bucket_salt must be rejected")
	}
	release = *valid
	release.CandidateVersion = ""
	routing.Release = &release
	if err := validateAIRouting(routing); err == nil {
		t.Fatal("empty candidate_version must be rejected")
	}
	routing.Release = nil
	if err := validateAIRouting(routing); err != nil {
		t.Fatalf("missing release block stays valid: %v", err)
	}
}

// 两份随仓配置都必须带齐 release 元数据,放量手册依赖这四个字段。
func TestShippedConfigsCarryReleaseBlock(t *testing.T) {
	t.Setenv("BAILIAN_WORKSPACE_ID", "workspace-test")
	for _, name := range []string{"ai-routing.example.json", "ai-routing.production.json"} {
		data, err := os.ReadFile("../../config/" + name)
		if err != nil {
			t.Fatal(err)
		}
		var routing AIRoutingConfig
		if err := json.Unmarshal(expandAIRoutingEnv(data), &routing); err != nil {
			t.Fatalf("decode %s: %v", name, err)
		}
		if err := validateAIRouting(routing); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if routing.Release == nil {
			t.Fatalf("%s: missing release block", name)
		}
		if routing.Release.CandidatePercent != 5 {
			t.Fatalf("%s: candidate_percent = %d, want 5", name, routing.Release.CandidatePercent)
		}
	}
}
