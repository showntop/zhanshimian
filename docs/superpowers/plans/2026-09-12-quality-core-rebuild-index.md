# Quality Core Rebuild Plan Set

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement the linked plans in the locked order below. Each linked plan uses checkbox (`- [ ]`) steps for tracking.

**Goal:** 规定七份质量核心实施计划的唯一执行顺序、跨计划接口和文件所有权，避免重复 codegen、接口漂移、提前删除或同一文件被多阶段相互覆盖。

**Architecture:** 设计基线见 `docs/superpowers/specs/2026-09-12-quality-core-rebuild-design.md`。部署保持 Go 模块化单体、独立 API/Worker、PostgreSQL 和私有 COS；服务端保持 `httpapi → service → repository/provider/storage`，所有 AI 产物先形成不可变候选并经过质量门禁。

**Tech Stack:** Go 1.24、PostgreSQL 16、pgx/v5、OpenAPI 3.1、TypeScript 5.6/5.9、Taro 4.2、React 18.3、Node 22、pnpm 10。

## Global Constraints

- 不保留历史数据、旧 API、旧 DTO、旧本地缓存、双写或兼容层。
- 不修改 `appearance-coach-prototype/`。
- 生成图角标为“风格参考”，Demo 为“效果示例”；来源以 `source_kind` 强类型区分。
- 小程序禁止 WebP 和裸 `px`；不把真机验收或微信提审材料列为任务。
- 不打颜值分、身材分，不推断医学、健康、族裔、年龄、人格或社会身份。
- 不引入微服务、Kafka、Redis 队列、Saga、事件溯源、通用工作流引擎、BaseRepository 或全能 Service。
- 每份计划执行前必须确保工作区既有用户改动不被加入该任务提交。
- 本索引的顺序、接口和所有权高于单份计划中的旧描述；发现冲突时先修正文档，不编写适配层。

## Task 0: Reconcile Workspace Rules and Freeze the Contract

**Files:**

- Modify: `AGENTS.md`
- Modify: `contracts/openapi.yaml`
- Modify: `contracts/scripts/check-sync.mjs`
- Modify: `packages/core/package.json`
- Modify: `pnpm-lock.yaml`
- Create: `packages/core/scripts/check-openapi.mjs`
- Create: `packages/core/src/api/generated/schema.ts`
- Create: `packages/core/src/api/client.ts`
- Create: `packages/core/src/api/types.ts`
- Modify: `packages/core/src/index.ts`

**Interfaces:**

- Produces the canonical DTO and Operation/Media types defined below.
- Produces `pnpm --filter @zsm/core api:generate` and `api:check`.
- Produces the workspace rules every later plan must obey.

- [ ] **Step 1: Write the rule/contract failing checks**

```bash
rg 'AI 风格预览' AGENTS.md
rg 'useTaskPolling' AGENTS.md
test -f packages/core/src/api/generated/schema.ts
pnpm --filter @zsm/core api:check
```

Expected before Task 0: first two commands find stale rules; generated schema/api check is absent or fails.

- [ ] **Step 2: Reconcile AGENTS.md with the approved product design**

Replace the image rule with:

```text
生成图必须携带 source_kind=generated_preview，用户角标统一“风格参考”；
Demo 使用 demo_example + “效果示例”；内置图使用 bundled_reference + “风格参考”。
来源真实性依赖强类型和埋点，不依赖角标文字或 URL。
```

Replace the polling rule with:

```text
异步页面只轮询公开 Operation；packages/core 提供平台无关 controller，
Miniapp 只使用 useOperationPolling React 包装；页面隐藏即停，连续失败 5 次进入失败态。
```

- [ ] **Step 3: Execute Miniapp plan Task 1 exactly once**

Freeze the final `/v1` schemas for Foundation, Assessment, Planning, Rendering, Execution, Feedback and retained peripherals. Generate `packages/core/src/api/generated/schema.ts`; delete no old client files until Miniapp Task 12.

- [ ] **Step 4: Verify the contract freeze**

```bash
pnpm --filter @zsm/core api:generate
pnpm --filter @zsm/core api:check
node contracts/scripts/check-sync.mjs
pnpm --filter @zsm/core typecheck
```

Expected: all commands exit 0; source-kind exact type has no missing or extra member; no `generated.ts` exists.

- [ ] **Step 5: Commit the rule/contract freeze**

```bash
git add AGENTS.md contracts packages/core/package.json packages/core/scripts \
  packages/core/src/api packages/core/src/index.ts pnpm-lock.yaml
git commit -m "docs(core): freeze quality rebuild rules and contract"
```

## Locked Execution Order

0. 执行本索引的 Rule/Contract Freeze。
1. 只执行 `2026-09-12-miniapp-quality-loop.md` 的 Task 1，冻结最终 OpenAPI 和 generated client；其余 Miniapp Task 暂停。
2. `2026-09-12-quality-core-foundation.md`
3. `2026-09-12-assessment-report.md`
4. `2026-09-12-planning-render-spec.md`
5. `2026-09-12-rendering-quality.md`
6. `2026-09-12-execution-feedback-billing.md`
7. 执行 `2026-09-12-miniapp-quality-loop.md` 的 Tasks 2–15。
8. `2026-09-12-peripherals-cutover.md`

原因：

- Rule/Contract Freeze 先消除 AGENTS 硬规则与已确认产品设计的冲突，并一次性冻结传输 DTO。
- Foundation 再提供 baseline、Media、Operation、Task、Invocation、幂等和 Billing reservation。
- Assessment 发布 Report 后，Planning 才能冻结 ReportReader。
- Planning 定义 RenderSpec，Rendering 只能消费，不能重复声明。
- Rendering 发布 Publication 后，Selection/Execution/Feedback/Billing 才有稳定引用。
- 服务端领域计划消费已冻结契约，不在实施中反复修改 OpenAPI；Miniapp 后续只消费 generated client。
- Peripherals 最后接入稳定 ReadModel，并删除旧系统。

## Exclusive Ownership

### Rule/Contract Freeze

独占：

- 更新 `AGENTS.md`：生成图角标改为已确认的“风格参考”，Demo 保持“效果示例”，并要求 `source_kind` 强类型区分。
- 更新 `AGENTS.md`：异步页面只轮询公开 Operation；`@zsm/core` 提供平台无关 controller，Miniapp 使用唯一 `useOperationPolling` React 包装。
- `contracts/**` 的最终质量主链和外围 API。
- OpenAPI codegen 版本、脚本和 `packages/core/src/api/generated/schema.ts`。

完成该阶段前不得执行任何业务实现任务。

### Foundation

独占：

- `apps/server/internal/database/migrations/001_baseline.sql` 的初始创建。
- Operation/Task/lease/heartbeat/CAS、共享 QualityEvaluation 和 ProviderInvocation 基础类型。
- Media/UploadIntent。
- `cmd/api` 与 `cmd/worker` 的独立入口。

Foundation 按设计和六份领域计划一次性写入最终 baseline；后续计划只验证自己的表与约束，不修改 baseline，也不得改变 Foundation 的 lease、Operation 或幂等语义。

### Assessment/Report

独占：

- Assessment/Report Go Domain、Service、Postgres、Provider 和 HTTP。
- `apps/server/eval/assessment/`。

不得修改 `packages/core/**` 或 `apps/miniapp/**`。

### Planning/RenderSpec

独占：

- Planning/RenderSpec Go Domain、Service、Postgres、Provider 和 HTTP。
- 六场景 Brief、PlanSet Gate、RenderSpec Compiler。
- `apps/server/eval/planning/`。

不得修改 `packages/core/**` 或 `apps/miniapp/**`。

### Rendering/Quality

独占：

- Rendering/Quality Go Domain、Service、Postgres、Provider、Storage 和 HTTP。
- `render_specs` 的读取，不得重新声明 RenderSpec。
- `apps/server/eval/rendering/`。
- AI 路由的多图能力元数据和 full-look 等价 fallback。

不得修改 `packages/core/**` 或 `apps/miniapp/**`。

### Execution/Feedback/Billing

独占：

- Selection、Execution、两类 Feedback、PreferenceMemory、Billing 生命周期。
- `packages/core/src/copy/zh.ts` 中反馈承诺文案。

不得修改手写 Core API 类型/端点或 Miniapp 页面。

### Miniapp Quality Loop

独占：

- Rule/Contract Freeze 后使用已固定的 `openapi-typescript@7.13.0` 和 `openapi-fetch@0.17.0`。
- Task 1 独占 `packages/core/src/api/generated/schema.ts`；Tasks 2–15 不修改 schema。
- `packages/core/src/api/client.ts`、`types.ts`、Operation polling 和媒体显示投影。
- 删除 `packages/core/src/types/index.ts`、`api/endpoints.ts` 和 Task polling。
- Miniapp `app/`、`features/`、薄 pages、资源 Cache 和唯一 Operation polling hook。
- 所有主闭环 UI 自动化 fixture。

Task 1 在服务端实现前冻结契约；服务端核心完成后执行 Tasks 2–15，并运行 `api:check` 验证无漂移。

### Peripherals/Cutover

独占：

- Home、Today、Wardrobe、Advisor、Hair、Diagnostic、Share 的窄 Reader。
- 成熟 Identity/Payment/Weather Adapter 的机械迁移。
- `service/account` 的窄接口和身份/会话用例迁移。
- 旧 Server/Client 路径删除、fresh baseline 验证、Staging 金集和放量手册。
- `apps/server/cmd/eval/main.go`，一次注册 assessment、planning、rendering 三个子命令。
- 唯一 root task：`Makefile` 与 `docker-compose.yml` 的独立 API/Worker 服务。

不得再次选择 codegen 版本或创建第二个 generated 文件。

## Canonical Interfaces

### Task Handler

所有业务 Handler 使用且只使用：

```go
type Handler interface {
	Type() domain.TaskType
	Execute(context.Context, domain.TaskLease) (domain.TaskResult, error)
	Commit(context.Context, domain.TaskLease, domain.TaskResult) (domain.CommitOutcome, error)
}
```

语义：

- `Execute` 在事务外调用 Provider。
- `Execute` 可以写不可公开、幂等的 staged/quarantined 产物。
- `Execute` 不得结束 Task、创建后继 Task、失败公开 Operation 或更新 current head。
- `Commit` 在一个短事务中同时检查 `task_id + lease_token + attempt` 和业务 `subject_generation/version`。
- 发布结果、创建下一 Candidate/内容重试、更新 Operation 终态和完成 Task 全部发生在 `Commit`。
- lease 或 generation 失效返回 `CommitSuperseded`，不能修改公开结果。
- transient/throttled error 且仍有基础设施重试预算时，由 Runner 将同一 Task 置为 `retry_wait`；预算耗尽时 Runner 调用 Handler `Commit`，传入 `TaskDomainFail + TaskFailure`，由领域 Commit 原子失败业务 Run、Operation 和 Task。

`TaskResult` 必须带显式 disposition：

```go
type TaskDisposition string

const (
	TaskPublish     TaskDisposition = "publish"
	TaskEnqueueNext TaskDisposition = "enqueue_next"
	TaskDomainFail  TaskDisposition = "domain_failed"
)

type TaskResult struct {
	Disposition TaskDisposition
	ResultType  string
	ResultID    string
	Failure     *TaskFailure
}
```

`CommitSuperseded` 只表示 lease/generation 真正过期，不能表示正常内容补生成或 Candidate 2。

### Operation

公开状态固定：

```text
accepted | running | retrying | succeeded | failed | cancelled | superseded
```

客户端字段固定：

```text
id, kind, subject_type, subject_id, status, progress_bps,
stage_code, public_message, retryable, error_code?,
result_type?, result_id?, created_at, updated_at, finished_at?
```

客户端不读取内部 Task。

### Task

内部状态固定：

```text
queued | leased | retry_wait | succeeded | failed | cancelled | superseded
```

Task 类型由 Registry 注册，不使用数据库 CHECK 枚举。重试预算、超时和并发限制只配置在 Registry。

### RenderSpec

- 唯一定义位置：`apps/server/internal/domain/planning.go`。
- 唯一 JSON Schema：`apps/server/internal/service/planning/schemas/render_spec.v1.json`。
- Rendering 通过 `RenderSpecReader.GetRenderSpecForVariant` 消费。
- Rendering 不重新定义同义 DTO，不从页面文案重建 Prompt。

### Media Source

`source_kind` 固定：

```text
user_original | generated_preview | bundled_reference | demo_example
```

展示文案固定：

```text
user_original      → 原本
generated_preview  → 风格参考
bundled_reference  → 风格参考
demo_example       → 效果示例
```

相同展示文案不代表相同来源；Cache、埋点、分享和客服追踪必须使用 asset ID 与 source kind。

### OpenAPI and Generated Client

- Rule/Contract Freeze 独占 `contracts/**` 与最终 codegen。
- 服务端各计划只消费冻结契约并编写 HTTP Contract Tests，不修改 OpenAPI。
- 生成文件唯一位置：`packages/core/src/api/generated/schema.ts`。
- 不允许 `packages/core/src/api/generated.ts`。
- 不允许重新创建手写 `packages/core/src/types/index.ts` 或 `api/endpoints.ts`。
- Cutover 只运行 `pnpm --filter @zsm/core api:check` 验证无漂移。

### Canonical API DTO

- `POST /v1/assessments` 只接收：

```json
{
  "photos": {
    "face_asset_id": "uuid",
    "side_asset_id": "uuid",
    "body_asset_id": "uuid"
  }
}
```

Profile 由服务端读取并固化快照，客户端不能在 Assessment 请求中提交另一份 Profile。

- `DisplayMedia` 必含 `asset_id`、`url`、`url_expires_at`、`mime_type`、`source_kind`、`display_label`。
- Report 使用 `source_media.{face,side,body}:{item_id,media}`。
- Finding 使用 `source_photo:{item_id,role}` 和 `anchor:{x,y,w,h}`。
- `POST /v1/generation-feedback` 只接收 `publication_id/tags/comment/media_asset_id`；RenderRun、Candidate、Asset 和 generation 由服务端派生。
- `POST /v1/plan-sets` 固定使用 `brief`，不使用 `answers`。General 默认 Brief 为 `balanced/closet/natural`，不存在“空但合法”的 GeneralBrief。
- Operation 失败终态必须含 `trace_id`；HTTP 同步错误继续使用 `request_id`。

### Foundation-owned Shared Contracts

- `domain/task.go`、`domain/operation.go`、`domain/quality.go`、`provider/ai/contracts.go` 和 `provider/ai/runtime.go` 只由 Foundation 定义。
- 结构化模型方法统一为 `Structured`。
- Invocation 字段统一为 `ProviderRequestID` 与 `LatencyMS`。
- `Task.Type` 是 `domain.TaskType`；lease token 只存在于 `domain.TaskLease`。
- Billing 固定为 `Reserve/Settle/Refund`，状态为 `reserved|settled|refunded`。
- 幂等表固定为 `idempotency_keys`，不得创建 `idempotency_records`。
- `brief_hash` 只覆盖 SceneBrief；`planning_input_hash` 覆盖 Report、Profile snapshot、Brief、PreferenceMemory 和生成器版本。
- immutable trigger 只拒绝 `UPDATE`；用户删除通过复合外键 `ON DELETE CASCADE` 完成。

### Bootstrap and Root Ownership

- Foundation 冻结最终签名：

```go
func BuildAPI(config.Config, *slog.Logger) (*bootstrap.APIApp, error)
func BuildWorker(config.Config, *slog.Logger) (*bootstrap.WorkerApp, error)
```

- 中间领域计划只提供构造函数和 Handler，不修改中央 bootstrap composition。
- Peripherals/Cutover 最终独占 `bootstrap/api.go`、`bootstrap/worker.go` 和 `httpapi/httpapi.go` 的完整装配。
- 唯一根级任务位于 Peripherals/Cutover；Foundation 只用 `go build ./cmd/api ./cmd/worker` 验证两个入口。Compose 中不保留 `RUN_WORKER`，两个二进制天然区分职责。

### Baseline and Evaluation

- 唯一迁移：`apps/server/internal/database/migrations/001_baseline.sql`。
- 统一评测入口：`apps/server/cmd/eval/main.go`。
- 金集目录：
  - `apps/server/eval/assessment/`
  - `apps/server/eval/planning/`
  - `apps/server/eval/rendering/`
- 不创建 `cmd/assessment-eval`、`cmd/render-eval`、`internal/eval` 或 `internal/evaluation`。
- 所有 PostgreSQL 集成测试统一使用管理员 DSN：

```text
TEST_DATABASE_URL=postgres://jianwo:jianwo@127.0.0.1:55432/postgres?sslmode=disable
```

测试 Harness 自建并删除随机临时数据库；不得直接复用 `jianwo`、`jianwo_test` 或手写固定测试库。
- OpenAPI lint 工具必须在 Rule/Contract Freeze 中固定为 workspace devDependency，并通过 `pnpm exec` 运行；后续计划不得使用 `npx --yes` 临时下载。

## Phase Gates

### Gate 1: Foundation

- fresh database 可启动。
- API/Worker 独立。
- lease 过期、heartbeat、重复 Claim、旧 lease Commit 和幂等请求测试通过。
- Provider Invocation 与 Billing reservation 可追踪。

### Gate 2: Assessment

- PhotoSet 恰好 face/side/body。
- 用户归属和同人检查通过。
- Report Finding 证据关联 100%。
- evidence threshold 来自版本化策略与锁定金集校准，不硬编码。

### Gate 3: Planning

- 六场景共用 PlanSet Generator。
- 三套方案、每套三步、Grounding 100%。
- 任意两套至少两个实质差异。
- 每套产生一个 Schema 合法的 RenderSpec。

### Gate 4: Rendering

- full-look 单图 fallback 为 0。
- Candidate 先 quarantined，Gate 通过后才 Publication。
- Candidate 2 只能由 Candidate 1 的质量 retry 触发。
- 旧 generation 无法更新 RenderHead。
- 发布图全部 JPEG。

### Gate 5: Execution and Billing

- Selection 独立于 Plan。
- Execution 复制不可变步骤快照。
- Event client ID 幂等。
- 两类 Feedback 关联完整。
- Candidate 2 与网络重试不二次扣费。

### Gate 6: Miniapp

- 页面只使用 generated client 与 Operation。
- Storage 不保存业务 ID。
- 无跨 asset pinned URL。
- 来源、错误、空态和 partial readiness fixture 通过。
- 不执行真机或提审材料任务。

### Gate 7: Cutover

- 外围模块只通过窄 Reader 读取质量核心。
- 仓库没有新旧双路径。
- fresh baseline、全仓测试、静态门禁和锁定金集通过。
- 生产只运行独立 Worker。

## Final Verification

全部计划完成后运行：

```bash
pnpm typecheck
pnpm lint
make design-build
node apps/miniapp/scripts/check.mjs
make server-vet
make server-test
pnpm --filter @zsm/core api:check
cd apps/server && go test ./... -count=1
```

再运行 Staging 评测：

```bash
cd apps/server
go run ./cmd/eval assessment --release
go run ./cmd/eval planning --release
go run ./cmd/eval rendering decide --rollout-config config/render-rollout.production.json
```

只有全部 Gate 通过后，才能开始 5% 放量。
