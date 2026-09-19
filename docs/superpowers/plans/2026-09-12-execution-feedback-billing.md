# Execution、Feedback 与 Billing 闭环 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 建立可追溯的 Selection → Execution → Feedback 闭环，并让付费 Operation 只在业务产物发布后结算，内部质量补生成不产生第二次扣费。

**Architecture:** Selection 是独立且追加式的用户选择事实；Execution 创建时复制所选 PlanVariant 的步骤，之后只通过带 `client_event_id` 的追加式事件改变执行投影。GenerationFeedback 只评价用户实际看到的 Publication，ExecutionFeedback 只在 Execution 完成后接收；只有可由结构化输入确定的显式偏好才形成 PreferenceMemory，Planning 通过窄 Reader 消费。Billing 以 Operation 为一次收费边界，Render 的结算额外绑定 Publication；Task、Candidate 1、Candidate 2 和同能力技术重试都不能成为扣费引用。

**Tech Stack:** Go 1.24、PostgreSQL 16、pgx/v5、REST/OpenAPI 3、TypeScript 5.6、Node.js 22、pnpm 10。

## Global Constraints

- 本计划在基础设施、Assessment、Planning/RenderSpec、Rendering/Quality 四份计划完成后执行；依赖的新 `operations`、`idempotency_keys`、`plan_sets`、`plan_variants`、`plan_steps`、`render_runs`、`render_candidates`、`render_publications`、`media_assets` 和 `user_profiles` 已存在于 `apps/server/internal/database/migrations/001_baseline.sql`。
- 不保留旧数据库数据、旧 API、旧本地缓存或双写路径；删除旧 `plans.selected_at`、`checklist_items`、`feedback` 与 Task 计费引用，不编写回填逻辑。
- 不修改 `appearance-coach-prototype/`、根 `package.json`、根 `tsconfig.base.json`、`Makefile` 或 `docker-compose.yml`。
- 不重写微信登录、短信、Apple 登录、支付签名、微信虚拟支付 Provider、COS SDK 或天气适配器内部实现。
- 服务端依赖方向固定为 `httpapi → service → repository / provider / storage`；handler 不直接访问数据库。
- 每次 Repository 读写都携带 `user_id + resource_id`；跨用户资源统一返回 404，数据库父子关系使用 `(user_id, parent_id)` 复合外键。
- Selection、ExecutionStep 快照、ExecutionEvent、GenerationFeedback、ExecutionFeedback、PreferenceMemory 和 BillingLedger 均追加写入；只有 `executions.state/version/timestamps`、`billing_reservations.status/timestamps` 与 `user_profiles.preferences/version` 是允许 CAS 更新的投影。
- 所有创建请求要求 `Idempotency-Key`；同一 key 与同一规范化请求返回原资源，同一 key 与不同请求返回 `409 idempotency_conflict`。
- Execution 事件额外要求 body 的 `client_event_id` 与 `Idempotency-Key` 相同；重复事件不增加 version，不重复改变状态。
- 可变 Execution 使用 `If-Match: "<version>"`；缺失返回 `428 precondition_required`，版本不匹配返回 `412 version_conflict`，成功响应返回新的 `ETag`。
- 用户上传图片仅接受 JPEG/PNG；反馈图片是可选项，上传失败不得阻止无图反馈保存。
- GenerationFeedback 必须关联用户实际看到的 Publication，并由服务端固化对应 RenderRun、Candidate、Asset 和 generation；客户端不能自行提交这些派生 ID。
- ExecutionFeedback 只有在 Execution `completed` 后才能创建；文字与标签先保存，PreferenceMemory 只读取结构化 `preference`，不从自由文本猜测。
- 页面只能承诺已经写入 PreferenceMemory 的内容；没有形成记忆时只确认“反馈已记录”，不得承诺后续一定调整。
- Planning 读取最近 20 条 PreferenceMemory，按 `created_at DESC, id DESC` 排序后作为 `feedback_memory` grounding；不在线训练，不把生成负反馈写入用户偏好。
- Billing 在付费 Operation 创建后预占；Report、PlanSet 业务产物成功后结算，Render 仅在合格 Candidate 创建 Publication 后结算；`failed/cancelled/superseded` Operation 自动退款。
- 一次 Render Operation 只允许一条 reservation；Candidate 2、网络重试和同能力路由切换复用原 Operation，不调用 `Reserve`。
- Billing Ledger 的业务引用只允许 Operation、Publication 和支付 Order；禁止 Task ID、Candidate ID 或 Provider invocation ID。
- AI 结果、Demo 和内置图来源规则、JPEG 输出规则、尊重表达和文案红线继续遵守 `AGENTS.md`。
- 不新增第三方 Go 或 Node 运行时依赖。

---

## Cross-plan Ownership Override

- 本计划拥有 Selection、Execution、Feedback、PreferenceMemory、Billing 生命周期的 Go Domain、Service、Postgres 与 HTTP；它消费 Rule/Contract Freeze 已确定的 OpenAPI，不修改 `contracts/**`。
- Foundation 独占并一次性创建最终 `001_baseline.sql`；本计划只验证 Execution/Feedback/Billing 表与约束。正文 DDL 是 Foundation 必须纳入的规范。
- 本计划不得修改或提交 `packages/core/src/types/index.ts`、`packages/core/src/api/endpoints.ts` 或任何 Miniapp 页面；最终 OpenAPI codegen、请求 Header 实现和反馈 UI 由 `2026-09-12-miniapp-quality-loop.md` 统一执行。
- 下文 Task 8 与 Task 9 中涉及 `contracts/openapi.yaml` 或旧手写 Core Client 的步骤和提交路径全部跳过；它们只完成服务端 Handler、HTTP 契约测试和 `packages/core/src/copy/zh.ts` 中与业务承诺有关的文案单源。
- 本计划必须在 Miniapp 计划之前执行，使最终 codegen 一次覆盖 Assessment、Planning、Rendering、Execution、Feedback 的完整契约。
- 本计划不修改中央 `bootstrap/api.go`、`bootstrap/worker.go` 或 `httpapi/httpapi.go` composition；Billing reconciliation 只提供可注入构造函数，最终周期注册由 Peripherals/Cutover 完成。
- Task 10 的旧 Service/Repository/migration 删除步骤全部跳过；物理清理统一由 Peripherals/Cutover 完成。本计划只交付新 Execution/Feedback/Billing 闭环及 E2E。

## File Structure

### Database and domain

- Verify: `apps/server/internal/database/migrations/001_baseline.sql` — Foundation 已一次性定义 Selection、Execution 快照/事件、两类 Feedback、PreferenceMemory、Operation 计费预占与账本约束；本计划只补 schema 测试。
- Create: `apps/server/internal/domain/execution.go` — Selection、Execution、ExecutionStep、ExecutionEvent 类型和状态常量。
- Create: `apps/server/internal/domain/feedback.go` — 两类反馈、结构化偏好、PreferenceMemory、确认码和标签常量。
- Modify: `apps/server/internal/domain/billing.go` — Operation/Publication 计费产品、预占状态和账本类型；订单类型保持不变。
- Create: `apps/server/internal/domain/execution_test.go` — Execution 状态转换和事件校验纯函数测试。
- Create: `apps/server/internal/domain/feedback_test.go` — 标签、结构化偏好和确认码纯函数测试。

### Selection and execution

- Create: `apps/server/internal/service/execution/ports.go` — Execution 领域最小 Repository 接口。
- Create: `apps/server/internal/service/execution/service.go` — 创建 Selection、创建 Execution、读取 Execution、追加幂等事件。
- Create: `apps/server/internal/service/execution/validation.go` — request hash、事件转换和 ETag version 校验。
- Create: `apps/server/internal/service/execution/service_test.go` — fake ports 的用例测试。
- Create: `apps/server/internal/repository/postgres/execution.go` — Selection/Execution 事务实现。
- Create: `apps/server/internal/repository/postgres/execution_test.go` — 真实 PostgreSQL 的租户、快照、幂等和并发 CAS 测试。

### Feedback and preference memory

- Create: `apps/server/internal/service/feedback/ports.go` — Feedback 写入和媒体/Publication/Execution 校验接口。
- Create: `apps/server/internal/service/feedback/service.go` — GenerationFeedback、ExecutionFeedback 用例。
- Create: `apps/server/internal/service/feedback/memory.go` — 结构化偏好到 PreferenceMemory 的确定性映射。
- Create: `apps/server/internal/service/feedback/service_test.go` — 无图保存、图片归属、关联完整和准确确认码测试。
- Create: `apps/server/internal/repository/postgres/feedback.go` — 两类反馈、PreferenceMemory 和 profile preference 投影事务。
- Create: `apps/server/internal/repository/postgres/feedback_test.go` — 复合外键、幂等、完成态门禁和 planning reader 集成测试。
- Modify: `apps/server/internal/service/planning/ports.go` — 增加 `PreferenceMemoryReader`。
- Modify: `apps/server/internal/service/planning/service.go` — 把 memory 固化进 PlanSet 输入和 grounding。
- Modify: `apps/server/internal/service/planning/service_test.go` — 证明明确偏好进入下一轮方案，生成反馈不会进入。

### Billing lifecycle

- Modify: `apps/server/internal/service/billing/ports.go` — 在 Foundation 的 `Reserve/Settle/Refund` 接口上补 Operation 终态 Reader。
- Modify: `apps/server/internal/service/billing/service.go` — 保持 Foundation 生命周期并增加 `ReconcileTerminalOperations`。
- Modify: `apps/server/internal/service/billing/service_test.go` — 增加崩溃协调、失败退款和补生成不重复扣费测试。
- Modify: `apps/server/internal/repository/postgres/billing.go` — Operation/Publication 引用的原子钱包和账本实现；支付订单方法保留。
- Modify: `apps/server/internal/repository/postgres/billing_test.go` — 真实 PostgreSQL 的并发和崩溃恢复测试。
- Modify: `apps/server/internal/service/assessment/service.go` — Assessment Operation 预占、Report 成功结算、终态失败退款。
- Modify: `apps/server/internal/service/planning/service.go` — PlanSet Operation 预占、文字 PlanSet 成功结算、终态失败退款。
- Modify: `apps/server/internal/service/rendering/service.go` — Render Operation 预占、Publication 成功结算；内部 Candidate 2 不再次预占。
- Modify: `apps/server/internal/service/rendering/service_test.go` — 两候选只调用一次 Reserve、只按 Publication Settle。
- Modify: `apps/server/internal/bootstrap/worker.go` — Worker 启动和固定周期调用幂等 reconciliation；不改 Task runner 主循环。

### HTTP contract and client-facing copy

- Create: `apps/server/internal/httpapi/executions.go` — Selection、Execution 和 Event handler。
- Create: `apps/server/internal/httpapi/feedback.go` — 两类反馈 handler。
- Modify: `apps/server/internal/httpapi/httpapi.go` — 注册新路由，移除旧 plan select/checklist/feedback 路由，增加 409/412/428 错误翻译。
- Create: `apps/server/internal/httpapi/executions_test.go` — 状态码、Idempotency-Key、If-Match、ETag 和越权契约测试。
- Create: `apps/server/internal/httpapi/feedback_test.go` — 两类反馈请求/响应和可选图片契约测试。
- Modify: `contracts/openapi.yaml` — 新 `/v1` 端点、schema、header 和 error code。
- Modify: `packages/core/src/types/index.ts` — Selection、Execution、Feedback、PreferenceMemory 传输类型。
- Modify: `packages/core/src/api/endpoints.ts` — 新端点；事件请求发送 `Idempotency-Key` 和 `If-Match`。
- Modify: `packages/core/src/copy/zh.ts` — 只按 `acknowledgement_code` 渲染准确反馈确认文案。
- Modify: `packages/core/tests/client.test.mjs` — headers、路径和 snake_case 类型行为测试。
- Modify: `packages/core/tests/copy.test.mjs` — 确认文案不作无依据承诺。

### End-to-end verification

- Modify: `apps/server/scripts/e2e.sh` — Publication → Selection → Execution snapshot → 幂等事件 → 两类反馈 → 下一轮 Planning memory → Billing settle/refund。

`apps/server/internal/provider/payment/`、`apps/server/internal/provider/wechat*`、`apps/server/internal/provider/sms*`、`apps/server/internal/provider/apple*` 和 `apps/server/internal/storage/` 不在本计划修改范围。

---

### Task 1: 固化数据库约束与领域类型

**Files:**
- Verify: `apps/server/internal/database/migrations/001_baseline.sql`
- Create: `apps/server/internal/domain/execution.go`
- Create: `apps/server/internal/domain/execution_test.go`
- Create: `apps/server/internal/domain/feedback.go`
- Create: `apps/server/internal/domain/feedback_test.go`
- Modify: `apps/server/internal/domain/billing.go`

**Interfaces:**
- Consumes: 已存在的 `plan_sets(user_id,id)`、`plan_variants(user_id,id,plan_set_id)`、`plan_steps(user_id,id,plan_variant_id)`、`render_publications(user_id,id,plan_variant_id,render_run_id,candidate_id,generation)`、`media_assets(user_id,id,purpose,state)`、`operations(user_id,id,kind,status,result_type,result_id)`。
- Produces:
  - `domain.ValidateTransition(state ExecutionState, event ExecutionEventType, allStepsCompleted bool) (ExecutionState, error)`
  - `domain.NormalizePreference(input StructuredPreference, tags []Tag) ([]PreferenceMemoryDraft, error)`
  - `domain.AcknowledgementCodeFor(memories []PreferenceMemoryDraft) AcknowledgementCode`
  - 数据库复合外键和唯一约束，供后续 Repository 事务依赖。

- [ ] **Step 1: 写 Execution 与 Feedback 领域失败测试**

```go
func TestValidateTransitionRequiresAllStepsBeforeCompleted(t *testing.T) {
	got, err := ValidateTransition(ExecutionActive, EventCompleted, false)
	if !errors.Is(err, ErrInvalidTransition) || got != ExecutionActive {
		t.Fatalf("got state=%q err=%v", got, err)
	}
}

func TestNormalizePreferenceOnlyUsesStructuredInput(t *testing.T) {
	got, err := NormalizePreference(
		StructuredPreference{Kind: PreferenceLessFormal, Category: CategoryOverall},
		[]Tag{ExecutionTooFormal},
	)
	if err != nil || len(got) != 1 || got[0].Key != "formality" || got[0].Value != "less" {
		t.Fatalf("unexpected memory: %#v err=%v", got, err)
	}
	none, err := NormalizePreference(StructuredPreference{}, []Tag{ExecutionEasy})
	if err != nil || len(none) != 0 {
		t.Fatalf("non-explicit feedback must not become memory: %#v err=%v", none, err)
	}
}

func TestGenerationTagsNeverCreatePreferenceMemory(t *testing.T) {
	_, err := NormalizePreference(
		StructuredPreference{Kind: PreferencePreserve, Category: CategoryHair, Value: "当前发型"},
		[]Tag{Tag("identity_mismatch")},
	)
	if !errors.Is(err, ErrPreferenceNotAllowed) {
		t.Fatalf("generation feedback must not create memory: %v", err)
	}
}
```

- [ ] **Step 2: 运行领域测试并确认失败**

Run:

```bash
cd apps/server
go test ./internal/domain -run 'Test(ValidateTransition|NormalizePreference|GenerationTags)' -v
```

Expected: FAIL，提示 `ValidateTransition`、`NormalizePreference` 或对应类型未定义。

- [ ] **Step 3: 定义精确领域类型与纯函数**

`apps/server/internal/domain/execution.go` 必须定义：

```go
type ExecutionState string
const (
	ExecutionPlanned   ExecutionState = "planned"
	ExecutionActive    ExecutionState = "active"
	ExecutionCompleted ExecutionState = "completed"
	ExecutionAbandoned ExecutionState = "abandoned"
)

type ExecutionEventType string
const (
	EventStarted       ExecutionEventType = "started"
	EventStepCompleted ExecutionEventType = "step_completed"
	EventStepReopened  ExecutionEventType = "step_reopened"
	EventCompleted     ExecutionEventType = "completed"
	EventAbandoned     ExecutionEventType = "abandoned"
)

type PlanSelection struct {
	ID string `json:"id"`; PlanSetID string `json:"plan_set_id"`
	PlanVariantID string `json:"plan_variant_id"`
	RenderPublicationID *string `json:"render_publication_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type ExecutionStep struct {
	ID string `json:"id"`; SourcePlanStepID string `json:"source_plan_step_id"`
	Category string `json:"category"`; Action string `json:"action"`
	Title string `json:"title"`; Summary string `json:"summary"`
	Details json.RawMessage `json:"details"`; Position int `json:"position"`
	Completed bool `json:"completed"`; CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type Execution struct {
	ID string `json:"id"`; SelectionID string `json:"selection_id"`
	State ExecutionState `json:"state"`; Version int `json:"version"`
	Steps []ExecutionStep `json:"steps"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	CreatedAt time.Time `json:"created_at"`; UpdatedAt time.Time `json:"updated_at"`
}

type ExecutionEvent struct {
	ID string `json:"id"`; ExecutionID string `json:"execution_id"`
	ClientEventID string `json:"client_event_id"`; Type ExecutionEventType `json:"type"`
	StepID *string `json:"step_id,omitempty"`; OccurredAt time.Time `json:"occurred_at"`
	CreatedAt time.Time `json:"created_at"`
}
```

`ValidateTransition` 使用以下唯一状态表；`step_completed` 仅允许当前未完成步骤，`step_reopened` 仅允许当前已完成步骤：

```text
planned  + started/step_completed/step_reopened → active
planned  + abandoned                            → abandoned
active   + step_completed/step_reopened          → active
active   + completed 且全部步骤完成               → completed
active   + abandoned                             → abandoned
completed/abandoned + 任意新事件                 → invalid_transition
```

`apps/server/internal/domain/feedback.go` 必须定义固定枚举：

```go
type Tag string
const (
	GenerationIdentityMismatch Tag = "identity_mismatch"
	GenerationHairMismatch Tag = "hair_mismatch"
	GenerationMakeupMismatch Tag = "makeup_mismatch"
	GenerationOutfitMismatch Tag = "outfit_mismatch"
	GenerationAnatomyIssue Tag = "anatomy_issue"
	GenerationUnnatural Tag = "unnatural"
	ExecutionEasy Tag = "easy_to_execute"
	ExecutionTooFormal Tag = "too_formal"
	ExecutionTooComplex Tag = "too_complex"
	ExecutionDislikeColor Tag = "dislike_color"
	ExecutionWantToKeep Tag = "want_to_keep"
)

type PreferenceKind string
const (
	PreferenceLessFormal PreferenceKind = "less_formal"
	PreferenceSimplify PreferenceKind = "simplify"
	PreferenceAvoid PreferenceKind = "avoid"
	PreferencePreserve PreferenceKind = "preserve"
)

type PreferenceCategory string
const (
	CategoryOverall PreferenceCategory = "overall"
	CategoryHair PreferenceCategory = "hair"
	CategoryMakeup PreferenceCategory = "makeup"
	CategoryOutfit PreferenceCategory = "outfit"
	CategoryColor PreferenceCategory = "color"
)

type StructuredPreference struct {
	Kind PreferenceKind `json:"kind,omitempty"`
	Category PreferenceCategory `json:"category,omitempty"`
	Value string `json:"value,omitempty"`
}

type PreferenceMemoryDraft struct {
	Key string `json:"key"`; Category PreferenceCategory `json:"category"`
	Value string `json:"value"`; SourceTag Tag `json:"source_tag"`
}

type AcknowledgementCode string
const (
	AckFeedbackRecorded AcknowledgementCode = "feedback_recorded"
	AckLessFormalSaved AcknowledgementCode = "less_formal_saved"
	AckSimplerSaved AcknowledgementCode = "simpler_saved"
	AckAvoidColorSaved AcknowledgementCode = "avoid_color_saved"
	AckPreserveSaved AcknowledgementCode = "preserve_saved"
)
```

映射只能是：

```text
too_formal + less_formal(overall)                  → formality=less
too_complex + simplify(overall)                    → complexity=simpler
dislike_color + avoid(color,value 非空且 ≤40 rune) → avoid_color=<trimmed value>
want_to_keep + preserve(hair|makeup|outfit,value)  → preserve_<category>=<trimmed value>
easy_to_execute                                    → 不生成 memory
comment                                            → 永不生成 memory
```

- [ ] **Step 4: 在 baseline 中加入强约束表**

加入以下表和等价约束；不得以应用层检查替代复合外键：

```sql
CREATE TABLE plan_selections (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL,
  plan_set_id uuid NOT NULL,
  plan_variant_id uuid NOT NULL,
  render_publication_id uuid,
  idempotency_key text NOT NULL,
  request_hash text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, idempotency_key),
  FOREIGN KEY (user_id, plan_set_id) REFERENCES plan_sets(user_id, id),
  FOREIGN KEY (user_id, plan_variant_id, plan_set_id)
    REFERENCES plan_variants(user_id, id, plan_set_id),
  FOREIGN KEY (user_id, render_publication_id, plan_variant_id)
    REFERENCES render_publications(user_id, id, plan_variant_id)
);

CREATE UNIQUE INDEX plan_selections_natural_uniq
ON plan_selections (
  user_id, plan_set_id, plan_variant_id,
  coalesce(render_publication_id, '00000000-0000-0000-0000-000000000000'::uuid)
);

CREATE TABLE executions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL,
  selection_id uuid NOT NULL,
  state text NOT NULL CHECK (state IN ('planned','active','completed','abandoned')),
  version integer NOT NULL DEFAULT 1 CHECK (version > 0),
  idempotency_key text NOT NULL,
  request_hash text NOT NULL,
  started_at timestamptz,
  completed_at timestamptz,
  abandoned_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, selection_id),
  UNIQUE (user_id, idempotency_key),
  FOREIGN KEY (user_id, selection_id) REFERENCES plan_selections(user_id, id)
);

CREATE TABLE execution_steps (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL,
  execution_id uuid NOT NULL,
  source_plan_step_id uuid NOT NULL,
  category text NOT NULL CHECK (category IN ('hair','makeup','outfit')),
  action text NOT NULL CHECK (action IN ('keep','adjust')),
  title text NOT NULL,
  summary text NOT NULL,
  details jsonb NOT NULL,
  position integer NOT NULL CHECK (position > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, execution_id, source_plan_step_id),
  UNIQUE (user_id, execution_id, position),
  FOREIGN KEY (user_id, execution_id) REFERENCES executions(user_id, id),
  FOREIGN KEY (user_id, source_plan_step_id) REFERENCES plan_steps(user_id, id)
);

CREATE TABLE execution_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL,
  execution_id uuid NOT NULL,
  client_event_id text NOT NULL,
  request_hash text NOT NULL,
  event_type text NOT NULL CHECK (event_type IN
    ('started','step_completed','step_reopened','completed','abandoned')),
  execution_step_id uuid,
  occurred_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, execution_id, client_event_id),
  FOREIGN KEY (user_id, execution_id) REFERENCES executions(user_id, id),
  FOREIGN KEY (user_id, execution_step_id) REFERENCES execution_steps(user_id, id),
  CHECK (
    (event_type IN ('step_completed','step_reopened') AND execution_step_id IS NOT NULL)
    OR
    (event_type NOT IN ('step_completed','step_reopened') AND execution_step_id IS NULL)
  )
);

CREATE TRIGGER plan_steps_immutable
BEFORE UPDATE ON plan_steps
FOR EACH ROW EXECUTE FUNCTION reject_immutable_artifact();
```

同一 baseline 还要创建 `generation_feedback`、`execution_feedback`、`preference_memories`、`billing_reservations` 和新版 `billing_ledger`；完整列在 Task 5、Task 6、Task 7 中，实施 Task 1 时一次写入 baseline，后续任务只补测试和 Repository。

- [ ] **Step 5: 运行领域与 baseline 启动测试**

Run:

```bash
cd apps/server
go test ./internal/domain -run 'Test(ValidateTransition|NormalizePreference|GenerationTags)' -v
cd ../..
docker compose up -d postgres
DATABASE_URL='postgres://jianwo:jianwo@127.0.0.1:55432/jianwo?sslmode=disable' \
  go -C apps/server test ./internal/database -run TestBaseline -v
```

Expected: 领域测试 PASS；baseline 在空数据库一次应用成功，第二次执行不重复建表。

- [ ] **Step 6: 提交领域和 schema 边界**

```bash
git add apps/server/internal/database/migrations/001_baseline.sql \
  apps/server/internal/domain/execution.go \
  apps/server/internal/domain/execution_test.go \
  apps/server/internal/domain/feedback.go \
  apps/server/internal/domain/feedback_test.go \
  apps/server/internal/domain/billing.go
git commit -m "feat(server): define execution feedback billing model"
```

---

### Task 2: 创建独立 Selection 资源

**Files:**
- Create: `apps/server/internal/service/execution/ports.go`
- Create: `apps/server/internal/service/execution/service.go`
- Create: `apps/server/internal/service/execution/validation.go`
- Create: `apps/server/internal/service/execution/service_test.go`
- Create: `apps/server/internal/repository/postgres/execution.go`
- Create: `apps/server/internal/repository/postgres/execution_test.go`

**Interfaces:**
- Consumes: `domain.PlanSelection` 和 Task 1 的 `plan_selections` 约束。
- Produces:

```go
type SelectionRepository interface {
	CreateSelection(ctx context.Context, command CreateSelectionCommand) (domain.PlanSelection, bool, error)
	GetSelection(ctx context.Context, userID, selectionID string) (domain.PlanSelection, error)
}

type PutSelectionInput struct {
	PlanVariantID string
	RenderPublicationID *string
	IdempotencyKey string
}

func (s *Service) PutSelection(
	ctx context.Context, userID, planSetID string, input PutSelectionInput,
) (selection domain.PlanSelection, created bool, err error)
```

- [ ] **Step 1: 写 Service 幂等与归属失败测试**

```go
func TestPutSelectionReturnsReplayForSameKeyAndBody(t *testing.T) {
	repo := newFakeRepository()
	svc := New(repo)
	in := PutSelectionInput{
		PlanVariantID: "22222222-2222-2222-2222-222222222222",
		IdempotencyKey: "select-1",
	}
	first, created, err := svc.PutSelection(ctx, userID, planSetID, in)
	if err != nil || !created { t.Fatalf("first: %#v %v", first, err) }
	second, created, err := svc.PutSelection(ctx, userID, planSetID, in)
	if err != nil || created || second.ID != first.ID {
		t.Fatalf("replay: %#v created=%v err=%v", second, created, err)
	}
}

func TestPutSelectionRejectsSameKeyWithDifferentVariant(t *testing.T) {
	repo := newFakeRepository()
	svc := New(repo)
	_, _, _ = svc.PutSelection(ctx, userID, planSetID, PutSelectionInput{
		PlanVariantID: variantA, IdempotencyKey: "select-1",
	})
	_, _, err := svc.PutSelection(ctx, userID, planSetID, PutSelectionInput{
		PlanVariantID: variantB, IdempotencyKey: "select-1",
	})
	if !errors.Is(err, ErrIdempotencyConflict) { t.Fatalf("err=%v", err) }
}
```

- [ ] **Step 2: 运行 Service 测试并确认失败**

Run:

```bash
cd apps/server
go test ./internal/service/execution -run TestPutSelection -v
```

Expected: FAIL，提示 `New` 或 `PutSelection` 未定义。

- [ ] **Step 3: 实现规范化 hash 与 Selection 用例**

规范化请求只包含 trim 后的 `plan_set_id`、`plan_variant_id`、`render_publication_id`；使用 `encoding/json` 编码固定 struct 后做 SHA-256 hex。验证所有 ID 为 UUID、`Idempotency-Key` 为 8–128 个可打印 ASCII 字符。Repository 返回：

- 同 key + 同 hash：原 Selection，`created=false`。
- 同 key + 不同 hash：`execution.ErrIdempotencyConflict`。
- variant 不属于该 plan set：`repository.ErrNotFound`。
- publication 非空但不属于该 variant、未发布或跨用户：`repository.ErrNotFound`。
- 同自然选择但使用新 key：返回既有 Selection，`created=false`，并记录新 key 不得覆盖旧事实；实现方式是在事务中先查自然唯一键，再写 Foundation 的 `idempotency_keys(scope='selection', resource_id=selection.id)`。

- [ ] **Step 4: 写 Postgres 复合归属测试**

```go
func TestCreateSelectionEnforcesVariantAndPublicationOwnership(t *testing.T) {
	store, fx := newExecutionFixture(t)
	_, _, err := store.CreateSelection(ctx, CreateSelectionCommand{
		UserID: fx.UserA, PlanSetID: fx.PlanSetA, PlanVariantID: fx.VariantA,
		RenderPublicationID: &fx.UserBPublication,
		IdempotencyKey: "selection-cross-user", RequestHash: hashA,
	})
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("cross-user publication must look absent: %v", err)
	}
}
```

- [ ] **Step 5: 实现 Selection 事务并运行测试**

事务按 `user_id + plan_set_id` 锁定 PlanSet，再验证 variant/publication，最后插入。捕获唯一冲突后必须重新读取 idempotency record 或自然选择，不能把并发重放返回 500。

Run:

```bash
cd apps/server
TEST_DATABASE_URL='postgres://jianwo:jianwo@127.0.0.1:55432/jianwo?sslmode=disable' \
  go test ./internal/repository/postgres -run TestCreateSelection -count=1 -v
go test ./internal/service/execution -run TestPutSelection -v
```

Expected: 跨用户测试返回 Repository 404 语义；并发相同请求只产生一个 Selection；全部 PASS。

- [ ] **Step 6: 提交 Selection 边界**

```bash
git add apps/server/internal/service/execution \
  apps/server/internal/repository/postgres/execution.go \
  apps/server/internal/repository/postgres/execution_test.go
git commit -m "feat(server): add independent plan selections"
```

---

### Task 3: 创建不可变 ExecutionStep 快照

**Files:**
- Modify: `apps/server/internal/service/execution/ports.go`
- Modify: `apps/server/internal/service/execution/service.go`
- Modify: `apps/server/internal/service/execution/service_test.go`
- Modify: `apps/server/internal/repository/postgres/execution.go`
- Modify: `apps/server/internal/repository/postgres/execution_test.go`

**Interfaces:**
- Consumes: Task 2 `SelectionRepository`。
- Produces:

```go
type ExecutionRepository interface {
	CreateExecutionFromSelection(
		ctx context.Context, command CreateExecutionCommand,
	) (domain.Execution, bool, error)
	GetExecution(ctx context.Context, userID, executionID string) (domain.Execution, error)
}

type CreateExecutionInput struct { IdempotencyKey string }

func (s *Service) CreateExecution(
	ctx context.Context, userID, selectionID string, input CreateExecutionInput,
) (execution domain.Execution, created bool, err error)

func (s *Service) GetExecution(
	ctx context.Context, userID, executionID string,
) (domain.Execution, error)
```

- [ ] **Step 1: 写快照不受 Plan 后续变化影响的失败测试**

```go
func TestCreateExecutionCopiesPlanStepsOnce(t *testing.T) {
	repo := newFakeRepository()
	repo.planSteps = []domain.PlanStep{
		{ID: stepHair, Category: "hair", Action: "keep", Title: "保留偏分", Summary: "整理分缝", Position: 1},
		{ID: stepMakeup, Category: "makeup", Action: "adjust", Title: "降低对比", Summary: "薄涂", Position: 2},
		{ID: stepOutfit, Category: "outfit", Action: "adjust", Title: "换浅色内搭", Summary: "保留外套", Position: 3},
	}
	execution, created, err := New(repo).CreateExecution(ctx, userID, selectionID, CreateExecutionInput{IdempotencyKey: "execution-1"})
	if err != nil || !created || len(execution.Steps) != 3 { t.Fatalf("%#v %v", execution, err) }
	repo.planSteps[0].Title = "被改动的源步骤"
	again, _, _ := New(repo).CreateExecution(ctx, userID, selectionID, CreateExecutionInput{IdempotencyKey: "execution-1"})
	if again.Steps[0].Title != "保留偏分" {
		t.Fatalf("snapshot changed with source: %#v", again.Steps[0])
	}
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run:

```bash
cd apps/server
go test ./internal/service/execution -run TestCreateExecution -v
```

Expected: FAIL，提示 `CreateExecution` 未定义。

- [ ] **Step 3: 实现单事务复制**

`CreateExecutionFromSelection` 在一个事务内：

1. 以 `(user_id, selection_id)` 读取 Selection 与 PlanVariant。
2. 处理 `(user_id,idempotency_key)` replay/hash 冲突。
3. 若该 Selection 已有 Execution，返回同一资源。
4. 插入 `executions(state='planned', version=1)`。
5. 使用 `INSERT ... SELECT` 复制该 PlanVariant 的全部 `plan_steps`。
6. 强制快照恰好包含 hair、makeup、outfit 且 position 唯一；不满足时回滚并返回 `ErrInvalidSnapshot`。
7. 查询 Execution；`completed` 由事件投影计算，不写入 `execution_steps`。

读取时用窗口查询每个 step 最后一条 `step_completed/step_reopened` 事件，计算 API 的 `completed/completed_at`，不更新快照表。

- [ ] **Step 4: 写真实数据库快照测试**

```go
func TestExecutionSnapshotSurvivesSourceDeletionAttempt(t *testing.T) {
	store, fx := newExecutionFixture(t)
	exec, _, err := store.CreateExecutionFromSelection(ctx, CreateExecutionCommand{
		UserID: fx.UserA, SelectionID: fx.SelectionA,
		IdempotencyKey: "execution-snapshot", RequestHash: hashA,
	})
	if err != nil { t.Fatal(err) }
	_, err = fx.Pool.Exec(ctx, `UPDATE plan_steps SET title='mutated' WHERE id=$1`, fx.HairStep)
	if err == nil {
		t.Fatal("plan_steps are immutable; update must be denied by database trigger")
	}
	got, err := store.GetExecution(ctx, fx.UserA, exec.ID)
	if err != nil || got.Steps[0].Title != "保留偏分" {
		t.Fatalf("snapshot=%#v err=%v", got.Steps, err)
	}
}
```

Task 1 已安装 `plan_steps_immutable` trigger，测试直接更新源步骤并断言 PostgreSQL 拒绝；不得删除“不变性”断言。

- [ ] **Step 5: 运行 Execution 创建测试**

Run:

```bash
cd apps/server
go test ./internal/service/execution -run TestCreateExecution -v
TEST_DATABASE_URL='postgres://jianwo:jianwo@127.0.0.1:55432/jianwo?sslmode=disable' \
  go test ./internal/repository/postgres -run 'TestExecution(Snapshot|Create)' -count=1 -v
```

Expected: 相同 Selection 并发创建只返回一个 Execution；快照保持原内容；全部 PASS。

- [ ] **Step 6: 提交 Execution 快照**

```bash
git add apps/server/internal/service/execution \
  apps/server/internal/repository/postgres/execution.go \
  apps/server/internal/repository/postgres/execution_test.go
git commit -m "feat(server): snapshot selected plan into execution"
```

---

### Task 4: 追加幂等 Execution Events 与 CAS

**Files:**
- Modify: `apps/server/internal/service/execution/ports.go`
- Modify: `apps/server/internal/service/execution/service.go`
- Modify: `apps/server/internal/service/execution/validation.go`
- Modify: `apps/server/internal/service/execution/service_test.go`
- Modify: `apps/server/internal/repository/postgres/execution.go`
- Modify: `apps/server/internal/repository/postgres/execution_test.go`

**Interfaces:**
- Consumes: Task 3 的 Execution/step 快照。
- Produces:

```go
type AppendEventInput struct {
	ClientEventID string
	Type domain.ExecutionEventType
	StepID *string
	OccurredAt time.Time
	ExpectedVersion int
}

type AppendEventResult struct {
	Event domain.ExecutionEvent
	Execution domain.Execution
	Replayed bool
}

func (s *Service) AppendEvent(
	ctx context.Context, userID, executionID string, input AppendEventInput,
) (AppendEventResult, error)
```

- [ ] **Step 1: 写重复事件和 stale CAS 失败测试**

```go
func TestAppendEventReplayDoesNotIncrementVersion(t *testing.T) {
	repo := newFakeRepositoryWithExecution(1)
	svc := New(repo)
	in := AppendEventInput{
		ClientEventID: "device-a-0001", Type: domain.EventStepCompleted,
		StepID: ptr(stepHair), OccurredAt: fixedTime, ExpectedVersion: 1,
	}
	first, err := svc.AppendEvent(ctx, userID, executionID, in)
	if err != nil || first.Execution.Version != 2 || first.Replayed { t.Fatalf("%#v %v", first, err) }
	second, err := svc.AppendEvent(ctx, userID, executionID, in)
	if err != nil || second.Execution.Version != 2 || !second.Replayed {
		t.Fatalf("replay must win before stale CAS: %#v %v", second, err)
	}
}

func TestAppendEventRejectsConcurrentDifferentEvent(t *testing.T) {
	repo := newFakeRepositoryWithExecution(2)
	_, err := New(repo).AppendEvent(ctx, userID, executionID, AppendEventInput{
		ClientEventID: "device-b-0001", Type: domain.EventStepCompleted,
		StepID: ptr(stepMakeup), OccurredAt: fixedTime, ExpectedVersion: 1,
	})
	if !errors.Is(err, ErrVersionConflict) { t.Fatalf("err=%v", err) }
}
```

- [ ] **Step 2: 运行事件测试并确认失败**

Run:

```bash
cd apps/server
go test ./internal/service/execution -run TestAppendEvent -v
```

Expected: FAIL，提示 `AppendEvent` 未定义。

- [ ] **Step 3: 实现事件事务顺序**

Postgres 事务必须严格按以下顺序：

1. 通过 `(user_id,execution_id,client_event_id)` 查询既有事件。
2. 若存在且 hash 相同，直接读取当前 Execution 返回 `Replayed=true`，忽略已经过期的 `ExpectedVersion`。
3. 若存在且 hash 不同，返回 `ErrIdempotencyConflict`。
4. `SELECT executions ... FOR UPDATE`，校验 `version=ExpectedVersion`。
5. 对 step 事件校验 `(user_id,execution_id,step_id)`。
6. 从事件账本计算全部 step 当前完成状态。
7. 调用 `domain.ValidateTransition`。
8. 插入事件。
9. CAS 更新 Execution：`version=version+1`；首次 active 写 `started_at`；completed/abandoned 写对应终态时间。
10. 返回事件和重新投影后的 Execution。

`occurred_at` 只接受服务器当前时间前后 24 小时范围；超出返回 validation error，避免离线设备异常时间污染顺序。step 投影顺序使用 `created_at,id`，不用客户端时间决定最后状态。

- [ ] **Step 4: 写真实数据库并发测试**

```go
func TestAppendExecutionEventConcurrentCASAllowsOneWriter(t *testing.T) {
	store, fx := newExecutionFixture(t)
	var ok, conflict atomic.Int32
	var wg sync.WaitGroup
	for _, stepID := range []string{fx.HairExecutionStep, fx.MakeupExecutionStep} {
		wg.Add(1)
		go func(stepID string) {
			defer wg.Done()
			_, err := store.AppendExecutionEvent(ctx, AppendEventCommand{
				UserID: fx.UserA, ExecutionID: fx.ExecutionA,
				ClientEventID: uuid.NewString(), Type: domain.EventStepCompleted,
				StepID: &stepID, OccurredAt: time.Now(), ExpectedVersion: 1,
			})
			if err == nil { ok.Add(1) } else if errors.Is(err, execution.ErrVersionConflict) { conflict.Add(1) }
		}(stepID)
	}
	wg.Wait()
	if ok.Load() != 1 || conflict.Load() != 1 {
		t.Fatalf("ok=%d conflict=%d", ok.Load(), conflict.Load())
	}
}
```

- [ ] **Step 5: 运行事件测试**

Run:

```bash
cd apps/server
go test ./internal/service/execution -run TestAppendEvent -v
TEST_DATABASE_URL='postgres://jianwo:jianwo@127.0.0.1:55432/jianwo?sslmode=disable' \
  go test ./internal/repository/postgres -run TestAppendExecutionEvent -count=10 -v
```

Expected: 每轮并发恰好一个成功、一个 version conflict；replay 不增加 version；全部 PASS。

- [ ] **Step 6: 提交事件账本**

```bash
git add apps/server/internal/service/execution \
  apps/server/internal/repository/postgres/execution.go \
  apps/server/internal/repository/postgres/execution_test.go
git commit -m "feat(server): add idempotent execution events"
```

---

### Task 5: 保存 GenerationFeedback 的完整发布链路

**Files:**
- Create: `apps/server/internal/service/feedback/ports.go`
- Create: `apps/server/internal/service/feedback/service.go`
- Create: `apps/server/internal/service/feedback/service_test.go`
- Create: `apps/server/internal/repository/postgres/feedback.go`
- Create: `apps/server/internal/repository/postgres/feedback_test.go`
- Verify: `apps/server/internal/database/migrations/001_baseline.sql`

**Interfaces:**
- Consumes: Published `render_publications` 和可选 `media_assets(purpose='feedback')`。
- Produces:

```go
type CreateGenerationFeedbackInput struct {
	PublicationID string
	Tags []domain.Tag
	Comment string
	MediaAssetID *string
	IdempotencyKey string
}

type GenerationFeedback struct {
	ID string `json:"id"`; PublicationID string `json:"publication_id"`
	RenderRunID string `json:"render_run_id"`; CandidateID string `json:"candidate_id"`
	AssetID string `json:"asset_id"`; Generation int `json:"generation"`
	Tags []domain.Tag `json:"tags"`; Comment string `json:"comment,omitempty"`
	MediaAssetID *string `json:"media_asset_id,omitempty"`
	AcknowledgementCode domain.AcknowledgementCode `json:"acknowledgement_code"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Service) CreateGenerationFeedback(
	ctx context.Context, userID string, input CreateGenerationFeedbackInput,
) (feedback GenerationFeedback, created bool, err error)
```

- [ ] **Step 1: 在 baseline 定义 GenerationFeedback**

```sql
CREATE TABLE generation_feedback (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL,
  publication_id uuid NOT NULL,
  render_run_id uuid NOT NULL,
  candidate_id uuid NOT NULL,
  asset_id uuid NOT NULL,
  generation integer NOT NULL CHECK (generation > 0),
  tags text[] NOT NULL CHECK (cardinality(tags) BETWEEN 1 AND 6),
  comment text NOT NULL DEFAULT '' CHECK (char_length(comment) <= 500),
  media_asset_id uuid,
  idempotency_key text NOT NULL,
  request_hash text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, idempotency_key),
  FOREIGN KEY (user_id, publication_id, render_run_id, candidate_id, generation)
    REFERENCES render_publications(user_id, id, render_run_id, candidate_id, generation),
  FOREIGN KEY (user_id, candidate_id, asset_id)
    REFERENCES render_candidates(user_id, id, asset_id),
  FOREIGN KEY (user_id, media_asset_id) REFERENCES media_assets(user_id, id)
);
```

为被引用表补齐对应 `UNIQUE` 组合键。`asset_id` 是用户实际看到的发布 Asset；`media_asset_id` 是用户额外附带的反馈图片，两者不能混用。

- [ ] **Step 2: 写“客户端不能伪造生成链路”和无图保存测试**

```go
func TestCreateGenerationFeedbackDerivesPublicationChain(t *testing.T) {
	repo := newFeedbackFake()
	got, created, err := New(repo).CreateGenerationFeedback(ctx, userID, CreateGenerationFeedbackInput{
		PublicationID: publicationID,
		Tags: []domain.Tag{domain.GenerationHairMismatch},
		IdempotencyKey: "generation-feedback-1",
	})
	if err != nil || !created { t.Fatalf("%#v %v", got, err) }
	if got.RenderRunID != renderRunID || got.CandidateID != candidateID ||
		got.AssetID != publishedAssetID || got.Generation != 2 {
		t.Fatalf("publication chain was not frozen: %#v", got)
	}
	if got.MediaAssetID != nil || got.AcknowledgementCode != domain.AckFeedbackRecorded {
		t.Fatalf("optional photo or acknowledgement wrong: %#v", got)
	}
}

func TestCreateGenerationFeedbackRejectsForeignFeedbackPhoto(t *testing.T) {
	repo := newFeedbackFake()
	_, _, err := New(repo).CreateGenerationFeedback(ctx, userID, CreateGenerationFeedbackInput{
		PublicationID: publicationID,
		Tags: []domain.Tag{domain.GenerationUnnatural},
		MediaAssetID: ptr(foreignAssetID),
		IdempotencyKey: "generation-feedback-2",
	})
	if !errors.Is(err, repository.ErrNotFound) { t.Fatalf("err=%v", err) }
}
```

- [ ] **Step 3: 运行测试并确认失败**

Run:

```bash
cd apps/server
go test ./internal/service/feedback -run TestCreateGenerationFeedback -v
```

Expected: FAIL，提示 Feedback service 未定义。

- [ ] **Step 4: 实现 GenerationFeedback 事务**

规则：

- tags 去重后保持客户端顺序，只允许六个 Generation 标签。
- comment trim 后最多 500 rune。
- publication 必须属于 user；Repository 从 publication join candidate/asset 派生全部链路字段。
- `media_asset_id` 可省略；非空时必须属于 user、`purpose='feedback'`、`state='ready'`，且 MIME 为 JPEG/PNG。
- 不创建 PreferenceMemory，不修改 `user_profiles.preferences`。
- 相同 idempotency key/hash replay；不同 hash 返回冲突。

- [ ] **Step 5: 运行 Service 和 Postgres 测试**

Run:

```bash
cd apps/server
go test ./internal/service/feedback -run TestCreateGenerationFeedback -v
TEST_DATABASE_URL='postgres://jianwo:jianwo@127.0.0.1:55432/jianwo?sslmode=disable' \
  go test ./internal/repository/postgres -run TestGenerationFeedback -count=1 -v
```

Expected: 无图反馈 201 所需数据完整；跨用户 publication/图片均为 404 语义；同 key 并发只有一行；全部 PASS。

- [ ] **Step 6: 提交 GenerationFeedback**

```bash
git add apps/server/internal/service/feedback \
  apps/server/internal/repository/postgres/feedback.go \
  apps/server/internal/repository/postgres/feedback_test.go
git commit -m "feat(server): record publication generation feedback"
```

---

### Task 6: 保存 ExecutionFeedback 并形成 PreferenceMemory

**Files:**
- Verify: `apps/server/internal/database/migrations/001_baseline.sql`
- Modify: `apps/server/internal/service/feedback/ports.go`
- Modify: `apps/server/internal/service/feedback/service.go`
- Create: `apps/server/internal/service/feedback/memory.go`
- Modify: `apps/server/internal/service/feedback/service_test.go`
- Modify: `apps/server/internal/repository/postgres/feedback.go`
- Modify: `apps/server/internal/repository/postgres/feedback_test.go`
- Modify: `apps/server/internal/service/planning/ports.go`
- Modify: `apps/server/internal/service/planning/service.go`
- Modify: `apps/server/internal/service/planning/service_test.go`

**Interfaces:**
- Consumes: `executions(state='completed')`、Selection/PlanSet 链路、Task 1 `NormalizePreference`。
- Produces:

```go
type CreateExecutionFeedbackInput struct {
	ExecutionID string
	Tags []domain.Tag
	Comment string
	MediaAssetID *string
	Preference domain.StructuredPreference
	IdempotencyKey string
}

type PreferenceMemory struct {
	ID string `json:"id"`; Key string `json:"key"`
	Category domain.PreferenceCategory `json:"category"`
	Value string `json:"value"`; SourceTag domain.Tag `json:"source_tag"`
	CreatedAt time.Time `json:"created_at"`
}

type ExecutionFeedback struct {
	ID string `json:"id"`; ExecutionID string `json:"execution_id"`
	SelectionID string `json:"selection_id"`; PlanSetID string `json:"plan_set_id"`
	Tags []domain.Tag `json:"tags"`; Comment string `json:"comment,omitempty"`
	MediaAssetID *string `json:"media_asset_id,omitempty"`
	AppliedMemories []PreferenceMemory `json:"applied_memories"`
	AcknowledgementCode domain.AcknowledgementCode `json:"acknowledgement_code"`
	CreatedAt time.Time `json:"created_at"`
}

type PreferenceMemoryReader interface {
	ListPreferenceMemories(ctx context.Context, userID string, limit int) ([]PreferenceMemory, error)
}
```

- [ ] **Step 1: 在 baseline 定义反馈与记忆表**

```sql
CREATE TABLE execution_feedback (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL,
  execution_id uuid NOT NULL,
  selection_id uuid NOT NULL,
  plan_set_id uuid NOT NULL,
  tags text[] NOT NULL CHECK (cardinality(tags) BETWEEN 1 AND 5),
  comment text NOT NULL DEFAULT '' CHECK (char_length(comment) <= 500),
  media_asset_id uuid,
  preference jsonb,
  idempotency_key text NOT NULL,
  request_hash text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, execution_id),
  UNIQUE (user_id, idempotency_key),
  FOREIGN KEY (user_id, execution_id, selection_id)
    REFERENCES executions(user_id, id, selection_id),
  FOREIGN KEY (user_id, selection_id, plan_set_id)
    REFERENCES plan_selections(user_id, id, plan_set_id),
  FOREIGN KEY (user_id, media_asset_id) REFERENCES media_assets(user_id, id)
);

CREATE TABLE preference_memories (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL,
  execution_feedback_id uuid NOT NULL,
  memory_key text NOT NULL CHECK (memory_key IN
    ('formality','complexity','avoid_color','preserve_hair','preserve_makeup','preserve_outfit')),
  category text NOT NULL CHECK (category IN ('overall','hair','makeup','outfit','color')),
  value text NOT NULL CHECK (char_length(value) BETWEEN 1 AND 40),
  source_tag text NOT NULL CHECK (source_tag IN
    ('too_formal','too_complex','dislike_color','want_to_keep')),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, execution_feedback_id, memory_key, value),
  FOREIGN KEY (user_id, execution_feedback_id)
    REFERENCES execution_feedback(user_id, id)
);
CREATE INDEX preference_memories_user_recent_idx
  ON preference_memories(user_id, created_at DESC, id DESC);
```

为 `executions(user_id,id,selection_id)` 与 `plan_selections(user_id,id,plan_set_id)` 增加唯一组合键。

- [ ] **Step 2: 写准确承诺和完成态门禁测试**

```go
func TestExecutionFeedbackPersistsMemoryBeforePromise(t *testing.T) {
	repo := newFeedbackFakeWithCompletedExecution()
	got, _, err := New(repo).CreateExecutionFeedback(ctx, userID, CreateExecutionFeedbackInput{
		ExecutionID: executionID,
		Tags: []domain.Tag{domain.ExecutionTooFormal},
		Preference: domain.StructuredPreference{
			Kind: domain.PreferenceLessFormal, Category: domain.CategoryOverall,
		},
		IdempotencyKey: "execution-feedback-1",
	})
	if err != nil { t.Fatal(err) }
	if got.AcknowledgementCode != domain.AckLessFormalSaved ||
		len(got.AppliedMemories) != 1 || got.AppliedMemories[0].Value != "less" {
		t.Fatalf("promise must match persisted memory: %#v", got)
	}
}

func TestExecutionFeedbackWithoutStructuredPreferenceMakesNoPromise(t *testing.T) {
	repo := newFeedbackFakeWithCompletedExecution()
	got, _, err := New(repo).CreateExecutionFeedback(ctx, userID, CreateExecutionFeedbackInput{
		ExecutionID: executionID,
		Tags: []domain.Tag{domain.ExecutionEasy},
		Comment: "这套很适合我，下次照做",
		IdempotencyKey: "execution-feedback-2",
	})
	if err != nil { t.Fatal(err) }
	if got.AcknowledgementCode != domain.AckFeedbackRecorded || len(got.AppliedMemories) != 0 {
		t.Fatalf("free text must not create a promise: %#v", got)
	}
}

func TestExecutionFeedbackRequiresCompletedExecution(t *testing.T) {
	repo := newFeedbackFakeWithActiveExecution()
	_, _, err := New(repo).CreateExecutionFeedback(ctx, userID, validInput)
	if !errors.Is(err, ErrExecutionNotCompleted) { t.Fatalf("err=%v", err) }
}
```

- [ ] **Step 3: 运行反馈测试并确认失败**

Run:

```bash
cd apps/server
go test ./internal/service/feedback -run TestExecutionFeedback -v
```

Expected: FAIL，提示 `CreateExecutionFeedback` 未定义。

- [ ] **Step 4: 实现一个事务中的反馈、记忆与 profile 投影**

事务顺序：

1. 按 user 锁定 Execution，要求 `state='completed'`。
2. join Selection 和 PlanSet，派生并固化 IDs。
3. 校验可选 feedback Asset。
4. 处理 idempotency replay/hash conflict。
5. 插入 ExecutionFeedback。
6. 插入 `NormalizePreference` 返回的 0 或 1 条 PreferenceMemory。
7. 若有 memory，以 CAS 更新 `user_profiles.preferences` 与 `version=version+1`；JSON 结构固定为：

```json
{
  "feedback_memory": {
    "formality": "less",
    "complexity": "simpler",
    "avoid_colors": ["荧光绿"],
    "preserve": {
      "hair": ["自然偏分"],
      "makeup": [],
      "outfit": []
    }
  }
}
```

数组去重、每类最多 10 项、保留最近写入顺序。数据库事实以 `preference_memories` 为准，profile JSON 是读取优化投影。

8. 只有事务提交后 Service 才根据已返回 memory 设置 `acknowledgement_code`。

- [ ] **Step 5: 把 PreferenceMemory 接入下一次 Planning**

Planning 每次生成新 PlanSet 前调用：

```go
memories, err := s.memories.ListPreferenceMemories(ctx, userID, 20)
```

并把以下确定结构加入 `profile_snapshot.feedback_memory` 和每个受影响步骤的 `plan_step_groundings(source_type='feedback_memory', source_id=memory.ID)`：

```go
type FeedbackMemorySnapshot struct {
	Items []struct {
		ID string `json:"id"`; Key string `json:"key"`
		Category string `json:"category"`; Value string `json:"value"`
	} `json:"items"`
}
```

`brief_hash` 只表示规范化 SceneBrief，不包含 PreferenceMemory。新增 `planning_input_hash`，按 report ID、profile snapshot、brief hash、memory IDs/values、planner schema version 和 style-rule version 计算；PlanSet 唯一键与 Task dedupe 使用 `planning_input_hash`。新增 memory 后，相同 report/scene/answers 产生新 PlanSet，不能复用旧内容。

- [ ] **Step 6: 运行 Feedback 与 Planning 测试**

Run:

```bash
cd apps/server
go test ./internal/service/feedback -run TestExecutionFeedback -v
go test ./internal/service/planning -run TestPreferenceMemory -v
TEST_DATABASE_URL='postgres://jianwo:jianwo@127.0.0.1:55432/jianwo?sslmode=disable' \
  go test ./internal/repository/postgres -run 'Test(ExecutionFeedback|PreferenceMemory)' -count=1 -v
```

Expected: active Execution 被拒绝；完成态无图反馈成功；仅结构化偏好生成 memory；下一轮 Planning 的 hash 和 grounding 包含 memory；全部 PASS。

- [ ] **Step 7: 提交执行反馈和记忆闭环**

```bash
git add apps/server/internal/database/migrations/001_baseline.sql \
  apps/server/internal/service/feedback \
  apps/server/internal/repository/postgres/feedback.go \
  apps/server/internal/repository/postgres/feedback_test.go \
  apps/server/internal/service/planning
git commit -m "feat(server): feed explicit execution preferences into planning"
```

---

### Task 7: 将 Billing 绑定 Operation 与 Publication

**Files:**
- Verify: `apps/server/internal/database/migrations/001_baseline.sql`
- Modify: `apps/server/internal/domain/billing.go`
- Modify: `apps/server/internal/service/billing/ports.go`
- Modify: `apps/server/internal/service/billing/service.go`
- Modify: `apps/server/internal/service/billing/service_test.go`
- Modify: `apps/server/internal/repository/postgres/billing.go`
- Modify: `apps/server/internal/repository/postgres/billing_test.go`
- Modify: `apps/server/internal/service/assessment/service.go`
- Modify: `apps/server/internal/service/planning/service.go`
- Modify: `apps/server/internal/service/rendering/service.go`
- Modify: `apps/server/internal/service/rendering/service_test.go`
- Modify: `apps/server/internal/bootstrap/worker.go`

**Interfaces:**
- Consumes: Operation 状态与 result 引用、Render Publication。
- Produces:

```go
type Product string
const (
	ProductAssessment Product = "assessment"
	ProductPlanSet Product = "plan_set"
	ProductRenderPublication Product = "render_publication"
)

type ChargeSource string
const (
	ChargeCredits ChargeSource = "credits"
	ChargeWelcomeAnalysis ChargeSource = "welcome_analysis"
	ChargeWelcomePlanSet ChargeSource = "welcome_plan_set"
)

type Lifecycle interface {
	Reserve(ctx context.Context, userID, operationID string, product Product, units int) (Reservation, error)
	Settle(ctx context.Context, userID, operationID string, publicationID *string) error
	Refund(ctx context.Context, userID, operationID string) error
	ReconcileTerminalOperations(ctx context.Context, limit int) (ReconcileResult, error)
}
```

- [ ] **Step 1: 用 Operation 计费结构替换 Task 引用**

```sql
CREATE TABLE billing_reservations (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL,
  operation_id uuid NOT NULL,
  product text NOT NULL CHECK (product IN ('assessment','plan_set','render_publication')),
  units integer NOT NULL CHECK (units > 0),
  charge_source text NOT NULL CHECK (charge_source IN
    ('credits','welcome_analysis','welcome_plan_set')),
  status text NOT NULL CHECK (status IN ('reserved','settled','refunded')),
  publication_id uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  settled_at timestamptz,
  refunded_at timestamptz,
  UNIQUE (user_id, id),
  UNIQUE (user_id, operation_id),
  FOREIGN KEY (user_id, operation_id) REFERENCES operations(user_id, id),
  FOREIGN KEY (user_id, publication_id) REFERENCES render_publications(user_id, id),
  CHECK (
    (product='render_publication' AND status='settled' AND publication_id IS NOT NULL)
    OR (product='render_publication' AND status<>'settled')
    OR (product<>'render_publication' AND publication_id IS NULL)
  )
);

CREATE TABLE billing_ledger (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL,
  entry_type text NOT NULL CHECK (entry_type IN ('reserve','settle','refund','purchase')),
  product text NOT NULL CHECK (product IN
    ('assessment','plan_set','render_publication','credit_pack')),
  charge_source text NOT NULL CHECK (charge_source IN
    ('credits','welcome_analysis','welcome_plan_set','purchase')),
  delta integer NOT NULL,
  operation_id uuid,
  publication_id uuid,
  order_id uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, operation_id, entry_type),
  FOREIGN KEY (user_id, operation_id) REFERENCES operations(user_id, id),
  FOREIGN KEY (user_id, publication_id) REFERENCES render_publications(user_id, id),
  FOREIGN KEY (user_id, order_id) REFERENCES billing_orders(user_id, id),
  CHECK (num_nonnulls(operation_id, order_id)=1),
  CHECK (
    (entry_type='reserve' AND operation_id IS NOT NULL AND publication_id IS NULL
      AND product<>'credit_pack'
      AND ((charge_source='credits' AND delta<0)
        OR (charge_source IN ('welcome_analysis','welcome_plan_set') AND delta=0)))
    OR (entry_type='settle' AND operation_id IS NOT NULL AND delta=0
      AND charge_source IN ('credits','welcome_analysis','welcome_plan_set')
      AND ((product='render_publication' AND publication_id IS NOT NULL)
        OR (product IN ('assessment','plan_set') AND publication_id IS NULL)))
    OR (entry_type='refund' AND operation_id IS NOT NULL AND publication_id IS NULL
      AND ((charge_source='credits' AND delta>0)
        OR (charge_source IN ('welcome_analysis','welcome_plan_set') AND delta=0)))
    OR (entry_type='purchase' AND order_id IS NOT NULL AND product='credit_pack'
      AND charge_source='purchase' AND delta>0)
  )
);
CREATE UNIQUE INDEX billing_ledger_order_purchase_uniq
  ON billing_ledger(user_id, order_id, entry_type)
  WHERE order_id IS NOT NULL;
```

免费欢迎权益也写完整 ledger，但 reserve/settle/refund 的 `delta=0`，并以 `charge_source` 区分；refund 同时恢复对应 welcome flag。credits 来源严格使用负数 reserve、零值 settle、正数 refund。测试必须覆盖两种来源。

删除 `BillingRefTask`、`RelinkBillingRefs`、`ListUnrefundedFailedTaskCharges` 和任何 `ref_type='task'` SQL。订单 purchase 继续绑定 Order，不改 Provider。

- [ ] **Step 2: 写并发预占和补生成不重复收费测试**

```go
func TestReserveSameRenderOperationChargesOnce(t *testing.T) {
	repo := newBillingFake(2)
	svc := New(repo)
	first, err := svc.Reserve(ctx, userID, renderOperationID, domain.ProductRenderPublication, 1)
	if err != nil { t.Fatal(err) }
	second, err := svc.Reserve(ctx, userID, renderOperationID, domain.ProductRenderPublication, 1)
	if err != nil || second.ID != first.ID { t.Fatalf("%#v %v", second, err) }
	if repo.walletCredits != 1 || repo.countLedger("reserve") != 1 {
		t.Fatalf("credits=%d ledger=%#v", repo.walletCredits, repo.ledger)
	}
}

func TestCandidateTwoUsesExistingReservation(t *testing.T) {
	billing := &billingSpy{}
	rendering := newRenderingServiceWithBilling(billing)
	if err := rendering.StartRun(ctx, paidRun); err != nil { t.Fatal(err) }
	if err := rendering.HandleQualityRejected(ctx, paidRun.ID, candidate1.ID); err != nil { t.Fatal(err) }
	if err := rendering.PublishCandidate(ctx, paidRun.ID, candidate2.ID); err != nil { t.Fatal(err) }
	if billing.reserveCalls != 1 || billing.settleCalls != 1 ||
		billing.settledPublicationID != publication2.ID {
		t.Fatalf("billing calls=%#v", billing)
	}
}
```

- [ ] **Step 3: 运行 Billing 测试并确认失败**

Run:

```bash
cd apps/server
go test ./internal/service/billing ./internal/service/rendering \
  -run 'Test(ReserveSameRenderOperation|CandidateTwoUsesExistingReservation)' -v
```

Expected: FAIL，提示 Operation 生命周期计费接口未定义。

- [ ] **Step 4: 实现 Reserve、Settle、Refund 状态机**

`Reserve`：

- 锁 wallet 和 Operation。
- Operation 必须属于 user 且状态为 `accepted|running|retrying`。
- `(user_id,operation_id)` 已有同 product/units 时 replay；不同参数返回 `ErrReservationConflict`。
- credits 来源扣减 wallet；welcome 来源 CAS 标记已用。
- 插入 reservation 和一条 reserve ledger；credits 来源 delta 为负数，welcome 来源 delta 为零。

`Settle`：

- 锁 reservation 和 Operation。
- Operation 必须 `succeeded`。
- Assessment 要求 `result_type='report'`；PlanSet 要求 `result_type='plan_set'`。
- Render 要求 `result_type='render_publication'` 且 result ID 等于参数 Publication ID；Publication 必须属于同 user 和该 Operation 对应 RenderRun。
- `reserved → settled` 并插入一条 settle ledger；重复调用 no-op。
- 已 refunded 时返回 `ErrAlreadyRefunded`，不能再次扣款。

`Refund`：

- 只接受 Operation `failed|cancelled|superseded`。
- `reserved → refunded`；credits 来源返还 wallet 并写 refund ledger，welcome 来源恢复对应 flag。
- settled reservation 不能退款，返回 `ErrAlreadySettled`。
- 重复调用 no-op。

- [ ] **Step 5: 接入三个付费 Operation**

统一调用顺序：

```go
operation, replayed, err := operations.Create(...)
if err != nil { return err }
reservation, err := billing.Reserve(ctx, userID, operation.ID, product, 1)
if err != nil {
	_ = operations.Fail(ctx, userID, operation.ID, "billing_rejected", false)
	return err
}
_ = reservation
// 只有 Reserve 成功后才创建首个 Task。
```

终态：

```go
if err := operations.Succeed(ctx, userID, operation.ID, resultType, resultID); err != nil { return err }
if err := billing.Settle(ctx, userID, operation.ID, publicationID); err != nil {
	logger.Error("billing settle pending reconciliation", "operation_id", operation.ID, "error", err)
}
```

失败、取消或 superseded：

```go
if err := operations.FailOrSupersede(...); err != nil { return err }
if err := billing.Refund(ctx, userID, operation.ID); err != nil {
	logger.Error("billing refund pending reconciliation", "operation_id", operation.ID, "error", err)
}
```

Render 的 `HandleQualityRejected` 只创建 Candidate 2 Task，禁止调用 `Reserve`。网络重试和 provider fallback 都保留同一个 Operation ID。

- [ ] **Step 6: 实现 reconciliation**

`ReconcileTerminalOperations(limit=100)` 查询：

- `succeeded + reserved`：按 result 类型调用 Settle。
- `failed|cancelled|superseded + reserved`：调用 Refund。

Worker 每 30 秒调用一次；单条失败记录 operation ID 和 error 后继续其他条。查询使用 `FOR UPDATE SKIP LOCKED`，多 Worker 可并行。该 reconciliation 不是业务 Task，不注册新的 Task 类型，也不修改 Task runner 循环。

- [ ] **Step 7: 运行 Billing 集成测试**

Run:

```bash
cd apps/server
go test ./internal/service/billing ./internal/service/assessment \
  ./internal/service/planning ./internal/service/rendering -count=1 -v
TEST_DATABASE_URL='postgres://jianwo:jianwo@127.0.0.1:55432/jianwo?sslmode=disable' \
  go test ./internal/repository/postgres -run 'TestBilling(Operation|Reconcile|Concurrent)' -count=10 -v
```

Expected:

- 同 Operation 并发 Reserve 只扣一次。
- Candidate 1 reject、Candidate 2 publish 只扣一次并绑定 Publication 2。
- Render 无 Publication 而失败时全额退款。
- succeeded 后 Settle 进程中断可由 reconciliation 补齐。
- Ledger 中不存在 Task ID。
- 全部 PASS。

- [ ] **Step 8: 提交 Operation 计费生命周期**

```bash
git add apps/server/internal/database/migrations/001_baseline.sql \
  apps/server/internal/domain/billing.go \
  apps/server/internal/service/billing \
  apps/server/internal/repository/postgres/billing.go \
  apps/server/internal/repository/postgres/billing_test.go \
  apps/server/internal/service/assessment/service.go \
  apps/server/internal/service/planning/service.go \
  apps/server/internal/service/rendering \
  apps/server/internal/bootstrap/worker.go
git commit -m "feat(server): settle billing on published operations"
```

---

### Task 8: 暴露 Selection 与 Execution API

**Files:**
- Create: `apps/server/internal/httpapi/executions.go`
- Create: `apps/server/internal/httpapi/executions_test.go`
- Modify: `apps/server/internal/httpapi/httpapi.go`
- Modify: `contracts/openapi.yaml`
- Modify: `packages/core/src/types/index.ts`
- Modify: `packages/core/src/api/endpoints.ts`
- Modify: `packages/core/tests/client.test.mjs`

**Interfaces:**
- Consumes: Tasks 2–4 Execution Service。
- Produces:

```text
PUT  /v1/plan-sets/{id}/selection
POST /v1/selections/{id}/executions
GET  /v1/executions/{id}
POST /v1/executions/{id}/events
```

- [ ] **Step 1: 写 HTTP header 和状态码失败测试**

```go
func TestPostExecutionEventRequiresIdempotencyAndIfMatch(t *testing.T) {
	api := newHTTPFixture(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/executions/"+executionID+"/events",
		strings.NewReader(`{"client_event_id":"device-a-1","type":"started","occurred_at":"2026-09-12T07:00:00Z"}`))
	req.Header.Set("Authorization", "Bearer "+api.Token)
	res := httptest.NewRecorder()
	api.Handler.ServeHTTP(res, req)
	if res.Code != http.StatusPreconditionRequired { t.Fatalf("code=%d body=%s", res.Code, res.Body) }

	req = cloneRequest(...)
	req.Header.Set("Idempotency-Key", "device-a-1")
	req.Header.Set("If-Match", `"1"`)
	res = httptest.NewRecorder()
	api.Handler.ServeHTTP(res, req)
	if res.Code != http.StatusCreated || res.Header().Get("ETag") != `"2"` {
		t.Fatalf("code=%d etag=%q body=%s", res.Code, res.Header().Get("ETag"), res.Body)
	}
}
```

- [ ] **Step 2: 运行 HTTP 测试并确认失败**

Run:

```bash
cd apps/server
go test ./internal/httpapi -run 'Test(PostExecutionEvent|PutSelection|CreateExecution)' -v
```

Expected: FAIL，新路由返回 404 或 handler 未定义。

- [ ] **Step 3: 实现 handler 协议翻译**

精确请求：

```json
PUT /v1/plan-sets/{plan_set_id}/selection
Idempotency-Key: selection-device-a-1
{
  "plan_variant_id": "uuid",
  "render_publication_id": "uuid-or-omitted"
}
```

首次 `201 {"data":Selection}`，replay `200`。

```json
POST /v1/selections/{selection_id}/executions
Idempotency-Key: execution-device-a-1
{}
```

首次 `201 {"data":Execution}`，replay `200`，两者都返回 `ETag: "<version>"`。

```json
POST /v1/executions/{execution_id}/events
Idempotency-Key: device-a-0001
If-Match: "3"
{
  "client_event_id": "device-a-0001",
  "type": "step_completed",
  "step_id": "uuid",
  "occurred_at": "2026-09-12T07:00:00Z"
}
```

首次 `201 {"data":{"event":ExecutionEvent,"execution":Execution}}`；replay `200`；ETag 来自返回 Execution version。

错误映射：

```text
400 validation_error
404 not_found
409 idempotency_conflict | invalid_transition | execution_not_completed
412 version_conflict
428 precondition_required
```

错误 envelope 必须继续包含 `retryable` boolean；version conflict 为 `true`，其余上述冲突为 `false`。

- [ ] **Step 4: 更新 OpenAPI 和 core endpoint**

OpenAPI 的 Selection/Execution schema 必须与 Task 1 JSON tag 完全一致；`Idempotency-Key`、`If-Match` 和 `ETag` 定义为 reusable parameters/headers。core 增加：

```ts
putSelection(planSetId: string, input: PutSelectionInput, idempotencyKey: string): Promise<Selection>
createExecution(selectionId: string, idempotencyKey: string): Promise<Execution>
getExecution(id: string): Promise<Execution>
appendExecutionEvent(
  executionId: string,
  input: ExecutionEventInput,
  expectedVersion: number
): Promise<ExecutionEventResult>
```

`appendExecutionEvent` headers 必须是：

```ts
{
  'Idempotency-Key': input.client_event_id,
  'If-Match': `"${expectedVersion}"`
}
```

不需要读取响应 ETag；客户端使用响应 body 的 `execution.version` 发下一次事件。

- [ ] **Step 5: 移除旧执行路由**

删除以下路由及对应旧 Service/Repository 方法：

```text
POST  /v1/plans/{id}/select
GET   /v1/plans/{id}/checklist
PATCH /v1/plans/{id}/checklist/{itemId}
POST  /v1/plans/{id}/feedback
```

不得保留别名或转发。core 的 `selectPlan/getChecklist/updateChecklistItem/sendPlanFeedback` 同步删除。

- [ ] **Step 6: 运行 HTTP 与契约测试**

Run:

```bash
cd apps/server
go test ./internal/httpapi -run 'Test(PostExecutionEvent|PutSelection|CreateExecution)' -v
cd ../..
node contracts/scripts/check-sync.mjs
pnpm --filter @zsm/core typecheck
pnpm --filter @zsm/core test
```

Expected: HTTP 状态码/header 测试 PASS；契约与 `API_PATHS` 完全一致；core typecheck/test PASS。

- [ ] **Step 7: 提交执行 API**

```bash
git add apps/server/internal/httpapi/executions.go \
  apps/server/internal/httpapi/executions_test.go \
  apps/server/internal/httpapi/httpapi.go \
  contracts/openapi.yaml \
  packages/core/src/types/index.ts \
  packages/core/src/api/endpoints.ts \
  packages/core/tests/client.test.mjs
git commit -m "feat(api): expose selection and execution resources"
```

---

### Task 9: 暴露两类 Feedback API 与准确文案

**Files:**
- Create: `apps/server/internal/httpapi/feedback.go`
- Create: `apps/server/internal/httpapi/feedback_test.go`
- Modify: `apps/server/internal/httpapi/httpapi.go`
- Modify: `contracts/openapi.yaml`
- Modify: `packages/core/src/types/index.ts`
- Modify: `packages/core/src/api/endpoints.ts`
- Modify: `packages/core/src/copy/zh.ts`
- Modify: `packages/core/tests/client.test.mjs`
- Modify: `packages/core/tests/copy.test.mjs`

**Interfaces:**
- Consumes: Tasks 5–6 Feedback Service。
- Produces:

```text
POST /v1/generation-feedback
POST /v1/execution-feedback
```

- [ ] **Step 1: 写无图反馈与准确确认码 HTTP 测试**

```go
func TestGenerationFeedbackAcceptsNoMedia(t *testing.T) {
	req := authenticatedJSON(t, http.MethodPost, "/v1/generation-feedback", map[string]any{
		"publication_id": publicationID,
		"tags": []string{"unnatural"},
	})
	req.Header.Set("Idempotency-Key", "generation-feedback-http-1")
	res := serve(t, req)
	assertStatus(t, res, http.StatusCreated)
	assertJSONPath(t, res.Body.Bytes(), "data.acknowledgement_code", "feedback_recorded")
	assertJSONPathAbsent(t, res.Body.Bytes(), "data.media_asset_id")
}

func TestExecutionFeedbackReturnsPromiseOnlyForAppliedMemory(t *testing.T) {
	req := authenticatedJSON(t, http.MethodPost, "/v1/execution-feedback", map[string]any{
		"execution_id": completedExecutionID,
		"tags": []string{"too_formal"},
		"preference": map[string]string{"kind":"less_formal","category":"overall"},
	})
	req.Header.Set("Idempotency-Key", "execution-feedback-http-1")
	res := serve(t, req)
	assertStatus(t, res, http.StatusCreated)
	assertJSONPath(t, res.Body.Bytes(), "data.acknowledgement_code", "less_formal_saved")
	assertJSONArrayLength(t, res.Body.Bytes(), "data.applied_memories", 1)
}
```

- [ ] **Step 2: 运行 HTTP 测试并确认失败**

Run:

```bash
cd apps/server
go test ./internal/httpapi -run 'Test(GenerationFeedback|ExecutionFeedback)' -v
```

Expected: FAIL，新路由返回 404。

- [ ] **Step 3: 实现反馈 handler 与 OpenAPI**

Generation 请求只接受：

```json
{
  "publication_id": "uuid",
  "tags": ["identity_mismatch"],
  "comment": "可省略",
  "media_asset_id": "可省略"
}
```

Execution 请求只接受：

```json
{
  "execution_id": "uuid",
  "tags": ["want_to_keep"],
  "comment": "可省略",
  "media_asset_id": "可省略",
  "preference": {
    "kind": "preserve",
    "category": "hair",
    "value": "自然偏分"
  }
}
```

两个端点首次创建返回 201，idempotent replay 返回 200。图片上传不嵌入反馈 endpoint：客户端先走既有 `upload-intent → complete`，成功后才传 `media_asset_id`；上传失败时省略该字段，文字和标签照常提交。

- [ ] **Step 4: 在 core 建立确认码到文案的唯一映射**

`packages/core/src/copy/zh.ts`：

```ts
export const FEEDBACK_ACK_COPY = {
  feedback_recorded: '反馈已记录。',
  less_formal_saved: '已记住：下次方案会降低正式度。',
  simpler_saved: '已记住：下次方案会减少复杂步骤。',
  avoid_color_saved: '已记住：下次方案会避开你指定的颜色。',
  preserve_saved: '已记住：下次方案会保留你指定的做法。'
} as const

export type FeedbackAcknowledgementCode = keyof typeof FEEDBACK_ACK_COPY

export function feedbackAcknowledgement(code: FeedbackAcknowledgementCode): string {
  return FEEDBACK_ACK_COPY[code]
}
```

服务端不返回任意 `message`，只返回受枚举约束的 code 和实际 `applied_memories`。删除旧“会用来微调后续建议”“下次优先保留”的无条件文案。

- [ ] **Step 5: 写文案真实性测试**

```js
test('feedback copy promises only persisted memory', () => {
  assert.equal(feedbackAcknowledgement('feedback_recorded'), '反馈已记录。')
  assert.doesNotMatch(feedbackAcknowledgement('feedback_recorded'), /下次|保留|调整|记住/)
  assert.match(feedbackAcknowledgement('less_formal_saved'), /下次方案会降低正式度/)
})
```

- [ ] **Step 6: 运行反馈 API 与 core 测试**

Run:

```bash
cd apps/server
go test ./internal/httpapi -run 'Test(GenerationFeedback|ExecutionFeedback)' -v
cd ../..
node contracts/scripts/check-sync.mjs
pnpm --filter @zsm/core typecheck
pnpm --filter @zsm/core test
```

Expected: 无图提交 201；错误 media ID 404；active Execution 409；确认码与 applied memory 对齐；契约同步；全部 PASS。

- [ ] **Step 7: 提交反馈 API 和文案**

```bash
git add apps/server/internal/httpapi/feedback.go \
  apps/server/internal/httpapi/feedback_test.go \
  apps/server/internal/httpapi/httpapi.go \
  contracts/openapi.yaml \
  packages/core/src/types/index.ts \
  packages/core/src/api/endpoints.ts \
  packages/core/src/copy/zh.ts \
  packages/core/tests/client.test.mjs \
  packages/core/tests/copy.test.mjs
git commit -m "feat(api): separate quality and execution feedback"
```

---

### Task 10: 完成端到端、删除旧路径并通过全部门禁

**Files:**
- Modify: `apps/server/scripts/e2e.sh`
- Verify: `apps/server/internal/database/migrations/001_baseline.sql`
- Test: `apps/server/internal/service/execution/**`
- Test: `apps/server/internal/service/feedback/**`
- Test: `apps/server/internal/service/billing/**`

**Interfaces:**
- Consumes: Tasks 1–9 的全部服务与 API。
- Produces: 一条从发布结果到选择、执行、反馈、下一轮规划和计费审计的可重复 E2E。旧路径、旧 migrations 和巨型 Repository 的物理删除统一由最终 Peripherals/Cutover 计划执行。

- [ ] **Step 1: 扩展 E2E 主链**

在已有 Report/PlanSet/Publication fixture 后加入以下断言流程：

```sh
selection_json="$(curl -fsS -X PUT "$api_base/v1/plan-sets/$plan_set_id/selection" \
  -H "Authorization: Bearer $token" -H 'content-type: application/json' \
  -H 'Idempotency-Key: e2e-selection-1' \
  -d "{\"plan_variant_id\":\"$variant_id\",\"render_publication_id\":\"$publication_id\"}")"
selection_id="$(printf '%s' "$selection_json" | jq -r '.data.id')"

execution_json="$(curl -fsS -X POST "$api_base/v1/selections/$selection_id/executions" \
  -H "Authorization: Bearer $token" -H 'content-type: application/json' \
  -H 'Idempotency-Key: e2e-execution-1' -d '{}')"
execution_id="$(printf '%s' "$execution_json" | jq -r '.data.id')"
test "$(printf '%s' "$execution_json" | jq '.data.steps | length')" -eq 3

version=1
for step_id in $(printf '%s' "$execution_json" | jq -r '.data.steps[].id'); do
  event_id="e2e-step-$step_id"
  event_json="$(curl -fsS -X POST "$api_base/v1/executions/$execution_id/events" \
    -H "Authorization: Bearer $token" -H 'content-type: application/json' \
    -H "Idempotency-Key: $event_id" -H "If-Match: \"$version\"" \
    -d "{\"client_event_id\":\"$event_id\",\"type\":\"step_completed\",\"step_id\":\"$step_id\",\"occurred_at\":\"$(date -u +%FT%TZ)\"}")"
  version="$(printf '%s' "$event_json" | jq -r '.data.execution.version')"
done

complete_json="$(curl -fsS -X POST "$api_base/v1/executions/$execution_id/events" \
  -H "Authorization: Bearer $token" -H 'content-type: application/json' \
  -H 'Idempotency-Key: e2e-complete-1' -H "If-Match: \"$version\"" \
  -d "{\"client_event_id\":\"e2e-complete-1\",\"type\":\"completed\",\"occurred_at\":\"$(date -u +%FT%TZ)\"}")"
printf '%s' "$complete_json" | jq -e '.data.execution.state == "completed"' >/dev/null

# 完全重放最后事件：即使 If-Match 仍是旧 version，也不能重复递增。
replay_json="$(curl -fsS -X POST "$api_base/v1/executions/$execution_id/events" \
  -H "Authorization: Bearer $token" -H 'content-type: application/json' \
  -H 'Idempotency-Key: e2e-complete-1' -H "If-Match: \"$version\"" \
  -d "{\"client_event_id\":\"e2e-complete-1\",\"type\":\"completed\",\"occurred_at\":\"$(printf '%s' "$complete_json" | jq -r '.data.event.occurred_at')\"}")"
test "$(printf '%s' "$replay_json" | jq -r '.data.execution.version')" = \
  "$(printf '%s' "$complete_json" | jq -r '.data.execution.version')"
```

- [ ] **Step 2: 扩展 Feedback 与 PreferenceMemory E2E**

```sh
curl -fsS -X POST "$api_base/v1/generation-feedback" \
  -H "Authorization: Bearer $token" -H 'content-type: application/json' \
  -H 'Idempotency-Key: e2e-generation-feedback-1' \
  -d "{\"publication_id\":\"$publication_id\",\"tags\":[\"unnatural\"]}" \
  | jq -e '.data.acknowledgement_code == "feedback_recorded" and (.data | has("media_asset_id") | not)' >/dev/null

curl -fsS -X POST "$api_base/v1/execution-feedback" \
  -H "Authorization: Bearer $token" -H 'content-type: application/json' \
  -H 'Idempotency-Key: e2e-execution-feedback-1' \
  -d "{\"execution_id\":\"$execution_id\",\"tags\":[\"too_formal\"],\"preference\":{\"kind\":\"less_formal\",\"category\":\"overall\"}}" \
  | jq -e '.data.acknowledgement_code == "less_formal_saved" and (.data.applied_memories | length) == 1' >/dev/null

next_plan_set_json="$(curl -fsS -X POST "$api_base/v1/plan-sets" \
  -H "Authorization: Bearer $token" -H 'content-type: application/json' \
  -H 'Idempotency-Key: e2e-plan-set-after-feedback-1' \
  -d "{\"report_id\":\"$report_id\",\"scene\":\"interview\",\"brief\":{\"when\":\"week\",\"format\":\"onsite\",\"preparation\":\"key_piece\",\"impression\":\"natural\"}}")"
next_plan_set_id="$(printf '%s' "$next_plan_set_json" | jq -r '.data.id')"
test "$next_plan_set_id" != "$plan_set_id"
```

公开 API 不暴露完整 `profile_snapshot`。PreferenceMemory 确实进入 planner 输入、PlanSet hash 和 grounding 的断言由 Task 6 的 `TestPreferenceMemory...` Service/Postgres 测试完成；E2E 只验证新增明确偏好后不复用旧 PlanSet。

- [ ] **Step 3: 加入 Billing 审计 E2E**

在测试环境提供只读诊断端点不可接受；直接在 `apps/server/internal/repository/postgres/billing_test.go` 查询 DB，并在 E2E 通过公开 `/v1/billing/me` 断言余额：

```text
余额变化 = Report 1 + PlanSet 1 + 每个成功 Publication 1
Candidate 2 不增加变化
失败 Render Operation 的余额恢复
同一 Idempotency-Key replay 不增加变化
```

支付订单 E2E 保持原样；不修改签名或通知解析。

- [ ] **Step 4: 删除旧实现和旧迁移**

Run:

```bash
rg -n "selected_at|checklist_items|AddPlanFeedback|feedbackMessage|BillingRefTask|RelinkBillingRefs|ListUnrefundedFailedTaskCharges|ref_type.?=.?['\"]task" \
  apps/server packages/core contracts
```

Expected: 无匹配。`billing_orders`、`purchase` ledger 和支付 Provider 仍存在。

Run:

```bash
rg -n "/v1/plans/\\{id\\}/(select|checklist|feedback)|/v1/plans/.+/(select|checklist|feedback)" \
  contracts apps/server packages/core
```

Expected: 无匹配。

- [ ] **Step 5: 运行服务端定向回归**

Run:

```bash
cd apps/server
go test ./internal/domain ./internal/service/execution ./internal/service/feedback \
  ./internal/service/billing ./internal/service/planning ./internal/service/rendering \
  ./internal/httpapi -count=1 -v
TEST_DATABASE_URL='postgres://jianwo:jianwo@127.0.0.1:55432/jianwo?sslmode=disable' \
  go test ./internal/repository/postgres -count=1 -v
```

Expected: 全部 PASS，无 race、唯一冲突或跨用户泄露。

- [ ] **Step 6: 运行端到端**

Run:

```bash
make e2e
```

Expected: 末行包含 `e2e passed`，并明确打印 `selection execution feedback billing passed`。

- [ ] **Step 7: 运行仓库全部门禁**

Run:

```bash
pnpm typecheck && pnpm lint && make design-build
node apps/miniapp/scripts/check.mjs
make server-vet && make server-test
```

Expected: 所有命令 exit 0；`make design-build` 无生成差异；miniapp 检查无裸 px/WebP/条件渲染违规；Go vet/test 全部通过。

- [ ] **Step 8: 检查提交边界**

Run:

```bash
git status --short
git diff --check
git log --oneline -10
```

Expected:

- 只有本计划 File Structure 列出的文件发生变化。
- `appearance-coach-prototype/`、根级冻结文件和 payment/login Provider 无变化。
- `git diff --check` 无输出。
- Tasks 1–9 各有一个可独立评审提交，Task 10 只包含旧路径删除和 E2E 收尾。

- [ ] **Step 9: 提交删除与 E2E 收尾**

```bash
git add apps/server/scripts/e2e.sh \
  apps/server/internal/service \
  apps/server/internal/repository \
  apps/server/internal/database/migrations \
  contracts/openapi.yaml \
  packages/core
git commit -m "test: close execution feedback billing loop"
```

---

## Completion Criteria

- Selection 不再修改 Plan 或 PlanVariant；同一自然选择和同一 idempotency 请求都不产生重复资源。
- Execution 创建时完整复制 hair、makeup、outfit 步骤；源方案之后不可影响快照。
- 所有清单变化来自 ExecutionEvent；相同 `client_event_id` 重放不增加 version，竞争写由 ETag CAS 拒绝。
- GenerationFeedback 的 run/candidate/asset/generation 全部由 Publication 派生，关联完整率 100%。
- ExecutionFeedback 只在 completed 后保存；无图路径与带图路径都通过，图片失败不阻塞无图反馈。
- 自由文本和生成负反馈不会形成 PreferenceMemory；明确结构化偏好写入 memory 并进入下一次 PlanSet hash、snapshot 和 grounding。
- `feedback_recorded` 文案不包含“下次”“记住”“保留”“调整”；只有实际持久化 memory 才返回对应承诺码。
- Assessment、PlanSet、Render 每个 Operation 最多一条 reservation；Render 只有 Publication 才 settle。
- Candidate 2、Provider fallback 和 Task retry 不调用第二次 Reserve；失败、取消和 superseded Operation 可幂等退款。
- Billing Ledger 不含 Task ID；Render settle 同时具有 Operation ID 和 Publication ID。
- 旧 selection/checklist/feedback API、旧表和旧 Task billing 引用完全删除，没有兼容分支。
- 登录、支付 Provider、COS SDK 和天气 Provider 内部实现无变化。
- 定向单测、PostgreSQL 集成测试、E2E 和仓库全部门禁通过。

---

> **执行修正（2026-09-15，monorepo → quality-core rebase 移植时记录）：**
>
> 1. **未开通支付不拦生成（旧 31916c1 移植）**：旧架构的 `SkipCreditCharge`
>    在按域服务 + 任务运行器架构下落为 `postgres.WithSkipCreditCharge(!cfg.BillingPaymentEnabled)`
>    （bootstrap/api.go）。Reserve 的 `ChargeCredits` 分支在次数不足时不再一律
>    `ErrInsufficientCredits`：开关打开时照常受理、reserve 台账记 `delta=0`；
>    每日配额与欢迎礼路径不受影响（与旧行为一致）。对称地，`refundReserved`
>    改为按该 operation 的 reserve 台账实扣退回（`creditBack = -delta`），
>    跳过扣次的受理退款不会凭空返次数。迁移 002 的
>    `billing_ledger_entry_shape_check` 相应放开 credits reserve/refund 的
>    `delta=0`（不扣不返），并在注释中说明。
> 2. **body_presentations 纳入用户删除与基线清单**：002 迁移新增
>    `body_presentations` 后，`DeleteUserData` 行级删除清单补上
>    `DELETE FROM body_presentations`（否则「删除我的数据」残留 3D 形象行），
>    基线 schema 断言表数钉为 50。
> 3. **演示判定只认 source_kind**：body-viewer 的对比滑杆门控（满 8 帧出对比）
>    由 `provider_version` 前缀改为服务端投影的 `source_kind === 'demo_example'`，
>    与角标红线一致（check.mjs 禁止客户端按 provider 推断）。
