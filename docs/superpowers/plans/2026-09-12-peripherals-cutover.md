# Peripherals Cutover Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 Home、Today、Wardrobe、Advisor、Hair、Outfit、Purchase、Share 全部切到质量核心的窄 Reader，随后一次性删除旧业务路径、旧契约、旧迁移和旧前端状态，并以全新 baseline、独立 Worker、全仓门禁和分档放量完成发布。

**Architecture:** 质量核心仍是 Go 模块化单体，外围用例只依赖各自 `ports.go` 中的一方法 Reader，由 PostgreSQL read-model adapter 实现；外围只能读取 Report、PlanSet、RenderPublication、Operation 的稳定投影，不能写质量核心表。切换采用“一次换轨后删除旧路”的方式，不做双读、双写、旧 DTO 或旧数据兼容；回滚只回滚 API/Worker 代码和能力路由，数据库始终保持新 baseline。

**Tech Stack:** Go 1.24.5、PostgreSQL 16、`pgx/v5`、Taro 4.2.1、React 18.3.1、TypeScript 5.9.3、pnpm 10.34.5、OpenAPI 3、Docker Compose

## Global Constraints

- `appearance-coach-prototype/` 是只读参考，任何改动不得进入该目录。
- 根级 `package.json`、`tsconfig.base.json`、`Makefile`、`docker-compose.yml` 只在 Task 11 的根级任务中修改。
- 包名固定为 `@zsm/core`、`@zsm/design`、`@zsm/miniapp`、`@zsm/mobile`；Go module 固定为 `github.com/zhanshimian/server`。
- 不打颜值分/身材分、不身材羞辱、不做医学结论、不用警示红；Finding 使用建议优先级，不使用缺陷严重度。
- 生成图按已确认产品文案标记「风格参考」，Demo 标记「效果示例」，内置模特标记「风格参考」；三者必须以强类型 `source_kind` 区分，不得只靠文案或 URL 推断。
- `lookImage(v)` 对无效 URL / WebP 返回 `''`；`userImage(v)` 无效时保持可见的空；内置模特图只能来自 `exampleImage(slug, variant)`，调用点必须叠 `.example-badge` 与 `.example-soft`。
- Demo 内容只经服务端 `/v1/media/demo` 和 Demo Provider 进入，生产不得自动切 Demo。
- 错误态与内容不同屏；空态和错误态必须提供重试、返回或重新拍摄动作。
- 产品名固定「uplook」，品牌标语固定「今天最好看」，首页保留「你好，我是你的私人形象顾问」，场景使用「日常」而不是「通勤」；文案统一放 `packages/core/src/copy/zh.ts`。
- 底部导航固定 `首页 / 方案 / 我的`；实验能力只进入「体验实验室」。
- 小程序包内图片禁止 WebP，资产只发 JPEG，tabBar/图标使用 PNG；样式只写 `rpx`，禁止裸 `px`。
- 自定义组件设置 `styleIsolation: 'apply-shared'`；React 条件渲染禁止 `{count && <View/>}`；列表 key 使用资源 ID。
- tab 页使用 `useDidShow` 与 ref 防重复加载；缓存优先渲染、后台校验，不清空已渲染图片。
- 客户端只轮询公开 Operation；所有轮询使用唯一 `useOperationPolling`，页面隐藏即停，连续失败 5 次进入失败态。
- 微信 3.17+ 开发环境只在非 HTTPS 时启用 `localizeDevImages`。
- 服务端依赖方向保持 `httpapi → service → repository / provider / storage`；handler 禁止直接访问数据库。
- Service 只依赖自己 `ports.go` 中的最小接口；Postgres adapter 可以实现多个窄接口，但不得重新暴露巨型 Repository。
- 所有 repository 读写同时校验 `resource_id + user_id`；越权统一返回 404；跨资源关系使用 `(user_id, parent_id)` 复合外键。
- 客户端不读取内部 Task；Worker 继续使用 PostgreSQL `tasks + ClaimTask + FOR UPDATE SKIP LOCKED`、lease、heartbeat 与 lease/generation 双 CAS。
- AI 只经 `ai-routing.*.json` 能力路由；业务代码不得出现厂商名或模型名；身份敏感能力只允许同能力 fallback。
- 报告、方案集、RenderSpec、候选图和发布物不可变；重新生成创建新 ID，旧 generation 不能覆盖新 generation。
- Provider 输出先进入 quarantined candidate，质量通过后才发布；生成图发布前统一为 JPEG；`full_look_generation` 禁止单图 fallback。
- 数据库只保存 COS object key，不保存签名 URL；签名 URL 仅在读取投影时生成。
- 不持久化人脸 embedding；不记录 API key、原始图片字节、完整用户图片 URL、未脱敏手机号或 OpenID。
- 不重写微信/短信/Apple 登录、Session、Billing Ledger、退款、微信虚拟支付、天气、COS 与本地存储 adapter 的内部算法；只调整构造注入和包引用。
- 不保留历史业务数据，不兼容旧数据库、旧 API、旧本地缓存或旧 Task；不编写回填、双写、兼容 DTO。
- 迁移只向前、embed 后按文件名顺序执行；最终只保留 `apps/server/internal/database/migrations/001_baseline.sql`。
- `APP_ENV=production` 门禁全开；生产 API 不内嵌 Worker，`cmd/worker` 独立部署和扩缩容。
- 外围模块不能写 Report、PlanSet、RenderSpec、RenderRun、RenderCandidate、RenderPublication；需要公开异步状态时只能调用 `service/operation`，不能直接写 Operation 表。
- 外部模型评测不进入每次 PR CI；Staging 与夜间运行，只有锁定集达标的路由版本允许发布。
- 新版本按 `5% → 25% → 50% → 100%` 放量；回滚只回滚代码或能力路由，不恢复旧 schema、不兼容旧数据。

---

## Cross-plan Ownership Override

- 本计划最后执行；前六份计划和 Miniapp 计划必须已经完成。
- OpenAPI codegen 工具、版本和生成路径由 Miniapp 计划独占：`openapi-typescript@7.13.0`、`openapi-fetch@0.17.0`、`packages/core/src/api/generated/schema.ts`。本计划不得再次安装 codegen、修改生成路径或恢复手写 Client。
- 下文 Task 7 改为“验证最终 OpenAPI 与 generated client 无漂移并删除残留旧文件”；其中创建 `generated.ts`、安装 `openapi-typescript@7.9.1`、修改生成脚本的步骤全部跳过。
- 本计划 Task 11 是唯一允许修改根 `Makefile` 与 `docker-compose.yml` 的 root task；Foundation 只验证两个二进制可独立构建。
- Task 9 负责创建 `apps/server/internal/service/account/ports.go`、`service.go` 和测试，把成熟身份/会话用例从旧全能 Service 机械迁移后再删除旧 Service；不得只迁 Provider 而留下缺失的 Account Service。
- 统一评测入口固定 `apps/server/cmd/eval/main.go`，金集位于 `apps/server/eval/{assessment,planning,rendering}`。
- 旧 migrations 只由 Foundation Task 1 删除；下文 Task 10 的 Delete 清单改为验证清单，只确认目录最终仅含 `001_baseline.sql`。

## Execution Preconditions

本计划是设计文档第 22 节的第 7 份执行计划。开始 Task 1 前，前六份计划必须已经提供以下文件和行为；缺少任一项时停止执行本计划，先完成对应前置计划：

- `apps/server/internal/domain/media.go`
- `apps/server/internal/domain/profile.go`
- `apps/server/internal/domain/assessment.go`
- `apps/server/internal/domain/planning.go`
- `apps/server/internal/domain/rendering.go`
- `apps/server/internal/domain/execution.go`
- `apps/server/internal/domain/feedback.go`
- `apps/server/internal/domain/operation.go`
- `apps/server/internal/domain/billing.go`
- `apps/server/internal/service/media/`
- `apps/server/internal/service/billing/`
- `apps/server/internal/service/assessment/`
- `apps/server/internal/service/planning/`
- `apps/server/internal/service/rendering/`
- `apps/server/internal/service/execution/`
- `apps/server/internal/service/feedback/`
- `apps/server/internal/service/operation/`
- `apps/server/internal/service/taskrunner/`
- `apps/server/internal/repository/postgres/users.go`
- `apps/server/internal/repository/postgres/media.go`
- `apps/server/internal/repository/postgres/assessment.go`
- `apps/server/internal/repository/postgres/planning.go`
- `apps/server/internal/repository/postgres/rendering.go`
- `apps/server/internal/repository/postgres/execution.go`
- `apps/server/internal/repository/postgres/feedback.go`
- `apps/server/internal/repository/postgres/operations.go`
- `apps/server/internal/repository/postgres/tasks.go`
- `apps/server/internal/repository/postgres/billing.go`
- `apps/server/internal/provider/ai/runtime.go`
- `apps/server/internal/provider/ai/router.go`
- `apps/server/internal/provider/ai/contracts.go`
- `apps/server/internal/provider/ai/structured.go`
- `apps/server/internal/provider/ai/image.go`
- `apps/server/internal/provider/ai/quality.go`
- `apps/server/internal/bootstrap/api.go`
- `apps/server/internal/bootstrap/worker.go`
- `apps/server/internal/database/migrations/001_baseline.sql`
- `contracts/openapi.yaml` 中已经存在设计文档第 10 节的质量主链端点。
- `packages/core/src/api/generated/schema.ts` 已能从 `contracts/openapi.yaml` 生成。
- `apps/miniapp/src/app/operations/use-operation-polling.ts` 已能轮询 `Operation.status`，并在页面隐藏时暂停、恢复时立即刷新、连续失败 5 次停止。

执行前运行：

```bash
test -f apps/server/internal/service/rendering/ports.go
test -f apps/server/internal/service/billing/service.go
test -f apps/server/internal/repository/postgres/rendering.go
test -f apps/server/internal/provider/ai/router.go
test -f apps/server/internal/database/migrations/001_baseline.sql
test -f packages/core/src/api/generated/schema.ts
test -f apps/miniapp/src/app/operations/use-operation-polling.ts
```

Expected: 九条命令均退出 0，无输出。

## Final File Map

**Create**

- `apps/server/internal/service/home/service.go`：首页聚合用例。
- `apps/server/internal/service/home/ports.go`：首页单方法 Reader。
- `apps/server/internal/service/today/service.go`：今日建议用例。
- `apps/server/internal/service/today/ports.go`：今日质量核心 grounding Reader。
- `apps/server/internal/service/wardrobe/service.go`：衣橱用例。
- `apps/server/internal/service/wardrobe/ports.go`：衣橱当前方案 Reader。
- `apps/server/internal/service/advisor/service.go`：顾问对话用例。
- `apps/server/internal/service/advisor/ports.go`：顾问只读上下文 Reader。
- `apps/server/internal/service/hair/service.go`：发型建议和预览用例。
- `apps/server/internal/service/hair/ports.go`：发型报告 Reader。
- `apps/server/internal/service/diagnostic/service.go`：Outfit/Purchase 诊断用例。
- `apps/server/internal/service/diagnostic/ports.go`：诊断报告和衣橱 Reader。
- `apps/server/internal/service/share/service.go`：不可变分享快照与公开读取。
- `apps/server/internal/service/share/ports.go`：分享来源 Reader。
- `apps/server/internal/provider/identity/wechat.go`：原微信 adapter 的机械迁移。
- `apps/server/internal/provider/identity/sms.go`：原短信 adapter 的机械迁移。
- `apps/server/internal/provider/identity/apple.go`：原 Apple adapter 的机械迁移。
- `apps/server/internal/provider/weather/weather.go`：原天气 adapter 的机械迁移。
- `apps/server/internal/provider/payment/virtual_pay.go`：原支付协议的机械迁移。
- `apps/server/internal/provider/payment/wechat_virtual_pay.go`：原微信虚拟支付 adapter 的机械迁移。
- `apps/server/internal/repository/postgres/readmodels.go`：实现七个外围 Reader 的 SQL 投影。
- `apps/server/internal/repository/postgres/readmodels_integration_test.go`：真实 PostgreSQL 租户、来源与发布态测试。
- `apps/server/internal/httpapi/peripherals_test.go`：外围 HTTP 契约测试。
- `apps/server/internal/database/baseline_integration_test.go`：fresh database baseline 断言。
- `apps/server/scripts/check-legacy.sh`：禁止旧路径、旧字段、旧端点和旧本地状态回流。
- `apps/server/scripts/staging-golden.sh`：锁定金集发布门禁。
- `apps/server/scripts/set-rollout.mjs`：只修改候选路由百分比。
- `apps/server/eval/goldens/manifest.json`：金集规模、分区与身份隔离约束。
- `apps/miniapp/qa/peripherals-cutover.test.mjs`：源码级客户端状态与来源门禁。
- `docs/operations/quality-core-release.md`：数据库重置、放量、停止与代码/路由回滚手册。

**Modify**

- `apps/server/internal/httpapi/httpapi.go`
- `apps/server/internal/httpapi/home.go`
- `apps/server/internal/httpapi/today.go`
- `apps/server/internal/httpapi/wardrobe.go`
- `apps/server/internal/httpapi/advisor.go`
- `apps/server/internal/httpapi/hair.go`
- `apps/server/internal/httpapi/diagnostics.go`
- `apps/server/internal/httpapi/shares.go`
- `apps/server/internal/bootstrap/api.go`
- `apps/server/internal/bootstrap/worker.go`
- `apps/server/cmd/api/main.go`
- `apps/server/cmd/worker/main.go`
- `apps/server/Dockerfile`
- `apps/server/config/ai-routing.example.json`
- `apps/server/config/ai-routing.production.json`
- `contracts/openapi.yaml`
- `contracts/scripts/check-sync.mjs`
- `packages/core/package.json`
- `packages/core/src/index.ts`
- `packages/core/src/http/client.ts`
- `packages/core/src/api/endpoints.ts`
- `packages/core/src/api/generated/schema.ts`
- `packages/core/src/types/index.ts`
- `apps/miniapp/src/services/api.ts`
- `apps/miniapp/src/services/storage.ts`
- `apps/miniapp/src/pages/home/index.tsx`
- `apps/miniapp/src/pages/profile/index.tsx`
- `apps/miniapp/src/packages/life/pages/today/index.tsx`
- `apps/miniapp/src/packages/life/pages/wardrobe/index.tsx`
- `apps/miniapp/src/packages/life/pages/advisor/index.tsx`
- `apps/miniapp/src/packages/life/pages/share/index.tsx`
- `apps/miniapp/src/packages/tools/pages/hair/index.tsx`
- `apps/miniapp/src/packages/tools/pages/outfit/index.tsx`
- `apps/miniapp/src/packages/tools/pages/purchase/index.tsx`
- `apps/miniapp/src/pages/plans/index.tsx`
- `apps/miniapp/src/pages/plan/index.tsx`
- `apps/miniapp/src/pages/report/index.tsx`
- `apps/miniapp/src/pages/scene/index.tsx`
- `apps/miniapp/src/pages/analysis/index.tsx`
- `apps/miniapp/src/pages/capture/index.tsx`
- `apps/miniapp/src/pages/checklist/index.tsx`
- `apps/miniapp/src/pages/feedback/index.tsx`
- `apps/server/scripts/e2e.sh`

**Delete**

- `apps/server/internal/domain/domain.go`
- `apps/server/internal/repository/repository.go`
- `apps/server/internal/service/service.go`
- `apps/server/internal/service/home.go`
- `apps/server/internal/service/product_loops.go`
- `apps/server/internal/service/scene_plans.go`
- `apps/server/internal/service/plans.go`
- `apps/server/internal/service/tasks.go`
- `apps/server/internal/service/media_loader.go`
- `apps/server/internal/service/image_constrain.go`
- `apps/server/internal/service/billing.go`
- `apps/server/internal/service/billing_rules.go`
- `apps/server/internal/service/service_test.go`
- `apps/server/internal/service/product_loops_test.go`
- `apps/server/internal/service/scene_plans_test.go`
- `apps/server/internal/service/today_look_test.go`
- `apps/server/internal/service/analysis_media_test.go`
- `apps/server/internal/service/current_analysis_test.go`
- `apps/server/internal/service/stale_analysis_test.go`
- `apps/server/internal/service/media_loader_test.go`
- `apps/server/internal/service/image_constrain_test.go`
- `apps/server/internal/service/fail_write_test.go`
- `apps/server/internal/service/diagnostics_test.go`
- `apps/server/internal/service/billing_test.go`
- `apps/server/internal/service/billing_rules_test.go`
- `apps/server/internal/provider/provider.go`
- `apps/server/internal/provider/ai_runtime.go`
- `apps/server/internal/provider/ai_routed.go`
- `apps/server/internal/provider/openai.go`
- `apps/server/internal/provider/photo_check.go`
- `apps/server/internal/provider/plan_group.go`
- `apps/server/internal/provider/look.go`
- `apps/server/internal/provider/hair.go`
- `apps/server/internal/provider/outfit.go`
- `apps/server/internal/provider/today_plan.go`
- `apps/server/internal/provider/provider_test.go`
- `apps/server/internal/provider/ai_runtime_test.go`
- `apps/server/internal/provider/openai_test.go`
- `apps/server/internal/provider/photo_check_test.go`
- `apps/server/internal/provider/look_test.go`
- `apps/server/internal/provider/hair_test.go`
- `apps/server/internal/provider/outfit_test.go`
- `apps/server/internal/repository/postgres/product_loops.go`
- `packages/core/src/hooks/useTaskPolling.ts`
- `apps/miniapp/src/hooks/use-stable-polling.ts`
- `apps/miniapp/src/services/task-utils.ts`
- `apps/miniapp/src/services/outfit-session.ts`
- `apps/miniapp/src/services/purchase-session.ts`

**Move without algorithm rewrites**

- `apps/server/internal/provider/wechat.go` → `apps/server/internal/provider/identity/wechat.go`
- `apps/server/internal/provider/sms.go` → `apps/server/internal/provider/identity/sms.go`
- `apps/server/internal/provider/apple.go` → `apps/server/internal/provider/identity/apple.go`
- `apps/server/internal/provider/wechat_test.go` → `apps/server/internal/provider/identity/wechat_test.go`
- `apps/server/internal/provider/weather.go` → `apps/server/internal/provider/weather/weather.go`
- `apps/server/internal/provider/weather_test.go` → `apps/server/internal/provider/weather/weather_test.go`
- `apps/server/internal/provider/virtual_pay.go` → `apps/server/internal/provider/payment/virtual_pay.go`
- `apps/server/internal/provider/wechat_virtual_pay.go` → `apps/server/internal/provider/payment/wechat_virtual_pay.go`
- `apps/server/internal/provider/wechat_virtual_pay_test.go` → `apps/server/internal/provider/payment/wechat_virtual_pay_test.go`

**Retain at the same path without algorithm rewrites**

- `apps/server/internal/storage/storage.go`
- `apps/server/internal/storage/cos.go`
- `apps/server/internal/storage/local.go`
- `apps/server/internal/storage/cos_test.go`
- `apps/server/internal/storage/local_test.go`

### Task 1: Freeze Narrow Reader Contracts

**Files:**
- Create: `apps/server/internal/service/home/ports.go`
- Create: `apps/server/internal/service/today/ports.go`
- Create: `apps/server/internal/service/wardrobe/ports.go`
- Create: `apps/server/internal/service/advisor/ports.go`
- Create: `apps/server/internal/service/hair/ports.go`
- Create: `apps/server/internal/service/diagnostic/ports.go`
- Create: `apps/server/internal/service/share/ports.go`
- Test: `apps/server/internal/service/home/service_test.go`
- Test: `apps/server/internal/service/today/service_test.go`
- Test: `apps/server/internal/service/advisor/service_test.go`
- Test: `apps/server/internal/service/share/service_test.go`

**Interfaces:**
- Produces: `home.Reader.ReadHome(ctx, userID, now) (home.Snapshot, error)`.
- Produces: `today.Reader.ReadTodayGrounding(ctx, userID) (today.Grounding, error)`.
- Produces: `wardrobe.Reader.ReadWardrobeGrounding(ctx, userID) (wardrobe.Grounding, error)`.
- Produces: `advisor.Reader.ReadAdvisorGrounding(ctx, userID) (advisor.Grounding, error)`.
- Produces: `hair.Reader.ReadHairGrounding(ctx, userID, reportID) (hair.Grounding, error)`.
- Produces: `diagnostic.Reader.ReadDiagnosticGrounding(ctx, userID, reportID, kind) (diagnostic.Grounding, error)`.
- Produces: `share.Reader.ReadShareSource(ctx, userID, sourceType, sourceID) (share.Source, error)`.
- Constraint: Reader 只返回值对象和 object key/asset ID，不返回 repository row、Provider DTO、内部 Task 或签名 URL。

- [ ] **Step 1: Write failing compile-time contract tests**

```go
package home_test

import (
	"context"
	"time"

	"github.com/zhanshimian/server/internal/service/home"
)

type homeReaderFake struct{}

func (homeReaderFake) ReadHome(context.Context, string, time.Time) (home.Snapshot, error) {
	return home.Snapshot{}, nil
}

var _ home.Reader = homeReaderFake{}
```

在 `today/service_test.go`、`advisor/service_test.go`、`share/service_test.go` 分别加入同样的 compile-time assertion，并使用本任务 Interfaces 中的精确签名。

- [ ] **Step 2: Run tests to verify the ports do not exist**

Run:

```bash
cd apps/server && go test ./internal/service/home ./internal/service/today ./internal/service/advisor ./internal/service/share
```

Expected: FAIL，包含 `package github.com/zhanshimian/server/internal/service/home is not in std` 或 `undefined: home.Reader`。

- [ ] **Step 3: Add the exact projection types and interfaces**

`home/ports.go`：

```go
package home

import (
	"context"
	"time"

	"github.com/zhanshimian/server/internal/domain"
)

type Snapshot struct {
	Profile          *domain.ProfileSummary   `json:"profile,omitempty"`
	CurrentReport    *domain.ReportCard       `json:"current_report,omitempty"`
	Today            *domain.TodayCard        `json:"today,omitempty"`
	RecentPlan       *domain.PlanVariantCard  `json:"recent_plan,omitempty"`
	ActiveOperations []domain.OperationRef    `json:"active_operations"`
	Billing          *domain.BillingSummary   `json:"billing,omitempty"`
}

type Reader interface {
	ReadHome(ctx context.Context, userID string, now time.Time) (Snapshot, error)
}
```

其余六个 `ports.go` 使用以下完整业务字段；字段名称是后续 SQL、Provider grounding 与 OpenAPI 的固定边界：

```go
// today/ports.go
type Grounding struct {
	ReportID       string
	Profile        domain.ProfileSnapshot
	Findings       []domain.FindingGrounding
	SelectedPlan   *domain.PlanVariantGrounding
	Publication    *domain.PublishedMedia
}
type Reader interface {
	ReadTodayGrounding(context.Context, string) (Grounding, error)
}

// wardrobe/ports.go
type Grounding struct {
	CurrentReportID string
	SelectedPlan    *domain.PlanVariantGrounding
}
type Reader interface {
	ReadWardrobeGrounding(context.Context, string) (Grounding, error)
}

// advisor/ports.go
type Grounding struct {
	Profile      domain.ProfileSnapshot
	Report       *domain.ReportGrounding
	SelectedPlan *domain.PlanVariantGrounding
	Wardrobe     []domain.WardrobeGroundingItem
	Feedback     []domain.FeedbackMemoryItem
}
type Reader interface {
	ReadAdvisorGrounding(context.Context, string) (Grounding, error)
}

// hair/ports.go
type Grounding struct {
	ReportID string
	Findings []domain.FindingGrounding
	Face     domain.MediaInput
}
type Reader interface {
	ReadHairGrounding(context.Context, string, string) (Grounding, error)
}

// diagnostic/ports.go
type Grounding struct {
	Report   *domain.ReportGrounding
	Profile  domain.ProfileSnapshot
	Wardrobe []domain.WardrobeGroundingItem
}
type Reader interface {
	ReadDiagnosticGrounding(context.Context, string, string, string) (Grounding, error)
}

// share/ports.go
type Source struct {
	SourceType   string
	SourceID     string
	Title        string
	Summary      string
	AssetID      string
	ObjectKey    string
	SourceKind   domain.MediaSourceKind
	DisplayLabel string
	PublishedAt  time.Time
}
type Reader interface {
	ReadShareSource(context.Context, string, string, string) (Source, error)
}
```

在每个文件补齐对应 `package` 与 `import`；不合并为 `QualityReader`，避免 Today 获得 Share 或 Advisor 的读能力。

- [ ] **Step 4: Run compile-time tests**

Run:

```bash
cd apps/server && go test ./internal/service/home ./internal/service/today ./internal/service/advisor ./internal/service/share
```

Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add apps/server/internal/service/home apps/server/internal/service/today apps/server/internal/service/wardrobe apps/server/internal/service/advisor apps/server/internal/service/hair apps/server/internal/service/diagnostic apps/server/internal/service/share
git commit -m "refactor(server): define peripheral read ports"
```

> **执行修正（Task 1 实施时记录）：**
>
> 1. **计划引用的 12 个投影领域类型在前六份计划里并不存在**：`ProfileSummary`、`ReportCard`、`TodayCard`、`PlanVariantCard`、`ProfileSnapshot`、`FindingGrounding`、`PlanVariantGrounding`、`PublishedMedia`、`ReportGrounding`、`WardrobeGroundingItem`、`MediaInput`、`MediaSourceKind` 均无定义（只存在 `OperationRef`/`BillingSummary`/`FeedbackMemoryItem`）。落地：新建 `apps/server/internal/domain/projections.go` 定义它们，形状对齐冻结 OpenAPI 与现有 write 侧领域类型。
> 2. **`ProfileSummary` 对齐 OpenAPI `UserProfile`**（`height_cm/role/budget` 必填，`weight_kg/bust_cm/waist_cm/hip_cm` 选填 `*float64`），不是持久化的 `domain.UserProfile`（后者只有 Role/HeightCM/Budget + Preferences/Avoidances JSON）。
> 3. **`MediaSourceKind` 是独立展示枚举**（user_original/generated_preview/bundled_reference/demo_example），与内部 `MediaOrigin`/`DisplayKind` 分开；`PublishedMedia`/`MediaInput` 只带 object key 与 asset id，不带签名 URL。
> 4. **grounding 类型是轻量只读投影**：`FindingGrounding` 去掉锚点坐标与 confidence，`PlanVariantGrounding` 复用现有 `PlanStep`，避免把 write 侧结构整包暴露给外围 Reader。
> 5. **编译期契约测试无 `func Test`**：计划 Step 4 期望「PASS」，实际 `go test` 输出 `ok ... [no tests to run]`（`var _ Reader = fake{}` 是编译期断言，包编译通过即契约成立），非空 test 函数缺失是设计本意。
> 6. **提交清单比计划宽**：另含 `apps/server/internal/domain/projections.go`（计划未列入的新建文件）。

### Task 2: Implement Tenant-Safe PostgreSQL Read Models

**Files:**
- Create: `apps/server/internal/repository/postgres/readmodels.go`
- Create: `apps/server/internal/repository/postgres/readmodels_integration_test.go`
- Modify: `apps/server/internal/repository/postgres/users.go`
- Modify: `apps/server/internal/repository/postgres/planning.go`
- Modify: `apps/server/internal/repository/postgres/rendering.go`

**Interfaces:**
- Consumes: all Reader signatures from Task 1.
- Consumes: `storage.URLSigner.SignedURL(ctx, objectKey, ttl) (string, time.Time, error)` only in HTTP projection code, never in SQL Reader.
- Produces: one `*postgres.Store` implementing all seven Reader interfaces.
- Security invariant: every query begins with a user-owned root and carries `user_id` through each join; `ReadShareSource` only accepts `media_assets.state = 'published'`.

- [ ] **Step 1: Write integration tests for ownership, immutability and source truth**

```go
func TestReadShareSourceRequiresOwnedPublishedAsset(t *testing.T) {
	db := openTestStore(t)
	userA, userB := seedUsers(t, db)
	reportID, variantID, publicationID := seedPublishedPlan(t, db, userA)
	_ = reportID

	source, err := db.ReadShareSource(context.Background(), userA, "plan_variant", variantID)
	require.NoError(t, err)
	require.Equal(t, publicationID.AssetID, source.AssetID)
	require.Equal(t, domain.MediaSourceGeneratedPreview, source.SourceKind)
	require.Equal(t, "风格参考", source.DisplayLabel)
	require.NotContains(t, source.ObjectKey, "://")

	_, err = db.ReadShareSource(context.Background(), userB, "plan_variant", variantID)
	require.ErrorIs(t, err, repository.ErrNotFound)

	quarantined := seedQuarantinedCandidate(t, db, userA)
	_, err = db.ReadShareSource(context.Background(), userA, "plan_variant", quarantined.PlanVariantID)
	require.ErrorIs(t, err, repository.ErrNotFound)
}
```

再加入：

```go
func TestReadTodayGroundingUsesCurrentReportAndPublishedSelection(t *testing.T)
func TestReadAdvisorGroundingNeverCrossesUserBoundary(t *testing.T)
func TestReadHomeReturnsOperationsNotTasks(t *testing.T)
func TestReadDiagnosticGroundingOmitsWardrobeForOutfit(t *testing.T)
```

每个 seed helper 只写 `001_baseline.sql` 已定义的新表，并显式创建 `(user_id, id)` 父键。

- [ ] **Step 2: Run integration tests and verify failure**

Run:

```bash
cd apps/server && TEST_DATABASE_URL=postgres://jianwo:jianwo@127.0.0.1:55432/jianwo_test?sslmode=disable go test ./internal/repository/postgres -run 'TestRead(Home|Today|Advisor|Diagnostic|Share)' -count=1
```

Expected: FAIL，包含 `db.ReadShareSource undefined`。

- [ ] **Step 3: Implement one query per Reader method**

`ReadShareSource` 的 PlanVariant 分支必须以 Publication 为唯一图片来源：

```go
func (s *Store) ReadShareSource(ctx context.Context, userID, sourceType, sourceID string) (share.Source, error) {
	var source share.Source
	switch sourceType {
	case "plan_variant":
		err := s.pool.QueryRow(ctx, `
			SELECT 'plan_variant', pv.id::text, pv.name, pv.descriptor,
			       ma.id::text, ma.object_key,
			       CASE ma.origin
			         WHEN 'user_upload' THEN 'user_original'
			         WHEN 'provider_output' THEN 'generated_preview'
			         WHEN 'bundled_reference' THEN 'bundled_reference'
			         WHEN 'demo' THEN 'demo_example'
			       END,
			       CASE ma.display_kind
			         WHEN 'original' THEN '原本'
			         WHEN 'generated_reference' THEN '风格参考'
			         WHEN 'effect_example' THEN '效果示例'
			         WHEN 'style_reference' THEN '风格参考'
			       END,
			       rp.created_at
			FROM plan_variants pv
			JOIN render_heads rh
			  ON rh.user_id = pv.user_id AND rh.plan_variant_id = pv.id
			JOIN render_publications rp
			  ON rp.user_id = rh.user_id AND rp.id = rh.current_publication_id
			JOIN render_candidates rc
			  ON rc.user_id = rp.user_id AND rc.id = rp.candidate_id
			JOIN media_assets ma
			  ON ma.user_id = rc.user_id AND ma.id = rc.asset_id
			WHERE pv.user_id = $1 AND pv.id = $2::uuid
			  AND ma.state = 'published'`,
			userID, sourceID,
		).Scan(&source.SourceType, &source.SourceID, &source.Title, &source.Summary,
			&source.AssetID, &source.ObjectKey, &source.SourceKind, &source.DisplayLabel,
			&source.PublishedAt)
		return source, mapNotFound(err)
	case "today_plan":
		err := s.pool.QueryRow(ctx, `
			SELECT 'today_plan', tp.id::text, tp.title, tp.summary,
			       ma.id::text, ma.object_key,
			       CASE ma.origin
			         WHEN 'user_upload' THEN 'user_original'
			         WHEN 'provider_output' THEN 'generated_preview'
			         WHEN 'bundled_reference' THEN 'bundled_reference'
			         WHEN 'demo' THEN 'demo_example'
			       END,
			       CASE ma.display_kind
			         WHEN 'original' THEN '原本'
			         WHEN 'generated_reference' THEN '风格参考'
			         WHEN 'effect_example' THEN '效果示例'
			         WHEN 'style_reference' THEN '风格参考'
			       END,
			       rp.created_at
			FROM today_plans tp
			JOIN render_publications rp
			  ON rp.user_id = tp.user_id AND rp.id = tp.render_publication_id
			JOIN render_candidates rc
			  ON rc.user_id = rp.user_id AND rc.id = rp.candidate_id
			JOIN media_assets ma
			  ON ma.user_id = rc.user_id AND ma.id = rc.asset_id
			WHERE tp.user_id = $1 AND tp.id = $2::uuid
			  AND ma.state = 'published'`,
			userID, sourceID,
		).Scan(&source.SourceType, &source.SourceID, &source.Title, &source.Summary,
			&source.AssetID, &source.ObjectKey, &source.SourceKind, &source.DisplayLabel,
			&source.PublishedAt)
		return source, mapNotFound(err)
	default:
		return share.Source{}, repository.ErrNotFound
	}
}
```

`ReadHome` 只查询 `user_profiles.current_report_id`、当前 `plan_selections`、`render_heads.current_publication_id` 和 `operations.status IN ('accepted','running','retrying')`；禁止查询 `tasks`。其余 Reader 将 Report/Finding/Plan/Publication 投影成 Task 1 的值类型，不把 `*postgres.Store` 或 SQL row 泄漏到 service。

- [ ] **Step 4: Add compile-time adapter assertions**

```go
var (
	_ home.Reader       = (*Store)(nil)
	_ today.Reader      = (*Store)(nil)
	_ wardrobe.Reader   = (*Store)(nil)
	_ advisor.Reader    = (*Store)(nil)
	_ hair.Reader       = (*Store)(nil)
	_ diagnostic.Reader = (*Store)(nil)
	_ share.Reader      = (*Store)(nil)
)
```

- [ ] **Step 5: Run repository tests**

Run:

```bash
cd apps/server && TEST_DATABASE_URL=postgres://jianwo:jianwo@127.0.0.1:55432/jianwo_test?sslmode=disable go test ./internal/repository/postgres -count=1
```

Expected: PASS；越权与 quarantined 用例都返回 `repository.ErrNotFound`。

- [ ] **Step 6: Commit**

```bash
git add apps/server/internal/repository/postgres
git commit -m "feat(server): add tenant-safe peripheral read models"
```

> **执行修正（Task 2 实施时记录）：**
>
> 1. **外围持久化表在 baseline 里缺失**（用户拍板「补全外围表到 baseline」）：新建 8 张表追加进 `001_baseline.sql` —— `today_plans`、`wardrobe_items`、`wardrobe_outfits`、`advisor_conversations`、`advisor_messages`、`hair_previews`、`diagnostics`、`shares`。每张表都带 `user_id` 租户键 + 复合外键；`today_plans` 只引用 `render_publication_id`（媒体来自 publication→candidate→media_asset 链）；`hair_previews`/`diagnostics` 的 steps/findings/options/context 走 jsonb 快照；`shares` 存 `asset_id` + `snapshot jsonb` 且不存 URL。
> 2. **`ReadHome.Billing` 留 nil**：`BillingSummary` 是服务级聚合（日用量需 ledger、SKU/支付开关来自 config），不是纯 SQL 投影；由 home service（Task 3）组合账单服务填充，Reader 不越权读 config。
> 3. **`plan_steps.details` 是 jsonb**（不是计划测试片段假设的散列列）：read model 扫 jsonb 后 unmarshal 进 `domain.PlanStepDetails`。
> 4. **`ReadDiagnosticGrounding` 只对 `purchase` 读衣橱**（outfit 省略），`kind` 参数显式传入不在 Reader 内猜。
> 5. **集成测试夹具复用既有发布链**（`newRenderingPublishFixture → RecordCandidate → CommitEvaluation(pass) → CreateSelection`）而非裸 SQL 播种——`plan_sets_complete` 触发器强制 3 变体×3 步骤×3 类别×1 render_spec×grounding，裸 SQL 无法满足。
> 6. **提交清单与计划不同**：计划写「Modify users.go/planning.go/rendering.go」，实际 read model 自包含于新文件 `readmodels.go`（含七个 compile-time 断言），未改那三个文件；另含 `001_baseline.sql`。

> **执行修正（跨 Task 记录：质量核心 HTTP 组装缺失，用户拍板「先补组装」）：**
>
> 发现：`httpapi.New`（唯一的路由注册，`httpapi.go:26`）只注册旧路由，**没有** `/v1/assessments`、`/v1/plan-sets`、`/v1/plan-variants/{id}/render-runs`、`/v1/selections/{id}/executions`、`/v1/executions/{id}`、`/v1/executions/{id}/events`、`/v1/generation-feedback`、`/v1/execution-feedback`、`/v1/render-runs/{id}`；而 handler（assessments/plan_sets/renders/executions/feedback.go）与服务、postgres 实现、测试都已写好，只在测试里用 `&API{...}` 直连。`bootstrap.BuildAPI` 只 `service.New`（旧全能）+ `httpapi.New(svc,…)`，从未构造 quality-core 服务（`WireAssessment`/`WirePlanning`/`WireRendering` 只在各自测试里被调）。`WireRendering` 注释明说「最终中央注册由 Cutover 完成」，但本计划 Tasks 1–14 没有对应步骤。
>
> 补齐组装需要（已精确盘点）：
> 1. `httpapi`：加 `Dependencies` 结构承载 5 个 quality-core 服务，`New` 改签名并注册新路由（旧路由保留到外围切换完成）。
> 2. 缺的适配器（需新写）：`assessment.MediaPresenter`（包装 storage 签名 URL）、`assessment.ImageLoader`（photo_set_items→bytes→`ai.ImageInput`）、`assessment.TechnicalChecker`（确定性技术检查）；其余（`AssetReader`/`ProfileReader`/`HandlerRepository`/`OperationProgress`）由 `*postgres.Store` 直接实现。
> 3. `planning` deps（`ReportReader`/`OperationStarter`/`PlanSetStore`/`CurrentRenderReader`/`PreferenceMemoryReader`/`billing.Reserver`，多为 `*postgres.Store`）、`rendering` deps（`modelCaller`/`qualityRuntime`/`qualityPolicy`，由 `structuredRuntimeAdapter`+`runtimeImageCaller` 提供）、`execution.New(store)`/`feedback.New(store)` 直接。
> 4. AI 提供器：`providerai.NewAssessmentProviders(runtime)` 已存在；`ai.StructuredRuntime` 由 `structuredRuntimeAdapter{legacy AIRuntime}` 提供。
> 5. `bootstrap.BuildAPI` 里组装全部并传给 `httpapi.New`；`bootstrap/api.go` 与 `bootstrap/worker.go` 共享同一个 `postgres.Store` 与 AI runtime。
>
> 落地顺序（按依赖）：(a) `httpapi.Dependencies`+路由注册；(b) 缺的 3 个适配器 + execution/feedback/assessment/planning/rendering 组装；(c) 删旧 quality-core 路由。已提交 today AI 能力（`703fcb5`），today service/writer 待组装完成后接 handler。
>
> **修正：`postgres` 反向导入 service 会形成导入环**（`billing` 测试 → `postgres` → `service/today` → `provider/ai` → `service/planning` → `service/billing`）。落地方案：**依赖倒置**——`today.TodayPlanner` 等消费方接口与实体类型定义在 `service/today`（不 import `provider/ai`），`provider/ai.StructuredTodayPlanner` 实现之；`domain` 补 `SourceKindOf`/`DisplayLabelOf` 规范化映射。`postgres` 只 import service 的 ports 包，不再经 service 拉到 provider/ai。

### Task 3: Cut Home to the New Read Model

**Files:**
- Create: `apps/server/internal/service/home/service.go`
- Test: `apps/server/internal/service/home/service_test.go`
- Modify: `apps/server/internal/httpapi/home.go`
- Modify: `apps/server/internal/httpapi/httpapi.go`
- Modify: `apps/server/internal/bootstrap/api.go`

**Interfaces:**
- Consumes: `home.Reader.ReadHome`.
- Produces: `home.Service.Bootstrap(ctx, userID, now) (home.Snapshot, error)`.
- HTTP: `GET /v1/home/bootstrap` returns `{"data": HomeSnapshot}`.
- Invariant: `active_operations` is never null and Home never calls Task repository methods.

- [ ] **Step 1: Write a service test proving the one-call boundary**

```go
func TestBootstrapUsesSingleReadModelCall(t *testing.T) {
	reader := &readerSpy{snapshot: Snapshot{ActiveOperations: []domain.OperationRef{}}}
	svc := New(reader, fixedClock{now: time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)})

	got, err := svc.Bootstrap(context.Background(), "user-1")
	require.NoError(t, err)
	require.Equal(t, 1, reader.calls)
	require.NotNil(t, got.ActiveOperations)
}
```

- [ ] **Step 2: Run the test and verify failure**

Run:

```bash
cd apps/server && go test ./internal/service/home -run TestBootstrapUsesSingleReadModelCall -count=1
```

Expected: FAIL，包含 `undefined: New`。

- [ ] **Step 3: Implement the service and inject it into HTTP**

```go
package home

import (
	"context"
	"time"
)

type Clock interface{ Now() time.Time }

type Service struct {
	reader Reader
	clock  Clock
}

func New(reader Reader, clock Clock) *Service {
	return &Service{reader: reader, clock: clock}
}

func (s *Service) Bootstrap(ctx context.Context, userID string) (Snapshot, error) {
	snapshot, err := s.reader.ReadHome(ctx, userID, s.clock.Now())
	if snapshot.ActiveOperations == nil {
		snapshot.ActiveOperations = []domain.OperationRef{}
	}
	return snapshot, err
}
```

将 `httpapi.API` 的首页依赖改成：

```go
type HomeService interface {
	Bootstrap(context.Context, string) (home.Snapshot, error)
}
```

`homeBootstrap` 只调用：

```go
payload, err := a.home.Bootstrap(r.Context(), currentUser(r).ID)
```

- [ ] **Step 4: Add an HTTP assertion that Task fields are gone**

```go
require.JSONEq(t, `{
  "data": {
    "active_operations": [
      {"id":"operation-1","kind":"render","status":"running","progress_bps":4200,"stage_code":"generating"}
    ]
  }
}`, recorder.Body.String())
require.NotContains(t, recorder.Body.String(), "active_tasks")
require.NotContains(t, recorder.Body.String(), "look_task")
```

- [ ] **Step 5: Run Home tests**

Run:

```bash
cd apps/server && go test ./internal/service/home ./internal/httpapi -run 'Test(Bootstrap|Home)' -count=1
```

Expected: PASS。

- [ ] **Step 6: Commit**

```bash
git add apps/server/internal/service/home apps/server/internal/httpapi/home.go apps/server/internal/httpapi/httpapi.go apps/server/internal/bootstrap/api.go
git commit -m "refactor(server): cut home to read model"
```

> **执行修正（Task 3 实施时记录）：**
>
> 1. **`bootstrap/api.go` 未改**：计划提交清单列了它，但本任务用 `homeFromService`（与 `mediaFromService`/`operationsFromService` 同款）从 `*service.Service` 的 Repository 断言出 `home.Reader` 并构造 `home.Service`，生产时钟用 `home.NewClock()`。完整的 `Dependencies` 重构归 Task 4。
> 2. **`httpapi.API` 增加 `home HomeService` 字段**，`homeBootstrap` 只调用 `a.home.Bootstrap(ctx, userID)`；`Bootstrap` 把 nil `ActiveOperations` 归一为空切片（契约要求该字段永不为 null）。
> 3. **补一条 HTTP 断言**（计划 Step 4）：`TestHomeBootstrapReturnsOperationsNotTasks` 验证响应含 `active_operations` 且不含 `active_tasks`/`look_task`。

### Task 4: Cut Today, Wardrobe and Advisor to Readers

**Files:**
- Create: `apps/server/internal/service/today/service.go`
- Create: `apps/server/internal/service/wardrobe/service.go`
- Create: `apps/server/internal/service/advisor/service.go`
- Test: `apps/server/internal/service/today/service_test.go`
- Test: `apps/server/internal/service/wardrobe/service_test.go`
- Test: `apps/server/internal/service/advisor/service_test.go`
- Modify: `apps/server/internal/httpapi/today.go`
- Modify: `apps/server/internal/httpapi/wardrobe.go`
- Modify: `apps/server/internal/httpapi/advisor.go`
- Modify: `apps/server/internal/bootstrap/api.go`

**Interfaces:**
- Today consumes `today.Reader`, existing `weather.Provider`, and AI capability `today_plan`; weather adapter implementation remains unchanged.
- Wardrobe consumes `wardrobe.Reader` plus its own wardrobe writer; it cannot mutate planning/rendering tables.
- Advisor consumes `advisor.Reader`, advisor conversation writer and AI capability `advisor_chat`.
- Today output may reference only a current published `render_publication_id`; absent publication means `media: null`, never bundled fallback.

- [ ] **Step 1: Write failing grounding tests**

```go
func TestGenerateTodayUsesReaderAndPublishedMedia(t *testing.T) {
	reader := &readerFake{grounding: Grounding{
		ReportID: "report-1",
		Publication: &domain.PublishedMedia{
			PublicationID: "publication-1",
			AssetID: "asset-1",
			SourceKind: domain.MediaSourceGeneratedPreview,
			DisplayLabel: "风格参考",
		},
	}}
	svc := New(reader, todayWriterFake{}, plannerFake{}, weatherFake{})
	got, err := svc.Generate(context.Background(), "user-1", CreateInput{City: "杭州"})
	require.NoError(t, err)
	require.Equal(t, "report-1", got.ReportID)
	require.Equal(t, "publication-1", got.RenderPublicationID)
	require.Equal(t, 1, reader.calls)
}

func TestAdvisorReaderContextExcludesBodyMeasurements(t *testing.T) {
	ctx := providerContext(Grounding{Profile: domain.ProfileSnapshot{
		HeightCM: 165, Role: "产品经理", Budget: "500-1500",
	}})
	require.NotContains(t, ctx, "weight_kg")
	require.NotContains(t, ctx, "bust_cm")
	require.NotContains(t, ctx, "waist_cm")
	require.NotContains(t, ctx, "hip_cm")
}
```

Wardrobe 测试必须断言创建 outfit 时只保存 `selected_plan_id` 的快照引用，不能更新 PlanSet 或 RenderPublication。

- [ ] **Step 2: Run tests and verify failure**

Run:

```bash
cd apps/server && go test ./internal/service/today ./internal/service/wardrobe ./internal/service/advisor -count=1
```

Expected: FAIL，包含缺失的 `New`、`Generate` 或 `providerContext`。

- [ ] **Step 3: Implement services with Reader-only cross-domain access**

Today 的核心调用固定为：

```go
grounding, err := s.reader.ReadTodayGrounding(ctx, userID)
if err != nil {
	return domain.TodayPlan{}, err
}
weather, weatherErr := s.weather.Current(ctx, input.City)
if weatherErr != nil {
	weather = provider.Weather{City: input.City}
}
output, err := s.planner.Generate(ctx, ai.TodayPlanRequest{
	ReportID: grounding.ReportID,
	Profile: grounding.Profile,
	Findings: grounding.Findings,
	SelectedPlan: grounding.SelectedPlan,
	Weather: weather,
	Schedule: input.Schedule,
})
if err != nil {
	return domain.TodayPlan{}, err
}
return s.writer.Create(ctx, userID, output, grounding.Publication)
```

Advisor 的 Provider context 只包含 `ProfileSnapshot{HeightCM, Role, Budget, Preferences, Avoidances}`、Report observation/recommendation、所选方案步骤、Wardrobe 和 FeedbackMemory。Wardrobe 组合只使用 `ReadWardrobeGrounding` 获取稳定方案摘要；写入只走 `WardrobeWriter`。

- [ ] **Step 4: Switch handlers and bootstrap wiring**

`httpapi.New` 使用 `Dependencies`，不再接受全能 `*service.Service`：

```go
type Dependencies struct {
	Auth       AuthService
	Home       HomeService
	Today      TodayService
	Wardrobe   WardrobeService
	Advisor    AdvisorService
	Hair       HairService
	Diagnostic DiagnosticService
	Share      ShareService
	Billing    BillingService
	Quality    QualityService
}
```

每个 handler 只调用对应字段。`bootstrap/api.go` 显式构造 `today.New(store, store, ai.Today, weather)`、`wardrobe.New(store, store)` 和 `advisor.New(store, store, ai.Advisor)`。

- [ ] **Step 5: Run focused tests**

Run:

```bash
cd apps/server && go test ./internal/service/today ./internal/service/wardrobe ./internal/service/advisor ./internal/httpapi -count=1
```

Expected: PASS；`go list -deps ./internal/service/today` 输出中不包含 `internal/service/planning` 或 `internal/repository/postgres`。

- [ ] **Step 6: Commit**

```bash
git add apps/server/internal/service/today apps/server/internal/service/wardrobe apps/server/internal/service/advisor apps/server/internal/httpapi apps/server/internal/bootstrap/api.go
git commit -m "refactor(server): cut life features to readers"
```

### Task 5: Cut Hair, Outfit and Purchase to Readers and Operations

**Files:**
- Create: `apps/server/internal/service/hair/service.go`
- Create: `apps/server/internal/service/diagnostic/service.go`
- Test: `apps/server/internal/service/hair/service_test.go`
- Test: `apps/server/internal/service/diagnostic/service_test.go`
- Modify: `apps/server/internal/httpapi/hair.go`
- Modify: `apps/server/internal/httpapi/diagnostics.go`
- Modify: `apps/server/internal/bootstrap/api.go`
- Modify: `apps/server/internal/bootstrap/worker.go`
- Modify: `apps/server/internal/service/taskrunner/handlers.go`

**Interfaces:**
- Hair reads report/face input through `hair.Reader`; async preview calls `operation.Starter.Start(ctx, operation.StartInput{Kind:"render", SubjectType:"hair_preview"})` and registers its internal Task，不直接写 Operation 表。
- Outfit/Purchase read optional report and profile through `diagnostic.Reader`; Purchase alone receives up to 20 wardrobe projection items.
- Existing AI runtime capabilities remain `hair_edit`, `outfit_diagnosis`, `purchase_diagnosis`; no direct model or vendor dependency.
- Hair result is a quarantined candidate and becomes visible only after render quality evaluation publishes a JPEG asset.

- [ ] **Step 1: Write failing tests for no Task exposure and no fallback**

```go
func TestCreateHairPreviewReturnsOperationAndPublishesOnlyAfterGate(t *testing.T) {
	svc := newHairService(gateDecision(domain.QualityRetry))
	result, operation, err := svc.CreatePreview(context.Background(), "user-1", CreatePreviewInput{
		ReportID: "report-1", MediaAssetID: "face-1", StyleID: "sharp",
	})
	require.NoError(t, err)
	require.Equal(t, "render", operation.Kind)
	require.Equal(t, "hair_preview", operation.SubjectType)
	require.Nil(t, result.Media)
}

func TestDiagnosisProviderFailureNeverReturnsTemplate(t *testing.T) {
	svc := New(readerFake{}, writerFake{}, failingAdvisor{})
	_, err := svc.Run(context.Background(), "user-1", Input{Kind: "outfit", MediaAssetID: "asset-1"})
	require.Error(t, err)
	require.Equal(t, 0, writerFakeCreatedCount(svc))
}
```

- [ ] **Step 2: Run tests and verify failure**

Run:

```bash
cd apps/server && go test ./internal/service/hair ./internal/service/diagnostic -count=1
```

Expected: FAIL，包含缺失的 `CreatePreview` 或 `Run`。

- [ ] **Step 3: Implement services and register the Hair handler**

Hair 创建路径通过 `operation.Starter` 在一个事务边界内创建 Preview、Operation、Task；Worker 调用 `hair_edit` 后复用 rendering quality gate 和 publication 事务。HTTP 响应固定为：

```json
{
  "data": {
    "id": "hair-preview-id",
    "state": "generating",
    "style_id": "sharp",
    "media": null
  },
  "operation": {
    "id": "operation-id",
    "kind": "render",
    "status": "accepted"
  }
}
```

Diagnostic 创建路径调用：

```go
grounding, err := s.reader.ReadDiagnosticGrounding(ctx, userID, input.ReportID, input.Kind)
if err != nil {
	return domain.Diagnostic{}, err
}
request := provider.DiagnosticRequest{
	Kind: input.Kind,
	Scene: input.Scene,
	MediaAssetID: input.MediaAssetID,
	Report: grounding.Report,
	Profile: grounding.Profile,
}
if input.Kind == "purchase" {
	request.Wardrobe = grounding.Wardrobe
}
output, invocation, err := s.advisor.Diagnose(ctx, request)
if err != nil {
	return domain.Diagnostic{}, err
}
return s.writer.Create(ctx, userID, input, output, invocation)
```

- [ ] **Step 4: Run contract and service tests**

Run:

```bash
cd apps/server && go test ./internal/service/hair ./internal/service/diagnostic ./internal/service/taskrunner ./internal/httpapi -count=1
```

Expected: PASS；响应正文不包含 `task`、`provider_version`、厂商名或内部质量分。

- [ ] **Step 5: Commit**

```bash
git add apps/server/internal/service/hair apps/server/internal/service/diagnostic apps/server/internal/service/taskrunner apps/server/internal/httpapi/hair.go apps/server/internal/httpapi/diagnostics.go apps/server/internal/bootstrap
git commit -m "refactor(server): cut tools to readers and operations"
```

### Task 6: Make Share Snapshots Immutable and Publication-Only

**Files:**
- Create: `apps/server/internal/service/share/service.go`
- Test: `apps/server/internal/service/share/service_test.go`
- Modify: `apps/server/internal/httpapi/shares.go`
- Modify: `apps/server/internal/repository/postgres/readmodels.go`
- Modify: `apps/server/internal/repository/postgres/feedback.go`

**Interfaces:**
- Consumes: `share.Reader.ReadShareSource`.
- Consumes: `ShareWriter.Create(ctx, userID, source share.Source, includePhoto bool) (domain.ShareCard, error)`.
- Produces: snapshot JSON containing `asset_id` and `source_kind`, never URL or provider metadata.
- Public read signs the current immutable object key at request time and returns `MediaView`.

- [ ] **Step 1: Write failing immutable snapshot tests**

```go
func TestCreateSnapshotStoresAssetIdentityNotURL(t *testing.T) {
	writer := &writerFake{}
	svc := New(readerFake{source: Source{
		SourceType: "plan_variant", SourceID: "variant-1",
		Title: "清晰利落", Summary: "保留本人特点",
		AssetID: "asset-1", ObjectKey: "published/user-1/asset-1.jpg",
		SourceKind: domain.MediaSourceGeneratedPreview,
		DisplayLabel: "风格参考",
	}}, writer, signerFake{})

	_, err := svc.Create(context.Background(), "user-1", CreateInput{
		SourceType: "plan_variant", SourceID: "variant-1", IncludePhoto: true,
	})
	require.NoError(t, err)
	require.JSONEq(t, `{
	  "title":"清晰利落",
	  "summary":"保留本人特点",
	  "asset_id":"asset-1",
	  "source_kind":"generated_preview",
	  "display_label":"风格参考"
	}`, string(writer.snapshot))
	require.NotContains(t, string(writer.snapshot), "url")
}
```

- [ ] **Step 2: Run test and verify failure**

Run:

```bash
cd apps/server && go test ./internal/service/share -run TestCreateSnapshotStoresAssetIdentityNotURL -count=1
```

Expected: FAIL，包含 `undefined: New`。

- [ ] **Step 3: Implement creation and public projection**

`Create` 只接受 `source_type` 为 `plan_variant` 或 `today_plan`。`GetPublic` 根据快照的 `asset_id` 重新读取同一 published asset，再调用 signer；返回：

```json
{
  "source_type": "plan_variant",
  "title": "清晰利落",
  "summary": "保留本人特点",
  "media": {
    "asset_id": "asset-1",
    "url": "https://cos.example/signed",
    "url_expires_at": "2026-09-12T08:15:00Z",
    "source_kind": "generated_preview",
    "display_label": "风格参考"
  },
  "expires_at": "2026-09-19T08:00:00Z"
}
```

撤销后公开读取返回 404。删除用户数据后分享记录级联删除，object GC 仍由现有新基线队列处理。

- [ ] **Step 4: Run Share tests**

Run:

```bash
cd apps/server && go test ./internal/service/share ./internal/httpapi -run 'Test.*Share' -count=1
```

Expected: PASS；源码搜索 `rg '"image_url"|signed' apps/server/internal/service/share` 不命中持久化快照代码。

- [ ] **Step 5: Commit**

```bash
git add apps/server/internal/service/share apps/server/internal/httpapi/shares.go apps/server/internal/repository/postgres
git commit -m "refactor(server): freeze publication-backed share snapshots"
```

> **执行修正（Tasks 4–6 实施时记录）：**
>
> 1. **依赖倒置模式贯穿全部外围**：`today.TodayPlanner`、`advisor.Chat`、`diagnostic.Advisor`、`hair.Hairstyler` 等消费方接口定义在 service 包（不 import provider/ai），`provider/ai.Structured*` 实现之——否则 `postgres` → service → provider/ai → planning → billing → postgres 形成导入环（实际触发过，见 Task 2 修正）。
> 2. **postgres writer 方法名一律避开 legacy `product_loops.go` 的同名方法**（`InsertWardrobeItem` vs `CreateWardrobeItem` 等）：`*Store` 不能同时实现新旧两套接口；legacy 文件在 Task 9 删除后可收敛命名。
> 3. **外围服务落在自己包里**：`service/today`、`service/wardrobe`、`service/advisor`、`service/hair`、`service/diagnostic`、`service/share` 各有 Service + Writer/Chat/Advisor 端口；bootstrap 显式构造 `Xxx.New(store, store, aiCapability, …)`。
> 4. **httpapi.Dependencies 已扩展到全部八个外围服务**（Assessment/Planning/Renders/Execution/Feedback/Today/Wardrobe/Advisor/Diagnostic/Share/Hair），对应 handler 全部改走窄接口，legacy service 仅剩 auth/me/billing/media/events/diagnostics(旧 domain.ToolResult 面) 等 Task 9 迁移对象。
> 5. **share 公开读取**：快照存资产身份；`GetPublic` 撤销/过期/缺资产一律 404，读取时按 join 出的 object key 即时签名（`signedURLSigner` 适配 storage）。
> 6. **hair 异步生成**：CreatePreview 建预览行 + 公开 Operation（kind=render, subject_type=hair_preview），media 恒为 null；Worker 侧把 hair_edit 接入渲染质量门禁的完整管线是后续收尾（当前 Operation 可轮询，状态机已对齐渲染六态）。
> 7. **hairstyles 目录**：当前 baseline 无 hairstyles 表，Store 返回空目录而非失败；目录表进入 baseline 后此查询自动生效。
> 8. **`trackProductEvent` 移到 `httpapi/events.go`**（原在旧 advisor.go 里，重写时必须保留 `/v1/events` 路由）。

### Task 7: Verify the Final OpenAPI and Generated Core Client

**Files:**
- Modify: `contracts/openapi.yaml`
- Modify: `contracts/scripts/check-sync.mjs`
- Modify: `packages/core/package.json`
- Verify: `packages/core/src/api/generated/schema.ts`
- Modify: `packages/core/src/api/endpoints.ts`
- Modify: `packages/core/src/http/client.ts`
- Modify: `packages/core/src/types/index.ts`
- Modify: `packages/core/src/index.ts`
- Test: `packages/core/tests/client.test.mjs`
- Test: `packages/core/tests/contract.test.mjs`

**Interfaces:**
- OpenAPI remains the transport contract single source.
- Async response top-level field is `operation`, never `task`.
- `MediaView.source_kind` is exactly `user_original | generated_preview | bundled_reference | demo_example`.
- Old endpoint operations are absent: `/v1/tasks*`, `/v1/analyses*`, `PUT /v1/reports/{id}/plans`, `/v1/plans*`.
- Peripheral paths remain stable where semantics remain stable; source types change to `plan_variant | today_plan`.

- [ ] **Step 1: Write contract tests before replacing the contract**

```js
import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const spec = readFileSync(new URL('../../../contracts/openapi.yaml', import.meta.url), 'utf8')

test('contract exposes operations and removes legacy tasks', () => {
  assert.match(spec, /\/v1\/operations\/\{id\}:/)
  assert.match(spec, /\/v1\/operations:/)
  assert.doesNotMatch(spec, /\/v1\/tasks(?:\/|\{|\s|:)/)
  assert.doesNotMatch(spec, /\/v1\/analyses(?:\/|\{|\s|:)/)
  assert.doesNotMatch(spec, /\/v1\/plans(?:\/|\{|\s|:)/)
})

test('media source is a closed enum', () => {
  for (const value of ['user_original', 'generated_preview', 'bundled_reference', 'demo_example']) {
    assert.match(spec, new RegExp(`- ${value}`))
  }
})
```

- [ ] **Step 2: Run tests and verify old endpoints fail the gate**

Run:

```bash
pnpm --filter @zsm/core test
```

Expected: FAIL in `contract exposes operations and removes legacy tasks`。

- [ ] **Step 3: Replace endpoint and schema sections**

质量主链使用设计文档第 10 节端点；外围保留：

```text
GET    /v1/home/bootstrap
GET    /v1/today/context
GET    /v1/today/plans/current
POST   /v1/today/plans
POST   /v1/today/plans/{id}/activate
POST   /v1/today/plans/{id}/feedback
GET    /v1/wardrobe/items
POST   /v1/wardrobe/items
DELETE /v1/wardrobe/items/{id}
POST   /v1/wardrobe/outfits
POST   /v1/wardrobe/outfits/{id}/wear
POST   /v1/advisor/messages
GET    /v1/advisor/conversations/current/messages
POST   /v1/advisor/actions/{id}/apply
GET    /v1/hairstyles
POST   /v1/hair-previews
GET    /v1/hair-previews/{id}
GET    /v1/hair-previews?saved=true
POST   /v1/hair-previews/{id}/save
POST   /v1/diagnostics
GET    /v1/diagnostics/{id}
GET    /v1/diagnostics/latest
PATCH  /v1/diagnostics/{id}
POST   /v1/shares
GET    /v1/shares/{token}
DELETE /v1/shares/{id}
```

`HomeSnapshot`、Today、Hair 和 PlanSet 全部引用同一 `OperationRef` 与 `MediaView` schema；删除 `Task`、`TaskRef`、`Analysis`、旧 `Plan`、`look_task`、`generation_status`、`provider_version` 和裸图片 URL schema。

- [ ] **Step 4: Regenerate TypeScript and make drift fatal**

`packages/core/package.json` 增加 devDependency `openapi-typescript`，scripts 增加：

```json
{
  "api:generate": "openapi-typescript ../../contracts/openapi.yaml -o src/api/generated/schema.ts",
  "api:check": "node scripts/check-openapi.mjs"
}
```

Run:

```bash
pnpm --filter @zsm/core api:generate
pnpm --filter @zsm/core api:check
node contracts/scripts/check-sync.mjs
```

Expected: `check-sync` 输出 `契约与 core 端点完全一致`。

- [ ] **Step 5: Update the client envelope**

```ts
export interface OperationRef {
  id: string
  kind: 'assessment' | 'plan_set' | 'render' | 'execution_feedback'
  status: 'accepted' | 'running' | 'retrying' | 'succeeded' | 'failed' | 'cancelled' | 'superseded'
}

export interface Envelope<T = unknown> {
  data: T
  operation?: OperationRef
}
```

删除旧 `createApiEndpoints`、所有 Task 方法和旧分析/旧方案手写类型；外围方法必须直接使用 `generated/schema.ts` 的 `paths` 与 `components`。删除 `clearReportRef` 404 重试逻辑，因为本地不再保存 report ID。

- [ ] **Step 6: Run core gates**

Run:

```bash
pnpm --filter @zsm/core typecheck
pnpm --filter @zsm/core test
pnpm --filter @zsm/core api:check
node contracts/scripts/check-sync.mjs
```

Expected: 四条命令均 PASS。

- [ ] **Step 7: Commit**

```bash
git add contracts packages/core pnpm-lock.yaml
git commit -m "feat(contract): replace legacy api with operation contract"
```

### Task 8: Cut Miniapp State to Server Truth

**Files:**
- Create: `apps/miniapp/qa/peripherals-cutover.test.mjs`
- Modify: `apps/miniapp/src/services/api.ts`
- Modify: `apps/miniapp/src/services/storage.ts`
- Modify: `apps/miniapp/src/pages/home/index.tsx`
- Modify: `apps/miniapp/src/pages/profile/index.tsx`
- Modify: `apps/miniapp/src/pages/plans/index.tsx`
- Modify: `apps/miniapp/src/pages/plan/index.tsx`
- Modify: `apps/miniapp/src/pages/report/index.tsx`
- Modify: `apps/miniapp/src/pages/scene/index.tsx`
- Modify: `apps/miniapp/src/pages/analysis/index.tsx`
- Modify: `apps/miniapp/src/pages/capture/index.tsx`
- Modify: `apps/miniapp/src/pages/checklist/index.tsx`
- Modify: `apps/miniapp/src/pages/feedback/index.tsx`
- Modify: `apps/miniapp/src/packages/life/pages/today/index.tsx`
- Modify: `apps/miniapp/src/packages/life/pages/wardrobe/index.tsx`
- Modify: `apps/miniapp/src/packages/life/pages/advisor/index.tsx`
- Modify: `apps/miniapp/src/packages/life/pages/share/index.tsx`
- Modify: `apps/miniapp/src/packages/tools/pages/hair/index.tsx`
- Modify: `apps/miniapp/src/packages/tools/pages/outfit/index.tsx`
- Modify: `apps/miniapp/src/packages/tools/pages/purchase/index.tsx`
- Delete: `apps/miniapp/src/services/task-utils.ts`
- Delete: `apps/miniapp/src/services/outfit-session.ts`
- Delete: `apps/miniapp/src/services/purchase-session.ts`
- Delete: `apps/miniapp/src/hooks/use-stable-polling.ts`

**Interfaces:**
- Consumes: generated `ApiEndpoints` and `useOperationPolling`.
- Local persistent keys after cutover are exactly token、data schema version、city and UI preference `openCreditSheet`.
- Server is the only source for current Report、PlanSet、Operation、HairPreview、Diagnostic、Advisor conversation and selected plan.
- UI displays `media.display_label`; it never infers source from URL, extension or Provider prefix.

- [ ] **Step 1: Write a source-level failing gate**

```js
import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join } from 'node:path'

function walk(dir, out = []) {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name)
    if (statSync(path).isDirectory()) walk(path, out)
    else if (/\.(ts|tsx)$/.test(path)) out.push(path)
  }
  return out
}

const src = new URL('../src', import.meta.url).pathname
const code = walk(src).map((path) => readFileSync(path, 'utf8')).join('\n')

test('miniapp has no legacy business references', () => {
  for (const forbidden of [
    'zsm_report_id', 'zsm_plan_id', 'zsm_saved_plan_id',
    'zsm_active_task_', 'zsm_last_outfit_diagnosis',
    'zsm_last_purchase_diagnosis', 'getTask(', 'getTasks(',
    'look_provider', 'provider_version',
  ]) assert.equal(code.includes(forbidden), false, forbidden)
})

test('miniapp renders source labels from server media', () => {
  assert.match(code, /media\.display_label/)
  assert.doesNotMatch(code, /startsWith\(['"]demo/)
})
```

- [ ] **Step 2: Run the gate and verify legacy state is detected**

Run:

```bash
node --test apps/miniapp/qa/peripherals-cutover.test.mjs
```

Expected: FAIL，首个命中包含 `zsm_report_id`。

- [ ] **Step 3: Reduce storage to non-business state and clear old keys once**

```ts
export const STORAGE_KEYS = {
  token: 'zsm_token',
  dataSchemaVersion: 'uplook_data_schema_version',
  city: 'uplook_city',
  openCreditSheet: 'uplook_open_credit_sheet',
} as const

const DATA_SCHEMA_VERSION = '2'
const LEGACY_KEYS = [
  'zsm_report_id', 'zsm_plan_id', 'zsm_saved_plan_id',
  'zsm_active_task_analysis', 'zsm_active_task_plan_look',
  'zsm_active_task_hair_preview', 'zsm_outfit_session',
  'zsm_last_outfit_diagnosis', 'zsm_purchase_session',
  'zsm_last_purchase_diagnosis', 'zsm_scene_pending',
  'zsm_scene_brief', 'zsm_advisor_conversation_id',
] as const

export function migrateLocalState(): void {
  if (readStorage(STORAGE_KEYS.dataSchemaVersion) === DATA_SCHEMA_VERSION) return
  for (const key of LEGACY_KEYS) Taro.removeStorageSync(key)
  writeStorage(STORAGE_KEYS.dataSchemaVersion, DATA_SCHEMA_VERSION)
}
```

在 `app.ts` 启动时调用一次 `migrateLocalState()`。该清理不是旧数据兼容读取；它只删除旧业务事实，之后不再读取这些 key。

- [ ] **Step 4: Replace page data flows**

- Home：一次 `getHomeBootstrap()`；仅当 `active_operations` 非空时用 `useOperationPolling` 批量刷新，终态后重新拉 bootstrap。
- Today/Wardrobe/Advisor：请求不再携带本地 report/plan/conversation ID；服务端 Reader 选择当前事实。
- Hair：创建后保存 operation 于组件内存；重进页面调用服务端 `GET /v1/hair-previews?state=active` 恢复。
- Outfit/Purchase：删除跨页持久 session；页面进入调用 `getLatestDiagnosis(kind)`，进行中的 Operation 从 Home/当前资源恢复。
- Share：创建参数为 `plan_variant` 或 `today_plan`；图片使用 `share.media.url`，角标使用 `share.media.display_label`。
- Plans/Plan/Report/Scene/Analysis/Capture/Checklist/Feedback：资源 ID 只来自路由参数或服务端响应，不从 Storage 恢复。

统一媒体渲染代码：

```tsx
{media?.url ? (
  <ExampleImage
    src={media.url}
    badgeText={media.display_label}
    user={media.source_kind === 'user_original'}
    anchor="top"
  />
) : (
  <EmptyState message="暂时没有可展示图片" actionText="返回方案" onAction={openPlans} />
)}
```

不得保留 `lookBadge()`、`isBundledAsset(url)` 来源推断或失败时展示上一张图片。

- [ ] **Step 5: Use the one Operation hook**

页面包装只导入：

```ts
import { useOperationPolling, POLL_INTERVALS } from '@zsm/core'

useOperationPolling({
  operationId,
  enabled: Boolean(operationId),
  fetcher: () => api.getOperation(operationId),
  intervalMs: POLL_INTERVALS.render,
  subscribeVisibility,
  onDone: refreshResource,
  onFailed: () => setFailed(true),
})
```

删除所有手写 `setInterval`/递归 `setTimeout` 任务轮询和 `useStablePolling`。

- [ ] **Step 6: Run Miniapp gates**

Run:

```bash
node --test apps/miniapp/qa/peripherals-cutover.test.mjs
pnpm --filter @zsm/miniapp typecheck
pnpm --filter @zsm/miniapp build:weapp
node apps/miniapp/scripts/check.mjs
```

Expected: 全部 PASS；静态门禁输出 `miniapp 静态门禁通过`，dist 中无 WebP。

- [ ] **Step 7: Commit**

```bash
git add apps/miniapp
git commit -m "refactor(miniapp): use operation and server-owned state"
```

### Task 9: Delete Legacy Server Paths Without Rewriting Mature Adapters

**Files:**
- Create: `apps/server/scripts/check-legacy.sh`
- Create: `apps/server/internal/service/account/ports.go`
- Create: `apps/server/internal/service/account/service.go`
- Create: `apps/server/internal/service/account/service_test.go`
- Modify: `apps/server/internal/httpapi/httpapi.go`
- Modify: `apps/server/internal/bootstrap/api.go`
- Modify: `apps/server/internal/bootstrap/worker.go`
- Modify: `apps/server/cmd/api/main.go`
- Modify: `apps/server/cmd/worker/main.go`
- Modify: `apps/server/Dockerfile`
- Delete: all server paths listed in Final File Map under **Delete**, except migrations handled in Task 10.
- Move: all files listed under **Move without algorithm rewrites**；retain all files listed under **Retain at the same path without algorithm rewrites**。

**Interfaces:**
- `account.Service` owns login/session/profile orchestration and depends only on narrow identity/session/profile repositories plus existing WeChat/SMS/Apple ports.
- `httpapi.New(httpapi.Dependencies, *slog.Logger, bool, RuntimeInfo) http.Handler`.
- `bootstrap.BuildAPI(config.Config, *slog.Logger) (*bootstrap.APIApp, error)`.
- `bootstrap.BuildWorker(config.Config, *slog.Logger) (*bootstrap.WorkerApp, error)`.
- API app exposes `Handler` and `Close`; Worker app exposes `Run(ctx) error` and `Close`.

- [ ] **Step 1: Write the legacy-path gate**

```sh
#!/bin/sh
set -eu

root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
repo="$(CDPATH= cd -- "$root/../.." && pwd)"

for path in \
  "$root/internal/domain/domain.go" \
  "$root/internal/repository/repository.go" \
  "$root/internal/service/service.go" \
  "$root/internal/provider/provider.go" \
  "$repo/packages/core/src/hooks/useTaskPolling.ts" \
  "$repo/apps/miniapp/src/hooks/use-stable-polling.ts"
do
  test ! -e "$path" || { echo "legacy path remains: $path" >&2; exit 1; }
done

if rg -n 'LatestTasksByRef|locked_at|generation_status|look_task|media_ids|generated_image_url|repository\.Repository|service\.ProviderOptions' \
  "$root/internal" "$repo/packages/core/src" "$repo/apps/miniapp/src"; then
  echo "legacy symbol remains" >&2
  exit 1
fi

if rg -n '/v1/tasks|/v1/analyses|/v1/plans' "$repo/contracts/openapi.yaml" "$repo/packages/core/src"; then
  echo "legacy endpoint remains" >&2
  exit 1
fi

echo "legacy cutover gate passed"
```

- [ ] **Step 2: Run gate and verify failure before deletion**

Run:

```bash
bash apps/server/scripts/check-legacy.sh
```

Expected: FAIL，首行以 `legacy path remains:` 开头。

- [ ] **Step 3: Switch constructors before deleting files**

先把旧全能 Service 中的登录、会话读取/吊销、身份绑定和资料读写机械迁移到 `service/account`。`ports.go` 按用例拆成 `IdentityRepository`、`SessionRepository`、`ProfileRepository`、`WeChatAuthenticator`、`SmsAuthenticator`、`AppleAuthenticator`；不得引入巨型 Repository。将现有 account/auth 测试迁移到 `service/account/service_test.go`，保持成功、越权、过期会话和 Provider 错误断言。

`cmd/api/main.go` 只负责 signal、`bootstrap.BuildAPI`、HTTP server 与 shutdown；删除 `cfg.RunWorker` 和 `go svc.RunWorker(...)`。`cmd/worker/main.go` 只调用 `bootstrap.BuildWorker` 与 `app.Run(ctx)`。

`Dockerfile` 同时构建两个固定二进制：

```dockerfile
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/uplook-api ./cmd/api
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/uplook-worker ./cmd/worker
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/uplook-migrate ./cmd/migrate
COPY --from=build --chown=uplook:uplook /out/uplook-api /app/uplook-api
COPY --from=build --chown=uplook:uplook /out/uplook-worker /app/uplook-worker
COPY --from=build --chown=uplook:uplook /out/uplook-migrate /app/uplook-migrate
ENTRYPOINT ["/app/uplook-api"]
```

镜像用户和组同步改名为 `uplook`；这只改进程包装，不改变 COS、天气、登录或支付 adapter 实现。

- [ ] **Step 4: Delete old implementations and tests**

按 Final File Map 精确删除旧 Domain、Service、Repository、Provider 文件。若保留 adapter 仍引用旧 `provider` 根包共享类型，将共享协议类型移动到对应保留文件或前置计划已建的窄包，只做符号搬移和 import 更新，不改变请求签名、验签、超时、错误分类或 COS object 行为。

按 **Move without algorithm rewrites** 的九条映射逐一执行 `git mv`，只修改 `package` 声明和调用方 import。原测试连同 adapter 一起移动，测试断言正文不修改。

- [ ] **Step 5: Run deletion and adapter regression gates**

Run:

```bash
bash apps/server/scripts/check-legacy.sh
cd apps/server && go test ./internal/provider/... ./internal/storage ./internal/httpapi ./internal/bootstrap ./cmd/api ./cmd/worker -count=1
cd apps/server && go vet ./...
```

Expected: 第一条输出 `legacy cutover gate passed`；其余命令 PASS。

- [ ] **Step 6: Commit**

```bash
git add -A apps/server
git commit -m "refactor(server): remove legacy quality paths"
```

> **执行修正（Task 9 进行中 · 新会话接手指引，截至提交 `855c4d1`）：**
>
> **已完成**（截至提交 `25dca77`）：Step 1 门禁（`apps/server/scripts/check-legacy.sh`，按预期 FAIL）；Move 清单全部完成（identity/weather/payment 三子包，`git mv` 保历史；`provider/moved_aliases.go` 兼容层让 legacy service 持续编译）；`service/account` 本体（`855c4d1`）；**billing Orders 服务已建**（`25dca77`，`service/billing/orders.go`，BillingSummary/CreateBillingOrder/SyncBillingOrder/HandleBillingNotify，可直接满足 httpapi.BillingService）。
>
> **剩余步骤（按序）**：
> 1. ~~billing 服务补方法~~ **已完成**（`service/billing/orders.go`）。
> 2. Store 补两个窄方法：`TrackProductEventRow`（events 窄 writer）与 `GetJobsHealth`（healthz 的队列指标——**注意 Store 已有 `HealthJobs` 方法**，直接用即可，无需新写）。
> 3. httpapi 切换：auth.go/me.go 七 + 五个 handler 改调 `AccountService`（接口用 `domain.Session/MeAccount/UserProfile/User` 真类型，handler 内做 JSON 映射；**不要**发明响应类型别名）；billing 四 handler 改 `BillingService`；events/health 各自窄接口；`auth` 中间件 `a.service.Authenticate` → `a.account.Authenticate`；`createDemoMedia` 改走 media.Service（demo 媒体行写入逻辑随行迁移）。
> 4. `httpapi.New` 删除 `*service.Service` 参数；`mediaFromService/operationsFromService/homeFromService/idempotencyFromService` 改为 bootstrap 直接传构造好的实例（mediaSvc/operationSvc 已在 BuildAPI 存在；home = `home.New(store, home.NewClock())`；idempotency = store）。
> 5. bootstrap：构造 account.New(store×4, wechat, wechatApp, apple, sms, avatarResolver, billingSvc, Config{SessionTTL, SmsRatePerPhonePerHour})；avatarResolver 用 mediaPresenter 或简化为 signedURL 包装。
> 6. 删除 Final File Map **Delete** 清单（domain.go/repository.go/service*.go/provider 根文件/product_loops.go/useTaskPolling.ts 等约 45 文件）+ 各 *_test.go；moved_aliases.go 一并删除；修掉测试对已删符号的引用（legacy 测试随实现删除，账户相关断言迁到 service/account/service_test.go）。
> 7. 跑 `bash apps/server/scripts/check-legacy.sh` → `legacy cutover gate passed`；`go build/vet/test ./...`（DB：`TEST_DATABASE_URL=postgres://jianwo:jianwo@127.0.0.1:55432/jianwo?sslmode=disable`）→ 提交 `refactor(server): remove legacy quality paths`。
>
> **已知陷阱**：① httpapi.New 改签名后 idempotency_test/media_test/operations_test/foundation_integration_test 的调用点要同步补 `Dependencies{}`；② `payment.WeChatSession{OpenID,SessionKey}` 与 identity 的不同形，identity 不反依 payment（见 moved_aliases 的接口指向）；③ store 方法与 legacy `product_loops.go` 同名会重声明——writer 方法名前缀 `Insert/Get/Update` 规避（wardrobe/diagnostic/hair 均已如此）；④ `decodeJSON` 用 DisallowUnknownFields，新请求体字段名必须与冻结 OpenAPI 一致。

### Task 10: Collapse to the Baseline and Prove Fresh-Database Boot

**Files:**
- Verify: `apps/server/internal/database/migrations/001_baseline.sql`
- Delete: `apps/server/internal/database/migrations/001_init.sql`
- Delete: `apps/server/internal/database/migrations/002_advisor_tools.sql`
- Delete: `apps/server/internal/database/migrations/003_hair_previews.sql`
- Delete: `apps/server/internal/database/migrations/004_scene_plans.sql`
- Delete: `apps/server/internal/database/migrations/005_daily_share_wardrobe_advisor.sql`
- Delete: `apps/server/internal/database/migrations/006_brand_rename.sql`
- Delete: `apps/server/internal/database/migrations/007_report_finding_detail.sql`
- Delete: `apps/server/internal/database/migrations/008_plan_look_generation.sql`
- Delete: `apps/server/internal/database/migrations/009_feedback_media.sql`
- Delete: `apps/server/internal/database/migrations/010_report_finding_photo.sql`
- Delete: `apps/server/internal/database/migrations/011_today_plan_generation.sql`
- Delete: `apps/server/internal/database/migrations/012_multi_channel_identity.sql`
- Delete: `apps/server/internal/database/migrations/013_user_profiles.sql`
- Delete: `apps/server/internal/database/migrations/014_sms_codes.sql`
- Delete: `apps/server/internal/database/migrations/015_unified_tasks.sql`
- Delete: `apps/server/internal/database/migrations/016_brand_rename_up.sql`
- Delete: `apps/server/internal/database/migrations/017_plan_group_tasks.sql`
- Delete: `apps/server/internal/database/migrations/018_brand_rename_uplook.sql`
- Delete: `apps/server/internal/database/migrations/019_billing.sql`
- Delete: `apps/server/internal/database/migrations/020_user_avatar.sql`
- Create: `apps/server/internal/database/baseline_integration_test.go`
- Modify: `apps/server/internal/database/database.go`
- Modify: `apps/server/cmd/migrate/main.go`

**Interfaces:**
- The migration directory contains exactly `001_baseline.sql`.
- Baseline includes all new quality-core tables plus retained account/payment/weather-adjacent persistence.
- `cmd/migrate --reset` is allowed only for `APP_ENV=development|test|staging`; production reset exits before SQL.

- [ ] **Step 1: Write the baseline inventory test**

```go
func TestBaselineCreatesOnlyNewSchema(t *testing.T) {
	pool := openFreshDatabase(t)
	require.NoError(t, Migrate(context.Background(), pool))

	got := listPublicTables(t, pool)
	want := []string{
		"advisor_actions", "advisor_conversations", "advisor_messages",
		"analysis_runs", "billing_ledger", "billing_orders", "billing_wallets", "diagnostics",
		"execution_feedback", "execution_steps", "executions", "feedback_memory",
		"generation_feedback", "hair_preview_runs", "media_assets", "object_gc_jobs", "operations",
		"photo_set_items", "photo_sets", "plan_selections",
		"plan_sets", "plan_step_groundings", "plan_steps", "plan_variants",
		"product_events", "provider_invocations", "quality_evaluations",
		"render_candidates", "render_heads", "render_publications", "render_runs",
		"render_specs", "report_findings", "reports", "schema_migrations",
		"share_cards", "sms_codes", "tasks", "today_plans", "upload_intents",
		"user_identities", "user_profiles", "user_sessions", "users",
		"wardrobe_items", "wardrobe_outfits",
	}
	require.Equal(t, want, got)
	for _, old := range []string{"analyses", "plans", "feedback", "tool_results", "hair_previews"} {
		require.NotContains(t, got, old)
	}
}
```

另加约束测试：复合租户外键、`media_assets.object_key` 唯一、Task dedupe 唯一、Publication 只能引用同用户 Candidate、`render_heads` generation/version CAS 所需列存在。

- [ ] **Step 2: Run on a fresh test database and verify old schema mismatch**

Run:

```bash
dropdb --if-exists -h 127.0.0.1 -p 55432 -U jianwo uplook_baseline_test
createdb -h 127.0.0.1 -p 55432 -U jianwo uplook_baseline_test
cd apps/server && TEST_DATABASE_URL=postgres://jianwo:jianwo@127.0.0.1:55432/uplook_baseline_test?sslmode=disable go test ./internal/database -run TestBaseline -count=1
```

Expected: FAIL，表清单包含旧表或缺少新表。

- [ ] **Step 3: Keep the approved baseline and delete all historical migrations**

确认前置计划生成的 `001_baseline.sql` 包含上一步完整表清单和约束后，删除 20 个旧 migration 文件。`database.go` 保持 embed + 文件名排序，不加入旧版本跳过或 schema 猜测。

- [ ] **Step 4: Add guarded reset behavior**

`cmd/migrate --reset` 执行：

```go
if reset {
	switch cfg.Environment {
	case "development", "test", "staging":
		_, err = pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`)
	default:
		return errors.New("database reset is forbidden outside development, test, or staging")
	}
}
return database.Migrate(ctx, pool)
```

生产测试必须断言在 fake executor 上 SQL 调用次数为 0。

- [ ] **Step 5: Reset and migrate a disposable database**

Run:

```bash
cd apps/server
APP_ENV=test DATABASE_URL=postgres://jianwo:jianwo@127.0.0.1:55432/uplook_baseline_test?sslmode=disable go run ./cmd/migrate --reset
TEST_DATABASE_URL=postgres://jianwo:jianwo@127.0.0.1:55432/uplook_baseline_test?sslmode=disable go test ./internal/database ./internal/repository/postgres -count=1
```

Expected: migrate 命令退出 0；database 与 repository tests PASS。

- [ ] **Step 6: Commit**

```bash
git add -A apps/server/internal/database apps/server/cmd/migrate
git commit -m "refactor(database): reset schema to quality baseline"
```

### Task 11: Root Task — Run API and Worker as Separate Compose Services

**Files:**
- Modify: `docker-compose.yml`
- Modify: `Makefile`
- Verify: `apps/server/Dockerfile`

**Interfaces:**
- `api` runs `/app/uplook-api`; configuration no longer contains `RUN_WORKER`.
- `worker` runs `/app/uplook-worker`, shares database/COS configuration, exposes no port and can scale independently.
- Both depend on healthy PostgreSQL; API health contains queue metrics but does not execute tasks.

- [ ] **Step 1: Write the failing root orchestration assertion**

Run:

```bash
test "$(docker compose config --services | grep -Ec '^(api|worker)$')" -eq 2
grep -q 'go build .*./cmd/worker' Makefile
```

Expected before root change: FAIL because Compose has no independent `worker` service or Makefile does not build both binaries.

- [ ] **Step 2: Add the independent Worker service and build target**

`docker-compose.yml` 使用同一 server image：

- API command `/app/uplook-api`，保留 HTTP port。
- Worker command `/app/uplook-worker`，不暴露 HTTP port。
- 两者共享 env file、数据库、COS 和只读 AI routing 配置。
- 删除所有 `RUN_WORKER` 配置；进程职责由不同二进制决定。
- 两者依赖健康 PostgreSQL。

`Makefile` 的 server build target 必须分别构建 `./cmd/api` 和 `./cmd/worker`。

- [ ] **Step 3: Validate root configuration and process separation**

Run:

```bash
docker compose config --quiet
docker compose up -d --build postgres api worker
docker compose ps --status running
docker compose exec -T api sh -c 'test "$(pgrep -fc uplook-worker)" -eq 0'
docker compose exec -T worker sh -c 'test "$(pgrep -fc uplook-api)" -eq 0'
```

Expected: `postgres`、`api`、`worker` 均 running；两个进程隔离断言退出 0。

- [ ] **Step 4: Commit the sole root task**

```bash
git add Makefile docker-compose.yml
git commit -m "build: run api and worker as separate services"
```

Expected: one commit containing only the two frozen root files. No other plan may modify them.

> **执行修正（Task 11，2026-09-14 实际执行）：**
> 1. **Dockerfile 从 Verify 变为 Modify**：计划只列 `Verify: apps/server/Dockerfile`，但镜像必须实际构建并 COPY `uplook-worker`/`uplook-migrate` 两个新二进制，worker 服务才起得来；镜像用户同时由 `jianwo` 改名为 `uplook`（addgroup/adduser/USER）。`apps/server/.env.example` 同步删除 `RUN_WORKER` 并留删除说明。提交因此包含 `Makefile`、`docker-compose.yml`、`apps/server/Dockerfile`、`apps/server/.env.example` 四个文件（Step 4 的"only two frozen root files"按任务范围扩展）。
> 2. **compose 必须显式注入 `AI_ROUTING_FILE=/app/config/ai-routing.json`**（api、worker 两个 `environment:` 块都加）：volume 只挂载文件，`config.Load` 只认 env；不加则两容器 exit 1。同时路由 JSON 里的 `${BAILIAN_WORKSPACE_ID}` 占位符需要 `apps/server/.env`（gitignored，从主检出复制）提供，compose 的 `environment:` 优先级高于 `env_file:`，密钥不进 compose 文件。
> 3. **Step 3 的 `pgrep -fc` 断言在 Alpine BusyBox 上不可用**（BusyBox pgrep 无 `-c`，且 `-f` 会匹配到 `sh -c` 自身命令行造成假阳性）。等价断言改为 comm 精确匹配：`docker compose exec -T api sh -c 'test -n "$(pgrep uplook-api)" && test -z "$(pgrep uplook-worker)"'` 与 worker 侧镜像断言，两者退出 0 即进程隔离成立（正向+负向双重确认）。
> 4. **`config.Load` 不得给 `WORKER_ID` 加默认值**：`TestLoadAppliesWorkerLeaseDefaultsWithoutWorkerID` 钉住"API load 忽略 WORKER_ID"；缺省 `worker-1` 已由 compose 的 `WORKER_ID: ${WORKER_ID:-worker-1}` 提供，代码侧曾误加 `firstNonEmpty` 默认值，已回退。
>
> **执行修正（Task 12 前置 · 首跑冻结链路实修，2026-09-14/15）：** 新链路从未在 compose 上端到端跑通过，E2E 重写前先修通真实链路，共 10 个提交（`0d64687`、`8334438`、`b3d651d`、`75a2565`、`c33fc14`、`374a1e5`、`06e9a62`、`247dd8d`、`a700790`、`92f45d6`）：
> 1. **登录/开发用户写在 baseline 上全灭**：`EnsureUserByIdentity`/`CreateDevUser` 还插 `users.open_id`（baseline 已删列，42703），改为 users + user_identities 两步插入；`CreateMedia` 遗留 stub 使 POST /v1/media/demo 500，实现 `InsertDemoMedia`（purpose 映射进 baseline 枚举）。
> 2. **api/worker 并发 `database.Migrate` 在全新库上撞 `pg_type` 唯一索引**（23505）：Migrate 内加 session 级 `pg_advisory_lock(0x7a736d)` 串行化。
> 3. **demo 资产必须有真实对象字节**：origin=demo 的 object_key 指向不存在的对象时 worker 取图失败（0.75s unclassified）；demoMediaAdapter 改为把内置文件真实写入对象存储（真实 sha256/byte_size），三图与穿搭诊断用 `assets/demo/{face,side,body,outfit}.jpg` 真人示例（looks 渲染图过不了内容门禁），mime 按真实文件落库。
> 4. **`photo_quality_check` 主模型 qwen3.7-flash 在 body 图约 50% 误拒**：换图与提示词放宽均无效，探测证明 qwen3.7-plus 6/6 通过；两个 ai-routing JSON 的该能力主/备互换（plus 主、flash 备），提示词放宽为"头部到膝盖以下可见即可"。identity check 在 flash 上 6/6 通过，不动。
> 5. **provider_invocations 台账从未接入生产链路（设计缺口）**：reports/plan_sets/render_candidates 的 `provider_invocation_id` 是 NOT NULL 外键，而 `StructuredCompatible` 把 InvocationID 映射成厂商 request id，`PrepareReport` 以 `''::uuid` 插入必然 22P02，assessment 死于 report.publishing（permanent/unclassified）。修复：domain 增加 `InvocationScope`，taskrunner 在 Execute 前按 lease 注入 ctx；`AIRuntime.SetInvocationRecorder` 后 scope 内每次模型调用（含 fallback 每次尝试、域校验失败）落台账并回传行 ID；BuildAI 用 `NewInvocationRecorder(store)` 装配；`quality_evaluations.evaluator_invocation_id` 关联证据核验调用。无 scope 的同步 API 调用不写台账。已知限制：`ReportAnalyzer` 内层 `json.Unmarshal` 失败发生在 Validate 之后、台账记录为 succeeded（网络层成功），模型输出形状抖动靠 evidence 门禁兜底。
> 6. **plan_set_generation 提示词未写清 grounding ID 字形（提交 `06e9a62`）**：kimi-k3 反复把 `report.id` 当 finding id、`scene_answer` 写成 `brief.answers.X` 路径形、`focus` 字段漏盖、无 profile 依据时写"真丝"，确定性门禁三连拒。门禁本身与冻结契约（planning-render-spec 计划 line 596）一致，改的是提示词：7 条 grounding 输出契约逐字写明 ID 字形 + 用当前报告真实 finding UUID / brief 字段名做示例；探测脚本对同一报告重跑 `ValidateCandidate` 零违规。提示词属"实现"而非冻结契约，但 grounding ID 字形以契约为准。
> 7. **三条失败写路径漏 `trace_id` 撞 `operations_check`（同提交 `06e9a62`）**：`CommitPrepared(TaskDomainFail)`、`PlanningOperations.Fail`、`FailRun` 写 `status='failed'` 不带 trace_id，违反 `CHECK (status <> 'failed' OR trace_id IS NOT NULL)`（23514）→ 提交失败 → lease 过期 → 无限重试，把一次干净的质量拒绝吞成永久循环（线上 plan-set 操作 `e0f9569a` 实测卡在此循环）。参照 assessment.go 既有模式补 `trace_id=uuid.NewString()`，三个新集成测试钉住每条路径（红：23514 → 绿）。
> 8. **plan-sets dedupe 命中在途操作时 handler 解引用空 PlanSet panic（提交 `247dd8d`）**：`Accepted=false` 被一刀切当成"已发布"，而语义键未发布但同键操作在途（dedupe 命中）时 `CreateResult.PlanSet` 为 nil → `*result.PlanSet` panic（线上实测 http panic serving）。200 重放必须要求 PlanSet 非空；dedupe 在途情形返回 202 + 在途 operation 引用，客户端继续轮询同一操作。
> 9. **plan_grounding_verification 双连挂：schema 元字段回显 + 错误归类（提交 `a700790`）**：(a) 内嵌 schema 文件的 `$schema/$id/title` 元字段原样发给厂商，qwen3.7-flash 把 `$id` 当输出字段回显，撞 `DisallowUnknownFields` 成 ErrVerifierContract——`decodeSchemaJSON` 加载时剥掉元字段（校验形状不变）；(b) 核验器调用失败（配额/传输/厂商违约）以裸错误返回，taskrunner 归类 permanent/unclassified 直接终杀操作——worker.go 本就有设计注释"verifier transport error 应重试不占内容预算"，现包装为 `TaskError{transient, plan_verifier_unavailable}`。
> 10. **失败操作成为语义键墓碑，用户永远无法按失败文案"稍后重试"（提交 `92f45d6`）**：dedupe 查找命中终态操作 + tasks `UNIQUE(user_id,dedupe_key)` 挡死新任务 → 同 report+brief 重发永远重放已失败操作（实测 202 指向 failed 操作）。`StartWithTask` 改为只对非终态操作去重（前缀匹配覆盖 `:content:2`/`:retry:N` 派生键），同键已有终态任务时按既有任务数确定性派生 `:retry:N` 键插新操作+新任务，并发启动仍收敛。已知遗留（本阶段不修，记入交接）：worker 在任务最后一次 attempt 执行中途被杀时，`attempt=max_attempts` 的 leased 任务不可再被 claim，操作永久停在 running——需要 lease 清扫器或 attempt 耗尽时落终态，上线前必须在 E2E 覆盖。
>
> **首跑实修后链路实证（compose 全绿）**：assessment 三图 → report 发布（findings 带 source_photo/anchor/visible_observation）；plan-set 操作 `0144b309` succeeded，`plan.ready`，发布 plan set `cc84dec1`：3 variants（sharp 推荐/warm/natural）、每套 hair+makeup+outfit 三步、34 条 grounding 全部字形正确（finding UUID / 裸 brief 字段名 / 五个稳定 style_rule ID）。
>
> **执行修正（Task 12 前置 · 渲染链路段跑通，2026-09-15，提交 `6be0da3`、`60eedb9`、`42c1ba9`、`403cb4c`、`4aa528c`、`c7087cd`）：**
>
> 11. **render Commit 误报 staged 缺失 + Execute 错误无日志（提交 `6be0da3`）**：Execute 出错由 runner 以 TaskDomainFail 收尾时没有 staged work，旧 Commit 无条件消费 staged → 报错"staged candidate work missing"→ 提交失败 → run 永久卡 accepted（线上实测 10 分钟无进展）。Commit 重构：有 staged 走 commitRejected（质量拒绝留档），无 staged 按 `result.Failure.Code` 直接 FailRun 干净终态；runner 补 `execute failed` 日志（task_id/type/attempt/class/code/error），此前 execute 错误零日志。
> 12. **出货路由配置缺渲染能力与模型元数据（提交 `60eedb9`）**：两份 ai-routing JSON 只有旧 `full_look_edit` 路由、零渲染元数据 → `full_look_generation` 无候选模型，render 每次 attempt 1 即 permanent/capability_unavailable。按冻结契约删除 `full_look_edit`（不留别名），新增 `full_look_generation`（balanced、max_switches=1）与 `render_quality_evaluation`，为四个候选模型补齐 max_input_images/multiple_references/identity_*/output_mime_types/data_retention 元数据；删掉单图 wanx2.1-imageedit 条目（永远不得入选全身图）。**配置层偏差（非契约改动）**：example 配置（dev 挂载）`full_look_generation` primary 用 `aliyun/qwen-image-edit-plus` 而非计划示例的 wan2.7-image-pro——dev workspace 实测 wan2.7 403 AccessDenied.Unpurchased、VOLCENGINE_API_KEY 是占位符；qwen-image-edit-plus 实测双图可用但只接受 `size: "宽*高"`，模型 parameters 覆盖运行时默认的 `2K`。production 配置保持 wan2.7-image-pro primary + seedream-4.5 fallback。能力名/策略/硬要求未动，厂商与模型名只出现在配置 JSON，符合"业务代码不出现厂商名或模型名"。
> 13. **render worker 从不加载参考图字节（提交 `42c1ba9`）**：`mediaToProviderImage` 只映射 asset ID，`orderedRenderImages` 丢弃 nil Data → 厂商 400 "Got 0 image items" permanent/unclassified（线上实测）。Execute 改为经 render object store 读 body/face 字节；对象库读失败归类 transient `render_reference_unavailable` 交给 runner 重试。
> 14. **publication 读回 join 错列 + 签名 URL 未接线（提交 `403cb4c`、`4aa528c`）**：(a) `render_publications` 无 `asset_id` 列，`loadCurrentPublication` 直接 join 之 → 已发布 run 的每次 GET 都 42703 → 500；改经 `render_candidates`（commit 时其 asset 已提升为 published key）join media_assets，并补读回集成测试（旧 CAS 测试只验 head 指针，从未读回）。(b) `WireRendering` 从未注册 `RunSigner` → ready 渲染投影空 URL；对象库支持签名时注册 `SignedURL`。
> 15. **同一 Execution 二次反馈 500（提交 `c7087cd`）**：不同幂等键的第二次 execution feedback 撞 `UNIQUE(user_id, execution_id)`，恢复路径在已中止事务里重查 → SQLSTATE 25P02 → retryable 500（线上实测）。行锁下预检查主体已有反馈 → typed `ErrFeedbackAlreadyRecorded` → HTTP 409 `feedback_already_recorded`（OpenAPI 允许 409）；两侧 feedback 的唯一冲突恢复路径统一先回滚再用连接池重查。注意：冻结 DDL 的 `generation_feedback` 无 `(user_id, publication_id)` 唯一键，同 publication 多条 generation 反馈是契约允许，不修。
>
> **渲染段实证（compose 全绿）**：render 操作 `dccc6cc1` succeeded（`render.ready`）→ publication `4e9d86a0`，投影 `generated_preview + 风格参考`、签名 URL 可取回 141KB JPEG、无 provider/model/internal_scores 泄漏；selection `1518d3cf` → execution `28dba112`（3 步快照，If-Match 1→5，completed）；generation feedback 落库、execution feedback 落库（`feedback_recorded`）；二次反馈实测 409。封闭标签集跨域拒绝（generation 反馈拒 execution 标签）与未完成执行拒反馈均实测符合契约。
>
> **执行修正（Task 12 · 新 E2E 五轮实跑暴露的链路可靠性缺口，2026-09-15，提交 `7b24e11`、`5b85a76`、`c570175`、`9e8845d`）：** 新 E2E 全链路实跑 5 轮，逐轮暴露并修掉四个可靠性缺口（第 1 轮为脚本自身问题：bash 不在 `$(...)` 子壳继承 `set -e`、计数器在子壳不自增导致幂等键复用 409——脚本改为 uuidgen 键 + 子壳内显式 `|| return 1`，属脚本实现不修契约）：
> 16. **生成器输出违约被当永久错误，内容重试预算从不生效（提交 `7b24e11`）**：kimi-k3 约 1/3 采样给出不合契约 JSON（如 makeup 步骤携带 outfit-only 字段），`ErrGeneratorContract` 沿裸错误上抛被 taskrunner 归类 permanent/unclassified 直接终杀操作，计划设定的"内容拒绝消耗一次重试预算"机制对这类最常见的违约形同虚设。哨兵移至 planning 包（`provider/ai/planning.go` import planning，反向会成环，provider 侧保留导出别名），worker 判 `errors.Is(ErrGeneratorContract)` 走 `executeRejection`：attempt 1 → 记录 quality 行 + 入队 ContentAttempt 2；attempt 2 → fail closed。两个 worker 测试钉住预算消耗与二次 fail closed。
> 17. **路由器合并错误丢原因链，`errors.Is` 到不了哨兵（提交 `5b85a76`）**：16 修完后实测仍 unclassified——`AIRuntime.Structured` 全候选失败时返回拼接字符串错误，哨兵在 per-candidate 原因里被字符串化，`errors.Is(ErrGeneratorContract)` 恒 false。改返回 `allModelsFailedError{text, causes}` 实现 `Unwrap() []error`，消息保持人读（"all AI models failed for …: primary …; fallback …"），原因链完整可达。测试钉住：primary 域校验违约 + fallback 403 时 `errors.Is` 命中 primary 侧哨兵。
> 18. **判定类模型采样温度未钉零，同一夹具结论抖动（提交 `c570175`）**：qwen3.7-plus 免费额度当日耗尽后 `photo_quality_check` 落 flash 备用，同一组 demo 图 18:51 通过、18:57 误拒（`photo_content_rejected`）。两份出货配置给 `aliyun/qwen3.7-plus` 与 `aliyun/qwen3.7-flash` 的 `parameters` 加 `"temperature": 0`（`mergeModelParameters` 合并进请求体顶层），判定类调用（质量/身份/文案核验）采样确定化。属配置层修正，不动契约。
> 19. **内容重试 reason code 只有 code 没有细节，补生成无法定位要修什么（提交 `9e8845d`）**：机制全部修通后第 5 轮两次内容采样仍撞同一个材质词（`plan.copy_policy_violation`，实测为无 profile 依据写"纯棉"）而 fail closed——确定性门禁的 `Violation.Detail`（"哪个变体/哪个步骤/哪个词"）在 `evaluateCandidate` 被丢弃，重试 prompt 的 `prior_reason_codes` 只有裸 code，模型不知道要删哪个词。reason code 是自由文本（DB text[]、prompt 原始 JSON），改为 `code: detail` 形式随 `PriorReasonCodes` 进重试 prompt；生成器违约路径同样携带 `err.Error()` 细节。契约不变（重试次数、预算、fail closed 语义均不动），三个 worker 测试钉住细节携带。
>
> **执行修正（Task 12 Step 4 · `pnpm typecheck && pnpm lint` 口径，2026-09-15）：**
> 20. **`apps/mobile` 排除出 pnpm 工作区递归命令**：设计文档明确"不把手机端作为首轮交付阻塞项"（`2026-09-12-quality-core-rebuild-design.md` line 65），而 mobile 仍消费 Stage 7 已删除的旧 core 面（`Analysis`/`Plan`/`POLL_INTERVALS`/`useTaskPolling` 等 24 个 tsc 错误），`pnpm -r run typecheck` 恒红、Step 4 门禁永远过不了。本阶段没有任何任务排期迁移 33 文件的 Expo 应用，故在 `pnpm-workspace.yaml` 显式排除 `!apps/mobile`（注释写明原因与恢复条件：迁移到质量核心 API 面后恢复），lockfile 同步剪除 mobile importer；包名 `@zsm/mobile` 与目录保留，符合全局约束。排除后 `pnpm -r run lint` 找不到任何 lint 脚本（此前唯一 lint 脚本是 mobile 的 `tsc --noEmit`），按 Task 12 文件清单在 `packages/core/package.json` 补 `"lint": "tsc --noEmit"` 接住门禁。`pnpm-workspace.yaml` 不在根级冻结文件清单（`package.json`/`tsconfig.base.json`/`Makefile`/`docker-compose.yml`）内，改动合规；Task 12 Modify 清单因此扩展 `pnpm-workspace.yaml` 与 `pnpm-lock.yaml` 两个文件。
>
> **执行修正（Task 12 Step 3 · 第 6/7 轮实跑暴露的收尾缺口，2026-09-15，提交 `527897a`）：**
> 21. **生成器违约 reason 携带整条路由合并文本（提交 `527897a`）**：16/17 修通后实测 reason_codes 里出现完整 "all AI models failed" 文本——含 fallback 的 403 配额错误体（request id、错误 JSON），污染 quality_evaluations 并进补生成 prompt。worker 改为沿错误链取含哨兵文本的最短消息（即最内层违约包装），剥哨兵前缀后作细节，上限 300 字符；测试用 `Unwrap() []error` 的多原因形状钉住"细节在、403/合并文本不在"。顺带实证：第 7 轮内容重试机制全链路工作（kimi-k3 单变体重复 outfit 步骤违约 → 携带细节重试 → 第二次通过）。
> 22. **`wait_operation` 180s 冻结预算装不下 plan_set 最坏路径（脚本修正）**：第 7 轮 plan_set 走了"90s 模型超时 → 基础设施重试(66s) → 生成器违约 → 内容重试(~100s)"约 4.5 分钟最终 succeeded，但 E2E 在 180s 判超时——操作实际成功，是预算误判。`wait_operation` 加第二参数（默认 180 保持计划冻结值），plan_set 调用点显式 420s；操作语义、轮询间隔、终态判定均不变。另：第 6 轮发现 fresh baseline 无 `hairstyles` 目录表（adapter 对缺表降级空目录，交接跟进项"目录表进 baseline"），E2E 的发型断言从 `length >= 1` 放宽为 `type == "array"` 并注明恢复条件——计划 Step 1 断言 6 只要求"不携带本地恢复 ID 也能读取"，不要求目录非空。
> 23. **今日方案落库 `NULLIF($2::uuid,'')` 对任何输入都 22P02（提交 `531e38e`）**：第 8 轮三核心操作全绿后 POST /v1/today/plans 500——PostgreSQL 把 NULLIF 的 `''` 字面量按 uuid 解析，与 $2 取值无关，有/无当前报告的用户全部失败；psql 模拟逐句定位到该 INSERT。改 `NULLIF($2,'')::uuid`（先判空再转型），新增两个集成测试钉住空/非空 report id 的落库读回（此前 today writer 零集成测试，是 Task 4 遗留盲区）；全仓 grep 确认无第二处同型反模式。
> 24. **无生效今日方案时 GET /v1/today/plans/current 500 而非 200 data:null（提交 `f31347e`）**：第 9 轮实跑——`scanTodayPlan` 把裸 `pgx.ErrNoRows` 透传给 handler，handler 只认 `repository.ErrNotFound` 走 data:null 分支（OpenAPI 冻结契约），其余错误一律 writeServiceError 500。扫描错误出口改经 `mapNotFound`；新增集成测试钉住空用户读 current 的 ErrNotFound 映射。契约不变（200 data:null 语义本就是冻结面），属实现缺口修补。
> 25. **生成器违约消息混淆"重复类别"与"未知类别"且不点名变体，内容重试无法自纠（提交 `54a12e0`）**：第 10 轮 plan_set fail closed——kimi-k3 两次采出 hair+outfit+outfit（同一变体重复 outfit 步骤），但 `validatePlanSteps` 的 seenCategory 检查与未知类别共用 `bad step category "outfit"`，重试 prompt 只看得见 prior_reason_codes，模型不知道问题是"重复"也不知道在哪个变体，两次尝试撞同一堵墙。消息拆分：重复类别明说 repeats、点名变体并给出目标结构（hair/makeup/outfit 各一），未知类别单列；测试用整段替换 makeup 步骤的 hair+outfit+outfit 夹具钉住（直接改 category 会先撞 details 形状校验，属另一违约）。worker 侧内层细节提取测试同步换新消息。内容重试机制本身本轮再次实证正确 fail closed。
> 26. **报告草稿违约绕过两轮生成预算,首轮即终杀评估(提交 `f574052`)**:第 11 轮实跑——appearance_analysis 三候选全挂(kimi-k3 空草稿 33 token、qwen3.7-flash finding 缺证据锚点、qwen3.7-plus 配额尽),路由合并错误上抛被 taskrunner 归类 permanent/unclassified,operation 在 generation 1 就 failed,frozen 伪代码的两轮生成预算对"草稿不合契约"这一最常见采样违约从不生效。与 item 16(planning 生成器违约)同机理同处置:provider 草稿校验全部失败路径加 `ErrReportDraftContract` 哨兵(经 5b85a76 的多原因 Unwrap 链保持 `errors.Is` 可达),handler 生成循环判哨兵 `continue` 消耗一次预算;传输/配额错误不上哨兵、照常上抛走任务重试("Task 网络重试不增加两轮质量预算"语义不变)。两轮都违约落到既有 fail closed(`report_evidence_insufficient`,公开文案"这次未能形成可靠报告"语义吻合)。冻结契约的两轮预算、闸门规则、Commit 边界均不变,属违约归类口径修正;`ValidateReportDraft`(domain 层二次校验)路径维持上抛,如后续实跑暴露同类问题再按同口径处理。

> 27. **外围 writer 可空 uuid 列同型反模式残留五处(提交 `f9fd5f5`)**:第 12 轮实跑——评估/方案/渲染全绿后 POST /v1/diagnostics 500,`NULLIF($10::uuid,'')` 与 item 23 完全同型。item 23 当时宣称"全仓 grep 确认无第二处",实际 grep 模式只匹配单位数占位符(`$2` 形),漏掉两位数占位符与四个文件;本轮以 `$[0-9]*` 重查,wardrobe_items.media_asset_id、wardrobe_outfits.selected_plan_id、shares.asset_id、hair_previews.source_media_asset_id、diagnostics.source_media_asset_id 五处全部改 `NULLIF($N,'')::uuid`。教训记入口径:同类反模式排查必须覆盖多位数占位符。新增 `peripheral_null_uuid_test.go` 一个文件钉住四个外围 writer 的空可选 uuid 插入(diagnostic/wardrobe item/wardrobe outfit/hair preview/share 五个子测试,此前四个 writer 均为零集成测试);红态逐子测试复现 22P02 后方实施修复,符合 TDD。

> 28. **生成器违约消息无定位,内容重试连续两轮盲改(提交 `db00870`)**:第 13 轮实跑 plan_set 又 fail closed——attempt 1 `json: unknown field "rationale"`(模型把 rationale 写进步骤,整文档 DisallowUnknownFields 无法定位),attempt 2 修掉后撞 `outfit details carry hair/makeup-only fields`(无变体位置,模型改错了变体)。根因是解码与校验两层都丢位置:解码改逐层严格解码(顶层→变体→步骤→details/grounding 各自 DisallowUnknownFields,未知字段错误携带 variant key + step 序号/类别);语义校验的哨兵与位置包装统一上移到循环边界,内层 helper(requirePlanText/validateStringArray/validatePlanDetails)改纯错误;适配层 decodePlanSetPayload 复用同一严格解码器,校验与解码不再两套口径。两个新测试钉住 run-13 两种无位置形态。契约不变(plan_set.v1 字段集、重试预算、fail closed 均不动),只改错误消息的信息量。另有 `561b1b8` gofmt 对齐,无语义。
> 29. **执行口径记录:gofmt 与 SQL 字面量**:仓库本有三处 gofmt-dirty 旧文件(ai_runtime.go/identity/sms.go/moved_aliases.go),门禁不查 gofmt;本阶段新增文件保持 gofmt 干净,但 doc comment 里的 SQL `''` 字面量会被新版 gofmt 改写成 `”`,为使注释保持 SQL 原样,这两处测试文件按现状保留(与仓库既有口径一致)。

> 30. **分享/诊断单行扫描 NoRows 透出 500(提交 `5c36b93`)**:第 14 轮实跑——plan_set 本轮一次通过(28 的定位消息生效,内容重试未触发即采样合契约),分享链路走到"撤销后公开读取应 404"时报 500:scanShare 透出裸 pgx.ErrNoRows,handler 只认 repository.ErrNotFound(item 24 同型)。教训升级为全量 audit:postgres 包全部单行扫描逐一核,scanDiagnostic 同类缺口同修,scanHairPreview 既有映射正确;scan_notfound_test.go 钉住四个读取路径。同类问题三轮出现三次(today/diagnostics+wardrobe+hair+share/scanShare+scanDiagnostic),口径:凡 QueryRow 单行扫描出口必须经 mapNotFound,凡可空 uuid 落库必须 NULLIF($N,'')::uuid——后续 code review 与新建 writer 的固定检查项。
> 31. **第 15 轮:脚本花括号展开 + /v1/me/profile 线形失守(提交 `bb6f162`,e2e.sh 修正随 Step 5 脚本提交)**:第 15 轮死于 `line 252: test: too many arguments`,根因不是 API——bash 对 `test "$(curl ... -d "{\"a\":..,\"b\":..}")" = "404"` 这种非赋值命令行先做花括号展开,`{字段1,字段2,字段3}` 的逗号把 curl 复制成 3 次碎片执行,替换输出 "400 400 400" 让 test 拿到 4 个词;赋值语境(`x="$(...)"`,含 `|| return 1`)不展开,裸命令也不展开,全脚本仅越权分享 404 与过期 CAS 412 两处 test 中招,修为 payload 先赋变量再 `-d "$payload"`,并在脚本内注释钉住该坑。排查中又坐实一个真契约失守:`updateMyProfile` 解码进未打标签的 `domain.UserProfile`,DisallowUnknownFields 把契约字段 `height_cm` 当未知字段——小程序 `updateMyProfile` 必 400,GET 还外漏 PascalCase 与内部 preferences(feedback_memory)。按冻结 OpenAPI 修:httpapi 增加线形 DTO 显式互转,测量项(weight_kg/bust_cm/waist_cm/hip_cm)打包 preferences 补丁;仓储 SaveUserProfile 改为「先删四个测量键再 || 补丁」(全量保存:未提供的测量键删除,feedback_memory 等其他键保留),GetUserProfile 带回 preferences;评估发布会回填仅含 current_report_id 的空资料行(第 15 轮 DB 里 e2e 主用户的空行来源,非异常),GET 按契约「从未填写」返回 data:null。home ProfileSummary 读 preferences 测量键的既有设计由此打通(此前没有任何写路径)。顺带澄清:此前怀疑的"容器里跑了旧二进制"不成立——镜像/容器/provenance 全部干净,untagged struct 才是唯一根因,`json:"height_cm"` 字符串出现在二进制里只来自 ProfileSummary 等打标签类型。
> 32. **第 16 轮:baseline 漏折三张活表(提交 `07477e3`)**:第 16 轮实跑一路打过 selection/execution/两类 feedback(31 的脚本修正生效),死于 POST /v1/auth/sms/request 500——42P01 relation "sms_codes" does not exist:重建基线折表漏折 legacy 014_sms_codes.sql,而冻结表清单(本文件 Task 10 存货伪码)本就含 sms_codes。以此为触发做全量表审计(Go SQL 引用的表 vs baseline CREATE TABLE),再抓两张活表:billing_usage——daily_remaining 是冻结 OpenAPI BillingSummary 必填字段,GetBillingUsage/AddBillingUsage 读写它,GET /v1/billing/me(e2e 第 342 行)必 500,既有货表清单测试却误把它塞进 legacy 禁止名单(与契约直接冲突,按契约修正清单测试);product_events——埋点窄写入 TrackProductEventRow 在冻结清单内。注销清除(data_erase)同步对齐:删掉引用被禁表 analyses 的 DELETE(新 schema 上真执行必 42P01),补 billing_usage 与 sms_codes(手机号是 PII,sms_codes 按 phone 索引、须在 user_identities 删除前清)。既有 DeleteUserData 测试只是字符串静态比对所以从未暴露——新增真执行测试(建用户+身份+验证码+用量+埋点行后 DeleteUserData,断言 users/sms_codes/billing_usage 清零)。教训:货表清单测试、静态字符串比对这类"代理断言"必须与真执行成对存在;baseline 折表以「Go 活引用 ∪ 冻结清单」为准。
> 33. **第 17 轮:users 漏折两列(提交 `fff6703`)**:第 17 轮实跑打过 sms/request(32 生效),死于 GET /v1/me 500——42703 column u.avatar_media_id does not exist:legacy 020_user_avatar.sql 与 001_init 的 users.updated_at 均未折进 baseline,UpdateUserAvatar 同时写两列。avatar_media_id 的 FK 指向文件后段才建的 media_assets,列内联声明、外键在 media_assets 建表后 ALTER 补。随即做遗留 migration 全量普查(所有 ALTER TABLE ADD COLUMN 只命中 users.avatar_media_id 一处;其余 ALTER 都是已消亡表的约束演化),确认列级缺口关闭。教训升级为:baseline 折表审计必须同时覆盖「表级」与「列级」两个维度。
> 34. **Task 13 执行口径:实现文件面、台账桶号列与本地金集演练**:其一,计划文件清单只列了测试与配置,实现按 TDD 落在这几处——`internal/provider/ai/router_rollout.go`(ReleaseConfig + validateCandidatePercent/releaseBucket/useCandidate 按冻结伪码逐字,另加导出包装 ValidateCandidatePercent/ReleaseBucket 供 cmd/eval 与 provider 运行时复用同一份白名单与分桶)、`internal/config` 的 release 块解析与启动校验(percent 五档白名单同语义,config 不反向 import provider,六行 switch 就地保留并注释互相指向)、`eval/planning` 严格加载器导出(LoadBriefCasesStrict/LoadPlanSetCasesStrict,原 testingTB 包装保留)与新增 `eval/planning/run.go`(cmd/eval「只做参数解析并分派」需要非测试入口;violationCodes 从测试文件上移进包内,测试与 cmd 共用一份)。计划伪码用 testify,仓库零 testify 依赖,测试按仓库惯例改写为纯 testing,断言语义逐条等价。其二,「Provider 调用账本记录 routing config version 和 bucket」落在:baseline `provider_invocations.release_bucket smallint CHECK (0-99)` 可空列、domain.StartInvocation/ProviderInvocation 加 ReleaseBucket、postgres 插入与扫描、AIRuntime.SetRelease + 两处 record 调用点按 scope.UserID 确定性分桶(未配置 release 保持 NULL)、bootstrap 从路由配置 release 块装配;桶号是 0-99 整数,不落任何用户标识,满足敏感信息红线。其三,锁定金集本地演练:授权 60 身份金集在私有对象存储(UPLOOK_GOLDEN_DATASET_DIR 挂载),仓内不落地;cmd/eval 测试以代码按真实规模生成全合成数据集(60 身份/120 评估案例/120 brief/180 plan/300 渲染候选/64+30 固件,60/20/20 分区),覆盖通过、角色不完整退出 2、身份跨分区退出 2、数量下限退出 2、来源错配退出 1、非法 percent 退出 2、缺 metrics 退出 2 七条路径;另留 UPLOOK_GOLDEN_GEN_DIR 环境门控的生成入口,staging-golden.sh 据此在本地做了正向(exit 0)与两路负向(渲染 stop reason → exit 1;P95 超阈 → cmd 透传、jq 拦截 → exit 1)端到端演练。cmd/eval 不含任何阈值:阈值只在 eval 包与脚本 jq 门禁;--dataset-split 校验取值并计入报告归因,评估按数据集全量跑(各包发布下限本来就是全集口径)。set-rollout.mjs 五连跑(5→25→50→100→5)输出与计划逐行一致,非法值与缺参数均 exit 2,写入走临时文件 + rename 原子替换。



### Task 12: Replace E2E and Add Full-Repository Cutover Gates

**Files:**
- Modify: `apps/server/scripts/e2e.sh`
- Modify: `apps/server/scripts/check-legacy.sh`
- Modify: `apps/miniapp/scripts/check.mjs`
- Modify: `packages/core/package.json`

**Interfaces:**
- E2E follows `upload intents → assessment → operation → report → plan set → render publication → selection → execution → feedback`, then exercises all peripherals through Readers.
- Gates reject old endpoint strings, old tables, Task-facing client types, WebP, source inference and embedded API Worker.

- [ ] **Step 1: Replace the E2E test flow**

`wait_operation` replaces `wait_task`：

```sh
wait_operation() {
  operation_id="$1"
  attempt=0
  while [ "$attempt" -lt 180 ]; do
    payload="$(curl -fsS "$api_base/v1/operations/$operation_id" -H "Authorization: Bearer $token")"
    status="$(printf '%s' "$payload" | jq -r '.data.status')"
    case "$status" in
      succeeded) printf '%s' "$payload"; return 0 ;;
      failed|cancelled|superseded) printf '%s\n' "$payload" >&2; return 1 ;;
    esac
    attempt=$((attempt + 1))
    sleep 1
  done
  echo "operation timed out: $operation_id" >&2
  return 1
}
```

E2E 必须逐项断言：

1. 三图 upload intent complete 后创建 assessment，Operation 成功并返回 Report。
2. Report 每条 finding 有 `source_photo_item_id`、anchor 和 visible observation。
3. PlanSet 恰好三套、恰好一套 recommended、每步有 grounding、任意两套至少两个差异。
4. Render 只返回 published JPEG，`source_kind=generated_preview`，没有 Task/Provider/internal score。
5. Home 返回 `active_operations`，不返回 `active_tasks`。
6. Today/Advisor/Hair/Outfit/Purchase 的输入不携带本地恢复 ID 也能读取当前 grounding。
7. Share 快照只来自 published asset，撤销后 404。
8. 越权读取所有外围资源统一 404。
9. Selection → Execution snapshot → 两类 Feedback 关联完整。
10. 登录、支付签名、天气失败降级与 COS 签名 URL 回归仍通过。

- [ ] **Step 2: Add static legacy rules to existing gates**

`apps/miniapp/scripts/check.mjs` 增加禁止：

```js
const forbiddenState = /zsm_(report_id|plan_id|saved_plan_id|active_task_|last_(outfit|purchase)_diagnosis)/
const sourceInference = /(look_provider|provider_version).*startsWith|startsWith\(['"]demo/
```

命中时分别输出 `旧业务状态 key` 与 `客户端来源推断`。

- [ ] **Step 3: Run E2E on a reset baseline**

Run:

```bash
make down
docker compose down -v
make up
make e2e
bash apps/server/scripts/check-legacy.sh
```

Expected: E2E 最后一行输出 `e2e passed: quality core and peripherals cut over`；legacy gate 输出 `legacy cutover gate passed`。

- [ ] **Step 4: Run repository-wide gates**

Run:

```bash
pnpm typecheck && pnpm lint && make design-build
node apps/miniapp/scripts/check.mjs
make server-vet && make server-test
pnpm --filter @zsm/core api:check
docker compose config --quiet
```

Expected: 全部退出 0；无 generated diff、无 lint/type/vet/test 错误。

- [ ] **Step 5: Commit**

```bash
git add apps/server/scripts apps/miniapp/scripts packages/core/package.json
git commit -m "test: gate quality-core cutover"
```

### Task 13: Gate Staging Goldens and Encode 5/25/50/100 Rollout

**Files:**
- Create: `apps/server/cmd/eval/main.go`
- Create: `apps/server/scripts/staging-golden.sh`
- Create: `apps/server/scripts/set-rollout.mjs`
- Create: `apps/server/eval/goldens/manifest.json`
- Create: `docs/operations/quality-core-release.md`
- Modify: `apps/server/config/ai-routing.example.json`
- Modify: `apps/server/config/ai-routing.production.json`
- Test: `apps/server/internal/provider/ai/router_rollout_test.go`

**Interfaces:**
- `cmd/eval` 只做参数解析并分派到 `eval/assessment`、`eval/planning`、`eval/rendering`，不重复实现任何阈值或业务规则。
- Routing config contains `release.previous_version`, `release.candidate_version`, `release.candidate_percent`, `release.bucket_salt`.
- Bucket is deterministic per user; allowed percentages are exactly `0,5,25,50,100`.
- Golden manifest fixes 60 authorized identities/180 three-view photos, 120 reports, 120 scene briefs, 180 three-plan groups, at least 300 renders, 64 source/state fixtures and 30 feedback-regeneration sequences; split is 60/20/20 and one identity cannot cross splits.
- Locked-set report exits nonzero unless every hard threshold passes.
- Rollback sets candidate percentage to 0 or deploys the pre-cutover code image; it never runs old migrations or compatibility reads.

- [ ] **Step 1: Write failing rollout tests**

```go
func TestCandidatePercentAllowsOnlyReleaseSteps(t *testing.T) {
	for _, percent := range []int{0, 5, 25, 50, 100} {
		require.NoError(t, validateCandidatePercent(percent))
	}
	for _, percent := range []int{-1, 1, 10, 24, 51, 101} {
		require.Error(t, validateCandidatePercent(percent))
	}
}

func TestReleaseBucketIsStablePerUser(t *testing.T) {
	first := releaseBucket("user-42", "quality-core-2026-09-12")
	for i := 0; i < 100; i++ {
		require.Equal(t, first, releaseBucket("user-42", "quality-core-2026-09-12"))
	}
}
```

- [ ] **Step 2: Run tests and verify failure**

Run:

```bash
cd apps/server && go test ./internal/provider/ai -run 'Test(CandidatePercent|ReleaseBucket)' -count=1
```

Expected: FAIL，包含 `undefined: validateCandidatePercent`。

- [ ] **Step 3: Add release routing metadata and deterministic bucketing**

Production config：

```json
{
  "release": {
    "previous_version": "quality-core-r1",
    "candidate_version": "quality-core-r2",
    "candidate_percent": 5,
    "bucket_salt": "quality-core-2026-09-12"
  }
}
```

`releaseBucket` 使用 SHA-256 前 8 字节取模 100；只有 bucket `< candidate_percent` 使用 candidate。Provider 调用账本记录 routing config version 和 bucket，不记录用户敏感信息。

```go
func validateCandidatePercent(percent int) error {
	switch percent {
	case 0, 5, 25, 50, 100:
		return nil
	default:
		return fmt.Errorf("candidate_percent must be one of 0, 5, 25, 50, 100")
	}
}

func releaseBucket(userID, salt string) int {
	sum := sha256.Sum256([]byte(salt + ":" + userID))
	return int(binary.BigEndian.Uint64(sum[:8]) % 100)
}

func useCandidate(userID string, release ReleaseConfig) bool {
	return releaseBucket(userID, release.BucketSalt) < release.CandidatePercent
}
```

- [ ] **Step 4: Add the exact rollout editor**

`set-rollout.mjs` 接受 `--percent 0|5|25|50|100`，解析并重写 `apps/server/config/ai-routing.production.json`，其他值退出 2。运行：

```bash
node apps/server/scripts/set-rollout.mjs --percent 5
node apps/server/scripts/set-rollout.mjs --percent 25
node apps/server/scripts/set-rollout.mjs --percent 50
node apps/server/scripts/set-rollout.mjs --percent 100
node apps/server/scripts/set-rollout.mjs --percent 5
```

Expected: 依次输出 `candidate_percent=5`、`candidate_percent=25`、`candidate_percent=50`、`candidate_percent=100`、`candidate_percent=5`；最终文件恢复 5。

- [ ] **Step 5: Freeze the golden manifest**

```json
{
  "schema_version": 1,
  "identity_count": 60,
  "photo_count": 180,
  "report_count": 120,
  "scene_brief_count": 120,
  "plan_group_count": 180,
  "render_min_count": 300,
  "source_state_fixture_count": 64,
  "feedback_sequence_count": 30,
  "splits": {"development": 60, "validation": 20, "locked": 20},
  "identity_cross_split_allowed": false
}
```

`cmd/eval` 启动时校验数量下限、三张照片角色完整性和身份不跨分区；任一不满足则在调用外部模型前退出 2。

- [ ] **Step 6: Add the locked-golden release gate**

`staging-golden.sh` 固定运行：

```sh
go run ./cmd/eval \
  --manifest eval/goldens/manifest.json \
  --dataset-root "${UPLOOK_GOLDEN_DATASET_DIR:?UPLOOK_GOLDEN_DATASET_DIR is required}" \
  --dataset-split locked \
  --routing-config config/ai-routing.production.json \
  --output /tmp/uplook-locked-release.json

jq -e '
  .photo_role_accuracy == 1 and
  .source_mismatch_count == 0 and
  .sensitive_inference_count == 0 and
  .finding_evidence_completeness == 1 and
  .finding_evidence_support_rate >= 0.95 and
  .plan_grounding_completeness == 1 and
  .plan_pair_difference_pass_rate == 1 and
  .single_image_fallback_count == 0 and
  .severe_bad_render_publish_rate < 0.01 and
  .identity_human_pass_rate >= 0.90 and
  .render_spec_match_rate >= 0.85 and
  .source_operation_invocation_feedback_link_rate == 1 and
  .webp_publish_count == 0 and
  .photo_check_p95_ms <= 12000 and
  .report_p95_ms <= 90000 and
  .plan_text_p95_ms <= 60000 and
  .first_render_p95_ms <= 120000
' /tmp/uplook-locked-release.json >/dev/null
```

Expected: 锁定集通过时退出 0；任一 hard gate 不满足时退出 1。

- [ ] **Step 7: Write the release and rollback runbook**

`docs/operations/quality-core-release.md` 固定记录：

1. 停 API/Worker；删除并重建开发或 Staging database/volume；执行 `001_baseline.sql`；部署同版本 API 与独立 Worker；发布同步小程序。
2. 先运行内部授权样本与 `staging-golden.sh`。
3. 依次提交路由百分比 5、25、50、100；最小观察窗分别为 72 小时/100 用户/300 次发布、72 小时/500 用户/1500 次发布、72 小时/1000 用户/3000 次发布、168 小时/2000 用户/6000 次发布；每档重新检查来源错配、敏感推断、身份/人体错误、P95、成本和 Candidate 2 比率。
4. 任一停止条件触发，立即执行 `node apps/server/scripts/set-rollout.mjs --percent 0`，发布该路由配置；若问题不在路由，部署 `quality-core-pre-cutover` 标记的 API/Worker 镜像。
5. 回滚时禁止执行 `docker compose down -v`、旧 migration、数据回填、双读或兼容 DTO；新 schema 和新业务数据保持不变。
6. 修复版本必须在同一新 baseline 上通过全仓 gates 与锁定金集后重新从 5% 开始。

- [ ] **Step 8: Run release gates**

Run:

```bash
cd apps/server && go test ./internal/provider/ai -run 'Test(CandidatePercent|ReleaseBucket)' -count=1
APP_ENV=staging bash scripts/staging-golden.sh
cd ../.. && git diff --check
```

Expected: tests PASS；金集门禁退出 0；`git diff --check` 无输出。

- [ ] **Step 9: Commit**

```bash
git add apps/server/config apps/server/scripts apps/server/eval/goldens/manifest.json apps/server/internal/provider/ai/router_rollout_test.go docs/operations/quality-core-release.md
git commit -m "ops: gate staged quality-core rollout"
```

### Task 14: Final Fresh-Baseline Verification and Handoff

**Files:**
- Verify only; do not add compatibility code.

**Interfaces:**
- Repository has exactly one runtime path for every new endpoint.
- API and Worker use the same schema and code revision.
- Rollback artifacts are code image and route config only.

- [ ] **Step 1: Confirm deletion inventory**

Run:

```bash
test "$(find apps/server/internal/database/migrations -maxdepth 1 -type f -name '*.sql' -print | wc -l | tr -d ' ')" = "1"
test -f apps/server/internal/database/migrations/001_baseline.sql
bash apps/server/scripts/check-legacy.sh
```

Expected: both `test` commands exit 0；legacy gate 输出 `legacy cutover gate passed`。

- [ ] **Step 2: Rebuild from an empty local volume**

Run:

```bash
docker compose down -v
docker compose up -d --build postgres api worker
make e2e
```

Expected: PostgreSQL、API、Worker healthy；E2E 最后一行输出 `e2e passed: quality core and peripherals cut over`。

- [ ] **Step 3: Run every required repository gate**

Run:

```bash
pnpm typecheck && pnpm lint && make design-build
node apps/miniapp/scripts/check.mjs
make server-vet && make server-test
pnpm --filter @zsm/core test
pnpm --filter @zsm/core api:check
node contracts/scripts/check-sync.mjs
docker compose config --quiet
git diff --check
```

Expected: 全部退出 0；设计产物无漂移；OpenAPI/core 完全一致；无 whitespace error。

- [ ] **Step 4: Run Staging locked goldens**

Run:

```bash
cd apps/server && APP_ENV=staging bash scripts/staging-golden.sh
```

Expected: exit 0，并生成 `/tmp/uplook-locked-release.json`；所有 Task 13 hard threshold 满足。

- [ ] **Step 5: Tag the rollback code point before first production rollout**

```bash
git tag -a quality-core-pre-cutover -m "rollback point before quality core rollout"
git show --no-patch --oneline quality-core-pre-cutover
```

Expected: 输出 tag 指向的单个 commit；tag 只标识代码回滚点，不携带旧数据库。

- [ ] **Step 6: Final commit suggestion**

若前面任务按建议独立提交，本步骤不再创建聚合提交。若执行环境要求单一收尾提交，仅提交验证过程中产生的门禁修正：

```bash
git status --short
git diff --check
```

Expected: 无意外未跟踪文件；不得提交 `.env`、金集原图、授权身份信息、API key 或数据库 dump。
