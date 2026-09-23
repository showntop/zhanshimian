package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 测试数据集由代码按真实金集规模生成(60 身份/120 评估案例/120 brief/
// 180 plan/300 渲染候选/64+30 固件),避免把授权素材或大体积合成固件提交进仓。

const (
	testContractPath  = "../../eval/goldens/manifest.json"
	assessmentFixture = "../../eval/assessment/testdata/manifest.json"
	briefsFixture     = "../../eval/planning/testdata/briefs.v1.jsonl"
	planSetsFixture   = "../../eval/planning/testdata/plan_sets.v1.jsonl"
	renderReportFile  = "../../eval/rendering/testdata/passing-report.json"
)

// TestWriteGoldenDatasetToDir 是开发工具:设置 UPLOOK_GOLDEN_GEN_DIR 时把
// 全规模合成金集写到该目录(不删除),供 scripts/staging-golden.sh 的本地
// 端到端演练使用;未设置时跳过。
func TestWriteGoldenDatasetToDir(t *testing.T) {
	dir := os.Getenv("UPLOOK_GOLDEN_GEN_DIR")
	if dir == "" {
		t.Skip("UPLOOK_GOLDEN_GEN_DIR is not set")
	}
	writeGoldenDataset(t, dir)
	t.Logf("golden dataset written to %s", dir)
}

func TestRunPassesOnFullScaleGoldenDataset(t *testing.T) {
	root := t.TempDir()
	writeGoldenDataset(t, root)
	output := filepath.Join(root, "locked.json")

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"--manifest", testContractPath,
		"--dataset-root", root,
		"--dataset-split", "locked",
		"--routing-config", writeRoutingConfig(t, root, 5),
		"--output", output,
	}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("exit = %d, stderr = %s", code, stderr.String())
	}

	var report map[string]any
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	// 与 staging-golden.sh 的 jq 门禁逐条同义断言。
	gates := map[string]bool{
		"photo_role_accuracy == 1":              report["photo_role_accuracy"] == 1.0,
		"source_mismatch_count == 0":            report["source_mismatch_count"] == 0.0,
		"sensitive_inference_count == 0":        report["sensitive_inference_count"] == 0.0,
		"finding_evidence_completeness == 1":    report["finding_evidence_completeness"] == 1.0,
		"finding_evidence_support_rate >= 0.95": report["finding_evidence_support_rate"].(float64) >= 0.95,
		"plan_grounding_completeness == 1":      report["plan_grounding_completeness"] == 1.0,
		"plan_pair_difference_pass_rate == 1":   report["plan_pair_difference_pass_rate"] == 1.0,
		"single_image_fallback_count == 0":      report["single_image_fallback_count"] == 0.0,
		"severe_bad_render_publish_rate < 0.01": report["severe_bad_render_publish_rate"].(float64) < 0.01,
		"identity_human_pass_rate >= 0.90":      report["identity_human_pass_rate"].(float64) >= 0.90,
		"render_spec_match_rate >= 0.85":        report["render_spec_match_rate"].(float64) >= 0.85,
		"link_rate == 1":                        report["source_operation_invocation_feedback_link_rate"] == 1.0,
		"webp_publish_count == 0":               report["webp_publish_count"] == 0.0,
		"photo_check_p95_ms <= 12000":           report["photo_check_p95_ms"].(float64) <= 12000,
		"report_p95_ms <= 90000":                report["report_p95_ms"].(float64) <= 90000,
		"plan_text_p95_ms <= 60000":             report["plan_text_p95_ms"].(float64) <= 60000,
		"first_render_p95_ms <= 120000":         report["first_render_p95_ms"].(float64) <= 120000,
	}
	for gate, ok := range gates {
		if !ok {
			t.Fatalf("gate %s failed in report: %v", gate, report)
		}
	}
	release := report["release"].(map[string]any)
	if release["candidate_version"] != "quality-core-r2" || release["candidate_percent"] != 5.0 {
		t.Fatalf("report must attribute the release metadata: %v", release)
	}
	if report["dataset_split"] != "locked" {
		t.Fatalf("dataset_split = %v", report["dataset_split"])
	}
}

func TestRunRejectsIncompletePhotoRoles(t *testing.T) {
	root := t.TempDir()
	writeGoldenDataset(t, root)
	mutateAssessmentCases(t, root, func(c map[string]any) {
		if c["id"] == "case-0000" {
			c["input"].(map[string]any)["face"] = ""
		}
	})
	assertRunExit(t, root, exitContractBad)
}

func TestRunRejectsIdentityCrossingSplits(t *testing.T) {
	root := t.TempDir()
	writeGoldenDataset(t, root)
	mutateAssessmentCases(t, root, func(c map[string]any) {
		if c["id"] == "case-0119" { // release 分区,复用 development 分区的身份
			c["identity_id"] = "identity-0000"
		}
	})
	assertRunExit(t, root, exitContractBad)
}

func TestRunRejectsCountFloorViolation(t *testing.T) {
	root := t.TempDir()
	writeGoldenDataset(t, root)
	// 只留 30 个案例:photos=90 < 180,identities=30 < 60。
	mutateAssessmentCases(t, root, func(c map[string]any) {
		id := c["id"].(string)
		if id >= "case-0030" {
			c["id"] = ""
		}
	})
	assertRunExit(t, root, exitContractBad)
}

func TestRunFailsGateOnSourceMismatch(t *testing.T) {
	root := t.TempDir()
	writeGoldenDataset(t, root)
	reportPath := filepath.Join(root, "rendering", "report.json")
	var report map[string]any
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	report["source_mismatch_count"] = 1
	writeJSON(t, reportPath, report)
	assertRunExit(t, root, exitGateFailed)
}

func TestRunRejectsInvalidCandidatePercent(t *testing.T) {
	root := t.TempDir()
	writeGoldenDataset(t, root)
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"--manifest", testContractPath,
		"--dataset-root", root,
		"--dataset-split", "locked",
		"--routing-config", writeRoutingConfig(t, root, 7),
		"--output", filepath.Join(root, "locked.json"),
	}, &stdout, &stderr)
	if code != exitContractBad {
		t.Fatalf("exit = %d, want 2", code)
	}
}

func TestRunRejectsMissingMetrics(t *testing.T) {
	root := t.TempDir()
	writeGoldenDataset(t, root)
	if err := os.Remove(filepath.Join(root, "metrics.json")); err != nil {
		t.Fatal(err)
	}
	assertRunExit(t, root, exitContractBad)
}

func assertRunExit(t *testing.T, root string, want int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"--manifest", testContractPath,
		"--dataset-root", root,
		"--dataset-split", "locked",
		"--routing-config", writeRoutingConfig(t, root, 5),
		"--output", filepath.Join(root, "locked.json"),
	}, &stdout, &stderr)
	if code != want {
		t.Fatalf("exit = %d, want %d (stderr = %s)", code, want, stderr.String())
	}
}

func writeRoutingConfig(t *testing.T, root string, percent int) string {
	t.Helper()
	path := filepath.Join(root, fmt.Sprintf("ai-routing-%d.json", percent))
	writeJSON(t, path, map[string]any{
		"models": map[string]any{},
		"routes": map[string]any{},
		"release": map[string]any{
			"previous_version":  "quality-core-r1",
			"candidate_version": "quality-core-r2",
			"candidate_percent": percent,
			"bucket_salt":       "quality-core-2026-09-12",
		},
	})
	return path
}

func writeGoldenDataset(t *testing.T, root string) {
	t.Helper()
	writeAssessmentManifest(t, root, 120)
	copyFile(t, briefsFixture, filepath.Join(root, "planning", "briefs.v1.jsonl"))
	copyFile(t, planSetsFixture, filepath.Join(root, "planning", "plan_sets.v1.jsonl"))
	writeRenderingManifest(t, root)
	copyFile(t, renderReportFile, filepath.Join(root, "rendering", "report.json"))
	writeLines(t, filepath.Join(root, "fixtures", "source_states.jsonl"), 64)
	writeLines(t, filepath.Join(root, "fixtures", "feedback_sequences.jsonl"), 30)
	writeJSON(t, filepath.Join(root, "metrics.json"), map[string]any{
		"photo_check_p95_ms": 8000,
		"report_p95_ms":      61000,
		"plan_text_p95_ms":   43000,
		"source_operation_invocation_feedback_link_rate": 1,
		"single_image_fallback_count":                    0,
	})
}

// writeAssessmentManifest 以 pass-evidence 模板克隆 n 个发布案例,
// 60/20/20 分区,每案例一个独立身份。
func writeAssessmentManifest(t *testing.T, root string, n int) {
	t.Helper()
	raw, err := os.ReadFile(assessmentFixture)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		SchemaVersion string           `json:"schema_version"`
		Cases         []map[string]any `json:"cases"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	var template map[string]any
	for _, c := range fixture.Cases {
		if c["id"] == "pass-evidence" {
			template = c
		}
	}
	if template == nil {
		t.Fatal("pass-evidence template missing from fixture")
	}
	cases := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		cloneBytes, err := json.Marshal(template)
		if err != nil {
			t.Fatal(err)
		}
		var clone map[string]any
		if err := json.Unmarshal(cloneBytes, &clone); err != nil {
			t.Fatal(err)
		}
		clone["id"] = fmt.Sprintf("case-%04d", i)
		clone["identity_id"] = fmt.Sprintf("identity-%04d", i)
		switch {
		case i*100 < n*60:
			clone["split"] = "development"
		case i*100 < n*80:
			clone["split"] = "validation"
		default:
			clone["split"] = "release"
		}
		cases = append(cases, clone)
	}
	writeJSON(t, filepath.Join(root, "assessment", "manifest.json"), map[string]any{
		"schema_version": fixture.SchemaVersion,
		"cases":          cases,
	})
}

func mutateAssessmentCases(t *testing.T, root string, mutate func(map[string]any)) {
	t.Helper()
	path := filepath.Join(root, "assessment", "manifest.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		SchemaVersion string           `json:"schema_version"`
		Cases         []map[string]any `json:"cases"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	kept := manifest.Cases[:0]
	for _, c := range manifest.Cases {
		mutate(c)
		if c["id"] != "" {
			kept = append(kept, c)
		}
	}
	writeJSON(t, path, map[string]any{"schema_version": manifest.SchemaVersion, "cases": kept})
}

// writeRenderingManifest 生成 60 身份 × 5 候选 = 300 行,分区 180/60/60。
func writeRenderingManifest(t *testing.T, root string) {
	t.Helper()
	hash := func(parts ...string) string {
		return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(parts, ":"))))
	}
	lines := make([]string, 0, 300)
	for identity := 0; identity < 60; identity++ {
		split := "development"
		if identity >= 36 && identity < 48 {
			split = "validation"
		} else if identity >= 48 {
			split = "locked"
		}
		for candidate := 0; candidate < 5; candidate++ {
			caseID := fmt.Sprintf("render-case-%04d", identity*5+candidate)
			lines = append(lines, mustJSON(t, map[string]any{
				"case_id":              caseID,
				"identity_hash":        hash("identity", fmt.Sprint(identity)),
				"split":                split,
				"consent_record_id":    fmt.Sprintf("consent-%04d", identity),
				"body_object_key":      fmt.Sprintf("goldens/%04d/body.jpg", identity),
				"face_object_key":      fmt.Sprintf("goldens/%04d/face.jpg", identity),
				"candidate_object_key": fmt.Sprintf("goldens/%04d/candidate-%d.jpg", identity, candidate),
				"sha256": map[string]string{
					"body":      hash("body", fmt.Sprint(identity)),
					"face":      hash("face", fmt.Sprint(identity)),
					"candidate": hash("candidate", fmt.Sprint(identity), fmt.Sprint(candidate)),
				},
				"expected": map[string]any{
					"identity_pass": true, "anatomy_pass": true,
					"composition_pass": true, "semantic_pass": true,
					"severe_reason_codes": []string{},
				},
			}))
		}
	}
	path := filepath.Join(root, "rendering", "manifest.jsonl")
	mkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeLines(t *testing.T, path string, n int) {
	t.Helper()
	lines := make([]string, n)
	for i := range lines {
		lines[i] = "{}"
	}
	mkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	mkdir(t, filepath.Dir(dst))
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	mkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(mustJSON(t, value)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func mkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}
