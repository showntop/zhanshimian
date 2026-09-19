# Planning and RenderSpec Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 构建六场景统一的、基于可信 Report 的不可变 PlanSet 生成链路，发布恰好三套有 grounding 且有实质差异的方案，并为每套方案确定性编译一个可供后续渲染链路消费的 RenderSpec。

**Architecture:** `httpapi` 只验证传输契约并调用 `service/planning`；Planning Service 通过 `ReportReader`、`OperationStarter`、`TaskEnqueuer`、`PlanSetStore` 等窄接口启动异步操作。Worker 在事务外调用结构化文本 Generator 和文字一致性 Verifier，通过确定性 Gate 后，在单个 PostgreSQL 事务中一次性发布 PlanSet、3 个 Variant、9 个 Step、全部 Grounding、质量记录和 3 个 RenderSpec；所有发布产物只插入、不更新。

**Tech Stack:** Go 1.24.0（toolchain go1.24.5）、PostgreSQL、pgx/v5、Go `testing`、OpenAPI 3、JSON Schema Draft 2020-12；不新增运行时依赖。

## Global Constraints

- 本计划只覆盖 Planning/RenderSpec：六场景 Brief、统一 PlanSet Generator、文字质量 Gate、不可变 PlanSet、RenderSpec Compiler、PlanSet API 和 Planning 金集。
- 不实现 `full_look_generation`、图片 Provider、RenderRun/Candidate/Publication、身份/人体/构图/图文图片质量判断；这些由后续 Rendering/Quality 计划消费本计划的 `RenderSpecReader`。
- 不保留历史业务数据，不兼容旧数据库、旧 API、旧本地缓存或旧任务；不得保留 `plans`、`plan_group`、`plan_look` 或 `/v1/reports/{id}/plans` 双路径。
- 保持模块化单体：一个 Go 代码库、API/Worker 两个进程、一个 PostgreSQL 和一个私有 COS；不引入 Kafka、Redis 队列、工作流引擎、Saga 或事件溯源。
- 依赖方向固定为 `httpapi → service → repository / provider / storage`；handler 不直查数据库，业务服务不依赖 PostgreSQL 或具体模型。
- AI 只经能力路由；Planning 仅使用 `plan_set_generation` 和 `plan_grounding_verification`，业务代码不得出现厂商或模型名，生产不得自动切 Demo。
- 报告、方案集和 RenderSpec 是不可变产物；重新生成或修改场景答案必须创建新 ID，禁止原地覆盖。
- 每个 PlanSet 恰好 3 个 Variant，key 固定为 `sharp | warm | natural`，恰好 1 个 `recommended=true`。
- 每个 Variant 恰好包含 `hair | makeup | outfit` 三个 Step；没有依据时使用 `keep`，不得编造调整。
- 每个 Step 至少 1 个 Grounding；最高优先 Report finding 至少被一个 Step 落实；场景 Brief 的每个答案至少被一个 Step grounding。
- 任意两个 Variant 在发型、妆容、穿搭、配色、正式度五个维度中至少有 2 个可验证差异。
- 不编造衣橱、品牌、价格、材质或身体特征；方案文字不得与 Report 矛盾。
- 不打颜值分/身材分、不身材羞辱、不做医学结论、不用警示红；只使用“可提升点”式尊重表达。
- 场景名称固定为 `通用 / 面试 / 婚礼 / 约会 / 日常 / 聚会`；产品文案中使用“日常”，不使用“通勤”作为场景名。
- 相同 `report_id + scene + brief_hash + planner_schema_version` 必须复用幂等结果；相同语义输入不得重复调用 AI。
- Generator 内容质量失败最多补生成 1 次；Task 的网络重试不得消耗这一次内容补生成预算。
- API 成功包络固定为 `{"data": ...}`；异步创建固定为 `202 {"data": ..., "operation": ...}`；错误固定为 `{"error":{"code","message","request_id","retryable"}}`。
- 所有创建请求必须支持 `Idempotency-Key`；越权统一返回 404；API 不返回厂商、模型名、Provider 临时 URL 或内部质量分数。
- 数据库每次用户资源读写必须同时限定 `resource_id + user_id`；所有跨用户资源关系使用 `(user_id, parent_id)` 复合外键。
- 数据库只允许删除不可变产物以满足用户数据删除；对 PlanSet、Variant、Step、Grounding、RenderSpec 和质量记录的 `UPDATE` 必须由数据库触发器拒绝。
- `profile_snapshot`、`scene_brief` 和 `render_specs.spec` 使用带明确版本的 JSON；JSON canonicalization 后再计算 SHA-256。
- 外部模型金集不进入每次 PR CI；PR CI 只运行纯函数、Service fake、PostgreSQL 集成、Provider Contract、HTTP 契约和金集结构检查。
- Planning 金集固定为 120 个 Brief、180 个三方案组，按 60%/20%/20% 拆分为 development/validation/release；同一 `report_fixture_id` 不得跨分区。
- 根级 `package.json`、`tsconfig.base.json`、`Makefile`、`docker-compose.yml` 已冻结，本计划不得修改。
- `appearance-coach-prototype/` 只读，本计划不得修改。

---

## Cross-plan Ownership Override

- 本计划拥有 Planning/RenderSpec 的 Go Domain、Service、Postgres、Provider 与 HTTP；它消费 Rule/Contract Freeze 已确定的 OpenAPI，不修改 `contracts/**`。
- Foundation 独占并一次性创建最终 `001_baseline.sql`；本计划只验证 Planning/RenderSpec 表、复合外键、不可变约束和索引。正文 DDL 是 Foundation 必须纳入的规范。
- 本计划不得修改或提交 `packages/core/src/types/**`、`packages/core/src/api/**` 或任何 Miniapp 页面；最终 OpenAPI codegen 与客户端切换由 `2026-09-12-miniapp-quality-loop.md` 在所有服务端核心计划完成后统一执行。
- 本计划不修改中央 `bootstrap/api.go`、`bootstrap/worker.go` 或 `httpapi/httpapi.go` composition；Task 9 只提供 Planning 构造函数、Handler 定义和独立 wiring test，最终注册与旧路删除由 Peripherals/Cutover 完成。
- 下文 Task 8 中关于 `contracts/openapi.yaml`、`packages/core/src/types/planning.ts`、`packages/core/src/types/index.ts`、`packages/core/src/api/endpoints.ts` 的步骤和提交路径全部跳过；Task 8 只完成服务端 Handler 与 HTTP 契约测试。
- Planning 金集统一放在 `apps/server/eval/planning/`；统一 `apps/server/cmd/eval/main.go` 由最终 Peripherals/Cutover 装配，本计划不修改该入口，不创建第二个评测二进制或 `internal/eval` 目录。
- Planning Handler 遵循总索引的 disposition 语义：`Execute` 不结束 Task、不入队内容补生成、不失败 Operation；它只调用 Provider 并写不可读 staged attempt。`Commit` 根据 `publish|enqueue_next|domain_failed` 在单个 lease CAS 事务中发布 PlanSet、创建第二内容 Task 或失败 Operation。`CommitSuperseded` 只表示真实 lease/generation 过期。
- 本计划不删除旧 PlanGroup/Plans/Repository/HTTP 文件，也不修改中央旧路径；正文 Delete 列表和 Task 9 删除步骤移交最终 Peripherals/Cutover。Planning 新包必须可独立测试，但在 Cutover 前不与旧实现形成生产双写。

## Preconditions

执行本计划前，基础设施与 Report 计划必须已经提供：

1. `apps/server/internal/domain/assessment.go` 中不可变的 Report/Finding 数据模型，以及已发布 Report 的复合租户约束。
2. `apps/server/internal/domain/operation.go` 和 `apps/server/internal/domain/task.go` 中公开 Operation、内部 Task、错误分类、lease/heartbeat/CAS。
3. `apps/server/internal/service/taskrunner` 的 Handler Registry；新增任务类型通过注册扩展，不修改 claim 主循环。
4. `apps/server/internal/provider/ai` 的能力路由与 invocation ledger；结构化调用返回已落账的 `InvocationID`。
5. `apps/server/internal/database/migrations/001_baseline.sql` 是唯一 baseline migration，且开发数据库允许重建。

如果任何签名尚未存在，基础设施计划必须按下方“跨计划接口冻结”实现，不得在 Planning 内复制一套近似接口。

## File Map

**Create**

- `apps/server/internal/domain/planning.go`：Scene、Brief、PlanSet、Variant、Step、Grounding 和 RenderSpec 输入契约；后续 Rendering 计划直接消费，不在 `domain/rendering.go` 重复声明。
- `apps/server/internal/service/planning/brief.go`：六场景 Brief Catalog、规范化和 hash。
- `apps/server/internal/service/planning/ports.go`：所有窄接口和跨边界 DTO。
- `apps/server/internal/service/planning/service.go`：创建、读取、列表用例。
- `apps/server/internal/service/planning/validation.go`：结构、grounding、差异、场景和 Report 一致性 Gate。
- `apps/server/internal/service/planning/compiler.go`：PlanSet → 3 个 RenderSpec 的确定性编译器。
- `apps/server/internal/service/planning/worker.go`：文本生成、核验、一次内容补生成和原子发布编排。
- `apps/server/internal/service/planning/*_test.go`：对应纯函数和 Service TDD 测试。
- `apps/server/internal/provider/ai/planning.go`：`plan_set_generation` Generator 和 `plan_grounding_verification` Verifier 适配器。
- `apps/server/internal/provider/ai/planning_test.go`：结构化输入、Schema、输出解码和错误分类 Contract 测试。
- `apps/server/internal/provider/ai/schemas/plan_set.v1.json`：Generator 严格输出 Schema。
- `apps/server/internal/provider/ai/schemas/plan_verification.v1.json`：文字一致性核验输出 Schema。
- `apps/server/internal/service/planning/schemas/render_spec.v1.json`：RenderSpec Schema。
- `apps/server/internal/repository/postgres/planning.go`：Planning 各窄接口的 PostgreSQL Adapter。
- `apps/server/internal/repository/postgres/planning_integration_test.go`：真实 PostgreSQL 不可变、租户和并发测试。
- `apps/server/internal/httpapi/plan_sets.go`：3 个 PlanSet endpoint。
- `apps/server/internal/httpapi/plan_sets_test.go`：请求/响应/404/幂等契约测试。
- `packages/core/src/types/planning.ts`：由 OpenAPI 形状对应的跨端 PlanSet/Brief/Operation 类型。
- `apps/server/eval/planning/golden.go`：金集加载和静态评测。
- `apps/server/eval/planning/golden_test.go`：数量、分区、Schema 和 Gate 期望测试。
- `apps/server/eval/planning/testdata/briefs.v1.jsonl`：120 个 Brief。
- `apps/server/eval/planning/testdata/plan_sets.v1.jsonl`：180 个三方案组。

**Modify**

- `apps/server/internal/database/migrations/001_baseline.sql`：加入不可变 Planning/RenderSpec 表、索引、复合外键、延迟约束触发器。
- `apps/server/internal/bootstrap/api.go`：注入 Planning API Service。
- `apps/server/internal/bootstrap/worker.go`：注册 `plan_set.generate` Handler。
- `apps/server/internal/httpapi/httpapi.go`：注入窄 `PlanSetService` 并注册新路由。
- `apps/server/internal/service/tasks.go`、`apps/server/internal/service/service.go`：删除 `plan_group/plan_look` Handler、字段和 ProviderOptions 接线。
- `apps/server/internal/repository/repository.go`、`apps/server/internal/repository/postgres/postgres.go`：删除旧 plans/checklist/selection/feedback 巨型 Repository 方法与 SQL。
- `apps/server/internal/bootstrap/ai.go`：删除旧 PlanGroup/Look bundle 接线，Planning 只从 `internal/provider/ai` 两个文字能力构造。
- `apps/server/internal/service/product_loops.go`：删除直接读取旧 `Plan` 的分支；外围能力在外围切换计划中改接 `PlanSetReader`。
- `contracts/openapi.yaml`：以新 `/v1/plan-sets` 契约替换旧方案端点。
- `packages/core/src/api/endpoints.ts`、`packages/core/src/types/index.ts`：删除旧 Plan API，导出新 PlanSet API 和类型。
- `apps/server/config/ai-routing.example.json`、`apps/server/config/ai-routing.production.json`：注册两项文字能力，不配置 Demo fallback。

**Delete**

- `apps/server/internal/provider/plan_group.go`
- `apps/server/internal/service/scene_plans.go`
- `apps/server/internal/service/scene_plans_test.go`
- `apps/server/internal/service/plans.go`
- `apps/server/internal/httpapi/plans.go`

Selection/Execution/Feedback 由对应后续计划以新资源实现，不在本计划保留临时入口。

## Cross-plan Interface Freeze

以下签名是本计划与基础设施、Report、Rendering 计划之间的冻结契约。实现者不得改名或换成巨型 Repository。

```go
package planning

type ReportReader interface {
	GetPlanningReport(ctx context.Context, userID, reportID string) (ReportSnapshot, error)
}

type OperationStarter interface {
	StartWithTask(ctx context.Context, command StartOperationCommand) (domain.OperationRef, bool, error)
}

type TaskEnqueuer interface {
	EnqueueRetry(ctx context.Context, lease domain.TaskLease, command EnqueueRetryCommand) (domain.CommitOutcome, error)
}

type OperationWriter interface {
	MarkRunning(ctx context.Context, lease domain.TaskLease, progressBPS int, stageCode, publicMessage string) (bool, error)
	Fail(ctx context.Context, lease domain.TaskLease, quality PlanQualityRecord, code, publicMessage string, retryable bool) (bool, error)
}

type PlanSetStore interface {
	FindPublished(ctx context.Context, key PlanSetKey) (domain.PlanSet, bool, error)
	Get(ctx context.Context, userID, planSetID string) (domain.PlanSet, error)
	List(ctx context.Context, userID, reportID string, scene *domain.Scene) ([]domain.PlanSet, error)
	Prepare(ctx context.Context, lease domain.TaskLease, command PrepareCommand) (domain.PlanSet, error)
	CommitPrepared(ctx context.Context, lease domain.TaskLease, result domain.TaskResult) (domain.CommitOutcome, error)
}

type PlanSetGenerator interface {
	Generate(ctx context.Context, input GenerationInput) (GeneratedPlanSet, error)
}

type PlanSetVerifier interface {
	Verify(ctx context.Context, input VerificationInput) (VerificationResult, error)
}

type RenderSpecReader interface {
	GetRenderSpecForVariant(ctx context.Context, userID, planVariantID string) (domain.RenderSpec, error)
}
```

`OperationStarter.StartWithTask` 必须在一个事务中插入 Operation 和初始 Task，并对 `(user_id, kind, dedupe_key)` 幂等；`TaskEnqueuer.EnqueueRetry` 必须校验 `TaskLease`，在一个事务中记录第一次被拒质量结果、结束第一内容 Task、将 Operation 置为 `retrying` 并插入第二内容 Task；`OperationWriter.Fail` 必须在同一 lease CAS 下记录第二次被拒质量结果并失败 Operation；`PlanSetStore.Prepare` 只写通过质量记录和不可变但尚不可读的产物，`CommitPrepared` 再以 lease token CAS 在一个事务中发布结果并将 Operation 置为 `succeeded`。这些原子边界不可拆成连续的两个数据库调用。

返回值语义固定：`StartWithTask` 的 bool 表示本次是否新建；`FindPublished` 的 bool 表示是否找到；`MarkRunning/Fail` 的 bool 表示 lease CAS 是否生效；`EnqueueRetry/CommitPrepared` 返回基础计划的 `domain.CommitApplied|domain.CommitSuperseded`。

### Task 1: Define Planning Domain and Six Brief Schemas

**Files:**
- Create: `apps/server/internal/domain/planning.go`
- Create: `apps/server/internal/service/planning/brief.go`
- Test: `apps/server/internal/service/planning/brief_test.go`

**Interfaces:**
- Consumes: string IDs、`json.RawMessage`。
- Produces: `domain.Scene`、`domain.SceneBrief`、`domain.PlanSet`、`NormalizeBrief(scene, answers)`、`BriefHash(brief)`。

- [ ] **Step 1: Write failing table-driven tests for all six scenes**

```go
func TestNormalizeBriefAcceptsEveryScene(t *testing.T) {
	tests := []struct {
		scene   domain.Scene
		answers map[string]string
	}{
		{domain.SceneGeneral, map[string]string{"focus": "balanced", "preparation": "closet", "impression": "natural"}},
		{domain.SceneInterview, map[string]string{"when": "three_days", "format": "final", "preparation": "key_piece", "impression": "reliable"}},
		{domain.SceneWedding, map[string]string{"role": "guest", "timing": "dinner", "dress_code": "elegant", "impression": "memorable"}},
		{domain.SceneDate, map[string]string{"activity": "dinner", "timing": "evening", "preparation": "closet", "impression": "natural"}},
		{domain.SceneDaily, map[string]string{"activity": "office", "weather": "air_conditioned", "preparation": "key_piece", "impression": "energetic"}},
		{domain.SceneGathering, map[string]string{"activity": "friends", "timing": "night", "preparation": "complete", "impression": "memorable"}},
	}
	for _, tt := range tests {
		t.Run(string(tt.scene), func(t *testing.T) {
			got, err := NormalizeBrief(tt.scene, tt.answers)
			if err != nil {
				t.Fatal(err)
			}
			if got.SchemaVersion != BriefSchemaVersion || len(got.Answers) != len(tt.answers) {
				t.Fatalf("unexpected brief: %#v", got)
			}
		})
	}
}

func TestNormalizeBriefRejectsUnknownMissingAndExtraAnswers(t *testing.T) {
	_, err := NormalizeBrief(domain.SceneDaily, map[string]string{
		"activity": "office", "weather": "air_conditioned", "preparation": "closet",
		"impression": "natural", "budget": "unknown",
	})
	if !errors.Is(err, ErrInvalidBrief) {
		t.Fatalf("got %v, want ErrInvalidBrief", err)
	}
}

func TestBriefHashIsMapOrderIndependent(t *testing.T) {
	a, _ := NormalizeBrief(domain.SceneGeneral, map[string]string{"focus": "balanced", "preparation": "closet", "impression": "natural"})
	b, _ := NormalizeBrief(domain.SceneGeneral, map[string]string{"impression": "natural", "focus": "balanced", "preparation": "closet"})
	if BriefHash(a) != BriefHash(b) {
		t.Fatal("semantically identical briefs must hash equally")
	}
}
```

- [ ] **Step 2: Run the focused tests and verify failure**

Run: `cd apps/server && go test ./internal/service/planning -run 'TestNormalizeBrief|TestBriefHash' -count=1`

Expected: FAIL with `undefined: NormalizeBrief` or missing package files.

- [ ] **Step 3: Implement exact enums, immutable snapshots and catalog**

`planning.go` must define:

```go
type Scene string

const (
	SceneGeneral   Scene = "general"
	SceneInterview Scene = "interview"
	SceneWedding   Scene = "wedding"
	SceneDate      Scene = "date"
	SceneDaily     Scene = "daily"
	SceneGathering Scene = "gathering"
)

type SceneBrief struct {
	SchemaVersion string            `json:"schema_version"`
	Scene         Scene             `json:"scene"`
	Answers       map[string]string `json:"answers"`
}

type StepCategory string
type StepAction string
type GroundingSourceType string
type PlanVariantKey string

const (
	CategoryHair   StepCategory = "hair"
	CategoryMakeup StepCategory = "makeup"
	CategoryOutfit StepCategory = "outfit"
	ActionKeep     StepAction = "keep"
	ActionAdjust   StepAction = "adjust"
	SourceReportFinding    GroundingSourceType = "report_finding"
	SourceSceneAnswer      GroundingSourceType = "scene_answer"
	SourceProfilePreference GroundingSourceType = "profile_preference"
	SourceStyleRule        GroundingSourceType = "style_rule"
	SourceFeedbackMemory   GroundingSourceType = "feedback_memory"
	VariantSharp   PlanVariantKey = "sharp"
	VariantWarm    PlanVariantKey = "warm"
	VariantNatural PlanVariantKey = "natural"
)

type PlanSet struct {
	ID                   string
	UserID               string
	ReportID             string
	ProfileSnapshot      json.RawMessage
	Scene                Scene
	SceneBrief           SceneBrief
	BriefHash            string
	PlannerSchemaVersion string
	StyleRuleVersion     string
	ProviderInvocationID string
	QualityEvaluationID  string
	CreatedAt            time.Time
	Variants             []PlanVariant
}

type PlanVariant struct {
	ID             string
	UserID         string
	PlanSetID      string
	Slot           int
	Key            PlanVariantKey
	Name           string
	Descriptor     string
	Rationale      string
	Recommended    bool
	OutcomeTags    []string
	DifferenceTags []string
	Steps          []PlanStep
	CreatedAt      time.Time
}

type PlanStep struct {
	ID            string
	UserID        string
	PlanVariantID string
	Category      StepCategory
	Action        StepAction
	Title         string
	Summary       string
	Details       PlanStepDetails
	Position      int
	Groundings    []PlanStepGrounding
	CreatedAt     time.Time
}

type PlanStepDetails struct {
	Target     string   `json:"target"`
	Intensity  string   `json:"intensity"`
	Silhouette string   `json:"silhouette"`
	Palette    []string `json:"palette"`
	Layers     []string `json:"layers"`
	Avoid      []string `json:"avoid"`
	Formality  string   `json:"formality"`
}

type PlanStepGrounding struct {
	UserID     string
	PlanStepID string
	SourceType GroundingSourceType
	SourceID   string
	Reason     string
}
```

同时定义 `PlanSet`、`PlanVariant`、`PlanStep`、`PlanStepDetails`、`PlanStepGrounding`。`PlanStepDetails` 固定字段为 `Target string`、`Intensity string`、`Silhouette string`、`Palette []string`、`Layers []string`、`Avoid []string`、`Formality string`；禁止无 Schema 的任意 map。

`brief.go` 中固定 catalog：

- `general`: `focus = balanced|hair_first|makeup_first|outfit_first`；`preparation = closet|key_piece|complete`；`impression = energetic|reliable|natural|memorable`
- `interview`: `when = today|three_days|week|later`；`format = onsite|video|final`；`preparation = closet|key_piece|complete`；`impression = energetic|reliable|natural|memorable`
- `wedding`: `role = guest|bridal_party|family|speaker`；`timing = lunch|afternoon|dinner|unknown`；`dress_code = relaxed|elegant|formal`；`impression = energetic|reliable|natural|memorable`
- `date`: `activity = coffee|dinner|exhibition|outdoor`；`timing = afternoon|evening|night|unknown`；`preparation = closet|key_piece|complete`；`impression = energetic|reliable|natural|memorable`
- `daily`: `activity = office|weekend|friends|city_walk`；`weather = air_conditioned|walking|rain|mild`；`preparation = closet|key_piece|complete`；`impression = energetic|reliable|natural|memorable`
- `gathering`: `activity = friends|dinner|birthday|drinks`；`timing = afternoon|evening|night|unknown`；`preparation = closet|key_piece|complete`；`impression = energetic|reliable|natural|memorable`

规范化必须拒绝缺字段、额外字段、额外 scene 和未列举 value；按字段名字典序编码 `{"schema_version","scene","answers"}` 后 SHA-256，返回 64 位小写 hex。

`style-rules.v1` 只允许以下稳定 grounding ID：`style.keep_without_evidence`、`style.low_intensity_hair`、`style.low_intensity_makeup`、`style.coherent_palette`、`style.scene_formality`。Provider 不得发明其他 style rule ID；规则版本变化必须发布新的 `StyleRuleVersion`，不得原地改写这些 ID 的语义。

- [ ] **Step 4: Run tests and verify pass**

Run: `cd apps/server && go test ./internal/service/planning -run 'TestNormalizeBrief|TestBriefHash' -count=1`

Expected: PASS.

- [ ] **Step 5: Suggested commit**

```bash
git add apps/server/internal/domain/planning.go apps/server/internal/service/planning/brief.go apps/server/internal/service/planning/brief_test.go
git commit -m "feat(planning): define six scene briefs"
```

### Task 2: Freeze Narrow Ports and PlanSet Creation Use Case

**Files:**
- Create: `apps/server/internal/service/planning/ports.go`
- Create: `apps/server/internal/service/planning/service.go`
- Test: `apps/server/internal/service/planning/service_test.go`

**Interfaces:**
- Consumes: Task 1 domain and canonical Brief.
- Produces: the exact port signatures in “Cross-plan Interface Freeze”; `Service.CreatePlanSet`、`Service.GetPlanSet`、`Service.ListPlanSets`。

- [ ] **Step 1: Write failing orchestration tests**

```go
func TestCreatePlanSetStartsOperationAndTaskOnce(t *testing.T) {
	reportID := "20000000-0000-0000-0000-000000000001"
	starter := &fakeStarter{}
	svc := NewService(Dependencies{
		Reports: fakeReports{report: validReport(reportID)},
		Operations: starter,
		Store: &fakeStore{},
		IDs: func() string { return "10000000-0000-0000-0000-000000000001" },
	})
	got, err := svc.CreatePlanSet(context.Background(), CreateCommand{
		UserID: "00000000-0000-0000-0000-000000000001", ReportID: reportID, Scene: domain.SceneDaily,
		Answers: map[string]string{"activity": "office", "weather": "air_conditioned", "preparation": "closet", "impression": "natural"},
		IdempotencyKey: "plan-daily-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Operation.ID == "" || got.PlanSetID == "" || !got.Accepted {
		t.Fatalf("unexpected result: %#v", got)
	}
	if starter.calls != 1 || starter.command.Task.Type != PlanSetGenerationTaskType {
		t.Fatalf("unexpected start command: %#v", starter.command)
	}
}

func TestCreatePlanSetReturnsPublishedResultWithoutStartingAI(t *testing.T) {
	store := &fakeStore{found: true, planSet: validPublishedPlanSet()}
	starter := &fakeStarter{}
	svc := NewService(Dependencies{Reports: fakeReports{report: validReport(store.planSet.ReportID)}, Operations: starter, Store: store})
	got, err := svc.CreatePlanSet(context.Background(), validCreateCommand(store.planSet.ReportID))
	if err != nil {
		t.Fatal(err)
	}
	if got.Accepted || got.PlanSetID != store.planSet.ID || starter.calls != 0 {
		t.Fatalf("published result was not reused: %#v", got)
	}
}
```

- [ ] **Step 2: Run focused tests and verify failure**

Run: `cd apps/server && go test ./internal/service/planning -run 'TestCreatePlanSet' -count=1`

Expected: FAIL with missing `NewService`/`CreatePlanSet`.

- [ ] **Step 3: Implement port DTOs exactly**

`ports.go` must include:

```go
const (
	PlannerSchemaVersion      = "plan-set.v1"
	StyleRuleVersion          = "style-rules.v1"
	PlanSetGenerationTaskType domain.TaskType = "plan_set.generate"
)

type ReportSnapshot struct {
	ID              string
	UserID          string
	PhotoSetID      string
	FaceAssetID     string
	BodyAssetID     string
	ProfileSnapshot json.RawMessage
	ImpressionTags []string
	PriorityTitle string
	PriorityCopy string
	PriorityFindingID string
	Findings        []FindingSnapshot
}

type FindingSnapshot struct {
	ID string
	Category string
	Priority int
	Label string
	VisibleObservation string
	Recommendation string
}

type StartOperationCommand struct {
	OperationID string
	UserID string
	Kind domain.OperationKind
	SubjectType string
	SubjectID string
	IdempotencyKey string
	DedupeKey string
	Task EnqueueTask
}

type EnqueueTask struct {
	Type domain.TaskType
	SubjectType string
	SubjectID string
	SubjectGeneration int
	PayloadVersion int
	Payload any
	DedupeKey string
}

type PlanSetKey struct {
	UserID string
	ReportID string
	Scene domain.Scene
	BriefHash string
	PlannerSchemaVersion string
}

type CreateCommand struct {
	UserID string
	ReportID string
	Scene domain.Scene
	Answers map[string]string
	IdempotencyKey string
}

type GenerateTaskPayload struct {
	PlanSetID string `json:"plan_set_id"`
	ReportID string `json:"report_id"`
	Scene domain.Scene `json:"scene"`
	Brief domain.SceneBrief `json:"brief"`
	BriefHash string `json:"brief_hash"`
	PlannerSchemaVersion string `json:"planner_schema_version"`
	StyleRuleVersion string `json:"style_rule_version"`
	ContentAttempt int `json:"content_attempt"`
	PriorReasonCodes []string `json:"prior_reason_codes"`
}

type EnqueueRetryCommand struct {
	UserID string
	OperationID string
	ProgressBPS int
	StageCode string
	PublicMessage string
	Quality PlanQualityRecord
	Task EnqueueTask
}

type GenerationInput struct {
	Report ReportSnapshot
	Brief domain.SceneBrief
	ContentAttempt int
	PriorReasonCodes []string
}

type GeneratedPlanSet struct {
	InvocationID string
	Variants []GeneratedPlanVariant
}

type GeneratedPlanVariant struct {
	Slot int
	Key domain.PlanVariantKey
	Name string
	Descriptor string
	Rationale string
	Recommended bool
	OutcomeTags []string
	DifferenceTags []string
	Steps []GeneratedPlanStep
}

type GeneratedPlanStep struct {
	Category domain.StepCategory
	Action domain.StepAction
	Title string
	Summary string
	Details domain.PlanStepDetails
	Groundings []GeneratedGrounding
}

type GeneratedGrounding struct {
	SourceType domain.GroundingSourceType
	SourceID string
	Reason string
}

type VerificationInput struct {
	Report ReportSnapshot
	Brief domain.SceneBrief
	Candidate GeneratedPlanSet
}

type VerificationResult struct {
	InvocationID string
	Decision string
	ReasonCodes []string
	Violations []string
}

type PlanQualityRecord struct {
	ID string
	UserID string
	SubjectID string
	PolicyVersion string
	Decision string
	ReasonCodes []string
	InternalScores json.RawMessage
	EvaluatorInvocationID string
}

type PrepareCommand struct {
	UserID string
	PlanSet domain.PlanSet
	Quality PlanQualityRecord
	RenderSpecs []domain.RenderSpec
}

type CreateResult struct {
	PlanSetID string
	Accepted bool
	Operation domain.OperationRef
	PlanSet *domain.PlanSet
}
```

`ReportSnapshot.ProfileSnapshot` 必须是 Report 发布时保存的实际快照；Planning 不读取可变 `user_profiles` 来回写旧 Report 的语义。

grounding `source_id` 规则固定为：`report_finding` 使用 finding UUID；`scene_answer` 使用 Brief 字段名；`profile_preference` 使用 `profile_snapshot` 内可解析的 RFC 6901 JSON Pointer；`style_rule` 使用 Task 1 的五个稳定 ID；本阶段 Generator 不允许输出 `feedback_memory`，后续 Feedback 计划只有在 `GenerationInput` 增加显式 memory snapshot 后才能启用，禁止使用无法核验的自由文本 ID。

- [ ] **Step 4: Implement create/read/list use cases**

`CreatePlanSet` 顺序固定为：规范化 Brief → 读取用户所属 Report → 计算 key → 查已发布结果 → 构造内容 dedupe key → 原子启动 Operation+Task。内容 dedupe key 为：

```go
func generationDedupeKey(key PlanSetKey) string {
	return fmt.Sprintf("plan-set:%s:%s:%s:%s",
		key.ReportID, key.Scene, key.BriefHash, key.PlannerSchemaVersion)
}
```

初始 task payload 固定 `ContentAttempt=1`，`PriorReasonCodes=nil`，`PayloadVersion=1`。`OperationStarter` 和 `TaskEnqueuer` 必须从 Task 9 注册的 Registry Definition 读取 `MaxAttempts`，command 不携带重试预算。`GetPlanSet` 和 `ListPlanSets` 只转调 `PlanSetStore`，不得读取 Task 推算状态。

- [ ] **Step 5: Run tests and verify pass**

Run: `cd apps/server && go test ./internal/service/planning -run 'TestCreatePlanSet|TestGetPlanSet|TestListPlanSets' -count=1`

Expected: PASS.

- [ ] **Step 6: Suggested commit**

```bash
git add apps/server/internal/service/planning/ports.go apps/server/internal/service/planning/service.go apps/server/internal/service/planning/service_test.go
git commit -m "feat(planning): add narrow plan set service"
```

### Task 3: Add Unified Structured PlanSet Generator Contract

**Files:**
- Create: `apps/server/internal/provider/ai/schemas/plan_set.v1.json`
- Create: `apps/server/internal/provider/ai/planning.go`
- Test: `apps/server/internal/provider/ai/planning_test.go`
- Modify: `apps/server/config/ai-routing.example.json`
- Modify: `apps/server/config/ai-routing.production.json`

**Interfaces:**
- Consumes: `planning.PlanSetGenerator.Generate(context.Context, planning.GenerationInput)`.
- Produces: `planning.GeneratedPlanSet` with one ledger-backed `InvocationID`; no database entities and no Provider identity.

- [ ] **Step 1: Write failing Provider Contract tests**

```go
func TestPlanSetGeneratorSendsReportBriefAndGroundingIDs(t *testing.T) {
	runtime := &fakeStructuredRuntime{result: validGeneratedPlanSetJSON()}
	generator := NewPlanSetGenerator(runtime)
	got, err := generator.Generate(context.Background(), planning.GenerationInput{
		Report: validPlanningReport(),
		Brief: validDailyBrief(),
		ContentAttempt: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.request.Capability != "plan_set_generation" || len(got.Variants) != 3 {
		t.Fatalf("unexpected generation: %#v", got)
	}
	if !bytes.Contains(runtime.request.Prompt, []byte(`"source_id"`)) {
		t.Fatal("prompt must expose stable grounding IDs")
	}
}

func TestPlanSetGeneratorRejectsUnknownGroundingSourceType(t *testing.T) {
	runtime := &fakeStructuredRuntime{result: generatedJSONWithSourceType("provider_guess")}
	_, err := NewPlanSetGenerator(runtime).Generate(context.Background(), validGenerationInput())
	if !errors.Is(err, planning.ErrGeneratorContract) {
		t.Fatalf("got %v, want ErrGeneratorContract", err)
	}
}
```

- [ ] **Step 2: Run focused tests and verify failure**

Run: `cd apps/server && go test ./internal/provider/ai -run 'TestPlanSetGenerator' -count=1`

Expected: FAIL with missing `NewPlanSetGenerator`.

- [ ] **Step 3: Define the exact structured output Schema**

`plan_set.v1.json` must be strict (`additionalProperties:false` at every object) and require:

```json
{
  "variants": [{
    "slot": 1,
    "key": "sharp",
    "name": "清晰利落",
    "descriptor": "更有精神且保持自然",
    "rationale": "落实报告优先建议并覆盖日常场景约束",
    "recommended": true,
    "outcome_tags": ["更有精神", "自然", "易执行"],
    "difference_tags": ["发型线条", "浅色层次"],
    "steps": [{
      "category": "hair",
      "action": "adjust",
      "title": "抬高发型重心",
      "summary": "保持原有长度，只整理颅顶和耳侧线条。",
      "details": {
        "target": "颅顶自然蓬松、耳侧线条整洁",
        "intensity": "low",
        "silhouette": "",
        "palette": [],
        "layers": [],
        "avoid": ["不改变发长"],
        "formality": ""
      },
      "groundings": [{
        "source_type": "report_finding",
        "source_id": "20000000-0000-0000-0000-000000000001",
        "reason": "报告观察到发型重心偏低"
      }]
    }]
  }]
}
```

Schema 强制 `variants` 恰好 3、每个 `steps` 恰好 3、用户可见文案和 grounding reason 长度 1–240；`details` 中不适用于当前 category 的字段以 `const:""` 或 `maxItems:0` 表达，适用字段长度 1–160；数组去重且最多 8 项。更复杂的 key/slot/category 集合和引用存在性由 Task 4 的确定性 Gate 校验。

- [ ] **Step 4: Implement adapter without vendor knowledge**

`planning.go` 只依赖：

```go
type StructuredRuntime interface {
	Structured(ctx context.Context, request StructuredRequest) (StructuredResult, error)
}
```

第一次 Prompt 包含完整 ReportSnapshot、profile snapshot、规范化 Brief 和 style rule IDs；第二次 Prompt 额外包含 `PriorReasonCodes`，明确只修复被拒原因。不得提供本地模板或 Demo fallback。返回值只暴露 `InvocationID` 和结构化 candidate，不暴露 vendor/model。

- [ ] **Step 5: Register exact capabilities**

两个 routing 文件都加入 `plan_set_generation` 与 `plan_grounding_verification`。生产配置必须引用已批准的结构化文字模型，`fallbacks` 只能是同能力文字模型；不得引用任何 `demo` key。业务代码只写 capability 常量。

- [ ] **Step 6: Run tests and verify pass**

Run: `cd apps/server && go test ./internal/provider/ai -run 'TestPlanSetGenerator' -count=1`

Expected: PASS.

- [ ] **Step 7: Suggested commit**

```bash
git add apps/server/internal/provider/ai apps/server/config/ai-routing.example.json apps/server/config/ai-routing.production.json
git commit -m "feat(planning): add structured plan set generator"
```

### Task 4: Implement Structural, Grounding, Difference and Consistency Gates

**Files:**
- Create: `apps/server/internal/service/planning/validation.go`
- Test: `apps/server/internal/service/planning/validation_test.go`
- Create: `apps/server/internal/provider/ai/schemas/plan_verification.v1.json`
- Modify: `apps/server/internal/provider/ai/planning.go`
- Test: `apps/server/internal/provider/ai/planning_test.go`

**Interfaces:**
- Consumes: `GeneratedPlanSet`、`ReportSnapshot`、`SceneBrief`。
- Produces: `ValidateCandidate(input ValidationInput) []Violation` and `PlanSetVerifier.Verify(...)`。

- [ ] **Step 1: Write failing Gate tests**

```go
func TestValidateCandidateRequiresTwoPairwiseDifferences(t *testing.T) {
	input := validValidationInput()
	input.Candidate.Variants[1].Steps = cloneSteps(input.Candidate.Variants[0].Steps)
	got := ValidateCandidate(input)
	if !hasViolation(got, "plan.difference_insufficient") {
		t.Fatalf("violations = %#v", got)
	}
}

func TestValidateCandidateRequiresEveryStepGrounded(t *testing.T) {
	input := validValidationInput()
	input.Candidate.Variants[0].Steps[0].Groundings = nil
	if !hasViolation(ValidateCandidate(input), "plan.step_grounding_missing") {
		t.Fatal("missing grounding passed")
	}
}

func TestValidateCandidateCoversPriorityFindingAndEveryBriefAnswer(t *testing.T) {
	input := validValidationInput()
	removeGrounding(&input.Candidate, domain.SourceReportFinding, input.Report.PriorityFindingID)
	removeGrounding(&input.Candidate, domain.SourceSceneAnswer, "weather")
	got := ValidateCandidate(input)
	if !hasViolation(got, "plan.report_priority_uncovered") || !hasViolation(got, "plan.scene_constraint_uncovered") {
		t.Fatalf("violations = %#v", got)
	}
}
```

- [ ] **Step 2: Run focused tests and verify failure**

Run: `cd apps/server && go test ./internal/service/planning -run 'TestValidateCandidate' -count=1`

Expected: FAIL with missing `ValidateCandidate`.

- [ ] **Step 3: Implement deterministic validation**

返回 reason code 必须稳定排序并去重。完整 code 集：

- `plan.variant_count`
- `plan.variant_key_set`
- `plan.variant_slot_set`
- `plan.recommended_count`
- `plan.step_category_set`
- `plan.step_action_invalid`
- `plan.step_details_invalid`
- `plan.step_grounding_missing`
- `plan.grounding_unknown_source`
- `plan.grounding_unknown_id`
- `plan.report_priority_uncovered`
- `plan.scene_constraint_uncovered`
- `plan.difference_insufficient`
- `plan.copy_policy_violation`

差异签名固定为：

```go
type DifferenceSignature struct {
	Hair       string
	Makeup     string
	Outfit     string
	Palette    string
	Formality  string
}
```

Hair/Makeup 使用 `action + normalized target + intensity`；Outfit 使用 `action + normalized silhouette + sorted layers`；Palette 使用排序去重颜色；Formality 使用规范化值。任意 pair 的不等字段数小于 2 即拒绝。copy policy 至少拒绝 `颜值`、`身材分`、`缺陷严重`、`医学诊断`、`年龄判定`、`族裔`，并拒绝价格、品牌和材质断言没有对应 grounding 的文本。

- [ ] **Step 4: Add semantic verification Contract**

`plan_verification.v1.json` 固定输出：

```json
{
  "decision": "pass",
  "reason_codes": [],
  "violations": []
}
```

`decision` 只允许 `pass|reject`；reject code 只允许：

- `plan.report_contradiction`
- `plan.unsupported_wardrobe_claim`
- `plan.unsupported_brand_claim`
- `plan.unsupported_price_claim`
- `plan.unsupported_material_claim`
- `plan.unsupported_body_claim`
- `plan.scene_mismatch`

Verifier 输入必须同时包含 Report observation/recommendation、Brief、candidate 和所有 grounding；不得包含图片。Verifier 只能在确定性 Gate 无 violation 时调用，避免为明显坏结构付费。

- [ ] **Step 5: Run Gate and Provider tests**

Run: `cd apps/server && go test ./internal/service/planning ./internal/provider/ai -run 'TestValidateCandidate|TestPlanSetVerifier' -count=1`

Expected: PASS.

- [ ] **Step 6: Suggested commit**

```bash
git add apps/server/internal/service/planning/validation.go apps/server/internal/service/planning/validation_test.go apps/server/internal/provider/ai
git commit -m "feat(planning): enforce plan quality gates"
```

### Task 5: Build RenderSpec Schema and Deterministic Compiler

**Files:**
- Modify: `apps/server/internal/domain/planning.go`
- Create: `apps/server/internal/service/planning/schemas/render_spec.v1.json`
- Create: `apps/server/internal/service/planning/compiler.go`
- Test: `apps/server/internal/service/planning/compiler_test.go`

**Interfaces:**
- Consumes: a Gate-passed PlanSet plus Report face/body/photo-set IDs.
- Produces: `CompileRenderSpecs(planSet, report) ([]domain.RenderSpec, error)` and `ValidateRenderSpec(spec) error`。

- [ ] **Step 1: Write failing exact-output tests**

```go
func TestCompileRenderSpecsPreservesIdentityCompositionAndBodyAspect(t *testing.T) {
	planSet := validDomainPlanSet()
	report := validPlanningReport()
	specs, err := CompileRenderSpecs(planSet, report)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 3 {
		t.Fatalf("got %d specs", len(specs))
	}
	for _, item := range specs {
		spec := item.Spec
		if spec.Identity.FaceAssetID != report.FaceAssetID || spec.Identity.BodyAssetID != report.BodyAssetID {
			t.Fatalf("identity source mismatch: %#v", spec.Identity)
		}
		if !spec.Identity.PreserveIdentity || !spec.Identity.PreserveBodyProportion || !spec.Identity.PreserveSkinTone || !spec.Identity.PreserveAgeImpression {
			t.Fatal("identity preservation must be fail-closed")
		}
		if !spec.Composition.PreservePose || !spec.Composition.PreserveBackground || !spec.Composition.PreserveLighting || !spec.Composition.PreserveSourceCrop || spec.Composition.AllowOutpaint {
			t.Fatalf("unsafe composition: %#v", spec.Composition)
		}
		if spec.Output.MIMEType != "image/jpeg" || spec.Output.AspectPolicy != "preserve_body_source" || spec.Output.Quality != "high" {
			t.Fatalf("unsafe output: %#v", spec.Output)
		}
	}
}

func TestCompileRenderSpecsDoesNotInventKeepAdjustments(t *testing.T) {
	planSet := validDomainPlanSet()
	planSet.Variants[0].Steps[0].Action = domain.ActionKeep
	planSet.Variants[0].Steps[0].Details.Target = "保持原发型，只整理碎发"
	specs, err := CompileRenderSpecs(planSet, validPlanningReport())
	if err != nil {
		t.Fatal(err)
	}
	if specs[0].Spec.Hair.Action != "keep" || specs[0].Spec.Hair.Target != "保持原发型，只整理碎发" {
		t.Fatalf("keep was changed: %#v", specs[0].Spec.Hair)
	}
}
```

- [ ] **Step 2: Run focused tests and verify failure**

Run: `cd apps/server && go test ./internal/service/planning -run 'TestCompileRenderSpecs' -count=1`

Expected: FAIL with missing `CompileRenderSpecs`.

- [ ] **Step 3: Define typed RenderSpec and JSON Schema**

`domain.RenderDirective` JSON 必须精确包含：

```json
{
  "identity": {
    "body_asset_id": "40000000-0000-0000-0000-000000000001",
    "face_asset_id": "40000000-0000-0000-0000-000000000002",
    "preserve_identity": true,
    "preserve_body_proportion": true,
    "preserve_skin_tone": true,
    "preserve_age_impression": true
  },
  "composition": {
    "preserve_pose": true,
    "preserve_background": true,
    "preserve_lighting": true,
    "preserve_source_crop": true,
    "allow_outpaint": false
  },
  "hair": {"action": "keep", "target": "保持原发型", "intensity": "low"},
  "makeup": {"action": "adjust", "target": "清晰眉形", "intensity": "low"},
  "outfit": {
    "silhouette": "合肩直线版型",
    "palette": ["象牙白", "深灰"],
    "layers": ["浅色内搭", "深色外层"],
    "avoid": ["夸张图案"]
  },
  "output": {
    "mime_type": "image/jpeg",
    "aspect_policy": "preserve_body_source",
    "quality": "high"
  }
}
```

Schema 必须 `additionalProperties:false`；四个 preserve 值只能为 `true`，`allow_outpaint` 只能为 `false`，MIME 只能为 JPEG，hair/makeup intensity 只允许 `low|medium`，禁止 `high`。

领域类型签名固定为：

```go
type RenderSpec struct {
	ID string
	UserID string
	PlanVariantID string
	SourcePhotoSetID string
	SchemaVersion string
	Spec RenderDirective
	ContentHash string
	CreatedAt time.Time
}

type RenderDirective struct {
	Identity RenderIdentity `json:"identity"`
	Composition RenderComposition `json:"composition"`
	Hair RenderHair `json:"hair"`
	Makeup RenderMakeup `json:"makeup"`
	Outfit RenderOutfit `json:"outfit"`
	Output RenderOutput `json:"output"`
}

type RenderIdentity struct {
	BodyAssetID string `json:"body_asset_id"`
	FaceAssetID string `json:"face_asset_id"`
	PreserveIdentity bool `json:"preserve_identity"`
	PreserveBodyProportion bool `json:"preserve_body_proportion"`
	PreserveSkinTone bool `json:"preserve_skin_tone"`
	PreserveAgeImpression bool `json:"preserve_age_impression"`
}

type RenderComposition struct {
	PreservePose bool `json:"preserve_pose"`
	PreserveBackground bool `json:"preserve_background"`
	PreserveLighting bool `json:"preserve_lighting"`
	PreserveSourceCrop bool `json:"preserve_source_crop"`
	AllowOutpaint bool `json:"allow_outpaint"`
}

type RenderHair struct {
	Action string `json:"action"`
	Target string `json:"target"`
	Intensity string `json:"intensity"`
}

type RenderMakeup struct {
	Action string `json:"action"`
	Target string `json:"target"`
	Intensity string `json:"intensity"`
}

type RenderOutfit struct {
	Silhouette string `json:"silhouette"`
	Palette []string `json:"palette"`
	Layers []string `json:"layers"`
	Avoid []string `json:"avoid"`
}

type RenderOutput struct {
	MIMEType string `json:"mime_type"`
	AspectPolicy string `json:"aspect_policy"`
	Quality string `json:"quality"`
}
```

- [ ] **Step 4: Implement pure compiler and canonical content hash**

编译器只从 typed StepDetails 映射，不拼接页面文案，不调用 AI。每个 Variant 生成一个新 RenderSpec ID；`source_photo_set_id` 使用 Report 的 PhotoSetID；`content_hash` 对不含数据库 `id/user_id/created_at` 的 canonical `spec` 计算 SHA-256。编译结果按 Variant slot 1/2/3 排序。

- [ ] **Step 5: Run tests and verify pass**

Run: `cd apps/server && go test ./internal/service/planning -run 'TestCompileRenderSpecs|TestValidateRenderSpec' -count=1`

Expected: PASS.

- [ ] **Step 6: Suggested commit**

```bash
git add apps/server/internal/domain/planning.go apps/server/internal/service/planning/compiler.go apps/server/internal/service/planning/compiler_test.go apps/server/internal/service/planning/schemas/render_spec.v1.json
git commit -m "feat(planning): compile immutable render specs"
```

### Task 6: Implement the Leased Planning Handler

**Files:**
- Create: `apps/server/internal/service/planning/worker.go`
- Test: `apps/server/internal/service/planning/worker_test.go`

**Interfaces:**
- Consumes: `PlanSetGenerator`、`PlanSetVerifier`、`TaskEnqueuer`、`OperationWriter`、`PlanSetStore`、Compiler，以及基础计划的 `taskrunner.Handler`。
- Produces: `Handler.Type/Execute/Commit`，Task type 固定 `plan_set.generate`，payload version 固定 `1`。

- [ ] **Step 1: Write failing worker behavior tests**

```go
func TestHandlerPreparesOnlyAfterAllGatesPass(t *testing.T) {
	deps := validHandlerDependencies()
	handler := NewHandler(deps)
	lease := validGenerateLease(1)
	result, err := handler.Execute(context.Background(), lease)
	if err != nil {
		t.Fatal(err)
	}
	if deps.store.prepareCalls != 1 || len(deps.store.command.RenderSpecs) != 3 {
		t.Fatalf("prepare command = %#v", deps.store.command)
	}
	if deps.enqueuer.calls != 0 {
		t.Fatal("passing candidate must not consume content retry")
	}
	outcome, err := handler.Commit(context.Background(), lease, result)
	if err != nil || outcome != domain.CommitApplied {
		t.Fatalf("outcome=%s err=%v", outcome, err)
	}
}

func TestHandlerEnqueuesSecondContentAttemptAfterQualityReject(t *testing.T) {
	deps := validHandlerDependencies()
	deps.generator.output = invalidDifferenceCandidate()
	result, err := NewHandler(deps).Execute(context.Background(), validGenerateLease(1))
	if err != nil {
		t.Fatal(err)
	}
	if result.ResultType != "plan_set_attempt" {
		t.Fatalf("result = %#v", result)
	}
	if result.Disposition != domain.TaskEnqueueNext || deps.store.prepareCalls != 0 || deps.enqueuer.calls != 0 {
		t.Fatalf("prepare=%d enqueue=%d", deps.store.prepareCalls, deps.enqueuer.calls)
	}
	outcome, err := NewHandler(deps).Commit(context.Background(), validGenerateLease(1), result)
	if err != nil || outcome != domain.CommitApplied || deps.enqueuer.calls != 1 {
		t.Fatalf("commit outcome=%s enqueue=%d err=%v", outcome, deps.enqueuer.calls, err)
	}
	if deps.enqueuer.command.Task.Payload.(GenerateTaskPayload).ContentAttempt != 2 {
		t.Fatalf("retry payload = %#v", deps.enqueuer.command.Task.Payload)
	}
}

func TestHandlerFailsClosedAfterSecondRejectedCandidate(t *testing.T) {
	deps := validHandlerDependencies()
	deps.generator.output = invalidDifferenceCandidate()
	_, err := NewHandler(deps).Execute(context.Background(), validGenerateLease(2))
	if !errors.Is(err, ErrQualityRejected) {
		t.Fatalf("got %v", err)
	}
	if deps.store.prepareCalls != 0 || deps.operations.failCalls != 1 {
		t.Fatal("rejected candidate must never publish")
	}
}
```

- [ ] **Step 2: Run focused tests and verify failure**

Run: `cd apps/server && go test ./internal/service/planning -run 'TestHandler' -count=1`

Expected: FAIL with missing `NewHandler`/`Execute`/`Commit`.

- [ ] **Step 3: Implement the exact state machine**

```text
read immutable Report
  → lease-guarded MarkRunning(1500, "plan.reading_report")
  → Generate outside transaction
  → lease-guarded MarkRunning(6500, "plan.checking")
  → deterministic Gate
  → semantic Verifier when deterministic Gate passed
  → reject content attempt 1: stage disposition=enqueue_next and prior reason codes
  → reject content attempt 2: stage disposition=domain_failed
  → pass: materialize IDs, compile 3 RenderSpecs, stage disposition=publish
  → taskrunner invokes Commit
  → Commit validates lease token and atomically publishes, enqueues attempt 2, or fails Operation
```

Generator 或 Verifier 的 transient/throttled error 直接返回给 Task runner，由同一个 Task 的基础设施重试；不得增加 `ContentAttempt`。Schema/contract error 属于 permanent；质量 violation 属于 quality_rejected。`Execute` 成功返回 `domain.TaskResult{ResultType:"plan_set",ResultID:PlanSet.ID}`；`CommitPrepared` 才能把 progress 置为 10000、stage 置为 `plan.ready`、public message 置为 `三套方案已准备好`。失效 lease 返回 `domain.CommitSuperseded`，prepared graph 保持不可读，不能推进 Operation。

- [ ] **Step 4: Verify retry dedupe**

第二内容任务 dedupe key 固定为初始 semantic dedupe key 加 `:content:2`。`Execute` 返回 `TaskResult{Disposition:TaskEnqueueNext,ResultType:"plan_set_attempt",ResultID:AttemptID}`；`Commit` 在同一 lease CAS 事务中结束当前 Task、把 Operation 置为 retrying 并插入第二内容 Task，然后返回 `CommitApplied`。第二次仍失败返回 `TaskDomainFail`，由 `Commit` 原子失败 Operation。`CommitSuperseded` 只表示真实 lease/generation 过期。

- [ ] **Step 5: Run tests and verify pass**

Run: `cd apps/server && go test ./internal/service/planning -run 'TestHandler' -count=1`

Expected: PASS.

- [ ] **Step 6: Suggested commit**

```bash
git add apps/server/internal/service/planning/worker.go apps/server/internal/service/planning/worker_test.go
git commit -m "feat(planning): add leased plan set handler"
```

### Task 7: Add Immutable PostgreSQL Schema and Adapter

**Files:**
- Verify: `apps/server/internal/database/migrations/001_baseline.sql`
- Create: `apps/server/internal/repository/postgres/planning.go`
- Test: `apps/server/internal/repository/postgres/planning_integration_test.go`

**Interfaces:**
- Consumes: `ReportReader`、`PlanSetStore`、`RenderSpecReader`。
- Produces: atomic graph publication and tenant-scoped reads.

- [ ] **Step 1: Write failing PostgreSQL integration tests**

```go
func TestPrepareAndCommitPlanSetIsAtomicImmutableAndTenantScoped(t *testing.T) {
	store, users := newPlanningStore(t)
	lease := validDatabaseLease(t, store.pool, users.A)
	command := validPrepareCommand(users.A)
	got, err := store.Prepare(context.Background(), lease, command)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Get(context.Background(), users.A, got.ID)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("prepared result became visible before commit: %v", err)
	}
	outcome, err := store.CommitPrepared(context.Background(), lease, domain.TaskResult{ResultType:"plan_set", ResultID:got.ID})
	if err != nil || outcome != domain.CommitApplied {
		t.Fatalf("outcome=%s err=%v", outcome, err)
	}
	if len(got.Variants) != 3 || totalSteps(got) != 9 || totalGroundings(got) < 9 {
		t.Fatalf("incomplete graph: %#v", got)
	}
	_, err = store.Get(context.Background(), users.B, got.ID)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("cross-tenant read = %v", err)
	}
	_, err = store.pool.Exec(context.Background(), `UPDATE plan_sets SET scene='date' WHERE id=$1 AND user_id=$2`, got.ID, users.A)
	if pgCode(err) != "55000" {
		t.Fatalf("immutable update code = %q, err=%v", pgCode(err), err)
	}
}

func TestPreparePlanSetRejectsIncompleteGraph(t *testing.T) {
	store, users := newPlanningStore(t)
	lease := validDatabaseLease(t, store.pool, users.A)
	command := validPrepareCommand(users.A)
	command.PlanSet.Variants = command.PlanSet.Variants[:2]
	_, err := store.Prepare(context.Background(), lease, command)
	if err == nil {
		t.Fatal("two variants committed")
	}
	assertTableCount(t, store.pool, "plan_sets", 0)
	assertOperationNotSucceeded(t, store.pool, lease.OperationID)
}
```

- [ ] **Step 2: Run focused integration tests and verify failure**

Run: `cd apps/server && TEST_DATABASE_URL='postgres://jianwo:jianwo@127.0.0.1:55432/postgres?sslmode=disable' go test ./internal/repository/postgres -run 'TestPrepareAndCommitPlanSet|TestPreparePlanSetRejects' -count=1`

Expected: FAIL because Planning tables/adapter do not exist.

- [ ] **Step 3: Add exact baseline tables and constraints**

在 `reports`、`provider_invocations`、`quality_evaluations`、`operations` 已创建后执行：

```sql
CREATE TABLE plan_sets (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL,
  report_id uuid NOT NULL,
  profile_snapshot jsonb NOT NULL,
  scene text NOT NULL CHECK (scene IN ('general','interview','wedding','date','daily','gathering')),
  scene_brief jsonb NOT NULL,
  brief_hash text NOT NULL CHECK (brief_hash ~ '^[0-9a-f]{64}$'),
  planner_schema_version text NOT NULL,
  style_rule_version text NOT NULL,
  provider_invocation_id uuid NOT NULL,
  quality_evaluation_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, report_id, scene, brief_hash, planner_schema_version),
  FOREIGN KEY (user_id, report_id) REFERENCES reports(user_id, id),
  FOREIGN KEY (user_id, provider_invocation_id) REFERENCES provider_invocations(user_id, id),
  FOREIGN KEY (user_id, quality_evaluation_id) REFERENCES quality_evaluations(user_id, id)
);

CREATE TABLE plan_variants (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL,
  plan_set_id uuid NOT NULL,
  slot smallint NOT NULL CHECK (slot BETWEEN 1 AND 3),
  key text NOT NULL CHECK (key IN ('sharp','warm','natural')),
  name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 80),
  descriptor text NOT NULL CHECK (length(btrim(descriptor)) BETWEEN 1 AND 160),
  rationale text NOT NULL CHECK (length(btrim(rationale)) BETWEEN 1 AND 240),
  recommended boolean NOT NULL,
  outcome_tags text[] NOT NULL,
  difference_tags text[] NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, plan_set_id, slot),
  UNIQUE (user_id, plan_set_id, key),
  FOREIGN KEY (user_id, plan_set_id)
    REFERENCES plan_sets(user_id, id) ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED
);

CREATE TABLE plan_steps (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL,
  plan_variant_id uuid NOT NULL,
  category text NOT NULL CHECK (category IN ('hair','makeup','outfit')),
  action text NOT NULL CHECK (action IN ('keep','adjust')),
  title text NOT NULL CHECK (length(btrim(title)) BETWEEN 1 AND 120),
  summary text NOT NULL CHECK (length(btrim(summary)) BETWEEN 1 AND 240),
  details jsonb NOT NULL,
  position smallint NOT NULL CHECK (position BETWEEN 1 AND 3),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, plan_variant_id, category),
  UNIQUE (user_id, plan_variant_id, position),
  FOREIGN KEY (user_id, plan_variant_id)
    REFERENCES plan_variants(user_id, id) ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED
);

CREATE TABLE plan_step_groundings (
  user_id uuid NOT NULL,
  plan_step_id uuid NOT NULL,
  source_type text NOT NULL CHECK (source_type IN (
    'report_finding','scene_answer','profile_preference','style_rule','feedback_memory'
  )),
  source_id text NOT NULL CHECK (length(btrim(source_id)) BETWEEN 1 AND 200),
  reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 240),
  PRIMARY KEY (user_id, plan_step_id, source_type, source_id),
  FOREIGN KEY (user_id, plan_step_id)
    REFERENCES plan_steps(user_id, id) ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED
);

CREATE TABLE render_specs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL,
  plan_variant_id uuid NOT NULL,
  source_photo_set_id uuid NOT NULL,
  schema_version text NOT NULL,
  spec jsonb NOT NULL,
  content_hash text NOT NULL CHECK (content_hash ~ '^[0-9a-f]{64}$'),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, plan_variant_id),
  FOREIGN KEY (user_id, plan_variant_id)
    REFERENCES plan_variants(user_id, id) ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY (user_id, source_photo_set_id)
    REFERENCES photo_sets(user_id, id) DEFERRABLE INITIALLY DEFERRED
);

CREATE INDEX plan_sets_report_scene_created_idx
  ON plan_sets(user_id, report_id, scene, created_at DESC, id DESC);
CREATE INDEX plan_variants_set_slot_idx
  ON plan_variants(user_id, plan_set_id, slot);
CREATE INDEX plan_steps_variant_position_idx
  ON plan_steps(user_id, plan_variant_id, position);

CREATE FUNCTION assert_plan_set_complete() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF (SELECT count(*) FROM plan_variants WHERE user_id=NEW.user_id AND plan_set_id=NEW.id) <> 3
     OR (SELECT count(*) FROM plan_variants WHERE user_id=NEW.user_id AND plan_set_id=NEW.id AND recommended) <> 1
     OR EXISTS (
       SELECT 1 FROM plan_variants v
       WHERE v.user_id=NEW.user_id AND v.plan_set_id=NEW.id
         AND (
           (SELECT count(*) FROM plan_steps s WHERE s.user_id=v.user_id AND s.plan_variant_id=v.id) <> 3
           OR (SELECT count(DISTINCT s.category) FROM plan_steps s WHERE s.user_id=v.user_id AND s.plan_variant_id=v.id) <> 3
           OR (SELECT count(*) FROM render_specs r WHERE r.user_id=v.user_id AND r.plan_variant_id=v.id) <> 1
         )
     )
     OR EXISTS (
       SELECT 1
       FROM plan_steps s
       JOIN plan_variants v ON v.user_id=s.user_id AND v.id=s.plan_variant_id
       WHERE v.user_id=NEW.user_id AND v.plan_set_id=NEW.id
         AND NOT EXISTS (
           SELECT 1 FROM plan_step_groundings g
           WHERE g.user_id=s.user_id AND g.plan_step_id=s.id
         )
     )
  THEN
    RAISE EXCEPTION 'incomplete plan set %', NEW.id USING ERRCODE='23514';
  END IF;
  RETURN NEW;
END $$;

CREATE CONSTRAINT TRIGGER plan_sets_complete
AFTER INSERT ON plan_sets
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION assert_plan_set_complete();

CREATE FUNCTION reject_immutable_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'immutable planning artifact' USING ERRCODE='55000';
END $$;

CREATE TRIGGER plan_sets_no_update BEFORE UPDATE ON plan_sets
FOR EACH ROW EXECUTE FUNCTION reject_immutable_update();
CREATE TRIGGER plan_variants_no_update BEFORE UPDATE ON plan_variants
FOR EACH ROW EXECUTE FUNCTION reject_immutable_update();
CREATE TRIGGER plan_steps_no_update BEFORE UPDATE ON plan_steps
FOR EACH ROW EXECUTE FUNCTION reject_immutable_update();
CREATE TRIGGER plan_groundings_no_update BEFORE UPDATE ON plan_step_groundings
FOR EACH ROW EXECUTE FUNCTION reject_immutable_update();
CREATE TRIGGER render_specs_no_update BEFORE UPDATE ON render_specs
FOR EACH ROW EXECUTE FUNCTION reject_immutable_update();
```

`quality_evaluations` 的不可变 UPDATE trigger 由 Assessment/Report 计划在同一 baseline 中创建；本任务不得重复定义函数或 trigger。DELETE 保留给账户数据清除级联。

- [ ] **Step 4: Implement one-transaction publication**

`Prepare` 使用 `pgx.BeginTx`，先校验 `TaskLease` 仍有效，再按 quality evaluation → plan set → variants → steps → groundings → render specs 的顺序插入。任何错误 rollback。`Get/List/FindPublished` 只返回 subject 指向该 PlanSet 且 status 为 succeeded 的 Operation 对应结果。`CommitPrepared` 在单一事务中再次校验 `(task_id,lease_token,lease_owner,status='leased')`、Task subject 与 ResultID 一致，然后完成 Task、发布 PlanSet 并成功 Operation；失效 lease 返回 `domain.CommitSuperseded`。并发 unique conflict 后允许相同 subject 的新 lease 复用已 prepared 且通过完整约束的图，或复用已 succeeded 的同 semantic key 结果；不得比较后更新，也不得覆盖。

所有读查询必须包含 `WHERE <table>.user_id=$1 AND <table>.id=$2`；加载图时固定按 slot、step position 排序。`GetRenderSpecForVariant` 只返回 Schema 已验证的 typed document。

- [ ] **Step 5: Run integration tests and verify pass**

Run: `cd apps/server && TEST_DATABASE_URL='postgres://jianwo:jianwo@127.0.0.1:55432/postgres?sslmode=disable' go test ./internal/repository/postgres -run 'TestPrepareAndCommitPlanSet|TestPreparePlanSetRejects|TestPlanningTenant|TestRenderSpecReader' -count=1`

Expected: PASS.

- [ ] **Step 6: Suggested commit**

```bash
git add apps/server/internal/database/migrations/001_baseline.sql apps/server/internal/repository/postgres/planning.go apps/server/internal/repository/postgres/planning_integration_test.go
git commit -m "feat(planning): persist immutable plan sets"
```

### Task 8: Expose the New PlanSet API

**Files:**
- Create: `apps/server/internal/httpapi/plan_sets.go`
- Create: `apps/server/internal/httpapi/plan_sets_test.go`
- Modify: `apps/server/internal/httpapi/httpapi.go`
- Modify: `contracts/openapi.yaml`
- Create: `packages/core/src/types/planning.ts`
- Modify: `packages/core/src/types/index.ts`
- Modify: `packages/core/src/api/endpoints.ts`

**Interfaces:**
- Consumes: `CreatePlanSet`、`GetPlanSet`、`ListPlanSets`。
- Produces: `POST /v1/plan-sets`、`GET /v1/plan-sets/{id}`、`GET /v1/plan-sets?report_id=&scene=`。

- [ ] **Step 1: Write failing HTTP contract tests**

```go
func TestPostPlanSetsReturns202OperationEnvelope(t *testing.T) {
	api := newPlanSetAPI(t, fakePlanSetService{create: planning.CreateResult{
		PlanSetID: "10000000-0000-0000-0000-000000000001",
		Accepted: true,
		Operation: domain.OperationRef{ID: "30000000-0000-0000-0000-000000000001", Kind: "plan_set", Status: "accepted"},
	}})
	body := `{"report_id":"20000000-0000-0000-0000-000000000001","scene":"daily","brief":{"activity":"office","weather":"air_conditioned","preparation":"closet","impression":"natural"}}`
	req := authenticatedRequest(http.MethodPost, "/v1/plan-sets", body)
	req.Header.Set("Idempotency-Key", "plan-daily-1")
	res := httptest.NewRecorder()
	api.ServeHTTP(res, req)
	if res.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	assertJSONEqual(t, res.Body.Bytes(), `{"data":{"id":"10000000-0000-0000-0000-000000000001","state":"planning"},"operation":{"id":"30000000-0000-0000-0000-000000000001","status":"accepted"}}`)
}

func TestPostPlanSetsRejectsMissingIdempotencyKey(t *testing.T) {
	api := newPlanSetAPI(t, fakePlanSetService{})
	res := httptest.NewRecorder()
	api.ServeHTTP(res, authenticatedRequest(http.MethodPost, "/v1/plan-sets", `{}`))
	if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), `"code":"idempotency_key_required"`) {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
}
```

- [ ] **Step 2: Run focused tests and verify failure**

Run: `cd apps/server && go test ./internal/httpapi -run 'TestPostPlanSets|TestGetPlanSet|TestListPlanSets' -count=1`

Expected: FAIL because routes/handlers do not exist.

- [ ] **Step 3: Implement transport-only handlers**

POST request:

```json
{
  "report_id": "20000000-0000-0000-0000-000000000001",
  "scene": "daily",
  "brief": {
    "activity": "office",
    "weather": "air_conditioned",
    "preparation": "closet",
    "impression": "natural"
  }
}
```

新/进行中请求返回 202；已发布 semantic key 返回 200 完整 PlanSet。GET by ID 返回完整不可变图；list 强制 `report_id`，可选 scene，按 `created_at desc,id desc`。请求未知字段、无效 UUID、无 Idempotency-Key、未知 scene 均为 400。Repository not found/越权均为 404。质量最终失败只通过 Operation 暴露，不创建 failed PlanSet。

- [ ] **Step 4: Replace OpenAPI paths and schemas**

删除旧 `/v1/reports/{id}/plans`、`/v1/plans/{id}`、look regenerate、select、checklist、old feedback paths。加入 `PlanSet`、`PlanVariant`、`PlanStep`、`PlanStepGrounding`、`PlanSetCreateRequest`、`OperationRef`；API 不暴露 RenderSpec、grounding 的内部 reason 之外的内部质量分数、Provider 信息。

- [ ] **Step 5: Replace cross-client endpoint and type contracts**

`packages/core/src/api/endpoints.ts` 删除 8 个旧 Plan 方法和路径，加入：

```ts
export interface OperationCreated<T> {
  data: T
  operation: OperationRef
}

createPlanSet(input: CreatePlanSetInput, idempotencyKey: string): Promise<OperationCreated<{ id: string; state: 'planning' }>>
getPlanSet(id: string): Promise<PlanSet>
listPlanSets(reportId: string, scene?: Scene): Promise<PlanSet[]>
```

`createPlanSet` 必须通过现有 `ApiClient` 传 `Idempotency-Key` header 并保留顶层 `operation`，不能用只解包 `data` 的普通 `request()`。`API_PATHS` 固定为 `POST /v1/plan-sets`、`GET /v1/plan-sets/{id}`、`GET /v1/plan-sets`。`planning.ts` 与 OpenAPI 同名字段一一对应，不包含旧 `Plan`、`generation_status`、`generated_image_url`、`selected`、`look_task`。这里只更新跨端 contract/client，不改小程序页面或状态管理。

- [ ] **Step 6: Run HTTP and contract sync tests**

Run: `cd apps/server && go test ./internal/httpapi -run 'TestPostPlanSets|TestGetPlanSet|TestListPlanSets' -count=1`

Expected: PASS.

Run: `node contracts/scripts/check-sync.mjs`

Expected: PASS with `契约与 core 端点完全一致`。

- [ ] **Step 7: Suggested commit**

```bash
git add apps/server/internal/httpapi/plan_sets.go apps/server/internal/httpapi/plan_sets_test.go apps/server/internal/httpapi/httpapi.go contracts/openapi.yaml packages/core/src/api/endpoints.ts packages/core/src/types
git commit -m "feat(api): replace plans with plan sets"
```

### Task 9: Register Planning in API/Worker Bootstrap and Remove Legacy Paths

**Files:**
- Modify: `apps/server/internal/bootstrap/api.go`
- Modify: `apps/server/internal/bootstrap/worker.go`
- Modify: `apps/server/internal/httpapi/httpapi.go`
- Modify: `apps/server/internal/service/tasks.go`
- Modify: `apps/server/internal/service/service.go`
- Modify: `apps/server/internal/repository/repository.go`
- Modify: `apps/server/internal/repository/postgres/postgres.go`
- Modify: `apps/server/internal/bootstrap/ai.go`
- Modify: `apps/server/internal/service/product_loops.go`
- Delete: `apps/server/internal/provider/plan_group.go`
- Delete: `apps/server/internal/service/scene_plans.go`
- Delete: `apps/server/internal/service/scene_plans_test.go`
- Delete: `apps/server/internal/service/plans.go`
- Delete: `apps/server/internal/httpapi/plans.go`
- Test: `apps/server/internal/bootstrap/planning_test.go`

**Interfaces:**
- Consumes: Generator、Verifier、Postgres adapters、Operation/Task services。
- Produces: one Planning API Service and one registered `plan_set.generate` handler.

- [ ] **Step 1: Write failing wiring tests**

```go
func TestWorkerRegistersPlanSetGenerationHandler(t *testing.T) {
	registry := taskrunner.NewRegistry()
	err := RegisterPlanningWorker(registry, validPlanningWorkerDependencies())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Handler(planning.PlanSetGenerationTaskType); !ok {
		t.Fatal("plan_set.generate handler not registered")
	}
}

func TestAPIDoesNotRegisterLegacyPlanRoutes(t *testing.T) {
	handler := buildTestAPI(t)
	for _, path := range []string{"/v1/reports/x/plans", "/v1/plans/x", "/v1/plans/x/look/regenerate"} {
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, authenticatedRequest(http.MethodGet, path, ""))
		if res.Code != http.StatusNotFound {
			t.Fatalf("%s status=%d", path, res.Code)
		}
	}
}
```

- [ ] **Step 2: Run wiring tests and verify failure**

Run: `cd apps/server && go test ./internal/bootstrap -run 'TestWorkerRegistersPlanSet|TestAPIDoesNotRegisterLegacy' -count=1`

Expected: FAIL because Planning is not wired and legacy routes still exist.

- [ ] **Step 3: Wire exact dependencies**

API 进程构造 read/create Planning Service，不启动 Worker。Worker 进程构造 Generator、Verifier、Planning Worker，并通过 Registry 注册任务。`cmd/api` 生产环境不得内嵌 Worker；如果基础设施计划已经删除 `RUN_WORKER`，本任务不重新引入。

Registry Definition 固定为：

```go
taskrunner.Definition{
	Type: planning.PlanSetGenerationTaskType,
	MaxAttempts: 3,
	Timeout: 90 * time.Second,
	LeaseDuration: 45 * time.Second,
	HeartbeatEvery: 15 * time.Second,
	Concurrency: 2,
	RetryBackoff: taskrunner.ExponentialBackoff(5*time.Second, time.Minute),
}
```

- [ ] **Step 4: Delete legacy implementation**

删除 `PlanGroupGenerator`、Demo plan group、`buildScenePlans`、`UpsertScenePlans`、`LatestTasksByRef` planning 调用、`plan_group`/`plan_look` Handler 注册和旧路由。不得留下旧 DTO 到新 DTO 的转换器、feature flag、双写、读取 fallback 或 legacy JSON 字段。

- [ ] **Step 5: Run static legacy scan**

Run:

```bash
rg -n 'PlanGroup|TaskTypePlanGroup|TaskTypePlanLook|UpsertScenePlans|buildScenePlans|/v1/reports/\{id\}/plans|/v1/plans/\{id\}' apps/server contracts packages/core
```

Expected: exit 1 with no matches.

- [ ] **Step 6: Run bootstrap and server tests**

Run: `cd apps/server && go test ./internal/bootstrap ./internal/httpapi ./internal/service/planning -count=1`

Expected: PASS.

- [ ] **Step 7: Suggested commit**

```bash
git add -A apps/server/internal apps/server/cmd contracts/openapi.yaml packages/core/src/api/endpoints.ts packages/core/src/types
git commit -m "refactor(planning): remove legacy plan pipeline"
```

### Task 10: Add the Planning Golden Set and Static Evaluation

**Files:**
- Create: `apps/server/eval/planning/golden.go`
- Create: `apps/server/eval/planning/golden_test.go`
- Create: `apps/server/eval/planning/testdata/briefs.v1.jsonl`
- Create: `apps/server/eval/planning/testdata/plan_sets.v1.jsonl`
- Deferred to Peripherals/Cutover: `apps/server/cmd/eval/main.go`

**Interfaces:**
- Consumes: `NormalizeBrief`、`ValidateCandidate`、Generator/Verifier JSON decoders。
- Produces: deterministic PR-CI gold-set checks; no live model call.

- [ ] **Step 1: Write failing count/partition/schema tests**

```go
func TestGoldenSetShapeAndPartitions(t *testing.T) {
	briefs := LoadBriefCases(t, "testdata/briefs.v1.jsonl")
	plans := LoadPlanSetCases(t, "testdata/plan_sets.v1.jsonl")
	if len(briefs) != 120 || len(plans) != 180 {
		t.Fatalf("briefs=%d plans=%d", len(briefs), len(plans))
	}
	assertSplit(t, briefs, map[Split]int{Development: 72, Validation: 24, Release: 24})
	assertSplit(t, plans, map[Split]int{Development: 108, Validation: 36, Release: 36})
	assertNoReportFixtureCrossesSplits(t, briefs, plans)
	assertPerScene(t, briefs, 20)
	assertPerScene(t, plans, 30)
}

func TestGoldenPlanSetGateExpectations(t *testing.T) {
	for _, tc := range LoadPlanSetCases(t, "testdata/plan_sets.v1.jsonl") {
		got := violationCodes(planning.ValidateCandidate(tc.Input))
		if !slices.Equal(got, tc.WantReasonCodes) {
			t.Fatalf("%s got=%v want=%v", tc.ID, got, tc.WantReasonCodes)
		}
	}
}
```

- [ ] **Step 2: Run tests and verify failure**

Run: `cd apps/server && go test ./eval/planning -count=1`

Expected: FAIL because loaders and datasets do not exist.

- [ ] **Step 3: Create exact JSONL record contracts**

Brief record fields固定为：

```json
{"id":"brief-daily-001","split":"development","report_fixture_id":"report-person-001-v1","scene":"daily","answers":{"activity":"office","weather":"air_conditioned","preparation":"closet","impression":"natural"},"valid":true,"want_error_code":""}
```

PlanSet record fields固定为：

```json
{"id":"plans-daily-invalid-001","split":"development","report_fixture_id":"report-person-001-v1","scene":"daily","brief_case_id":"brief-daily-001","candidate":{"variants":[]},"want_reason_codes":["plan.variant_count"]}
```

数据必须覆盖：六场景有效/无效 Brief、3/2/4 variants、重复 key/slot、0/2 recommended、缺 category、keep 路径、缺 grounding、未知 finding、遗漏每一种 scene answer、最高优先 finding 未落实、五个差异维度的边界 pair、禁用文案、报告矛盾核验 fixture。每条负例只标注最小预期 reason code 集，所有 code 按字典序。

- [ ] **Step 4: Populate fixed counts with reviewed fixtures**

每场景 20 个 Brief，其中 16 valid、4 invalid；每场景 30 个 PlanSet，其中 12 pass、18 覆盖上述拒绝原因。development/validation/release 按 report fixture 分组分配，比例严格 60/20/20；同一人不同场景仍使用同一个 `report_fixture_id`，因此只能进入同一 split。JSONL 必须提交为静态数据，不在测试运行时由生产函数生成，避免自证正确。

- [ ] **Step 5: Implement strict loader and run tests**

Loader 使用 `json.Decoder.DisallowUnknownFields()`，拒绝空行、重复 case ID、未知 split/scene/reason code；测试逐行报告 `file:line`。

Run: `cd apps/server && go test ./eval/planning -count=1`

Expected: PASS with all 300 cases loaded and no split leakage.

- [ ] **Step 6: Suggested commit**

```bash
git add apps/server/eval/planning
git commit -m "test(planning): add planning golden set"
```

### Task 11: Final Verification and Plan Boundary Audit

**Files:**
- Verify only; no new production files.

**Interfaces:**
- Consumes: all previous tasks.
- Produces: evidence that Planning is complete without entering image generation scope.

- [ ] **Step 1: Run focused Planning tests**

Run:

```bash
cd apps/server && go test ./internal/service/planning ./internal/provider/ai ./internal/httpapi ./eval/planning -count=1
```

Expected: PASS.

- [ ] **Step 2: Run PostgreSQL integration tests**

Run:

```bash
cd apps/server && TEST_DATABASE_URL='postgres://jianwo:jianwo@127.0.0.1:55432/postgres?sslmode=disable' go test ./internal/repository/postgres -run 'TestPrepareAndCommitPlanSet|TestPreparePlanSetRejects|TestPlanningTenant|TestRenderSpecReader' -count=1
```

Expected: PASS, including atomic rollback, immutable UPDATE rejection, tenant 404 and concurrent idempotency.

- [ ] **Step 3: Run repository-wide required gates**

Run:

```bash
pnpm typecheck && pnpm lint && make design-build
node apps/miniapp/scripts/check.mjs
make server-vet && make server-test
```

Expected: all commands exit 0.

- [ ] **Step 4: Verify OpenAPI and absence of legacy compatibility**

Run:

```bash
node contracts/scripts/check-sync.mjs
rg -n 'PlanGroup|TaskTypePlanGroup|TaskTypePlanLook|UpsertScenePlans|buildScenePlans|generation_status|generated_image_url|/v1/reports/\{id\}/plans|/v1/plans/\{id\}' apps/server contracts packages/core
```

Expected: contract sync passes; `rg` exits 1 with no matches.

- [ ] **Step 5: Verify image-generation scope was not implemented**

Run:

```bash
git diff --name-only -- apps/server | rg 'render_runs|render_candidates|render_publications|image_provider|render_quality'
```

Expected: exit 1 with no matches. `domain/planning.go` 内的 RenderSpec 和 `render_spec.v1.json` 是允许项，因为它们是编译器输出契约，不是图片生成。

- [ ] **Step 6: Suggested final commit**

```bash
git add apps/server contracts packages/core/src/api/endpoints.ts packages/core/src/types
git commit -m "feat(planning): complete immutable plan set pipeline"
```

## Acceptance Matrix

- 六场景 Brief Schema：Task 1、Task 10。
- Report + profile snapshot + Brief 统一 Generator：Task 2、Task 3。
- 不可变 PlanSet / 3 Variants / Steps / Groundings：Task 1、Task 6、Task 7。
- Grounding 100%、最高优先 finding 落实、场景硬约束覆盖：Task 4。
- 任意 pair 至少两个实质差异：Task 4、Task 10。
- 方案与 Report 不矛盾、不编造：Task 4 的确定性规则与 semantic Verifier。
- RenderSpec Compiler 和 Schema：Task 5。
- 后续 Rendering 可消费的 `RenderSpecReader`：接口冻结、Task 7。
- PlanSet API 和公开 Operation：Task 2、Task 8。
- 内容失败最多补生成一次，网络重试不消耗内容预算：Task 6。
- 相同语义输入不重复 AI：Task 2、Task 6、Task 7。
- 120 Brief / 180 三方案组 / 60-20-20 / 身份不跨分区：Task 10。
- 无图片 Provider 或图片质量判断：Global Constraints、Task 11。
- 无历史兼容和双路径：Task 8、Task 9、Task 11。

## Execution Notes

每个任务只在其测试由预期 FAIL 变为 PASS 后进入建议提交步骤。执行者若发现基础设施接口与“Cross-plan Interface Freeze”不一致，应先修正基础设施计划或适配器，不得在 Planning 包引入巨型 Service、巨型 Repository、动态 `refKey`、Provider 参数袋或旧 plans DTO。
