# Rendering Quality Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 建立从不可变 RenderSpec 到隔离候选、五类质量门禁和 CAS 发布的完整渲染链路，确保每套方案初始只生成一个候选、质量失败时最多补第二个候选，且旧任务永远不能覆盖新发布。

**Architecture:** Rendering 是独立领域：API 创建 RenderRun 和公开 Operation，Worker 通过统一 Task Registry 执行候选生成，Provider Runtime 只按能力元数据选择满足双图身份保持要求的模型。生成字节先归一化为 JPEG 并写入私有 COS 隔离前缀，质量通过后复制到发布前缀，再由同一数据库事务校验 task lease CAS 与 RenderHead generation/version CAS，创建不可变 Publication 并切换公开指针。

**Tech Stack:** Go 1.24、PostgreSQL 16、pgx/v5、腾讯云私有 COS、OpenAPI 3.1、Go `image/jpeg`、`golang.org/x/image/webp`、Node.js 22 契约检查。

## Global Constraints

- 本计划只覆盖 `RenderHead / RenderRun / RenderCandidate / QualityEvaluation / RenderPublication`、RenderSpec 消费、能力路由、隔离存储、质量门禁、API、评测与放量；不实现 Planning、Execution、Feedback 或 Miniapp 页面。
- 执行前必须已有新的 `apps/server/internal/database/migrations/001_baseline.sql`、Operation/Task lease 基础设施、MediaAsset、ProviderInvocation 和 Planning/RenderSpec；若签名与下文依赖契约不同，直接修正前置计划产物，不添加适配层。
- 不兼容旧数据库、旧 API、旧本地缓存或旧任务；不写 `ALTER`、回填、双写、旧 DTO、旧路由别名或 fallback 读取。
- 服务端依赖方向固定为 `httpapi → service → repository / provider / storage`；handler 禁止直查数据库。
- 业务代码不得出现厂商名或模型名；模型选择只通过 `full_look_generation` 和 `render_quality_evaluation` 能力路由。
- `full_look_generation` 的每个可选模型必须支持至少两张输入图、多参考图、身份保持、私有输入和 JPEG 可交付输出；任一硬要求不满足即过滤，不允许单图降级。
- body 图固定是构图与身体比例基准，face 图固定是身份参考；Provider 输入顺序必须为 body、face。
- 图片模型只消费已校验的 RenderSpec，不消费页面文案、方案名、营销文案或旧 `PlanStep` 拼接 Prompt。
- 默认禁止 outpaint；输出比例跟随 body 原图，不固定为 `1024×1536`。
- 每个 RenderRun 初始只允许 Candidate 1；只有质量判定为 `retry` 时才把预算扩为 2 并生成 Candidate 2。
- 每个 RenderRun 最多两个候选、最多一次同能力模型切换；Task 网络重试不得增加候选 ordinal，也不得重新获得一次模型切换额度。
- Provider 输出先归一化为 JPEG；`media_assets.mime_type` 固定 `image/jpeg`，小程序不得收到 WebP。
- 候选只写 `users/{user_id}/render-quarantine/...`，发布对象只写 `users/{user_id}/render-published/...`；两类对象均不可覆盖。
- 质量检查顺序固定为技术、身份、人体、构图、图文；无法确认身份或真实性时失败关闭。
- 人脸比较只在内存短暂执行；数据库和日志不得保存 embedding、完整照片 URL、data URL 或原始图片字节。
- 质量内部得分只写 `quality_evaluations.internal_scores`，不得进入用户 API。
- 用户可见生成图角标固定为“风格参考”；Demo 固定为“效果示例”。不得把生成图角标改成含“AI”的文案。
- `provider_version` 以 `demo` 开头的结果不能进入 RenderCandidate/Publication，只能通过 `/v1/media/demo` 形成 `demo_example + 效果示例`。
- 失败 Candidate 和未采用 Candidate 默认保留 30 天；对象 GC 由基础计划提供的队列处理。
- 越权读写统一返回 404；每次 Repository 查询都必须同时携带 `resource_id + user_id`。
- 公开异步创建响应固定为 `202 {"data":...,"operation":...}`；所有创建请求必须携带 `Idempotency-Key`。
- 本计划不修改 `appearance-coach-prototype/`，不修改冻结的根级 `package.json`、`tsconfig.base.json`、`Makefile` 或 `docker-compose.yml`。

---

## Cross-plan Ownership Override

- 本计划拥有 Rendering/Quality 的 Go Domain、Service、Postgres、Provider、Storage、HTTP 与渲染专用策略配置；它消费 Rule/Contract Freeze 已确定的 OpenAPI，不修改 `contracts/**`。共享 AI routing JSON 的最终合并由 Peripherals/Cutover 独占。
- Foundation 独占并一次性创建最终 `001_baseline.sql` 及共享 `quality_evaluations`；本计划只验证 Rendering 表和约束，不重复建表，不修改 baseline。
- 本计划不得修改或提交 `packages/core/src/types/index.ts`、`packages/core/src/api/endpoints.ts` 或任何 Miniapp 文件；最终 OpenAPI codegen 与客户端切换由 `2026-09-12-miniapp-quality-loop.md` 统一执行。
- 本计划不修改中央 `bootstrap/api.go`、`bootstrap/worker.go` 或 `httpapi/httpapi.go` composition；只提供 Rendering 构造函数、Handler 和独立 wiring test，最终注册由 Peripherals/Cutover 完成。
- 下文公开 API Task 中涉及 `contracts/openapi.yaml` 或旧手写 Core Client 的步骤与提交路径全部跳过，只完成服务端 Handler 和 HTTP 契约测试。
- 统一评测目录为 `apps/server/eval/rendering/`；统一 `apps/server/cmd/eval/main.go` 由最终 Peripherals/Cutover 一次装配，本计划不修改该入口，不创建 `cmd/render-eval` 或 `internal/evaluation`。
- Task Handler 严格使用基础计划的 `Execute/Commit`：`Execute` 可以调用 Provider、写不可公开的 quarantined Candidate 和 staged QualityEvaluation，但不得结束 Task、创建 Candidate 2、失败 Operation 或更新 RenderHead；这些终态副作用全部由 `Commit` 在 lease + generation 双 CAS 事务中完成。
- 本计划不删除旧 `provider/look.go`、旧路由测试或中央 HTTP/Bootstrap 路径；正文 Delete 步骤作为最终 Peripherals/Cutover 清理清单跳过。

## Prerequisite Contracts

实现者先运行以下检查，确认前置计划已经提供这些签名。缺失时停止本计划，先完成对应前置计划；本计划不得在 Rendering 包里复制这些类型。

```go
// apps/server/internal/domain/planning.go
type RenderSpec struct {
	ID               string
	UserID           string
	PlanVariantID    string
	SourcePhotoSetID string
	SchemaVersion    string
	Spec             RenderDirective
	ContentHash      string
	CreatedAt        time.Time
}

type RenderDirective struct {
	Identity    RenderIdentity    `json:"identity"`
	Composition RenderComposition `json:"composition"`
	Hair        RenderHair        `json:"hair"`
	Makeup      RenderMakeup      `json:"makeup"`
	Outfit      RenderOutfit      `json:"outfit"`
	Output      RenderOutput      `json:"output"`
}

type RenderIdentity struct {
	BodyAssetID           string `json:"body_asset_id"`
	FaceAssetID           string `json:"face_asset_id"`
	PreserveIdentity      bool   `json:"preserve_identity"`
	PreserveBodyProportion bool  `json:"preserve_body_proportion"`
	PreserveSkinTone      bool   `json:"preserve_skin_tone"`
	PreserveAgeImpression bool   `json:"preserve_age_impression"`
}

type RenderComposition struct {
	PreservePose       bool `json:"preserve_pose"`
	PreserveBackground bool `json:"preserve_background"`
	PreserveLighting   bool `json:"preserve_lighting"`
	PreserveSourceCrop bool `json:"preserve_source_crop"`
	AllowOutpaint      bool `json:"allow_outpaint"`
}

type RenderOutput struct {
	MIMEType    string `json:"mime_type"`
	AspectPolicy string `json:"aspect_policy"`
	Quality     string `json:"quality"`
}
```

```go
// apps/server/internal/service/taskrunner/ports.go
type Handler interface {
	Type() domain.TaskType
	Execute(context.Context, domain.TaskLease) (domain.TaskResult, error)
	Commit(context.Context, domain.TaskLease, domain.TaskResult) (domain.CommitOutcome, error)
}
```

```go
// apps/server/internal/domain/operation.go
type OperationRef struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
}

type Operation struct {
	ID         string
	UserID     string
	Kind       string
	Status     string
	StageCode  string
	Retryable  bool
	ResultType string
	ResultID   string
}
```

```go
// apps/server/internal/provider/ai/contracts.go
type InvocationMeta struct {
	InvocationID  string
	ModelKey      string
	Protocol      string
	ProviderRequestID string
	InputImages   int
	OutputImages  int
	LatencyMS     int64
	EstimatedCostCNY float64
}

type Failure struct {
	Class string
	Code  string
	Err   error
}

```

前置数据库表必须至少存在：`users`、`media_assets`、`photo_sets`、`photo_set_items`、`plan_variants`、`render_specs`、`operations`、`tasks`、`provider_invocations`。`media_assets` 必须含 `origin / purpose / object_key / sha256 / mime_type / byte_size / width / height / state / display_kind / provider_invocation_id`。

## File Structure

| Path | Responsibility |
|---|---|
| `apps/server/internal/domain/rendering.go` | Rendering 枚举、实体、状态投影和 reason code |
| `apps/server/internal/domain/rendering_test.go` | 纯状态规则和公开投影单测 |
| `apps/server/internal/database/migrations/001_baseline.sql` | 全新 Rendering 表、约束、复合租户外键和索引 |
| `apps/server/internal/service/rendering/ports.go` | Rendering 用例所需最小 Repository、Provider、Storage 端口 |
| `apps/server/internal/service/rendering/service.go` | 创建/查询 RenderRun 和 Worker 编排 |
| `apps/server/internal/service/rendering/policy.go` | 候选预算、reason code 到 retry/reject 的确定性策略 |
| `apps/server/internal/service/rendering/jpeg.go` | Provider 输出解码、像素上限和 JPEG 归一化 |
| `apps/server/internal/service/rendering/handler.go` | `render_candidate_generate` Task Handler |
| `apps/server/internal/service/rendering/*_test.go` | fake port 用例、JPEG 和候选策略单测 |
| `apps/server/internal/repository/postgres/rendering.go` | Rendering 读写、幂等创建和双 CAS 事务 |
| `apps/server/internal/repository/postgres/rendering_integration_test.go` | 真实 PostgreSQL 并发、租户和 CAS 测试 |
| `apps/server/internal/provider/ai/contracts.go` | 图片生成/质量评估请求响应和硬需求 |
| `apps/server/internal/provider/ai/router.go` | 能力过滤、平衡选择和一次切换 |
| `apps/server/internal/provider/ai/image.go` | RenderSpec 到协议无关图片编辑请求 |
| `apps/server/internal/provider/ai/quality.go` | 身份/人体/构图/图文结构化评估 |
| `apps/server/internal/provider/ai/*_test.go` | Router 和 Provider contract tests |
| `apps/server/internal/storage/render_objects.go` | 隔离 key、不可覆盖写入和发布复制 |
| `apps/server/internal/storage/render_objects_test.go` | 前缀、覆盖保护和 promote 清理测试 |
| `apps/server/internal/httpapi/renders.go` | 创建/查询 RenderRun 的协议翻译 |
| `apps/server/internal/httpapi/renders_test.go` | 202/200/404/幂等键 HTTP 契约测试 |
| `apps/server/internal/httpapi/httpapi.go` | 注册新路由并删除旧生成路由 |
| `apps/server/internal/service/planning/ports.go` | 通过窄只读接口读取各 variant 当前 Publication |
| `apps/server/internal/service/planning/service.go` | 在 PlanSet read model 中投影每套 render 状态 |
| `apps/server/internal/service/planning/service_test.go` | ready_partial 和来源文案投影测试 |
| `contracts/openapi.yaml` | Render API、状态、来源和错误契约 |
| `packages/core/src/types/index.ts` | Render DTO 类型 |
| `packages/core/src/api/endpoints.ts` | 新 Render API 客户端，删除旧 look regenerate |
| `packages/core/src/copy/zh.ts` | “风格参考”“效果示例”单源 |
| `packages/core/tests/copy.test.mjs` | 角标文案回归测试 |
| `apps/server/internal/config/ai_routing.go` | 模型能力元数据和生产硬校验 |
| `apps/server/config/ai-routing.example.json` | 示例能力路由 |
| `apps/server/config/ai-routing.production.json` | 生产能力路由，删除单图 fallback |
| `apps/server/internal/bootstrap/worker.go` | 构造 Rendering Service 并注册 Handler |
| `apps/server/internal/bootstrap/api.go` | 向 HTTP API 注入 Rendering 查询/创建用例 |
| `apps/server/cmd/eval/main.go` | 统一评测入口，新增 rendering 子命令 |
| `apps/server/eval/rendering/*.go` | 金集清单校验、指标聚合和发布门禁 |
| `apps/server/eval/rendering/*_test.go` | 身份分区、阈值、停止条件单测 |
| `apps/server/eval/rendering/testdata/passing-report.json` | 不含用户数据的 release decision 合格 fixture |
| `apps/server/config/render-quality-policy.v1.json` | Gate 阈值和 reason code 策略 |
| `apps/server/config/render-rollout.production.json` | shadow、5%、25%、50%、100% 放量状态机 |
| `apps/server/scripts/e2e.sh` | Render 主链和公开响应回归 |
| `apps/server/internal/provider/look.go` | 删除旧 `LookInput/LookGenerator/DemoLookGenerator` 路径 |
| `apps/server/internal/provider/look_test.go` | 删除旧 Prompt 和 Demo pass-through 测试 |
| `apps/server/internal/config/full_look_route_test.go` | 删除，能力元数据测试替代 |

### Task 1: Define Rendering Domain and Fresh Baseline Schema

**Files:**
- Create: `apps/server/internal/domain/rendering.go`
- Create: `apps/server/internal/domain/rendering_test.go`
- Verify: `apps/server/internal/database/migrations/001_baseline.sql`

**Interfaces:**
- Consumes: `domain.RenderSpec`、`media_assets`、`plan_variants`、`operations`、`provider_invocations`
- Produces: `domain.RenderRun`、`domain.RenderCandidate`、`domain.QualityEvaluation`、`domain.RenderPublication`、`domain.RenderRunView`

- [ ] **Step 1: Write failing domain tests for valid states, candidate limits, labels, and private fields**

```go
func TestRenderRunViewUsesProductCopyAndHidesInternalScores(t *testing.T) {
	view := NewRenderRunView(RenderRun{
		ID: "run-1", PlanVariantID: "variant-1", Generation: 3,
		Outcome: RenderOutcomePublished,
	}, &RenderPublication{
		ID: "publication-1", AssetID: "asset-1",
		URL: "https://signed.example/render.jpg",
	}, Operation{ID: "operation-1", Kind: "render", Status: "succeeded"})

	if view.Render.State != RenderStateReady {
		t.Fatalf("state = %q", view.Render.State)
	}
	if view.Render.Media.SourceKind != SourceKindGeneratedPreview {
		t.Fatalf("source_kind = %q", view.Render.Media.SourceKind)
	}
	if view.Render.Media.DisplayLabel != "风格参考" {
		t.Fatalf("display_label = %q", view.Render.Media.DisplayLabel)
	}
	encoded, _ := json.Marshal(view)
	for _, forbidden := range []string{"internal_scores", "provider", "model", "reason_codes"} {
		if bytes.Contains(encoded, []byte(forbidden)) {
			t.Fatalf("public view leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestCanRequestCandidateOnlyExpandsAfterQualityRetry(t *testing.T) {
	run := RenderRun{CandidateLimit: 1}
	if !CanRequestCandidate(run, 1, nil) {
		t.Fatal("candidate 1 must be allowed")
	}
	retry := &QualityEvaluation{Decision: QualityDecisionRetry}
	if CanRequestCandidate(run, 2, retry) {
		t.Fatal("candidate 2 must wait for the persisted budget expansion")
	}
	run.CandidateLimit = 2
	if !CanRequestCandidate(run, 2, retry) {
		t.Fatal("quality retry must allow candidate 2")
	}
	reject := &QualityEvaluation{Decision: QualityDecisionReject}
	if CanRequestCandidate(run, 2, reject) || CanRequestCandidate(run, 3, retry) {
		t.Fatal("reject or ordinal 3 must not create another candidate")
	}
}
```

- [ ] **Step 2: Run the domain tests and verify the new package surface is absent**

Run: `cd apps/server && go test ./internal/domain -run 'TestRenderRunView|TestCanRequestCandidate' -count=1`

Expected: FAIL with undefined Rendering types/functions.

- [ ] **Step 3: Implement the complete domain contract**

```go
package domain

import (
	"encoding/json"
	"time"
)

const (
	RenderOutcomePublished  = "published"
	RenderOutcomeUnavailable = "unavailable"
	RenderOutcomeFailed     = "failed"
	RenderOutcomeSuperseded = "superseded"

	QualityDecisionPass   = "pass"
	QualityDecisionRetry  = "retry"
	QualityDecisionReject = "reject"
	QualityDecisionError  = "error"

	QualitySubjectRenderCandidate = "render_candidate"

	RenderStateQueued      = "queued"
	RenderStateGenerating  = "generating"
	RenderStateChecking    = "checking"
	RenderStateReady       = "ready"
	RenderStateFailed      = "failed"
	RenderStateUnavailable = "unavailable"

	SourceKindGeneratedPreview = "generated_preview"
	SourceKindDemoExample      = "demo_example"
	DisplayLabelStyleReference = "风格参考"
	DisplayLabelEffectExample  = "效果示例"
)

type RenderHead struct {
	UserID               string
	PlanVariantID        string
	Generation           int
	CurrentPublicationID string
	Version              int64
}

type RenderRun struct {
	ID                   string
	UserID               string
	PlanVariantID        string
	RenderSpecID         string
	Generation           int
	OperationID          string
	CandidateLimit       int
	RoutingPolicyVersion string
	QualityPolicyVersion string
	Outcome              string
	CreatedAt            time.Time
	FinishedAt           *time.Time
}

type RenderCandidate struct {
	ID                   string
	UserID               string
	RenderRunID          string
	Ordinal              int
	AssetID              string
	ProviderInvocationID string
	CreatedAt            time.Time
}

type RenderPublication struct {
	ID                  string
	UserID              string
	PlanVariantID       string
	RenderRunID         string
	CandidateID         string
	QualityEvaluationID string
	AssetID             string
	Generation          int
	ObjectKey           string
	URL                 string
	URLExpiresAt        time.Time
	CreatedAt           time.Time
}

type RenderMediaView struct {
	AssetID      string    `json:"asset_id"`
	URL          string    `json:"url"`
	URLExpiresAt time.Time `json:"url_expires_at"`
	SourceKind   string    `json:"source_kind"`
	DisplayLabel string   `json:"display_label"`
}

type RenderStatusView struct {
	State       string           `json:"state"`
	Retryable   bool             `json:"retryable"`
	OperationID string           `json:"operation_id"`
	Media       *RenderMediaView `json:"media,omitempty"`
}

type RenderRunView struct {
	ID            string           `json:"id"`
	PlanVariantID string           `json:"plan_variant_id"`
	Generation    int              `json:"generation"`
	Render        RenderStatusView `json:"render"`
}

func CanRequestCandidate(run RenderRun, ordinal int, previous *QualityEvaluation) bool {
	if ordinal == 1 {
		return run.CandidateLimit == 1
	}
	return ordinal == 2 && run.CandidateLimit == 2 &&
		previous != nil && previous.Decision == QualityDecisionRetry
}

func NewRenderRunView(run RenderRun, publication *RenderPublication, operation Operation) RenderRunView
func NewDemoMediaView(assetID, signedURL string, expiresAt time.Time) RenderMediaView
```

`NewRenderRunView` 必须只按领域 outcome/当前阶段投影公开状态；Publication 存在时固定返回 `generated_preview + 风格参考`。Demo 投影使用独立 `NewDemoMediaView`，固定返回 `demo_example + 效果示例`，不得复用生成图函数。

- [ ] **Step 4: Add only fresh-create DDL to the baseline migration**

在 `001_baseline.sql` 的 Planning 表之后加入完整建表语句；不得读取或迁移旧 `plans.generated_image_url`：

```sql
CREATE TABLE render_heads (
  user_id uuid NOT NULL,
  plan_variant_id uuid NOT NULL,
  generation integer NOT NULL DEFAULT 0 CHECK (generation >= 0),
  current_publication_id uuid,
  version bigint NOT NULL DEFAULT 0 CHECK (version >= 0),
  PRIMARY KEY (user_id, plan_variant_id),
  FOREIGN KEY (user_id, plan_variant_id)
    REFERENCES plan_variants(user_id, id) ON DELETE CASCADE
);

CREATE TABLE render_runs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL,
  plan_variant_id uuid NOT NULL,
  render_spec_id uuid NOT NULL,
  generation integer NOT NULL CHECK (generation > 0),
  operation_id uuid NOT NULL,
  candidate_limit smallint NOT NULL DEFAULT 1 CHECK (candidate_limit IN (1, 2)),
  routing_policy_version text NOT NULL,
  quality_policy_version text NOT NULL,
  outcome text CHECK (outcome IN ('published','unavailable','failed','superseded')),
  created_at timestamptz NOT NULL DEFAULT now(),
  finished_at timestamptz,
  UNIQUE (user_id, id),
  UNIQUE (user_id, plan_variant_id, generation),
  UNIQUE (user_id, plan_variant_id, generation, id),
  FOREIGN KEY (user_id, plan_variant_id)
    REFERENCES plan_variants(user_id, id),
  FOREIGN KEY (user_id, render_spec_id)
    REFERENCES render_specs(user_id, id),
  FOREIGN KEY (user_id, operation_id)
    REFERENCES operations(user_id, id)
);

CREATE TABLE render_candidates (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL,
  render_run_id uuid NOT NULL,
  ordinal smallint NOT NULL CHECK (ordinal IN (1, 2)),
  asset_id uuid NOT NULL,
  provider_invocation_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, render_run_id, ordinal),
  UNIQUE (user_id, render_run_id, id),
  UNIQUE (user_id, asset_id),
  FOREIGN KEY (user_id, render_run_id)
    REFERENCES render_runs(user_id, id) ON DELETE CASCADE,
  FOREIGN KEY (user_id, asset_id)
    REFERENCES media_assets(user_id, id),
  FOREIGN KEY (user_id, provider_invocation_id)
    REFERENCES provider_invocations(user_id, id)
);

CREATE TABLE quality_evaluations (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL,
  subject_type text NOT NULL CHECK (subject_type IN ('report','plan_set','render_candidate')),
  subject_id uuid NOT NULL,
  render_candidate_subject_id uuid GENERATED ALWAYS AS (
    CASE WHEN subject_type='render_candidate' THEN subject_id END
  ) STORED,
  policy_version text NOT NULL,
  decision text NOT NULL CHECK (decision IN ('pass','retry','reject','error')),
  reason_codes text[] NOT NULL DEFAULT '{}',
  internal_scores jsonb NOT NULL DEFAULT '{}',
  evaluator_invocation_id uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  FOREIGN KEY (user_id, evaluator_invocation_id)
    REFERENCES provider_invocations(user_id, id),
  FOREIGN KEY (user_id, render_candidate_subject_id)
    REFERENCES render_candidates(user_id, id)
);

CREATE TABLE render_publications (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL,
  plan_variant_id uuid NOT NULL,
  render_run_id uuid NOT NULL,
  candidate_id uuid NOT NULL,
  quality_evaluation_id uuid NOT NULL,
  generation integer NOT NULL CHECK (generation > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, plan_variant_id, id),
  UNIQUE (user_id, render_run_id),
  UNIQUE (user_id, candidate_id),
  FOREIGN KEY (user_id, plan_variant_id, generation, render_run_id)
    REFERENCES render_runs(user_id, plan_variant_id, generation, id),
  FOREIGN KEY (user_id, render_run_id, candidate_id)
    REFERENCES render_candidates(user_id, render_run_id, id),
  FOREIGN KEY (user_id, quality_evaluation_id)
    REFERENCES quality_evaluations(user_id, id)
);

ALTER TABLE render_heads
  ADD CONSTRAINT render_heads_current_publication_fk
  FOREIGN KEY (user_id, plan_variant_id, current_publication_id)
  REFERENCES render_publications(user_id, plan_variant_id, id);

CREATE INDEX render_runs_variant_created_idx
  ON render_runs(user_id, plan_variant_id, created_at DESC);
CREATE INDEX render_candidates_run_idx
  ON render_candidates(user_id, render_run_id, ordinal);
CREATE INDEX quality_evaluations_subject_idx
  ON quality_evaluations(user_id, subject_type, subject_id, created_at DESC);
```

- [ ] **Step 5: Run domain and baseline migration tests**

Run: `cd apps/server && go test ./internal/domain ./internal/database -count=1`

Expected: PASS; baseline migration creates all five Rendering tables on an empty database and contains no reference to old rendering columns.

- [ ] **Step 6: Commit the domain/schema boundary**

```bash
git add apps/server/internal/domain/rendering.go \
  apps/server/internal/domain/rendering_test.go \
  apps/server/internal/database/migrations/001_baseline.sql
git commit -m "feat(server): define rendering quality domain"
```

### Task 2: Create RenderRun Idempotently and Read Immutable Inputs

**Files:**
- Create: `apps/server/internal/service/rendering/ports.go`
- Create: `apps/server/internal/service/rendering/service.go`
- Create: `apps/server/internal/service/rendering/service_test.go`
- Create: `apps/server/internal/repository/postgres/rendering.go`
- Create: `apps/server/internal/repository/postgres/rendering_integration_test.go`

**Interfaces:**
- Consumes: `RenderSpecReader.GetForVariant(ctx,userID,planVariantID)`、Operation/Task transaction support
- Produces: `Service.StartRun`、`Service.GetRun`、`Repository.CreateRun`

- [ ] **Step 1: Write failing service tests for idempotency, generation, RenderSpec validation, and tenant isolation**

```go
func TestStartRunUsesValidatedRenderSpecAndOneInitialCandidate(t *testing.T) {
	repo := newRepoFake()
	repo.spec = validRenderSpec("user-1", "variant-1")
	svc := New(repo, nil, nil, nil, Config{
		RoutingPolicyVersion: "render-route-v1",
		QualityPolicyVersion: "render-quality-v1",
	})

	got, err := svc.StartRun(context.Background(), StartRunCommand{
		UserID: "user-1", PlanVariantID: "variant-1", IdempotencyKey: "idem-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Run.CandidateLimit != 1 || got.Run.Generation != 1 {
		t.Fatalf("unexpected run: %#v", got.Run)
	}
	if repo.enqueued.Ordinal != 1 || repo.enqueued.SubjectGeneration != 1 {
		t.Fatalf("unexpected initial task: %#v", repo.enqueued)
	}
}

func TestStartRunReusesSameIdempotencyKey(t *testing.T) {
	repo := newRepoFake()
	repo.spec = validRenderSpec("user-1", "variant-1")
	svc := New(repo, nil, nil, nil, testConfig())
	first, _ := svc.StartRun(context.Background(), StartRunCommand{
		UserID: "user-1", PlanVariantID: "variant-1", IdempotencyKey: "same",
	})
	second, _ := svc.StartRun(context.Background(), StartRunCommand{
		UserID: "user-1", PlanVariantID: "variant-1", IdempotencyKey: "same",
	})
	if first.Run.ID != second.Run.ID || repo.createCalls != 1 {
		t.Fatalf("idempotency failed: first=%s second=%s calls=%d",
			first.Run.ID, second.Run.ID, repo.createCalls)
	}
}
```

Rendering 使用 Foundation `domain.QualityEvaluation`，不得在 `rendering.go` 重复声明。

- [ ] **Step 2: Run the service tests and verify they fail before ports exist**

Run: `cd apps/server && go test ./internal/service/rendering -run 'TestStartRun' -count=1`

Expected: FAIL because package/signatures are absent.

- [ ] **Step 3: Define narrow ports and exact commands**

```go
package rendering

type Repository interface {
	GetRenderSpecForVariant(context.Context, string, string) (domain.RenderSpec, error)
	CreateRun(context.Context, CreateRunCommand) (CreateRunResult, error)
	GetRun(context.Context, string, string) (domain.RenderRun, *domain.RenderPublication, domain.Operation, error)
	ListCurrentByVariantIDs(context.Context, string, []string) (map[string]CurrentRender, error)
	GetCandidateJob(context.Context, string, string, int) (CandidateJob, error)
	RecordCandidate(context.Context, RecordCandidateCommand) (domain.RenderCandidate, error)
	CommitEvaluation(context.Context, CommitEvaluationCommand) (CommitEvaluationResult, error)
}

type ImageGenerator interface {
	Generate(context.Context, providerai.GenerationRequest) (providerai.GenerationResult, error)
}

type QualityGate interface {
	Evaluate(context.Context, QualityInput) (QualityResult, error)
}

type RenderObjectStore interface {
	PutCandidate(context.Context, CandidateObjectInput) (StoredObject, error)
	Promote(context.Context, PromoteObjectInput) (StoredObject, error)
	Delete(context.Context, string) error
	Open(context.Context, string) (io.ReadCloser, error)
	SignedURL(context.Context, string, time.Duration) (string, error)
}

type JPEGNormalizer interface {
	Normalize([]byte, string) (NormalizedJPEG, error)
}

type StartRunCommand struct {
	UserID          string
	PlanVariantID   string
	IdempotencyKey  string
}

type StartRunResult struct {
	Run       domain.RenderRun
	Operation domain.OperationRef
}

type Config struct {
	RoutingPolicyVersion string
	QualityPolicyVersion string
	AssetURLTTL           time.Duration
}

func New(
	repo Repository,
	generator ImageGenerator,
	normalizer JPEGNormalizer,
	objects RenderObjectStore,
	config Config,
) *Service

type CreateRunCommand struct {
	UserID               string
	PlanVariantID        string
	RenderSpecID         string
	IdempotencyKey       string
	RoutingPolicyVersion string
	QualityPolicyVersion string
}

type CreateRunResult struct {
	Run       domain.RenderRun
	Operation domain.OperationRef
	Created   bool
}

type CurrentRender struct {
	Run         domain.RenderRun
	Publication *domain.RenderPublication
	Operation   domain.Operation
}
```

`StartRun` 必须先校验 RenderSpec：`identity.body_asset_id` 和 `face_asset_id` 非空且不同，所有 preserve 开关为 true，`allow_outpaint=false`，`output.mime_type=image/jpeg`，`aspect_policy=preserve_body_source`。校验失败返回 `validation_error`，不得创建 Operation、Run 或 Task。

`GetRun` 从 Repository 同时读取 Run、当前 Publication 和 Operation；ready 时仅对 Publication 的 published object key 签发 15 分钟 URL，并设置 `URLExpiresAt=clock.Now()+15m`。状态映射固定为：published→ready、unavailable→unavailable、failed→failed、Operation stage `render.generating`→generating、`render.checking`→checking，其余非终态→queued；`retryable` 只取 Operation 字段。

```go
func (s *Service) GetRun(ctx context.Context, userID, runID string) (domain.RenderRunView, error)
func (s *Service) ListCurrentByVariantIDs(
	ctx context.Context,
	userID string,
	variantIDs []string,
) (map[string]domain.RenderRunView, error)
```

批量读取方法必须验证返回集合仅含请求 IDs、为每个 ready Publication 签发独立短期 URL，并保持输入中无 RenderHead 的 variant 不出现在 map 中。

- [ ] **Step 4: Implement the idempotent PostgreSQL creation transaction**

`CreateRun` 在单一事务中按以下顺序执行：

1. 使用基础计划的 `(user_id, idempotency_key, request_hash)` API 创建或读取 Operation。
2. `SELECT ... FOR UPDATE` RenderHead；不存在则插入 generation 0/version 0。
3. 若 Operation 已有关联 Run，直接读取并返回 `Created=false`。
4. 将 generation 加一并把旧的未终态 Run 标为 `superseded`。
5. 插入 CandidateLimit=1 的 RenderRun。
6. 插入 `render_candidate_generate` Task，payload 固定 `{"render_run_id","ordinal":1}`，`subject_generation` 等于新 generation。
7. Operation subject 指向新 Run，提交。

Repository 暴露以下任务 payload，禁止把 RenderSpec JSON 复制进 Task：

```go
type GenerateCandidatePayload struct {
	RenderRunID string `json:"render_run_id"`
	Ordinal     int    `json:"ordinal"`
}
```

- [ ] **Step 5: Add PostgreSQL integration tests**

测试必须使用仓库 PostgreSQL 16 测试 DSN，覆盖：

逐个测试的固定 Given/When/Then：

- `TestCreateRunSameKeyReturnsSameRunAndOneTask`：同一 user/variant/key 并发调用两次；断言 Run ID、Operation ID 相同且 Task 数量为 1。
- `TestCreateRunDifferentKeyIncrementsGeneration`：同一 user/variant 使用两个 key；断言 generation 依次为 1、2。
- `TestGetRunRequiresMatchingUserID`：用另一 user 读取已有 Run；断言 `repository.ErrNotFound`。
- `TestCreateRunRejectsVariantSpecFromAnotherUser`：variant 属于 user A、spec 属于 user B；断言复合外键拒绝且无 Operation/Task。
- `TestCreateRunSupersedesOlderUnfinishedRun`：generation 1 为 running 后创建 generation 2；断言旧 Run 和 Operation 均为 superseded。

并发测试同时启动两个 goroutine 使用相同 key；预期一个 `Created=true`、一个 `Created=false`，数据库仅一条 RenderRun 和一条 Candidate 1 Task。

- [ ] **Step 6: Run service and repository tests**

Run: `cd apps/server && TEST_DATABASE_URL='postgres://jianwo:jianwo@127.0.0.1:55432/jianwo?sslmode=disable' go test ./internal/service/rendering ./internal/repository/postgres -run 'Render|CreateRun|GetRun' -count=1`

Expected: PASS;重复 key 无重复 Task，跨用户读写返回 `repository.ErrNotFound`。

- [ ] **Step 7: Commit run creation/read boundary**

```bash
git add apps/server/internal/service/rendering/ports.go \
  apps/server/internal/service/rendering/service.go \
  apps/server/internal/service/rendering/service_test.go \
  apps/server/internal/repository/postgres/rendering.go \
  apps/server/internal/repository/postgres/rendering_integration_test.go
git commit -m "feat(server): create immutable render runs"
```

### Task 3: Enforce Multi-Image Capability Routing

**Files:**
- Modify: `apps/server/internal/config/ai_routing.go`
- Create: `apps/server/internal/config/render_routing_test.go`
- Modify: `apps/server/internal/provider/ai/contracts.go`
- Create: `apps/server/internal/provider/ai/router.go`
- Create: `apps/server/internal/provider/ai/router_test.go`
- Modify: `apps/server/config/ai-routing.example.json`
- Modify: `apps/server/config/ai-routing.production.json`
- Delete: `apps/server/internal/config/full_look_route_test.go`

**Interfaces:**
- Consumes: routing JSON and ProviderInvocation history
- Produces: `Router.Generate` and `Router.Evaluate` with hard requirement filtering and one-switch state

- [ ] **Step 1: Write failing config tests for hard metadata and fail-closed routing**

```go
func TestFullLookRouteRejectsSingleImageOrWeakIdentityModel(t *testing.T) {
	cfg := validRouting()
	model := cfg.Models[cfg.Routes["full_look_generation"].Primary]
	model.MaxInputImages = 1
	cfg.Models[cfg.Routes["full_look_generation"].Primary] = model
	if err := validateAIRouting(cfg); err == nil ||
		!strings.Contains(err.Error(), "requires at least 2 input images") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProductionRenderingRoutesHaveEquivalentFallbacks(t *testing.T) {
	cfg := readProductionRouting(t)
	for _, capability := range []string{
		"full_look_generation", "render_quality_evaluation",
	} {
		route := cfg.Routes[capability]
		for _, key := range append([]string{route.Primary}, route.Fallbacks...) {
			model := cfg.Models[key]
			minImages := 2
			if capability == "render_quality_evaluation" {
				minImages = 3
			}
			if model.MaxInputImages < minImages ||
				!model.SupportsMultipleReferenceImages ||
				(capability == "full_look_generation" && !model.IdentityPreservation) ||
				(capability == "render_quality_evaluation" && !model.IdentityComparison) ||
				model.DataRetention != "zero" {
				t.Fatalf("%s route contains ineligible model %s: %#v",
					capability, key, model)
			}
		}
	}
}
```

- [ ] **Step 2: Run routing tests to verify old `full_look_edit` metadata fails**

Run: `cd apps/server && go test ./internal/config ./internal/provider/ai -run 'FullLook|Route' -count=1`

Expected: FAIL because capability metadata and new Router are absent.

- [ ] **Step 3: Add exact model metadata and route requirements**

```go
type AIModelConfig struct {
	Vendor                          string         `json:"vendor"`
	Protocol                        string         `json:"protocol"`
	Model                           string         `json:"model"`
	BaseURL                         string         `json:"base_url"`
	APIKeyEnv                       string         `json:"api_key_env"`
	InputModalities                 []string       `json:"input_modalities"`
	MaxInputImages                  int            `json:"max_input_images"`
	SupportsMultipleReferenceImages bool           `json:"supports_multiple_reference_images"`
	IdentityPreservation            bool           `json:"identity_preservation"`
	IdentityComparison              bool           `json:"identity_comparison"`
	OutputMIMETypes                 []string       `json:"output_mime_types"`
	DataRetention                   string         `json:"data_retention"`
	Parameters                      map[string]any `json:"parameters,omitempty"`
	TimeoutSeconds                  int            `json:"timeout_seconds"`
	InputImageCost                  float64        `json:"input_image_cost,omitempty"`
	OutputImageCost                 float64        `json:"output_image_cost,omitempty"`
}

type AIRoutingConfig struct {
	Version string                   `json:"version"`
	Models  map[string]AIModelConfig `json:"models"`
	Routes  map[string]AIRouteConfig `json:"routes"`
}

type RouteRequirements struct {
	MinInputImages       int      `json:"min_input_images"`
	MultipleReferences  bool     `json:"multiple_references"`
	IdentityPreservation bool    `json:"identity_preservation"`
	IdentityComparison  bool     `json:"identity_comparison"`
	OutputMIMETypes     []string `json:"output_mime_types"`
	DataRetention       string   `json:"data_retention"`
}

type AIRouteConfig struct {
	Policy       string            `json:"policy"`
	Primary      string            `json:"primary"`
	Fallbacks    []string          `json:"fallbacks,omitempty"`
	MaxSwitches  int               `json:"max_switches"`
	MaxCostCNY   float64           `json:"max_cost_cny"`
	Requirements RouteRequirements `json:"requirements"`
}
```

固定校验：

- 路由文件根级 `version` 必填；Rendering 生产 route 的 `policy` 固定为 `balanced`。
- `full_look_generation`: `min_input_images=2`、`multiple_references=true`、`identity_preservation=true`、`output_mime_types=["image/jpeg"]`、`data_retention="zero"`、`max_switches=1`。
- `render_quality_evaluation`: `min_input_images=3`（candidate、body、face）、`multiple_references=true`、`identity_comparison=true`、零保留、`max_switches=1`。
- 生产配置不允许 `provider_version`/model key 以 `demo` 开头。
- 删除 `full_look_edit` route；不得保留别名。
- 删除当前只发送 `Images[0]` 的单图协议模型作为 full-look fallback。

- [ ] **Step 4: Define route state and typed Router API**

```go
type Capability string

const (
	CapabilityFullLookGeneration      Capability = "full_look_generation"
	CapabilityRenderQualityEvaluation Capability = "render_quality_evaluation"
)

type RouteState struct {
	AttemptedModelKeys []string
	SwitchesUsed       int
}

type ImageInput struct {
	AssetID string
	Role    string
	MIMEType string
	Width   int
	Height  int
	Data    []byte
}

type GenerationRequest struct {
	RenderRunID      string
	Ordinal          int
	Spec             domain.RenderDirective
	Body             ImageInput
	Face             ImageInput
	RetryReasonCodes []string
	RouteState       RouteState
}

type GenerationResult struct {
	Data           []byte
	MIMEType       string
	Meta           InvocationMeta
	NextRouteState RouteState
}

type Failure struct {
	Class      string
	Code       string
	RouteState RouteState
	Err        error
}

type ImageGenerator interface {
	Generate(context.Context, GenerationRequest) (GenerationResult, error)
}
```

Router 规则：

1. 先按 requirements 过滤，不满足任一硬条件的模型不进入候选。
2. `AttemptedModelKeys` 为空时调用 primary；只在 typed technical failure 后切一次 eligible fallback。
3. `SwitchesUsed==1` 的 Task 重试只重试最后一个 model key，不回到 primary、不选择第三个模型。
4. quality rejection 不在 Router 内重试；它由 Rendering Service 创建 Candidate 2。
5. 无 eligible model 返回 `Failure{Class:"permanent",Code:"capability_unavailable"}`。
6. 401/403/不支持参数/合同错误分类 `permanent`；408/429/5xx/网络中断分类 `transient` 或 `throttled`；不按错误字符串搜索。
7. 每次实际 HTTP 调用无论成功失败都先落一条 ProviderInvocation；request hash、能力、route version、model key、输入图数量、耗时、成本和 typed error 必填，图片字节/URL/embedding 禁止记录。

- [ ] **Step 5: Add Router unit tests for one switch across Task retries**

```go
func TestRouterNeverSwitchesMoreThanOnceAcrossRetries(t *testing.T) {
	router := newRouterWithModels("primary", "fallback", "third")
	_, firstErr := router.Generate(ctx, validGenerationRequest(RouteState{}))
	first := failureState(t, firstErr)
	if first.SwitchesUsed != 1 ||
		!reflect.DeepEqual(first.AttemptedModelKeys, []string{"primary", "fallback"}) {
		t.Fatalf("first state = %#v", first)
	}

	router.resetCalls()
	_, _ = router.Generate(ctx, validGenerationRequest(first))
	if !reflect.DeepEqual(router.calledModels(), []string{"fallback"}) {
		t.Fatalf("retry switched again: %v", router.calledModels())
	}
}

```

同一测试文件再加入三个表驱动 case：单图 fallback 的 call count 必须为 0；所有模型被过滤时 error code 必须为 `capability_unavailable`；quality rejection 后调用模型集合必须只含 primary。

- [ ] **Step 6: Run config and Router tests**

Run: `cd apps/server && go test ./internal/config ./internal/provider/ai -run 'Routing|Router|FullLook|Quality' -count=1`

Expected: PASS;production route 中不存在任何单图 full-look 候选，技术重试调用序列不超过两个不同 model key。

- [ ] **Step 7: Commit capability routing boundary**

```bash
git add apps/server/internal/config/ai_routing.go \
  apps/server/internal/config/render_routing_test.go \
  apps/server/internal/provider/ai/contracts.go \
  apps/server/internal/provider/ai/router.go \
  apps/server/internal/provider/ai/router_test.go \
  apps/server/config/ai-routing.example.json \
  apps/server/config/ai-routing.production.json
git rm apps/server/internal/config/full_look_route_test.go
git commit -m "feat(server): enforce equivalent rendering routes"
```

### Task 4: Consume RenderSpec in Provider Generation Contracts

**Files:**
- Create: `apps/server/internal/provider/ai/image.go`
- Create: `apps/server/internal/provider/ai/image_contract_test.go`
- Modify: `apps/server/internal/provider/ai/protocols/openai_image.go`
- Modify: `apps/server/internal/provider/ai/protocols/dashscope_image.go`
- Modify: `apps/server/internal/provider/ai/protocols/ark_image.go`

**Interfaces:**
- Consumes: `providerai.GenerationRequest`
- Produces: protocol requests with body/face order, source aspect ratio, RenderSpec-only prompt, typed failures

- [ ] **Step 1: Write failing Provider contract tests with `httptest.Server`**

```go
func TestFullLookContractSendsBodyThenFaceAndConsumesSpec(t *testing.T) {
	var captured ProtocolImageRequest
	server := captureImageServer(t, &captured, jpegFixture(t, 1200, 1800))
	generator := newContractGenerator(t, server)

	req := validGenerationRequest(RouteState{})
	req.Body = ImageInput{AssetID: "body-1", MIMEType: "image/jpeg",
		Width: 1200, Height: 1800, Data: []byte("body-bytes")}
	req.Face = ImageInput{AssetID: "face-1", MIMEType: "image/jpeg",
		Width: 1000, Height: 1000, Data: []byte("face-bytes")}
	result, err := generator.Generate(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	if captured.Images[0].Role != "body" || captured.Images[1].Role != "face" {
		t.Fatalf("image order = %#v", captured.Images)
	}
	if captured.AspectRatio != "2:3" {
		t.Fatalf("aspect ratio = %q", captured.AspectRatio)
	}
	for _, required := range []string{
		`"preserve_identity":true`,
		`"preserve_body_proportion":true`,
		`"allow_outpaint":false`,
		`"aspect_policy":"preserve_body_source"`,
	} {
		if !strings.Contains(captured.Prompt, required) {
			t.Fatalf("prompt misses %s: %s", required, captured.Prompt)
		}
	}
	for _, forbidden := range []string{"方案名", "plan_name", "1024x1536", "补齐脚部"} {
		if strings.Contains(captured.Prompt, forbidden) {
			t.Fatalf("prompt leaked forbidden value %q", forbidden)
		}
	}
	if result.Meta.InputImages != 2 {
		t.Fatalf("input image count = %d", result.Meta.InputImages)
	}
}
```

- [ ] **Step 2: Add second-candidate feedback and protocol error classification tests**

使用一个表驱动 contract test 覆盖以下精确输入/预期：

- ordinal=2、reasons=`["identity_drift"]`：请求包含该 code，且不包含 Provider 原始文本。
- face 为空：HTTP call count=0，error code=`invalid_image_count`。
- HTTP 429：class=`throttled`。
- HTTP 503：class=`transient`。
- HTTP 400 且 code=`unsupported_parameter`：class=`permanent`。
- 请求成功和失败日志：不包含 base64、`data:image/`、body/face 原始字节。

Candidate 2 的 Prompt 只允许加入以下稳定 reason codes，不拼接 Provider 原始错误：

```go
var AllowedRetryReasons = map[string]bool{
	"identity_drift": true,
	"anatomy_head": true,
	"anatomy_torso": true,
	"anatomy_arms": true,
	"anatomy_legs": true,
	"anatomy_hands": true,
	"composition_head_crop": true,
	"composition_pose_changed": true,
	"composition_background_changed": true,
	"semantic_hair_mismatch": true,
	"semantic_makeup_mismatch": true,
	"semantic_outfit_mismatch": true,
	"unrequested_skin_tone_change": true,
	"unrequested_age_change": true,
	"unrequested_body_change": true,
}
```

- [ ] **Step 3: Run Provider contract tests and verify failure**

Run: `cd apps/server && go test ./internal/provider/ai -run 'FullLookContract|CandidateTwo|Protocol' -count=1`

Expected: FAIL because RenderSpec-only generation adapter is absent.

- [ ] **Step 4: Implement deterministic RenderSpec serialization**

`image.go` 必须：

- 先调用 `validateGenerationRequest`，精确要求两张且 asset ID 不同。
- 使用 `json.Marshal(req.Spec)` 作为指令主体；不读取 Plan 或页面文案。
- 将 Candidate 2 reason code 排序、去重后放入 `corrections` JSON。
- 从 body width/height 计算约分后的比例；不得写固定输出尺寸。
- 对协议仅传 body、face 两张图；不得追加 side 图或内置图。
- 在 Provider 返回时检查 `InputImages==2`、`OutputImages==1`。

```go
type ProtocolImageRequest struct {
	Capability  Capability
	Images      []ProtocolImage
	Prompt      string
	AspectRatio string
	OutputMIME  string
}

type ProtocolImage struct {
	Role     string
	MIMEType string
	Data     []byte
}

func buildProtocolRequest(req GenerationRequest) (ProtocolImageRequest, error)
```

- [ ] **Step 5: Implement each protocol adapter against the same contract**

OpenAI、DashScope 和 Ark adapter 必须各自有 contract subtest，验证：

- 多图字段实际包含两张图。
- body 始终在前。
- 不发送只对文生图有效、但图片编辑无效的参数。
- `watermark=false`。
- 返回 URL 只能是 HTTPS 公网地址；下载上限 20 MiB。
- 结果 MIME 只接受 JPEG/PNG/WebP，后续由 normalizer 转 JPEG。

- [ ] **Step 6: Run all Provider tests**

Run: `cd apps/server && go test ./internal/provider/ai/... -count=1`

Expected: PASS;三个协议共享相同语义合同，单图请求在发 HTTP 前失败。

- [ ] **Step 7: Commit Provider generation contracts**

```bash
git add apps/server/internal/provider/ai/image.go \
  apps/server/internal/provider/ai/image_contract_test.go \
  apps/server/internal/provider/ai/protocols/openai_image.go \
  apps/server/internal/provider/ai/protocols/dashscope_image.go \
  apps/server/internal/provider/ai/protocols/ark_image.go
git commit -m "feat(server): generate looks from render specs"
```

### Task 5: Normalize JPEG and Isolate Candidate Objects

**Files:**
- Create: `apps/server/internal/service/rendering/jpeg.go`
- Create: `apps/server/internal/service/rendering/jpeg_test.go`
- Create: `apps/server/internal/storage/render_objects.go`
- Create: `apps/server/internal/storage/render_objects_test.go`
- Modify: `apps/server/go.mod`
- Modify: `apps/server/go.sum`

**Interfaces:**
- Consumes: Provider JPEG/PNG/WebP bytes and base ObjectStorage
- Produces: `NormalizedJPEG` and immutable quarantine/published object operations

- [ ] **Step 1: Write failing JPEG normalization tests**

```go
func TestNormalizeAlwaysReturnsDecodableJPEGWithoutMetadata(t *testing.T) {
	for _, fixture := range []struct {
		name string
		mime string
		data []byte
	}{
		{"jpeg", "image/jpeg", jpegFixture(t, 800, 1200)},
		{"png", "image/png", pngFixture(t, 800, 1200)},
		{"webp", "image/webp", webpFixture(t, 800, 1200)},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			got, err := NewJPEGNormalizer().Normalize(fixture.data, fixture.mime)
			if err != nil {
				t.Fatal(err)
			}
			if got.MIMEType != "image/jpeg" ||
				!bytes.HasPrefix(got.Data, []byte{0xff, 0xd8, 0xff}) {
				t.Fatalf("not JPEG: mime=%s prefix=%x", got.MIMEType, got.Data[:3])
			}
			if got.Width != 800 || got.Height != 1200 ||
				got.SHA256 == "" || got.ByteSize != int64(len(got.Data)) {
				t.Fatalf("metadata mismatch: %#v", got)
			}
		})
	}
}

```

同一测试文件加入表驱动拒绝用例：DecodeConfig 声明 40,000,001 像素时返回 `image_too_large`；PNG 魔数配 `image/jpeg` 时返回 `mime_mismatch`；空字节和 animated WebP 分别返回 `image_empty`、`animated_image_unsupported`。

- [ ] **Step 2: Run JPEG tests and verify failure**

Run: `cd apps/server && go test ./internal/service/rendering -run 'Normalize' -count=1`

Expected: FAIL because JPEG normalizer is absent.

- [ ] **Step 3: Add WebP decoder and implement normalization**

Run: `cd apps/server && go get golang.org/x/image@v0.30.0`

Expected: `go.mod` contains direct `golang.org/x/image v0.30.0`; `go.sum` is updated.

```go
type NormalizedJPEG struct {
	Data     []byte
	MIMEType string
	SHA256   string
	ByteSize int64
	Width    int
	Height   int
}

type DecoderConfig struct {
	MaxInputBytes int64
	MaxPixels     int64
	JPEGQuality   int
}

func NewJPEGNormalizer() *Decoder {
	return &Decoder{config: DecoderConfig{
		MaxInputBytes: 20 << 20,
		MaxPixels: 40_000_000,
		JPEGQuality: 92,
	}}
}

func (d *Decoder) Normalize(data []byte, declaredMIME string) (NormalizedJPEG, error)
```

实现顺序固定：限制输入 20 MiB → 魔数比对 MIME → `image.DecodeConfig` → 拒绝宽高小于 1 或像素超过 40,000,000 → 完整 decode → 在新 buffer 以质量 92 编码 JPEG → 重新 DecodeConfig 验证 → 计算 SHA-256。重新编码会移除 EXIF/XMP/ICC 和 Provider 元数据。

- [ ] **Step 4: Write failing isolated storage tests**

```go
func TestRenderObjectStoreSeparatesQuarantineAndPublishedPrefixes(t *testing.T) {
	base := newMemoryObjectStore()
	store := NewRenderObjectStore(base)
	candidate, err := store.PutCandidate(ctx, CandidateObjectInput{
		UserID: "user-1", RunID: "run-1", CandidateID: "candidate-1",
		Data: []byte("jpeg"), SHA256: "abc",
	})
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Key != "users/user-1/render-quarantine/run-1/candidate-1.jpg" {
		t.Fatalf("candidate key = %q", candidate.Key)
	}
	published, err := store.Promote(ctx, PromoteObjectInput{
		UserID: "user-1", PublicationID: "publication-1",
		SourceKey: candidate.Key, ExpectedSHA256: "abc",
	})
	if err != nil {
		t.Fatal(err)
	}
	if published.Key != "users/user-1/render-published/publication-1.jpg" {
		t.Fatalf("published key = %q", published.Key)
	}
}

```

同一测试文件加入三个完整场景：第二次写同一 Candidate key 返回 `object_exists`；user B promote user A key 返回 `cross_user_object`；copy 后读回 hash 不同返回 `object_hash_mismatch` 并删除目标 published key。

- [ ] **Step 5: Implement immutable candidate/promote storage**

```go
type CandidateObjectInput struct {
	UserID      string
	RunID       string
	CandidateID string
	Data        []byte
	SHA256      string
}

type PromoteObjectInput struct {
	UserID        string
	PublicationID string
	SourceKey     string
	ExpectedSHA256 string
}

type StoredObject struct {
	Key      string
	SHA256   string
	ByteSize int64
}
```

`PutCandidate` 和 `Promote` 必须采用 create-only 语义。Local adapter 使用 `O_CREATE|O_EXCL`；COS adapter 使用条件写/复制并拒绝目标已存在。Promote 先复制、再读回计算 SHA-256；数据库 CAS 失败时由调用者删除 published key，quarantine key 留给 30 天 GC。

- [ ] **Step 6: Run normalization and storage tests**

Run: `cd apps/server && go test ./internal/service/rendering ./internal/storage -run 'Normalize|RenderObject|Promote' -count=1`

Expected: PASS;所有成功输出 MIME 为 `image/jpeg`，隔离与发布 key 永不混用。

- [ ] **Step 7: Commit JPEG/storage boundary**

```bash
git add apps/server/internal/service/rendering/jpeg.go \
  apps/server/internal/service/rendering/jpeg_test.go \
  apps/server/internal/storage/render_objects.go \
  apps/server/internal/storage/render_objects_test.go \
  apps/server/go.mod apps/server/go.sum
git commit -m "feat(server): quarantine normalized jpeg candidates"
```

### Task 6: Implement Ordered Technical, Identity, Anatomy, Composition, and Semantic Gates

**Files:**
- Create: `apps/server/internal/service/rendering/policy.go`
- Create: `apps/server/internal/service/rendering/policy_test.go`
- Create: `apps/server/internal/provider/ai/quality.go`
- Create: `apps/server/internal/provider/ai/quality_contract_test.go`
- Create: `apps/server/config/render-quality-policy.v1.json`

**Interfaces:**
- Consumes: normalized candidate JPEG, body/face references, RenderSpec
- Produces: one immutable `QualityEvaluation` decision with stable reason codes and internal-only scores

- [ ] **Step 1: Write failing Gate policy tests for strict order and fail-closed behavior**

```go
func TestGateChainStopsAtFirstFailedStage(t *testing.T) {
	evaluator := &qualityFake{
		result: providerai.QualityResult{
			Technical: providerai.StageResult{Pass: true, Confidence: .99},
			Identity: providerai.StageResult{Pass: false, Confidence: .96,
				ReasonCodes: []string{"identity_drift"}},
			Anatomy: providerai.StageResult{Pass: true, Confidence: .99},
		},
	}
	chain := NewQualityGate(evaluator, loadTestPolicy(t))
	got, err := chain.Evaluate(ctx, validQualityInput())
	if err != nil {
		t.Fatal(err)
	}
	if got.Decision != domain.QualityDecisionRetry ||
		!reflect.DeepEqual(got.ReasonCodes, []string{"identity_drift"}) {
		t.Fatalf("unexpected decision: %#v", got)
	}
	if got.CompletedStages != 2 { // technical, identity
		t.Fatalf("completed stages = %d", got.CompletedStages)
	}
}

```

补充三个表驱动场景：identity confidence 缺失时 Candidate 1=`retry/identity_unknown`；ordinal=2 的同一失败=`reject`；将结果编码为 `RenderRunView` 后 JSON 不含 `InternalScores` 中的任意 key。

- [ ] **Step 2: Write Provider quality contract tests**

质量 Provider 请求固定三张图：

1. candidate JPEG；
2. body source；
3. face reference。

Provider contract 采用表驱动请求断言：成功请求图片 role 必须严格为 candidate/body/face 且含完整 RenderSpec；缺 face 返回 `invalid_image_count` 且不发 HTTP；未知 reason code 返回 `quality_schema_invalid`；confidence=-0.01 或 1.01 均拒绝；响应若含 `embedding` 字段因 strict schema 失败，账本和日志均无 embedding。

结构化结果类型：

```go
type QualityRequest struct {
	RenderRunID string
	Candidate   ImageInput
	Body        ImageInput
	Face        ImageInput
	Spec        domain.RenderDirective
}

type StageResult struct {
	Pass        bool
	Confidence  float64
	ReasonCodes []string
}

type QualityResult struct {
	Technical   StageResult
	Identity    StageResult
	Anatomy     StageResult
	Composition StageResult
	Semantic    StageResult
	Meta        InvocationMeta
}
```

- [ ] **Step 3: Run Gate tests and verify failure**

Run: `cd apps/server && go test ./internal/service/rendering ./internal/provider/ai -run 'Gate|QualityContract' -count=1`

Expected: FAIL because Gate chain and quality adapter are absent.

- [ ] **Step 4: Create exact v1 policy**

```json
{
  "version": "render-quality-v1",
  "technical": {
    "mime_type": "image/jpeg",
    "max_bytes": 20971520,
    "max_pixels": 40000000,
    "min_short_edge": 512,
    "min_long_edge": 1024,
    "require_single_person": true,
    "allow_watermark": false,
    "allow_text": false
  },
  "identity": {
    "minimum_confidence": 0.82,
    "unknown_decision": "retry"
  },
  "anatomy": {
    "minimum_confidence": 0.90,
    "parts": ["head", "torso", "arms", "legs", "hands"]
  },
  "composition": {
    "minimum_confidence": 0.90,
    "require_uncropped_head": true
  },
  "semantic": {
    "minimum_confidence": 0.85,
    "dimensions": ["hair", "makeup", "outfit", "unrequested_changes"]
  }
}
```

Technical Gate 本地检查 JPEG 魔数、可解码、尺寸和大小；单人、无水印、无文字由质量视觉结果中的 technical 字段确认。质量 Provider 无结果、拒答、Schema 错误或低于 stage 最低置信度时失败关闭。

- [ ] **Step 5: Implement stable decision policy**

```go
type QualityInput struct {
	Run       domain.RenderRun
	Candidate domain.RenderCandidate
	Image     NormalizedJPEG
	Body      providerai.ImageInput
	Face      providerai.ImageInput
	Spec      domain.RenderDirective
}

type QualityResult struct {
	Decision              string
	ReasonCodes           []string
	InternalScores        json.RawMessage
	EvaluatorInvocationID string
	CompletedStages       int
}
```

判定规则：

- 任一技术格式/解码/尺寸失败：Candidate 1 为 `retry`，Candidate 2 为 `reject`。
- 身份、人体、构图、图文任一失败：Candidate 1 为 `retry`，Candidate 2 为 `reject`。
- Provider 技术错误：返回 typed error 给 Task runner，不创建质量拒绝，不消耗第二候选。
- 所有 stage pass：`pass`。
- `internal_scores` 只包含各 stage confidence 和 boolean；不得包含 embedding、URL 或图片内容。
- reason codes 排序、去重；未知 code 使整个 evaluation 为 `error` 并失败关闭。

- [ ] **Step 6: Run Gate and Provider contract tests**

Run: `cd apps/server && go test ./internal/service/rendering ./internal/provider/ai -run 'Gate|Quality' -count=1`

Expected: PASS;stage 顺序固定，Candidate 2 不可能返回可创建 Candidate 3 的决策。

- [ ] **Step 7: Commit quality Gate boundary**

```bash
git add apps/server/internal/service/rendering/policy.go \
  apps/server/internal/service/rendering/policy_test.go \
  apps/server/internal/provider/ai/quality.go \
  apps/server/internal/provider/ai/quality_contract_test.go \
  apps/server/config/render-quality-policy.v1.json
git commit -m "feat(server): gate render candidate quality"
```

### Task 7: Orchestrate Candidate 1 and At Most Candidate 2

**Files:**
- Create: `apps/server/internal/service/rendering/handler.go`
- Create: `apps/server/internal/service/rendering/handler_test.go`
- Modify: `apps/server/internal/service/rendering/service.go`
- Modify: `apps/server/internal/service/rendering/ports.go`

**Interfaces:**
- Consumes: `taskrunner.Claim`、ImageGenerator、JPEGNormalizer、RenderObjectStore、QualityGate、Rendering Repository
- Produces: registered `render_candidate_generate` Handler and deterministic retry/terminal outcomes

- [ ] **Step 1: Write failing orchestration tests**

使用同一 fake harness 的七个场景，输入/预期固定为：

- Candidate 1 pass：一个 Publication，第二任务数 0。
- Candidate 1 `identity_drift`：无 Publication，仅一条 ordinal 2 Task。
- Candidate 2 再失败：Run=`failed`，PlanVariant/PlanStep 行不变。
- 第一次 Generate=`transient`、第二次 pass：两次 request ordinal 都为 1。
- Router=`capability_unavailable`：Run=`unavailable`，公开 media=nil。
- Generator request：只含 Spec/body/face，不含 Plan 名称或页面 copy。
- Candidate 2 request：reasons 与第一条 Evaluation 排序去重后的 codes 完全相同。

关键断言：

```go
if repo.enqueuedCount != 1 || repo.lastEnqueued.Ordinal != 2 {
	t.Fatalf("quality failure must enqueue exactly candidate 2: %#v", repo)
}
if generator.requests[1].Ordinal != 2 ||
	!reflect.DeepEqual(generator.requests[1].RetryReasonCodes, []string{"identity_drift"}) {
	t.Fatalf("candidate 2 correction input = %#v", generator.requests[1])
}
if repo.run.CandidateLimit != 2 {
	t.Fatalf("candidate budget was not expanded exactly once")
}
```

- [ ] **Step 2: Run handler tests and verify failure**

Run: `cd apps/server && go test ./internal/service/rendering -run 'Candidate|TechnicalRetry|UnavailableRoute' -count=1`

Expected: FAIL because Handler orchestration is absent.

- [ ] **Step 3: Define exact candidate job and commit commands**

```go
type CandidateJob struct {
	Run             domain.RenderRun
	Spec            domain.RenderSpec
	Body            providerai.ImageInput
	Face            providerai.ImageInput
	Previous        *domain.QualityEvaluation
	RouteState      providerai.RouteState
}

type GenerateCandidatePayload struct {
	RenderRunID string                `json:"render_run_id"`
	Ordinal     int                   `json:"ordinal"`
	RouteState  providerai.RouteState `json:"route_state"`
}

type CandidateAsset struct {
	ObjectKey string
	SHA256    string
	MIMEType  string
	ByteSize  int64
	Width     int
	Height    int
}

type RecordCandidateCommand struct {
	TaskID              string
	LeaseToken          string
	UserID              string
	RenderRunID         string
	SubjectGeneration   int
	Ordinal             int
	Asset               CandidateAsset
	ProviderInvocationID string
}

type TechnicalFailureCommand struct {
	TaskID            string
	LeaseToken        string
	UserID            string
	RenderRunID       string
	SubjectGeneration int
	Ordinal           int
	ErrorClass        string
	ErrorCode         string
	RouteState        providerai.RouteState
}

type CommitEvaluationCommand struct {
	TaskID             string
	LeaseToken         string
	UserID             string
	RenderRunID        string
	SubjectGeneration  int
	CandidateID        string
	Evaluation         QualityResult
	PublishedObject    *StoredObject
	PublicationID      string
}

type CommitEvaluationResult struct {
	Outcome       string
	Publication  *domain.RenderPublication
	EnqueuedTaskID string
}
```

- [ ] **Step 4: Implement Handler flow in exact order**

```text
GetCandidateJob(user_id, run_id, ordinal)
→ validate claim.subject_generation == run.generation
→ generator.Generate(spec, body, face, previous reason codes, persisted route state)
→ JPEGNormalizer.Normalize
→ PutCandidate(quarantine)
→ StageCandidate(task lease check + generation check; not publicly readable)
→ QualityGate.Evaluate
→ if pass: pre-create publication ID, Promote to published prefix
→ return TaskResult with publish | enqueue_next | domain_failed disposition
→ taskrunner invokes Handler.Commit
→ CommitEvaluation(task lease CAS + generation/version CAS)
→ if commit CAS fails: delete only the just-created published object
```

异常处理：

- typed `transient/throttled`：返回 typed error，由 Task runner 把同一 Task 更新为 retry_wait；ordinal 不变。
- `capability_unavailable`：Execute 返回 `TaskDomainFail`；Commit 原子设置 run=`unavailable`、operation=`failed`、task=`failed`，API retryable=false。
- Candidate 1 `retry`：Execute 返回 `TaskEnqueueNext`；Commit 原子保存 evaluation、扩展 candidate_limit、完成当前 task并只入队 Candidate 2。
- Candidate 2 `reject`：Execute 返回 `TaskDomainFail`；Commit 原子保存 evaluation、设置 run/operation failed 并完成 task；文字方案不变。
- generation 已过期：run/task=`superseded`，不调用 Provider 或立即丢弃结果，不发布。
- Demo ProviderVersion：按 permanent contract violation 失败，绝不写 candidate。

- [ ] **Step 5: Prove Task retries do not consume candidate/model budgets**

测试安排第一次 Generate 返回 `transient`，第二次用相同 claim ordinal 成功；断言：

- 数据库只有 ordinal=1。
- `candidate_limit` 仍为 1。
- RouteState 在第一次失败后持久化，第二次只调用已经切换到的 fallback。
- Candidate 2 只由 quality `retry` 创建。

- [ ] **Step 6: Run the complete service suite**

Run: `cd apps/server && go test ./internal/service/rendering -count=1`

Expected: PASS;所有候选路径终止于 publication、failed、unavailable 或 superseded，永远没有 ordinal 3。

- [ ] **Step 7: Commit orchestration boundary**

```bash
git add apps/server/internal/service/rendering/handler.go \
  apps/server/internal/service/rendering/handler_test.go \
  apps/server/internal/service/rendering/service.go \
  apps/server/internal/service/rendering/ports.go
git commit -m "feat(server): orchestrate bounded render candidates"
```

### Task 8: Publish with Task Lease CAS and RenderHead Generation CAS

**Files:**
- Modify: `apps/server/internal/repository/postgres/rendering.go`
- Modify: `apps/server/internal/repository/postgres/rendering_integration_test.go`

**Interfaces:**
- Consumes: `RecordCandidateCommand`、`CommitEvaluationCommand`
- Produces: atomic candidate persistence, second-candidate enqueue, and publication

- [ ] **Step 1: Write failing concurrent integration tests**

集成测试逐项安排：

- 过期 lease token：`CommitEvaluation` 返回 `taskrunner.ErrLeaseLost`，五张相关表均无新增/变更。
- 旧 generation：按下述五步竞态执行并断言 Head 保持 generation 2。
- pass：Asset published、Evaluation、Publication、Head、Run、Operation、Task 七项在一次 commit 后同时可见。
- Candidate 1 retry：candidate_limit 从 1 变 2，且只入队一个 ordinal 2。
- 两 goroutine 同时 retry：一个创建 Task，另一个读取同一 dedupe row，总数仍为 1。
- user B 使用 user A candidate/publication ID：返回 `repository.ErrNotFound`。

旧任务覆盖测试必须按真实竞态顺序执行：

1. 创建 generation 1 并 claim task A。
2. 创建 generation 2 并 claim task B。
3. B 通过并发布。
4. A 晚到并尝试发布。
5. 断言 RenderHead 仍指向 generation 2 Publication；A 的 run/task 为 superseded；generation 1 published object 被调用层清理。

- [ ] **Step 2: Run CAS tests and verify old write path fails**

Run: `cd apps/server && TEST_DATABASE_URL='postgres://jianwo:jianwo@127.0.0.1:55432/jianwo?sslmode=disable' go test ./internal/repository/postgres -run 'Publish|QualityRetry|OldGeneration|ExpiredLease' -count=1`

Expected: FAIL until dual CAS transaction exists.

- [ ] **Step 3: Implement candidate record lease CAS**

事务第一条更新必须锁定 lease：

```sql
UPDATE tasks
SET heartbeat_at = now()
WHERE id = $1::uuid
  AND user_id = $2::uuid
  AND status = 'leased'
  AND lease_token = $3::uuid
  AND lease_expires_at > now()
  AND subject_generation = $4
RETURNING id;
```

无返回行映射 `taskrunner.ErrLeaseLost`。随后再次读取 RenderHead；generation 不同映射 `rendering.ErrSuperseded`。只有两项都通过才插入 quarantined MediaAsset 和 RenderCandidate。

- [ ] **Step 4: Implement atomic pass publication**

`CommitEvaluation` 的 pass 分支必须在一个事务中完成：

1. 使用上面的 task lease CAS。
2. `SELECT generation,version FROM render_heads WHERE user_id=$user AND plan_variant_id=$variant FOR UPDATE`。
3. 插入 QualityEvaluation。
4. 把 Candidate Asset 从 `quarantined` 改为 `published`，同时将 object_key 改为已 promote 的 immutable published key、MIME 固定 JPEG。
5. 插入 RenderPublication。
6. 执行 Head CAS：

```sql
UPDATE render_heads
SET current_publication_id = $publication_id,
    version = version + 1
WHERE user_id = $user_id
  AND plan_variant_id = $plan_variant_id
  AND generation = $generation
  AND version = $expected_version;
```

7. 更新 RenderRun=`published`、Operation=`succeeded/result_type=render_publication`、Task=`succeeded`。
8. 任一步影响行数不是 1，整个事务 rollback。

- [ ] **Step 5: Implement atomic retry/reject/superseded branches**

retry 分支：

```sql
UPDATE render_runs
SET candidate_limit = 2
WHERE id=$run_id AND user_id=$user_id
  AND generation=$generation AND candidate_limit=1;
```

随后插入 dedupe key 固定为 `render:{run_id}:candidate:2` 的 Task。唯一约束保证并发只创建一条。

reject 分支保存 evaluation 后终结 Run/Operation；superseded 分支不改 Head，不创建 Publication，不把 Asset 标为 published。

- [ ] **Step 6: Run PostgreSQL race tests repeatedly**

Run: `cd apps/server && TEST_DATABASE_URL='postgres://jianwo:jianwo@127.0.0.1:55432/jianwo?sslmode=disable' go test -race ./internal/repository/postgres -run 'Publish|QualityRetry|OldGeneration|ExpiredLease' -count=20`

Expected: PASS 20 次；旧 generation 覆盖次数为 0，Candidate 2 重复任务为 0。

- [ ] **Step 7: Commit publication transaction boundary**

```bash
git add apps/server/internal/repository/postgres/rendering.go \
  apps/server/internal/repository/postgres/rendering_integration_test.go
git commit -m "feat(server): publish renders with dual cas"
```

### Task 9: Expose the New Render API and Exact Product Labels

**Files:**
- Create: `apps/server/internal/httpapi/renders.go`
- Create: `apps/server/internal/httpapi/renders_test.go`
- Modify: `apps/server/internal/httpapi/httpapi.go`
- Modify: `apps/server/internal/service/planning/ports.go`
- Modify: `apps/server/internal/service/planning/service.go`
- Modify: `apps/server/internal/service/planning/service_test.go`
- Modify: `contracts/openapi.yaml`
- Modify: `packages/core/src/types/index.ts`
- Modify: `packages/core/src/api/endpoints.ts`
- Modify: `packages/core/src/copy/zh.ts`
- Modify: `packages/core/tests/copy.test.mjs`

**Interfaces:**
- Consumes: `rendering.Service.StartRun`、`rendering.Service.GetRun`
- Produces: `POST /v1/plan-variants/{id}/render-runs`、`GET /v1/render-runs/{id}`、PlanSet 中每套方案的当前 render 投影

- [ ] **Step 1: Write failing HTTP tests**

```go
func TestCreateRenderRunReturns202DataAndOperation(t *testing.T) {
	req := authenticatedRequest(http.MethodPost,
		"/v1/plan-variants/variant-1/render-runs", nil)
	req.Header.Set("Idempotency-Key", "render-variant-1-v1")
	res := serve(t, req, renderServiceStub{start: startResult()})

	if res.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s", res.Code, res.Body.String())
	}
	assertJSONEqual(t, res.Body.Bytes(), `{
	  "data":{
	    "id":"run-1",
	    "plan_variant_id":"variant-1",
	    "generation":1,
	    "render":{
	      "state":"queued",
	      "retryable":true,
	      "operation_id":"operation-1"
	    }
	  },
	  "operation":{"id":"operation-1","kind":"render","status":"accepted"}
	}`)
}

```

在同一 HTTP harness 加入四个表驱动 case：空 Idempotency-Key 返回 400；跨用户 service error 映射 404；ready body 精确包含 `generated_preview/风格参考`；将响应 JSON 转成 map 后断言 provider/model/internal_scores/reason_codes 四个 key 递归不存在。

Planning read-model 测试再覆盖：三套 variant 分别为 ready/generating/failed 时，PlanSet state=`ready_partial`，每套 render 使用对应 Operation ID；没有 Publication 的 failed variant 不得出现 media。

- [ ] **Step 2: Run HTTP tests and verify routes are absent**

Run: `cd apps/server && go test ./internal/httpapi -run 'RenderRun|ReadyRender' -count=1`

Expected: FAIL with 404/undefined handlers.

- [ ] **Step 3: Implement handlers with no business logic**

```go
type RenderingService interface {
	StartRun(context.Context, rendering.StartRunCommand) (rendering.StartRunResult, error)
	GetRun(context.Context, string, string) (domain.RenderRunView, error)
}

func (a *API) createRenderRun(w http.ResponseWriter, r *http.Request)
func (a *API) getRenderRun(w http.ResponseWriter, r *http.Request)
```

`createRenderRun` 只读取 current user、path id、`Idempotency-Key`，调用 Service 并写 202。空 key 返回 `400 validation_error`。`getRenderRun` 只调用 Service 并写 200。不得读取 Repository。

Planning 只能依赖窄 Reader：

```go
type CurrentRenderReader interface {
	ListCurrentByVariantIDs(
		context.Context,
		string,
		[]string,
	) (map[string]domain.RenderRunView, error)
}
```

`GetPlanSet` 在原事务外批量读取一次当前 RenderHead/Publication，不逐套 N+1 查询；只合并 view，不写 Rendering 表。PlanSet state 映射：全部 ready→ready，至少一个 ready 且其余为 generating/failed/unavailable→ready_partial，全部终态失败→failed，其余→rendering。

- [ ] **Step 4: Replace old OpenAPI rendering surface**

删除：

```text
POST /v1/plans/{id}/look/regenerate
```

新增：

```text
POST /v1/plan-variants/{id}/render-runs
GET  /v1/render-runs/{id}
```

OpenAPI schema 必须枚举：

```yaml
RenderState:
  type: string
  enum: [queued, generating, checking, ready, failed, unavailable]
RenderSourceKind:
  type: string
  enum: [generated_preview, demo_example]
RenderMedia:
  type: object
  additionalProperties: false
  required: [asset_id, url, url_expires_at, source_kind, display_label]
  properties:
    asset_id: { type: string, format: uuid }
    url: { type: string, format: uri }
    url_expires_at: { type: string, format: date-time }
    source_kind: { $ref: '#/components/schemas/RenderSourceKind' }
    display_label:
      type: string
      enum: [风格参考, 效果示例]
```

API 不定义 provider/model/internal_scores/reason_codes 字段。

现有 `PlanVariant` schema 必须新增 required `render`，复用 `RenderStatusView`；`PlanSet.state` 枚举固定为 `planning|rendering|ready|ready_partial|failed`。这只是当前 Publication 的读投影，不把 RenderRun/Candidate 内部字段复制进 PlanSet。

- [ ] **Step 5: Update core types/client and copy tests**

```ts
export interface RenderMedia {
  asset_id: string
  url: string
  url_expires_at: string
  source_kind: 'generated_preview' | 'demo_example'
  display_label: '风格参考' | '效果示例'
}

export interface RenderRun {
  id: string
  plan_variant_id: string
  generation: number
  render: {
    state: 'queued' | 'generating' | 'checking' | 'ready' | 'failed' | 'unavailable'
    retryable: boolean
    operation_id: string
    media?: RenderMedia
  }
}
```

`API_PATHS` 使用：

```ts
renderRunCreate: 'POST /v1/plan-variants/{id}/render-runs',
renderRun: 'GET /v1/render-runs/{id}',
```

`copy/zh.ts` 固定：

```ts
export const MEDIA_LABEL = {
  generatedPreview: '风格参考',
  bundledReference: '风格参考',
  demoExample: '效果示例',
} as const
```

回归测试必须断言生成图文案恰好等于“风格参考”，Demo 恰好等于“效果示例”。

- [ ] **Step 6: Run HTTP, core and OpenAPI checks**

Run: `cd apps/server && go test ./internal/httpapi -run 'Render' -count=1`

Expected: PASS.

Run: `pnpm --filter @zsm/core typecheck && pnpm --filter @zsm/core test`

Expected: PASS.

Run: `node contracts/scripts/check-sync.mjs`

Expected: `契约与 core 端点完全一致`.

Run: `npx --yes @redocly/cli@2 lint --config contracts/redocly.yaml contracts/openapi.yaml`

Expected: exit 0 with no OpenAPI errors.

- [ ] **Step 7: Commit API/copy boundary**

```bash
git add apps/server/internal/httpapi/renders.go \
  apps/server/internal/httpapi/renders_test.go \
  apps/server/internal/httpapi/httpapi.go \
  apps/server/internal/service/planning/ports.go \
  apps/server/internal/service/planning/service.go \
  apps/server/internal/service/planning/service_test.go \
  contracts/openapi.yaml \
  packages/core/src/types/index.ts \
  packages/core/src/api/endpoints.ts \
  packages/core/src/copy/zh.ts \
  packages/core/tests/copy.test.mjs
git commit -m "feat(api): expose quality-gated render runs"
```

### Task 10: Wire Worker/API and Remove the Legacy Look Path

**Files:**
- Modify: `apps/server/internal/bootstrap/worker.go`
- Modify: `apps/server/internal/bootstrap/api.go`
- Modify: `apps/server/cmd/worker/main.go`
- Modify: `apps/server/cmd/api/main.go`
- Delete: `apps/server/internal/provider/look.go`
- Delete: `apps/server/internal/provider/look_test.go`
- Remove legacy render methods from: `apps/server/internal/service/plans.go`
- Remove legacy render methods from: `apps/server/internal/service/tasks.go`
- Remove legacy render methods from: `apps/server/internal/repository/repository.go`
- Remove legacy render methods from: `apps/server/internal/repository/postgres/postgres.go`

**Interfaces:**
- Consumes: Task Registry、Rendering Service、AI Router、RenderObjectStore
- Produces: API without embedded Worker in production and one registered render task type

- [ ] **Step 1: Write failing bootstrap tests**

Bootstrap 测试固定断言：Registry 中 `render_candidate_generate` 数量恰为 1；API bundle 不持有 Runner/Handler；生产配置缺 `render_quality_evaluation` 时 Build 返回 `required capability route missing`；依赖图类型名和 route key 中均不存在 Demo render generator。

- [ ] **Step 2: Run bootstrap tests and verify legacy wiring remains**

Run: `cd apps/server && go test ./internal/bootstrap ./cmd/... -run 'Render|Worker|Demo' -count=1`

Expected: FAIL because current wiring still injects `LookGenerator` and API can embed Worker.

- [ ] **Step 3: Build Rendering dependencies in worker bootstrap**

`worker.go` 必须构造：

```go
router := providerai.NewRouter(aiConfig, invocationRecorder, httpClient, logger)
objects := storage.NewRenderObjectStore(baseObjects)
gate := rendering.NewQualityGate(router, qualityPolicy)
renderService := rendering.New(
	renderRepo,
	router,
	rendering.NewJPEGNormalizer(),
	objects,
	rendering.Config{
		RoutingPolicyVersion: aiConfig.Version,
		QualityPolicyVersion: qualityPolicy.Version,
	},
)
registry.Register(renderService.Handler())
```

API bootstrap 只构造 Rendering create/read Service，不注册 Handler、不启动 worker goroutine。生产 `cmd/api` 删除 `RUN_WORKER` 分支。

- [ ] **Step 4: Delete the old rendering implementation, not alias it**

删除：

- `LookInput`、`LookOutput`、`LookGenerator`、`RoutedLookGenerator`、`DemoLookGenerator`。
- `processPlanLook`、`planLookTaskHandler`、`TaskTypePlanLook`。
- `ApplyPlanLookResult`、`GetPlanLookJob`、`generated_image_url` 投影。
- `/v1/plans/{id}/look/regenerate` handler/service/client。
- `full_look_edit` capability constant。

不得保留把旧 Plan 转成 RenderSpec 的运行时适配；RenderSpec 只能由 Planning 编译器产生。

- [ ] **Step 5: Add repository-wide absence checks**

Run:

```bash
rg -n "DemoLookGenerator|LookGenerator|LookInput|ApplyPlanLookResult|GetPlanLookJob|TaskTypePlanLook|full_look_edit|look/regenerate|generated_image_url" \
  apps/server/internal/provider \
  apps/server/internal/service/rendering \
  apps/server/internal/service/plans.go \
  apps/server/internal/httpapi/renders.go \
  packages/core/src/api/endpoints.ts contracts/openapi.yaml
```

Expected: no matches.

Run:

```bash
rg -n "风格参考|效果示例" packages/core/src/copy/zh.ts contracts/openapi.yaml
```

Expected: generated preview maps to “风格参考”; Demo maps to “效果示例”.

- [ ] **Step 6: Run server build/tests**

Run: `make server-vet && make server-test && make server-build`

Expected: PASS;API 和 Worker 均编译，legacy look symbols 为 0。

- [ ] **Step 7: Commit wiring/deletion boundary**

```bash
git add apps/server/internal/bootstrap/worker.go \
  apps/server/internal/bootstrap/api.go \
  apps/server/cmd/worker/main.go \
  apps/server/cmd/api/main.go \
  apps/server/internal/service/plans.go \
  apps/server/internal/service/tasks.go \
  apps/server/internal/repository/repository.go \
  apps/server/internal/repository/postgres/postgres.go
git rm apps/server/internal/provider/look.go \
  apps/server/internal/provider/look_test.go
git commit -m "refactor(server): remove legacy look generation"
```

### Task 11: Build the Rendering Gold-Set Evaluator and Rollout Gates

**Files:**
- Deferred to Peripherals/Cutover: `apps/server/cmd/eval/main.go`
- Create: `apps/server/eval/rendering/manifest.go`
- Create: `apps/server/eval/rendering/metrics.go`
- Create: `apps/server/eval/rendering/release.go`
- Create: `apps/server/eval/rendering/manifest_test.go`
- Create: `apps/server/eval/rendering/metrics_test.go`
- Create: `apps/server/eval/rendering/release_test.go`
- Create: `apps/server/eval/rendering/testdata/passing-report.json`
- Create: `apps/server/config/render-rollout.production.json`

**Interfaces:**
- Consumes: private COS manifest `quality-gold/rendering/v1/manifest.jsonl` and published routing policy
- Produces: `rendering.Run`、`ValidateManifest`、`DecideRelease`；Peripherals/Cutover 将其接入统一 `cmd/eval rendering`。

- [ ] **Step 1: Write failing manifest integrity tests**

```go
func TestManifestRequiresMinimumAuthorizedCoverage(t *testing.T) {
	manifest := syntheticValidManifest()
	if err := ValidateManifest(manifest); err != nil {
		t.Fatal(err)
	}
	if got := manifest.IdentityCount(); got != 60 {
		t.Fatalf("identities = %d", got)
	}
	if got := manifest.CandidateCount(); got < 300 {
		t.Fatalf("candidates = %d", got)
	}
}

```

补充表驱动 manifest 错误用例：同一 identity hash 出现在 development/locked 返回 `identity_split_leak`；36/12/12 以外返回 `invalid_split_ratio`；空 consent ID 或 64 位小写 hex 以外 hash 返回 `missing_consent`/`invalid_sha256`；出现 `name/phone/open_id/url` 任一字段返回 `pii_field_forbidden`。

Manifest row contract:

```go
type GoldCase struct {
	CaseID             string            `json:"case_id"`
	IdentityHash       string            `json:"identity_hash"`
	Split              string            `json:"split"` // development|validation|locked
	ConsentRecordID    string            `json:"consent_record_id"`
	BodyObjectKey      string            `json:"body_object_key"`
	FaceObjectKey      string            `json:"face_object_key"`
	CandidateObjectKey string            `json:"candidate_object_key"`
	SHA256             map[string]string `json:"sha256"`
	Expected           GoldLabels        `json:"expected"`
}

type GoldLabels struct {
	IdentityPass       bool     `json:"identity_pass"`
	AnatomyPass        bool     `json:"anatomy_pass"`
	CompositionPass    bool     `json:"composition_pass"`
	SemanticPass       bool     `json:"semantic_pass"`
	SevereReasonCodes  []string `json:"severe_reason_codes"`
}
```

金集只保存在私有 COS；仓库不提交用户照片、签名 URL、姓名或联系方式。清单必须有 60 个授权身份、至少 300 个跨模型/失败类型候选；身份按 hash 固定为开发 36、验证 12、锁定 12，同一身份不得跨分区。

- [ ] **Step 2: Write failing metric and release-decision tests**

Release test 使用一张表：passing baseline 必须 eligible；source mismatch=1、WebP=1、old-generation overwrite=1、severe false accept=0.01、first-candidate P95=240.001s 分别必须 `eligible=false`，并返回对应唯一 stop reason。`SevereFailureFalseAcceptRateMax` 是排他上限，判定使用 `actual >= 0.01` 即停止。

发布阈值固定为：

```go
type ReleaseThresholds struct {
	SourceMismatchMax                 int
	WebPPublicationMax                int
	OldGenerationOverwriteMax        int
	SevereFailureFalseAcceptRateMax  float64
	HumanIdentityPassRateMin          float64
	SemanticComplianceRateMin         float64
	FirstCandidateP95Max              time.Duration
	SecondCandidateP95Max             time.Duration
}

var ProductionThresholds = ReleaseThresholds{
	SourceMismatchMax: 0,
	WebPPublicationMax: 0,
	OldGenerationOverwriteMax: 0,
	SevereFailureFalseAcceptRateMax: 0.01,
	HumanIdentityPassRateMin: 0.90,
	SemanticComplianceRateMin: 0.85,
	FirstCandidateP95Max: 120 * time.Second,
	SecondCandidateP95Max: 240 * time.Second,
}
```

- [ ] **Step 3: Run evaluator tests and verify failure**

Run: `cd apps/server && go test ./eval/rendering -count=1`

Expected: FAIL because evaluator package is absent.

- [ ] **Step 4: Implement deterministic metrics and CLI-neutral result codes**

Peripherals/Cutover 最终 CLI 调用：

```text
render-eval validate-manifest --manifest-key quality-gold/rendering/v1/manifest.jsonl
render-eval run --manifest-key quality-gold/rendering/v1/manifest.jsonl --split locked --routing-config config/ai-routing.production.json --output /tmp/render-eval.json
render-eval decide --report /tmp/render-eval.json --rollout-config config/render-rollout.production.json
```

退出码：

- 0：manifest 合法或 release gates 全部通过。
- 2：manifest/参数错误。
- 3：质量阈值未通过。
- 4：Provider/Storage 暂时不可用。

报告只写 case ID、匿名 identity hash、decision、reason codes、延迟和成本；不得写图片字节、embedding、签名 URL、厂商密钥。

- [ ] **Step 5: Define exact rollout state machine**

```json
{
  "version": "render-rollout-v1",
  "route_version": "render-route-v1",
  "quality_policy_version": "render-quality-v1",
  "current_stage": "shadow",
  "stages": [
    {"name": "shadow", "percent": 0, "minimum_published_results": 300},
    {"name": "5_percent", "percent": 5, "minimum_published_results": 200},
    {"name": "25_percent", "percent": 25, "minimum_published_results": 500},
    {"name": "50_percent", "percent": 50, "minimum_published_results": 1000},
    {"name": "100_percent", "percent": 100, "minimum_published_results": 2000}
  ],
  "bucket_key": "user_id",
  "change_one_factor_only": true,
  "stop_on": {
    "source_mismatch_count": 1,
    "webp_publication_count": 1,
    "old_generation_overwrite_count": 1,
    "severe_failure_false_accept_rate": 0.01,
    "first_candidate_p95_seconds": 240,
    "provider_retention_violation_count": 1
  }
}
```

每档晋级必须同时满足：锁定集通过、达到当前档最小样本量、线上护栏通过、且只改变 model/prompt-render-spec compiler/quality policy 中一个因素。分桶使用稳定 `sha256(user_id + route_version) % 100`。

- [ ] **Step 6: Run evaluator tests and a synthetic locked-set decision**

Run: `cd apps/server && go test ./eval/rendering -count=1`

Expected: PASS.

- [ ] **Step 7: Commit evaluation/rollout boundary**

```bash
git add apps/server/eval/rendering \
  apps/server/config/render-rollout.production.json
git commit -m "feat(quality): gate render rollout with gold set"
```

### Task 12: Add End-to-End Regression and Run the Full Quality Gate

**Files:**
- Modify: `apps/server/scripts/e2e.sh`
- Create: `apps/server/internal/service/rendering/e2e_fixture_test.go`

**Interfaces:**
- Consumes: complete Rendering chain
- Produces: one reproducible regression from RenderSpec to Publication plus terminal failure paths

- [ ] **Step 1: Add an E2E fixture Provider**

Fixture Provider 必须：

- 接收 body、face 和 RenderSpec。
- Candidate 1 模式可返回 pass JPEG 或指定 quality reason。
- Candidate 2 读取第一候选 reason codes。
- 输出真实可解码 JPEG。
- 暴露调用次数和 image role 顺序，仅用于测试。

- [ ] **Step 2: Add the happy-path shell regression**

`apps/server/scripts/e2e.sh` 增加：

```text
创建授权用户和三图/报告/方案/RenderSpec fixture
POST /v1/plan-variants/{id}/render-runs（带 Idempotency-Key）
轮询 /v1/operations/{id}
GET /v1/render-runs/{id}
断言 ready + generated_preview + 风格参考 + image/jpeg
重复 POST 相同 Idempotency-Key
断言 run ID 不变、Provider 调用次数不增加
```

- [ ] **Step 3: Add bounded-failure E2E regressions**

覆盖：

1. Candidate 1 identity_drift → Candidate 2 pass → 只扣一次发布额度。
2. Candidate 1、2 都 quality fail → run failed，方案文字仍可 GET。
3. 无等价多图 route → unavailable，无内置图/用户原图冒充。
4. generation 1 慢、generation 2 先发布 → 最终只展示 generation 2。
5. Provider 返回 WebP → 存储/API 仍为 JPEG。
6. Demo provider_version → contract failure，公开 API 无 media。

- [ ] **Step 4: Run focused race and contract checks**

Run: `cd apps/server && go test -race ./internal/service/rendering ./internal/repository/postgres ./internal/provider/ai/... -count=1`

Expected: PASS with no race detector output.

Run: `node contracts/scripts/check-sync.mjs && npx --yes @redocly/cli@2 lint --config contracts/redocly.yaml contracts/openapi.yaml`

Expected: both exit 0.

- [ ] **Step 5: Run repository-required verification**

Run:

```bash
pnpm typecheck && pnpm lint && make design-build
node apps/miniapp/scripts/check.mjs
make server-vet && make server-test
```

Expected: every command exits 0.

- [ ] **Step 6: Run local E2E**

Run: `make e2e`

Expected: exit 0;日志显示 Candidate→Gate→Publication，公开结果角标为“风格参考”，Demo 为“效果示例”。

- [ ] **Step 7: Run final forbidden-pattern and source-truth checks**

Run:

```bash
rg -n "DemoLookGenerator|LookGenerator|LookInput|ApplyPlanLookResult|GetPlanLookJob|TaskTypePlanLook|full_look_edit|look/regenerate|generated_image_url" \
  apps/server/internal/provider \
  apps/server/internal/service/rendering \
  apps/server/internal/service/plans.go \
  apps/server/internal/httpapi/renders.go \
  packages/core/src/api/endpoints.ts contracts/openapi.yaml
```

Expected: no matches.

Run:

```bash
rg -n "provider|model|internal_scores|reason_codes" \
  apps/server/internal/httpapi/renders.go packages/core/src/types/index.ts
```

Expected: no public Render DTO fields matching these names.

- [ ] **Step 8: Commit the E2E boundary**

```bash
git add apps/server/scripts/e2e.sh \
  apps/server/internal/service/rendering/e2e_fixture_test.go
git commit -m "test(server): cover rendering quality lifecycle"
```

## Commit Boundaries

实现阶段严格保持以下 12 个提交；一个提交失败时只回退该边界，不把相邻领域混入：

1. `feat(server): define rendering quality domain`
2. `feat(server): create immutable render runs`
3. `feat(server): enforce equivalent rendering routes`
4. `feat(server): generate looks from render specs`
5. `feat(server): quarantine normalized jpeg candidates`
6. `feat(server): gate render candidate quality`
7. `feat(server): orchestrate bounded render candidates`
8. `feat(server): publish renders with dual cas`
9. `feat(api): expose quality-gated render runs`
10. `refactor(server): remove legacy look generation`
11. `feat(quality): gate render rollout with gold set`
12. `test(server): cover rendering quality lifecycle`

本次只创建计划文档，不执行上述提交。

## Final Acceptance Matrix

| Requirement | Evidence |
|---|---|
| RenderHead/Run/Candidate/Evaluation/Publication | Task 1 domain + baseline constraints |
| RenderSpec-only consumption | Task 4 contract tests |
| body/face multi-image routing | Task 3 hard metadata + Task 4 input-order test |
| no single-image fallback | Task 3 production route validation |
| one initial candidate, at most second | Task 2 initial limit + Task 7 orchestration + Task 8 dedupe |
| technical/identity/anatomy/composition/semantic Gates | Task 6 ordered Gate tests |
| JPEG normalization | Task 5 JPEG/PNG/WebP tests |
| isolated COS | Task 5 quarantine/published prefix tests |
| task lease CAS + generation/version CAS | Task 8 repeated PostgreSQL race tests |
| old task cannot overwrite | Task 8 race scenario + Task 12 E2E |
| API does not leak internals | Task 9 HTTP/OpenAPI tests |
| generated label “风格参考” | Task 1 projection + Task 9 core copy test |
| Demo label “效果示例” | Task 1 separate Demo projection + Task 9 copy test |
| Provider contract tests | Tasks 4 and 6 |
| gold set and rollout | Task 11 manifest, metrics and stage gate |
| no historical compatibility | Task 10 symbol deletion and absence scan |

计划完成后，执行者从 Task 1 开始，不并行修改共享的 baseline、OpenAPI 或 routing config。Task 8 的双 CAS 测试、Task 9 的用户文案测试和 Task 11 的停止条件是发布阻断项，不能降级为警告。
