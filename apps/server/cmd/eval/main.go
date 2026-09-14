// Command eval 是统一的金集评测入口。它先做数据集契约校验(数量下限、
// 三张照片角色完整性、身份不跨分区),违反时在运行任何评测前退出 2;契约
// 通过后分派到 eval/assessment、eval/planning、eval/rendering,任一硬门禁
// 失败退出 1;全部通过则把锁定集报告写到 --output 并退出 0。阈值只存在
// 于各 eval 包与 scripts/staging-golden.sh 的 jq 门禁,本命令不重复实现。
//
// 数据集目录布局(由私有对象存储挂载到 UPLOOK_GOLDEN_DATASET_DIR):
//
//	assessment/manifest.json      授权评估案例(内联 provider 样本)
//	planning/briefs.v1.jsonl      场景 brief 归一化案例
//	planning/plan_sets.v1.jsonl   PlanSet Gate 期望案例
//	rendering/manifest.jsonl      渲染金集清单(GoldCase 行)
//	rendering/report.json         渲染线束产出的 EvalReport(含 P95)
//	fixtures/source_states.jsonl  来源/状态固件(64 行)
//	fixtures/feedback_sequences.jsonl 反馈-再生成序列(30 行)
//	metrics.json                  线束实测指标(三端 P95、链接率、单图降级数)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/zhanshimian/server/eval/assessment"
	"github.com/zhanshimian/server/eval/planning"
	"github.com/zhanshimian/server/eval/rendering"
	providerai "github.com/zhanshimian/server/internal/provider/ai"
)

const (
	exitOK          = 0
	exitGateFailed  = 1
	exitContractBad = 2
)

// goldenContract 是 eval/goldens/manifest.json 冻结的金集契约。
type goldenContract struct {
	SchemaVersion             int            `json:"schema_version"`
	IdentityCount             int            `json:"identity_count"`
	PhotoCount                int            `json:"photo_count"`
	ReportCount               int            `json:"report_count"`
	SceneBriefCount           int            `json:"scene_brief_count"`
	PlanGroupCount            int            `json:"plan_group_count"`
	RenderMinCount            int            `json:"render_min_count"`
	SourceStateFixtureCount   int            `json:"source_state_fixture_count"`
	FeedbackSequenceCount     int            `json:"feedback_sequence_count"`
	Splits                    map[string]int `json:"splits"`
	IdentityCrossSplitAllowed bool           `json:"identity_cross_split_allowed"`
}

// measuredMetrics 是线束实测随数据集携带的指标;cmd 只校验齐全并透传,
// 阈值判断留给 staging-golden.sh 的 jq 门禁。
type measuredMetrics struct {
	PhotoCheckP95MS          *int64   `json:"photo_check_p95_ms"`
	ReportP95MS              *int64   `json:"report_p95_ms"`
	PlanTextP95MS            *int64   `json:"plan_text_p95_ms"`
	LinkRate                 *float64 `json:"source_operation_invocation_feedback_link_rate"`
	SingleImageFallbackCount *int     `json:"single_image_fallback_count"`
}

type goldenDataset struct {
	assessment       assessment.Manifest
	briefs           []planning.BriefCase
	plans            []planning.PlanSetCase
	rendering        rendering.Manifest
	renderReport     rendering.EvalReport
	metrics          measuredMetrics
	sourceStateCount int
	feedbackSeqCount int
	briefsPath       string
	plansPath        string
}

type lockedReport struct {
	Schema       string                   `json:"schema"`
	DatasetSplit string                   `json:"dataset_split"`
	Release      providerai.ReleaseConfig `json:"release"`

	PhotoRoleAccuracy                         float64 `json:"photo_role_accuracy"`
	SourceMismatchCount                       int     `json:"source_mismatch_count"`
	SensitiveInferenceCount                   int     `json:"sensitive_inference_count"`
	FindingEvidenceCompleteness               float64 `json:"finding_evidence_completeness"`
	FindingEvidenceSupportRate                float64 `json:"finding_evidence_support_rate"`
	PlanGroundingCompleteness                 float64 `json:"plan_grounding_completeness"`
	PlanPairDifferencePassRate                float64 `json:"plan_pair_difference_pass_rate"`
	SingleImageFallbackCount                  int     `json:"single_image_fallback_count"`
	SevereBadRenderPublishRate                float64 `json:"severe_bad_render_publish_rate"`
	IdentityHumanPassRate                     float64 `json:"identity_human_pass_rate"`
	RenderSpecMatchRate                       float64 `json:"render_spec_match_rate"`
	SourceOperationInvocationFeedbackLinkRate float64 `json:"source_operation_invocation_feedback_link_rate"`
	WebPPublishCount                          int     `json:"webp_publish_count"`
	PhotoCheckP95MS                           int64   `json:"photo_check_p95_ms"`
	ReportP95MS                               int64   `json:"report_p95_ms"`
	PlanTextP95MS                             int64   `json:"plan_text_p95_ms"`
	FirstRenderP95MS                          int64   `json:"first_render_p95_ms"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("eval", flag.ContinueOnError)
	flags.SetOutput(stderr)
	manifestPath := flags.String("manifest", "", "frozen golden contract manifest (eval/goldens/manifest.json)")
	datasetRoot := flags.String("dataset-root", "", "mounted golden dataset directory")
	datasetSplit := flags.String("dataset-split", "", "gated partition: development|validation|locked")
	routingConfigPath := flags.String("routing-config", "", "AI routing config carrying the release block")
	outputPath := flags.String("output", "", "locked-set report output path")
	if err := flags.Parse(args); err != nil {
		return exitContractBad
	}
	if *manifestPath == "" || *datasetRoot == "" || *datasetSplit == "" || *routingConfigPath == "" || *outputPath == "" {
		fmt.Fprintln(stderr, "eval: --manifest, --dataset-root, --dataset-split, --routing-config and --output are all required")
		return exitContractBad
	}

	contract, err := loadContract(*manifestPath)
	if err != nil {
		fmt.Fprintln(stderr, "eval: contract:", err)
		return exitContractBad
	}
	release, err := loadRelease(*routingConfigPath)
	if err != nil {
		fmt.Fprintln(stderr, "eval: routing config:", err)
		return exitContractBad
	}
	dataset, err := loadDataset(*datasetRoot)
	if err != nil {
		fmt.Fprintln(stderr, "eval: dataset:", err)
		return exitContractBad
	}
	// 契约校验必须先于一切评测:数量下限、三张照片角色完整性、身份不跨分区。
	if err := validateDataset(contract, dataset, *datasetSplit); err != nil {
		fmt.Fprintln(stderr, "eval: contract violation:", err)
		return exitContractBad
	}

	report, err := evaluate(dataset, release, *datasetSplit)
	if err != nil {
		fmt.Fprintln(stderr, "eval: hard gate failed:", err)
		return exitGateFailed
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintln(stderr, "eval: encode report:", err)
		return exitGateFailed
	}
	if err := os.WriteFile(*outputPath, append(data, '\n'), 0o644); err != nil {
		fmt.Fprintln(stderr, "eval: write report:", err)
		return exitGateFailed
	}
	fmt.Fprintf(stdout, "locked-set evaluation passed: %s\n", *outputPath)
	return exitOK
}

func loadContract(path string) (goldenContract, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return goldenContract{}, err
	}
	var contract goldenContract
	if err := json.Unmarshal(data, &contract); err != nil {
		return goldenContract{}, fmt.Errorf("decode %s: %w", path, err)
	}
	if contract.SchemaVersion != 1 {
		return goldenContract{}, fmt.Errorf("schema_version = %d, want 1", contract.SchemaVersion)
	}
	if contract.IdentityCount <= 0 || contract.PhotoCount <= 0 || contract.ReportCount <= 0 ||
		contract.SceneBriefCount <= 0 || contract.PlanGroupCount <= 0 || contract.RenderMinCount <= 0 ||
		contract.SourceStateFixtureCount <= 0 || contract.FeedbackSequenceCount <= 0 {
		return goldenContract{}, fmt.Errorf("contract counts must all be positive")
	}
	if contract.IdentityCrossSplitAllowed {
		return goldenContract{}, fmt.Errorf("identity_cross_split_allowed must be false")
	}
	want := map[string]int{"development": 60, "validation": 20, "locked": 20}
	if len(contract.Splits) != len(want) {
		return goldenContract{}, fmt.Errorf("splits = %v, want 60/20/20", contract.Splits)
	}
	for split, pct := range want {
		if contract.Splits[split] != pct {
			return goldenContract{}, fmt.Errorf("splits[%s] = %d, want %d", split, contract.Splits[split], pct)
		}
	}
	return contract, nil
}

func loadRelease(path string) (providerai.ReleaseConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return providerai.ReleaseConfig{}, err
	}
	var routing struct {
		Release *providerai.ReleaseConfig `json:"release"`
	}
	if err := json.Unmarshal(data, &routing); err != nil {
		return providerai.ReleaseConfig{}, fmt.Errorf("decode %s: %w", path, err)
	}
	if routing.Release == nil {
		return providerai.ReleaseConfig{}, fmt.Errorf("%s is missing the release block", path)
	}
	if err := providerai.ValidateCandidatePercent(routing.Release.CandidatePercent); err != nil {
		return providerai.ReleaseConfig{}, err
	}
	if strings.TrimSpace(routing.Release.BucketSalt) == "" {
		return providerai.ReleaseConfig{}, fmt.Errorf("release.bucket_salt is required")
	}
	return *routing.Release, nil
}

func loadDataset(root string) (goldenDataset, error) {
	dataset := goldenDataset{
		briefsPath: filepath.Join(root, "planning", "briefs.v1.jsonl"),
		plansPath:  filepath.Join(root, "planning", "plan_sets.v1.jsonl"),
	}
	assessmentRaw, err := os.ReadFile(filepath.Join(root, "assessment", "manifest.json"))
	if err != nil {
		return dataset, err
	}
	if err := json.Unmarshal(assessmentRaw, &dataset.assessment); err != nil {
		return dataset, fmt.Errorf("decode assessment/manifest.json: %w", err)
	}
	if dataset.briefs, err = planning.LoadBriefCasesStrict(dataset.briefsPath); err != nil {
		return dataset, err
	}
	if dataset.plans, err = planning.LoadPlanSetCasesStrict(dataset.plansPath); err != nil {
		return dataset, err
	}
	if dataset.rendering, err = rendering.LoadManifest(filepath.Join(root, "rendering", "manifest.jsonl")); err != nil {
		return dataset, err
	}
	reportRaw, err := os.ReadFile(filepath.Join(root, "rendering", "report.json"))
	if err != nil {
		return dataset, err
	}
	if dataset.renderReport, err = rendering.LoadReport(reportRaw); err != nil {
		return dataset, err
	}
	metricsRaw, err := os.ReadFile(filepath.Join(root, "metrics.json"))
	if err != nil {
		return dataset, err
	}
	if err := json.Unmarshal(metricsRaw, &dataset.metrics); err != nil {
		return dataset, fmt.Errorf("decode metrics.json: %w", err)
	}
	if dataset.sourceStateCount, err = countLines(filepath.Join(root, "fixtures", "source_states.jsonl")); err != nil {
		return dataset, err
	}
	if dataset.feedbackSeqCount, err = countLines(filepath.Join(root, "fixtures", "feedback_sequences.jsonl")); err != nil {
		return dataset, err
	}
	return dataset, nil
}

// validateDataset 执行契约校验:任一不满足返回错误,调用方在评测前退出 2。
func validateDataset(contract goldenContract, dataset goldenDataset, split string) error {
	switch split {
	case "development", "validation", "locked":
	default:
		return fmt.Errorf("dataset-split %q is not development|validation|locked", split)
	}

	// assessment:三张照片角色完整性 + 数量下限 + 分区比例 + 身份不跨分区。
	identities := map[string]string{}
	reports := 0
	assessmentSplits := map[string]int{}
	for _, c := range dataset.assessment.Cases {
		if c.Input.Face == "" || c.Input.Side == "" || c.Input.Body == "" {
			return fmt.Errorf("assessment case %q does not carry the complete face/side/body photo roles", c.ID)
		}
		normalized := normalizeSplit(c.Split)
		if prev, ok := identities[c.IdentityID]; ok && prev != normalized && !contract.IdentityCrossSplitAllowed {
			return fmt.Errorf("assessment identity %s crosses splits (%s, %s)", c.IdentityID, prev, normalized)
		}
		identities[c.IdentityID] = normalized
		assessmentSplits[normalized]++
		if c.Expected.Outcome == assessment.OutcomePublish {
			reports++
		}
	}
	if got := len(identities); got < contract.IdentityCount {
		return fmt.Errorf("assessment identities = %d, want >= %d", got, contract.IdentityCount)
	}
	if got := len(dataset.assessment.Cases) * 3; got < contract.PhotoCount {
		return fmt.Errorf("assessment photos = %d, want >= %d", got, contract.PhotoCount)
	}
	if reports < contract.ReportCount {
		return fmt.Errorf("assessment reports = %d, want >= %d", reports, contract.ReportCount)
	}
	if err := checkSplitShares("assessment", len(dataset.assessment.Cases), assessmentSplits, contract.Splits); err != nil {
		return err
	}

	// planning:brief/plan 数量下限与分区比例。
	if got := len(dataset.briefs); got < contract.SceneBriefCount {
		return fmt.Errorf("scene briefs = %d, want >= %d", got, contract.SceneBriefCount)
	}
	if got := len(dataset.plans); got < contract.PlanGroupCount {
		return fmt.Errorf("plan groups = %d, want >= %d", got, contract.PlanGroupCount)
	}
	briefSplits := map[string]int{}
	for _, c := range dataset.briefs {
		briefSplits[normalizeSplit(string(c.Split))]++
	}
	if err := checkSplitShares("planning briefs", len(dataset.briefs), briefSplits, contract.Splits); err != nil {
		return err
	}
	planSplits := map[string]int{}
	for _, c := range dataset.plans {
		planSplits[normalizeSplit(string(c.Split))]++
	}
	if err := checkSplitShares("planning plan sets", len(dataset.plans), planSplits, contract.Splits); err != nil {
		return err
	}

	// rendering:候选/身份数量、分区比例、授权与 PII 红线(包内校验器)。
	if got := dataset.rendering.CandidateCount(); got < contract.RenderMinCount {
		return fmt.Errorf("render candidates = %d, want >= %d", got, contract.RenderMinCount)
	}
	if got := dataset.rendering.IdentityCount(); got < contract.IdentityCount {
		return fmt.Errorf("render identities = %d, want >= %d", got, contract.IdentityCount)
	}
	renderSplits := map[string]int{}
	for _, c := range dataset.rendering.Cases {
		renderSplits[normalizeSplit(string(c.Split))]++
	}
	if err := checkSplitShares("rendering", len(dataset.rendering.Cases), renderSplits, contract.Splits); err != nil {
		return err
	}
	if err := rendering.ValidateManifest(dataset.rendering); err != nil {
		return err
	}

	// fixtures:来源/状态固件与反馈序列数量下限。
	if dataset.sourceStateCount < contract.SourceStateFixtureCount {
		return fmt.Errorf("source/state fixtures = %d, want >= %d", dataset.sourceStateCount, contract.SourceStateFixtureCount)
	}
	if dataset.feedbackSeqCount < contract.FeedbackSequenceCount {
		return fmt.Errorf("feedback sequences = %d, want >= %d", dataset.feedbackSeqCount, contract.FeedbackSequenceCount)
	}

	// metrics:线束实测指标必须齐全。
	m := dataset.metrics
	if m.PhotoCheckP95MS == nil || m.ReportP95MS == nil || m.PlanTextP95MS == nil || m.LinkRate == nil || m.SingleImageFallbackCount == nil {
		return fmt.Errorf("metrics.json must carry photo_check_p95_ms, report_p95_ms, plan_text_p95_ms, source_operation_invocation_feedback_link_rate and single_image_fallback_count")
	}
	return nil
}

// evaluate 分派到三个 eval 包;任一硬门禁失败返回错误(调用方退出 1)。
func evaluate(dataset goldenDataset, release providerai.ReleaseConfig, split string) (lockedReport, error) {
	summary, err := assessment.Run(context.Background(), dataset.assessment, true)
	if err != nil {
		return lockedReport{}, fmt.Errorf("assessment: %w", err)
	}
	planSummary, err := planning.Run(dataset.briefsPath, dataset.plansPath, "")
	if err != nil {
		return lockedReport{}, fmt.Errorf("planning: %w", err)
	}
	if ok, reason := rendering.DecideRelease(dataset.renderReport, rendering.ProductionThresholds); !ok {
		return lockedReport{}, fmt.Errorf("rendering: stop reason %s", reason)
	}

	report := dataset.renderReport
	m := dataset.metrics
	return lockedReport{
		Schema:       "uplook-locked-release.v1",
		DatasetSplit: split,
		Release:      release,

		PhotoRoleAccuracy:                         rate(summary.RoleChecksCorrect, summary.RoleChecks),
		SourceMismatchCount:                       report.SourceMismatchCount,
		SensitiveInferenceCount:                   summary.SensitiveViolations,
		FindingEvidenceCompleteness:               rate(summary.EvidenceLinked, summary.EvidenceTotal),
		FindingEvidenceSupportRate:                rate(summary.EvidenceSupported, summary.EvidenceTotal),
		PlanGroundingCompleteness:                 planSummary.GroundingCompleteness,
		PlanPairDifferencePassRate:                planSummary.PairDifferencePassRate,
		SingleImageFallbackCount:                  *m.SingleImageFallbackCount,
		SevereBadRenderPublishRate:                rate(report.SevereAccepted, report.SevereFailures),
		IdentityHumanPassRate:                     rate(report.HumanIdentityPasses, report.HumanIdentityTotal),
		RenderSpecMatchRate:                       rate(report.SemanticCompliant, report.SemanticTotal),
		SourceOperationInvocationFeedbackLinkRate: *m.LinkRate,
		WebPPublishCount:                          report.WebPPublicationCount,
		PhotoCheckP95MS:                           *m.PhotoCheckP95MS,
		ReportP95MS:                               *m.ReportP95MS,
		PlanTextP95MS:                             *m.PlanTextP95MS,
		FirstRenderP95MS:                          report.FirstCandidateP95.Milliseconds(),
	}, nil
}

// normalizeSplit 把 assessment/planning 的 release 分区名对齐到契约的 locked。
func normalizeSplit(split string) string {
	if split == "release" {
		return "locked"
	}
	return split
}

// checkSplitShares 校验一个数据集分区的案例分布与契约的 60/20/20 一致。
func checkSplitShares(part string, total int, counts map[string]int, splits map[string]int) error {
	if total == 0 {
		return fmt.Errorf("%s: no cases", part)
	}
	for _, split := range []string{"development", "validation", "locked"} {
		pct, ok := splits[split]
		if !ok {
			return fmt.Errorf("contract splits missing %q", split)
		}
		if counts[split]*100 != total*pct {
			return fmt.Errorf("%s split %s = %d/%d, want %d%%", part, split, counts[split], total, pct)
		}
	}
	return nil
}

func countLines(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count, nil
}

func rate(part, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total)
}
