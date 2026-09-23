# Miniapp Quality Loop Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 用新的 `/v1` OpenAPI 契约重建 uplook 小程序从三图建档、可信报告、三套方案、渐进式效果图、选择执行到两类反馈的完整质量闭环。

**Architecture:** `contracts/openapi.yaml` 是传输契约单源，`@zsm/core` 只承载生成的类型、由生成 `paths` 约束的客户端、媒体真实性纯函数和平台无关 Operation 轮询控制器；`apps/miniapp/src/app` 承载 Taro transport、资源缓存和唯一 React 轮询包装，`features/*` 承载用例与界面，`pages/*` 只读取路由参数并组合 Feature Screen。Report、PlanSet、Operation、Selection、Execution 与当前指针全部以服务端为事实源，本地只缓存按资源 ID 分区的数据和短期签名 URL，不持久化业务 ID。

**Tech Stack:** OpenAPI 3.1、openapi-typescript 7.13.0、openapi-fetch 0.17.0、TypeScript 5.6.3（core）/ 5.9.3（miniapp）、React 18.3.1、Taro 4.2.1、Node 22 `node:test`、SCSS/rpx。

## Global Constraints

- `appearance-coach-prototype/` 只读，任何任务不得修改该目录。
- 根级 `package.json`、`tsconfig.base.json`、`Makefile`、`docker-compose.yml` 冻结；依赖只改 `packages/core/package.json`、`apps/miniapp/package.json` 和 `pnpm-lock.yaml`。
- 包名保持 `@zsm/core`、`@zsm/design`、`@zsm/miniapp`；不增加新的共享包。
- 直接重定义 `/v1`，不增加 `/v2`，不保留旧 API、旧 DTO、旧 Storage key、旧 Task 客户端读取或任何兼容层。
- 不写占位符，不保留双路径，不用 `unknown` 承载业务 DTO；OpenAPI 中的 `PlanStep.details` 必须是按 `category` 判别的结构化联合类型。
- 客户端只读取公开 Operation；禁止请求 `/v1/tasks`，禁止显示 Task payload、厂商、模型、内部质量分数或内部错误。
- 服务端是 Report、PlanSet、Operation、Selection、Execution 和当前状态的唯一事实源；Storage 只允许 token、`uiSchemaVersion`、`compareHint`、`city`、`openCreditSheet`。
- Report、PlanSet、RenderPublication 是不可变资源；重新分析、重新规划、重新生成返回新 ID，客户端不得原地拼接旧资源。
- PlanSet Cache 按 `plan_set_id` 分区，Report Cache 按 `report_id` 分区，Operation Cache 按 `operation_id` 分区，Media Cache 按 `asset_id + url` 分区；不得按页面、slug、slot 或“最近一次”共享图片。
- 当前报告只由 `GET /v1/reports/current` 或 `GET /v1/home/bootstrap` 返回；方案对比的“原本”必须读取该 PlanSet 绑定 `report_id` 的 body Asset，禁止读取最新报告、Storage reportId 或上一套方案图片。
- 图片来源只读服务端强类型 `source_kind`：`user_original | generated_preview | bundled_reference | demo_example`；不得从 URL、文件后缀、Provider 名称或路径推断来源和状态。
- 用户原图角标“原本”；生成图和内置图角标统一“风格参考”；Demo 角标“效果示例”。`demo_example` 必须使用 `.example-badge + .example-soft`，`bundled_reference` 也必须使用 `.example-badge + .example-soft`。
- `provider_version` 以 `demo` 开头的旧判断全部删除；Demo 只由服务端 `/v1/media/demo` 与 Demo Provider 进入，客户端不得注入 Demo。
- `lookImage(v)` 与 `userImage(v)` 无效时返回空，不回退内置图；`exampleImage(slug, variant)` 是唯一内置模特图入口。
- 删除 `PinnedImage`、`pinnedUrl` 和跨资源旧图保留；资源后台校验期间可继续显示同一 `asset_id` 的已提交缓存，服务端返回空、失效 URL、不同 `asset_id` 或来源不符时必须立即显示空态。
- 用户上传只接受 JPEG/PNG；Provider 输出、Demo 与包内照片只展示 JPEG；小程序收到或打包 WebP 次数必须为 0。
- 生成中、失败、不可用、部分就绪、完全就绪、Demo、内置参考和真实生成结果都有显式状态；单套效果图失败不遮挡文字方案和其他合格方案。
- 错误态与内容不同屏；空态和错误态必须提供“重试 / 返回 / 重新拍摄”中的明确下一步。
- Operation 进度只显示服务端 `progress_bps`、`stage_code`、`public_message`，客户端不得按时间推算业务进度。
- 所有轮询只能通过 `apps/miniapp/src/app/operations/use-operation-polling.ts` 的 `useOperationPolling`；内部只创建一个 handle，页面隐藏立即停，恢复立即刷新，卸载清理，连续失败 5 次进入失败态。
- tab 页使用 `useDidShow + ref`：有缓存先渲染，后台校验不清空已显示图片，不在首次 `useDidShow` 重复加载。
- 样式一律使用 `rpx`，`designWidth: 750`；业务 SCSS 和 JSX style 字符串禁止裸 `px`，发丝线使用 `1rpx`。
- 包内照片只发 JPEG，tabBar/图标使用 PNG；禁止 SVG 作为小程序运行时图片。
- 所有 Taro 自定义组件的 `index.config.ts` 必须设置 `styleIsolation: 'apply-shared'`。
- React 条件渲染禁止 `{count && <View />}`；使用 `count > 0 ? <View /> : null` 或 `Boolean(value) ? ... : null`。
- 列表 key 必须使用资源 ID、角色、枚举值或稳定 `client_event_id`；禁止数组下标。
- 动效参数只取 `packages/design/src/motion.ts`，`prefers-reduced-motion` 时持续时间归零。
- 不打颜值分或身材分，不身材羞辱，不做医学、年龄、族裔、性格或社会身份结论，不使用警示红；Finding 只使用“可提升点”和建议优先级。
- 文案只放 `packages/core/src/copy/zh.ts`；产品名“uplook”，标语“今天最好看”，首页保留“你好，我是你的私人形象顾问”，场景使用“日常”而不是“通勤”。
- 底部导航固定“首页 / 方案 / 我的”；实验能力只进入“体验实验室”。
- 本计划不包含手机端、不包含数据库或服务端实现、不包含真机验收任务、不包含微信提审材料；HTTP fixture 和自动化 E2E 是验收依据。
- 每个提交只覆盖本任务列出的边界；计划执行前已有未跟踪文件不得加入提交。

---

## Execution Order and Ownership

- 本计划在 Foundation、Assessment/Report、Planning/RenderSpec、Rendering/Quality、Execution/Feedback/Billing 五份服务端计划完成后执行。
- 前置服务端计划拥有各自 `contracts/openapi.yaml` 段；本计划拥有唯一一次最终 OpenAPI codegen、`packages/core/src/api/**`、旧手写 Core 类型/端点删除，以及 Miniapp 全部主闭环改造。
- Task 1 先验证最终 OpenAPI 已包含本计划“Contract Locked”中的全部 operationId；只允许修复传输层缺失或 codegen 不兼容，不得在客户端计划中发明服务端尚未实现的业务语义。
- 生成文件唯一位置固定为 `packages/core/src/api/generated/schema.ts`；仓库不得再出现 `packages/core/src/api/generated.ts`。
- Task 1 在 Rule/Contract Freeze 阶段只新增 generated client/types，不删除仍被旧应用引用的 `packages/core/src/types/index.ts`、`api/endpoints.ts` 或 Task polling；这些旧文件只能在 Task 12 的一次性客户端切换提交中删除。
- 本计划完成后，外围切换计划只能验证 generated client 无漂移，不能再次选择 codegen 工具版本、修改生成路径或恢复手写 `types/index.ts`、`api/endpoints.ts`。

## File Map

### Contract and generated core

- Modify `contracts/openapi.yaml`: 用新质量主链端点和强类型响应替换旧 Analysis/Task/Plan 契约。
- Modify `contracts/scripts/check-sync.mjs`: 改为检查 OpenAPI `operationId` 与生成 client 的漂移，不再读取手写 `API_PATHS`。
- Modify `packages/core/package.json`: 固定 codegen/runtime 版本并增加 `api:generate`、`api:check`。
- Modify `pnpm-lock.yaml`: 只记录 `openapi-typescript@7.13.0` 与 `openapi-fetch@0.17.0` 的依赖变化。
- Create `packages/core/scripts/check-openapi.mjs`: 在临时目录重新生成并逐字节比较。
- Create `packages/core/src/api/generated/schema.ts`: `openapi-typescript` 生成文件，禁止手改。
- Create `packages/core/src/api/client.ts`: 从生成 `paths` 创建 `openapi-fetch` 客户端。
- Create `packages/core/src/api/types.ts`: 只给生成 components 起稳定别名，不重新声明字段。
- Create `packages/core/src/operations/polling.ts`: 平台无关 Operation 轮询控制器。
- Create `packages/core/src/media/display.ts`: 强类型来源校验和展示投影。
- Modify `packages/core/src/index.ts`: 只导出新 API、Operation、媒体与 copy。
- Delete `packages/core/src/api/endpoints.ts`.
- Delete `packages/core/src/types/index.ts`.
- Delete `packages/core/src/hooks/useTaskPolling.ts`.
- Modify `packages/core/src/media/truth.ts`: 保留 `lookImage/userImage/exampleImage` 真实性契约，删除旧 PNG/WebP 改写与 URL 来源推断。

### Miniapp application shell

- Create `apps/miniapp/src/app/api/client.ts`: 单例生成 client、鉴权与 401 单飞。
- Create `apps/miniapp/src/app/api/taro-fetch.ts`: Taro.request 到 Fetch-compatible transport。
- Create `apps/miniapp/src/app/api/result.ts`: 统一解包成功/错误，不吞 request ID。
- Create `apps/miniapp/src/app/api/media-upload.ts`: Upload Intent、COS 直传、complete。
- Create `apps/miniapp/src/app/api/quality.ts`: 主闭环的窄 API 函数。
- Create `apps/miniapp/src/app/cache/resource-cache.ts`: 资源级 stale-while-revalidate cache。
- Create `apps/miniapp/src/app/cache/use-resource.ts`: React 订阅包装。
- Create `apps/miniapp/src/app/operations/use-operation-polling.ts`: 唯一 React/Taro Operation 轮询 hook。
- Create `apps/miniapp/src/app/operations/operation-view.ts`: Operation 到 UI 状态的纯投影。
- Modify `apps/miniapp/src/services/storage.ts`: 只留下允许的五个 key。
- Delete `apps/miniapp/src/services/api.ts`.
- Delete `apps/miniapp/src/services/task-utils.ts`.
- Delete `apps/miniapp/src/hooks/use-stable-polling.ts`.
- Modify `apps/miniapp/src/services/local-looks.ts`: 继续作为 `exampleImage` 的小程序 JPEG resolver，不承载业务结果 fallback。

### Features

- Create `apps/miniapp/src/features/capture/model.ts`.
- Create `apps/miniapp/src/features/capture/CaptureScreen.tsx`.
- Create `apps/miniapp/src/features/capture/index.scss`.
- Create `apps/miniapp/src/features/assessment/model.ts`.
- Create `apps/miniapp/src/features/assessment/AssessmentScreen.tsx`.
- Create `apps/miniapp/src/features/assessment/index.scss`.
- Create `apps/miniapp/src/features/report/model.ts`.
- Create `apps/miniapp/src/features/report/ReportScreen.tsx`.
- Create `apps/miniapp/src/features/report/index.scss`.
- Create `apps/miniapp/src/features/planning/model.ts`.
- Create `apps/miniapp/src/features/planning/PlansScreen.tsx`.
- Create `apps/miniapp/src/features/planning/PlanDetailScreen.tsx`.
- Create `apps/miniapp/src/features/planning/SceneBriefScreen.tsx`.
- Create `apps/miniapp/src/features/planning/index.scss`.
- Create `apps/miniapp/src/features/execution/model.ts`.
- Create `apps/miniapp/src/features/execution/ExecutionScreen.tsx`.
- Create `apps/miniapp/src/features/execution/index.scss`.
- Create `apps/miniapp/src/features/feedback/model.ts`.
- Create `apps/miniapp/src/features/feedback/GenerationFeedback.tsx`.
- Create `apps/miniapp/src/features/feedback/ExecutionFeedbackScreen.tsx`.
- Create `apps/miniapp/src/features/feedback/index.scss`.

### Shared components and thin routes

- Create `apps/miniapp/src/components/source-image/index.tsx`.
- Create `apps/miniapp/src/components/source-image/index.scss`.
- Create `apps/miniapp/src/components/source-image/index.config.ts`.
- Create `apps/miniapp/src/components/operation-status/index.tsx`.
- Create `apps/miniapp/src/components/operation-status/index.scss`.
- Create `apps/miniapp/src/components/operation-status/index.config.ts`.
- Create `apps/miniapp/src/components/render-state/index.tsx`.
- Create `apps/miniapp/src/components/render-state/index.scss`.
- Create `apps/miniapp/src/components/render-state/index.config.ts`.
- Modify `apps/miniapp/src/pages/capture/index.tsx`.
- Modify `apps/miniapp/src/pages/analysis/index.tsx`.
- Modify `apps/miniapp/src/pages/report/index.tsx`.
- Modify `apps/miniapp/src/pages/scene/index.tsx`.
- Modify `apps/miniapp/src/pages/plans/index.tsx`.
- Modify `apps/miniapp/src/pages/plan/index.tsx`.
- Modify `apps/miniapp/src/pages/checklist/index.tsx`.
- Modify `apps/miniapp/src/pages/feedback/index.tsx`.
- Modify `apps/miniapp/src/pages/home/index.tsx`.
- Modify `apps/miniapp/src/pages/profile/index.tsx`.
- Delete上述页面迁出的 `index.scss`; page config 保留，样式由 Feature Screen 引入。

### Automated tests and gates

- Create `packages/core/tests/generated-client.test.mjs`.
- Replace `packages/core/tests/polling.test.mjs` 中 Task 用例为 Operation 用例。
- Replace `packages/core/tests/truth.test.mjs` 为强来源媒体用例。
- Create `apps/miniapp/tests/api-result.test.mjs`.
- Create `apps/miniapp/tests/resource-cache.test.mjs`.
- Create `apps/miniapp/tests/operation-view.test.mjs`.
- Create `apps/miniapp/tests/capture-model.test.mjs`.
- Create `apps/miniapp/tests/report-model.test.mjs`.
- Create `apps/miniapp/tests/planning-model.test.mjs`.
- Create `apps/miniapp/tests/execution-model.test.mjs`.
- Create `apps/miniapp/tests/feedback-model.test.mjs`.
- Create `apps/miniapp/tests/architecture.test.mjs`.
- Create `apps/miniapp/qa/quality-loop.fixture.test.mjs`.
- Modify `apps/miniapp/package.json`: 增加 `test` 与 `qa`，不增加 UI 测试框架。
- Modify `apps/miniapp/scripts/check.mjs`: 增加 rpx、WebP、styleIsolation、条件渲染、key、轮询与薄 page 门禁。

## Contract Locked for This Plan

OpenAPI 必须暴露以下 operationId；Feature 不得自行发明其他质量主链路径：

- `createUploadIntent`: `POST /v1/media/upload-intents`
- `completeUploadIntent`: `POST /v1/media/upload-intents/{id}/complete`
- `createDemoMedia`: `POST /v1/media/demo`
- `createAssessment`: `POST /v1/assessments`
- `getOperation`: `GET /v1/operations/{id}`
- `listOperations`: `GET /v1/operations?ids=op-1,op-2`
- `getCurrentReport`: `GET /v1/reports/current`
- `getReport`: `GET /v1/reports/{id}`
- `createPlanSet`: `POST /v1/plan-sets`
- `getPlanSet`: `GET /v1/plan-sets/{id}`
- `listPlanSets`: `GET /v1/plan-sets?report_id=&scene=`
- `createRenderRun`: `POST /v1/plan-variants/{id}/render-runs`
- `getRenderRun`: `GET /v1/render-runs/{id}`
- `putPlanSetSelection`: `PUT /v1/plan-sets/{id}/selection`
- `createSelectionExecution`: `POST /v1/selections/{id}/executions`
- `getExecution`: `GET /v1/executions/{id}`
- `createExecutionEvent`: `POST /v1/executions/{id}/events`
- `createGenerationFeedback`: `POST /v1/generation-feedback`
- `createExecutionFeedback`: `POST /v1/execution-feedback`
- `getHomeBootstrap`: `GET /v1/home/bootstrap`

异步响应固定为：

```ts
type AsyncAccepted<T> = {
  data: T
  operation: {
    id: string
    kind: 'assessment' | 'plan_set' | 'render' | 'execution_feedback'
    status: 'accepted'
  }
}
```

前端使用的核心生成类型别名固定为：

```ts
import type { components } from './generated/schema.ts'

export type Operation = components['schemas']['Operation']
export type DisplayMedia = components['schemas']['DisplayMedia']
export type Assessment = components['schemas']['Assessment']
export type Report = components['schemas']['Report']
export type ReportFinding = components['schemas']['ReportFinding']
export type PlanSet = components['schemas']['PlanSet']
export type PlanVariant = components['schemas']['PlanVariant']
export type PlanStep = components['schemas']['PlanStep']
export type Selection = components['schemas']['Selection']
export type Execution = components['schemas']['Execution']
export type ExecutionStep = components['schemas']['ExecutionStep']
export type HomeBootstrap = components['schemas']['HomeBootstrap']
```

`DisplayMedia` 必填字段固定为：

```ts
type DisplayMediaContract = {
  asset_id: string
  url: string
  url_expires_at: string
  mime_type: 'image/jpeg' | 'image/png'
  source_kind:
    | 'user_original'
    | 'generated_preview'
    | 'bundled_reference'
    | 'demo_example'
  display_label: '原本' | '风格参考' | '效果示例'
}
```

`Operation` 必填字段固定为 `id`、`kind`、`subject_type`、`subject_id`、`status`、`progress_bps`、`stage_code`、`public_message`、`retryable`、`created_at`、`updated_at`；终态可带 `error_code`、`trace_id`、`result_type`、`result_id`、`finished_at`，失败终态必须带 `trace_id`。状态只允许 `accepted | running | retrying | succeeded | failed | cancelled | superseded`。

`PlanSet.state` 只允许 `planning | rendering | ready | ready_partial | failed`。每个 `PlanVariant.render.state` 只允许 `queued | generating | checking | ready | failed | unavailable`，并显式携带 `retryable`、`operation_id`、可空 `media`、可空 `render_run_id`、可空 `publication_id`。

`Report` 必须包含 `photo_set_id` 和 `source_media: { face, side, body }`；每项固定为 `{ item_id, media: DisplayMedia }`。Finding 固定使用 `source_photo: { item_id, role }` 与 `anchor: { x, y, w, h }`。`PlanSet.report_id` 必须指向生成该方案集的不可变 Report。任何用于“原本”的图片都从 `getReport(planSet.report_id).source_media.body.media` 读取。

### Task 1: Validate the Final Contract and Generate the Core Client

**Files:**
- Modify: `contracts/openapi.yaml`
- Modify: `contracts/scripts/check-sync.mjs`
- Modify: `packages/core/package.json`
- Modify: `pnpm-lock.yaml`
- Create: `packages/core/scripts/check-openapi.mjs`
- Create: `packages/core/src/api/generated/schema.ts`
- Create: `packages/core/src/api/client.ts`
- Create: `packages/core/src/api/types.ts`
- Create: `packages/core/tests/generated-client.test.mjs`
- Modify: `packages/core/src/index.ts`

**Interfaces:**
- Consumes: 本计划“Contract Locked for This Plan”列出的 20 个 operationId 和字段。
- Produces: `createGeneratedApiClient(options)`, `GeneratedApiClient`, `Operation`, `DisplayMedia`, `Assessment`, `Report`, `PlanSet`, `Selection`, `Execution`, `HomeBootstrap`。

- [ ] **Step 1: Write the failing contract/type test**

```ts
// packages/core/src/api/generated/contract.type-test.ts
import type { paths, components } from './schema.ts'

type Expect<T extends true> = T
type HasAssessment = Expect<'/v1/assessments' extends keyof paths ? true : false>
type HasOperation = Expect<'/v1/operations/{id}' extends keyof paths ? true : false>
type HasPlanSet = Expect<'/v1/plan-sets/{id}' extends keyof paths ? true : false>
type HasExecution = Expect<'/v1/executions/{id}' extends keyof paths ? true : false>
type SourceKind = components['schemas']['DisplayMedia']['source_kind']
type AllowedSourceKinds =
  | 'user_original'
  | 'generated_preview'
  | 'bundled_reference'
  | 'demo_example'
type ExactSourceKinds = Expect<
  Exclude<SourceKind, AllowedSourceKinds> extends never
    ? Exclude<AllowedSourceKinds, SourceKind> extends never
      ? true
      : false
    : false
>

export type ContractAssertions = [
  HasAssessment,
  HasOperation,
  HasPlanSet,
  HasExecution,
  ExactSourceKinds,
]
```

```js
// packages/core/tests/generated-client.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { createGeneratedApiClient } from '../src/api/client.ts'

test('generated paths serialize path params and preserve the envelope', async () => {
  let request
  const client = createGeneratedApiClient({
    baseUrl: 'https://api.example',
    fetch: async (input) => {
      request = input
      return new Response(
        JSON.stringify({ data: { id: 'report-1', photo_set_id: 'photos-1' } }),
        {
          status: 200,
          headers: { 'content-type': 'application/json' },
        },
      )
    },
  })
  const result = await client.GET('/v1/reports/{id}', {
    params: { path: { id: 'report-1' } },
  })
  assert.equal(request.url, 'https://api.example/v1/reports/report-1')
  assert.equal(result.error, undefined)
  assert.equal(result.data.data.id, 'report-1')
})
```

- [ ] **Step 2: Run typecheck and verify the new contract is absent**

Run: `pnpm --filter @zsm/core typecheck`

Expected: FAIL with `Cannot find module './schema.ts'` or missing `/v1/operations/{id}` / `DisplayMedia`.

- [ ] **Step 3: Replace the quality contract and wire deterministic generation**

Use these package scripts and exact versions:

```json
{
  "scripts": {
    "api:generate": "openapi-typescript ../../contracts/openapi.yaml -o src/api/generated/schema.ts",
    "api:check": "node scripts/check-openapi.mjs",
    "typecheck": "tsc --noEmit",
    "test": "pnpm api:check && node --test --experimental-strip-types tests/*.mjs"
  },
  "dependencies": {
    "openapi-fetch": "0.17.0"
  },
  "devDependencies": {
    "openapi-typescript": "7.13.0",
    "typescript": "5.6.3"
  }
}
```

`client.ts` 只从生成 paths 建 client：

```ts
import createClient from 'openapi-fetch'
import type { paths } from './generated/schema.ts'

export type GeneratedApiClient = ReturnType<typeof createGeneratedApiClient>

export function createGeneratedApiClient(
  options: Parameters<typeof createClient<paths>>[0],
) {
  return createClient<paths>(options)
}
```

`check-openapi.mjs` 必须生成到临时目录、比较当前文件并始终清理：

```js
import { execFileSync } from 'node:child_process'
import { mkdtempSync, readFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const root = new URL('../../../', import.meta.url)
const temp = mkdtempSync(join(tmpdir(), 'zsm-openapi-'))
const output = join(temp, 'schema.ts')

try {
  execFileSync(
    'pnpm',
    ['exec', 'openapi-typescript', 'contracts/openapi.yaml', '-o', output],
    { cwd: root, stdio: 'inherit' },
  )
  const generated = readFileSync(output, 'utf8')
  const committed = readFileSync(
    new URL('../src/api/generated/schema.ts', import.meta.url),
    'utf8',
  )
  if (generated !== committed) {
    throw new Error('OpenAPI generated schema drifted; run pnpm --filter @zsm/core api:generate')
  }
} finally {
  rmSync(temp, { recursive: true, force: true })
}
```

在 `contracts/openapi.yaml` 中删除 `/v1/tasks*`、`/v1/analyses*`、旧 `/v1/reports/{id}/plans`、`/v1/plans*` 主链定义；加入锁定 operationId、`retryable` 错误字段、Idempotency-Key、If-Match/ETag 和上述状态枚举。更新 `check-sync.mjs`，从生成 schema 对应的 OpenAPI operationId 集合检查必需 operationId，不再读取 `API_PATHS`。

- [ ] **Step 4: Generate and verify**

Run:

```bash
pnpm install --lockfile-only
pnpm --filter @zsm/core api:generate
pnpm --filter @zsm/core api:check
pnpm --filter @zsm/core typecheck
```

Expected: `api:check` exit 0；typecheck PASS；生成文件含 `/v1/operations/{id}` 且不含 `/v1/tasks/{id}`。

- [ ] **Step 5: Commit the contract boundary**

```bash
git add contracts/openapi.yaml contracts/scripts/check-sync.mjs \
  packages/core/package.json packages/core/scripts/check-openapi.mjs \
  packages/core/src/api/generated/schema.ts packages/core/src/api/generated/contract.type-test.ts \
  packages/core/src/api/client.ts packages/core/src/api/types.ts packages/core/src/index.ts \
  packages/core/tests/generated-client.test.mjs pnpm-lock.yaml
git commit -m "feat(core): generate quality loop api client"
```

### Task 2: Build the Taro API Boundary

**Files:**
- Create: `apps/miniapp/src/app/api/taro-fetch.ts`
- Create: `apps/miniapp/src/app/api/client.ts`
- Create: `apps/miniapp/src/app/api/result.ts`
- Create: `apps/miniapp/src/app/api/quality.ts`
- Create: `apps/miniapp/src/app/api/media-upload.ts`
- Create: `apps/miniapp/tests/api-result.test.mjs`
- Modify: `apps/miniapp/package.json`
- Delete: `apps/miniapp/src/services/api.ts` — **推迟到 Task 12**（此刻仍有 18 个文件从旧客户端导入）

**Interfaces:**
- Consumes: `createGeneratedApiClient`, generated path/request/response types, existing runtime `baseURL`, token store and `localizeDevImages`.
- Produces: `qualityApi.createAssessment`, `qualityApi.getOperation`, `qualityApi.getCurrentReport`, `qualityApi.getReport`, `qualityApi.createPlanSet`, `qualityApi.getPlanSet`, `qualityApi.listPlanSets`, `qualityApi.createRenderRun`, `qualityApi.selectPlanSet`, `qualityApi.createExecution`, `qualityApi.getExecution`, `qualityApi.createExecutionEvent`, `qualityApi.createGenerationFeedback`, `qualityApi.createExecutionFeedback`, `qualityApi.getHomeBootstrap`, `uploadMedia`.

- [ ] **Step 1: Write the failing result/error tests**

```js
// apps/miniapp/tests/api-result.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { dataOrThrow, PublicApiError } from '../src/app/api/result.ts'

test('dataOrThrow returns typed data', () => {
  const data = dataOrThrow({
    data: { data: { id: 'report-1' } },
    response: { status: 200 },
  })
  assert.deepEqual(data, { id: 'report-1' })
})

test('dataOrThrow preserves public request id and retryability', () => {
  assert.throws(
    () =>
      dataOrThrow({
        error: {
          error: {
            code: 'render_failed',
            message: '这一套暂时没有生成成功',
            request_id: 'req-1',
            retryable: true,
          },
        },
        response: { status: 422 },
      }),
    (error) => {
      assert.ok(error instanceof PublicApiError)
      assert.equal(error.requestId, 'req-1')
      assert.equal(error.retryable, true)
      assert.equal(error.message, '这一套暂时没有生成成功')
      return true
    },
  )
})
```

- [ ] **Step 2: Run the focused test**

Run: `node --test --experimental-strip-types apps/miniapp/tests/api-result.test.mjs`

Expected: FAIL with `Cannot find module '../src/app/api/result.ts'`.

- [ ] **Step 3: Implement the generated-client façade and upload transaction**

`result.ts` 必须只有公开错误：

```ts
export class PublicApiError extends Error {
  constructor(
    readonly code: string,
    message: string,
    readonly statusCode: number,
    readonly requestId: string,
    readonly retryable: boolean,
  ) {
    super(message)
    this.name = 'PublicApiError'
  }
}

type ApiResult<T, E> = {
  data?: T
  error?: E
  response: { status: number }
}

type PublicErrorBody = {
  error: {
    code?: string
    message?: string
    request_id?: string
    retryable?: boolean
  }
}

export function bodyOrThrow<
  T,
>(result: ApiResult<T, PublicErrorBody>): T {
  if (result.data !== undefined) return result.data
  const error = result.error?.error
  throw new PublicApiError(
    error?.code ?? 'request_failed',
    error?.message ?? '请求没有成功，请重试',
    result.response.status,
    error?.request_id ?? '',
    error?.retryable === true,
  )
}

export function dataOrThrow<T>(
  result: ApiResult<{ data: T }, PublicErrorBody>,
): T {
  return bodyOrThrow(result).data
}
```

> **语法修正（2026-09-13）**：上面的构造器用了参数属性（`readonly code: string` 直接写在参数里），
> Node 22 的 `--experimental-strip-types` 只支持可擦除语法，参数属性会抛
> `ERR_INVALID_TYPESCRIPT_SYNTAX`。实现里改成显式字段声明 + 构造器赋值，
> `apps/miniapp/tests/*.test.mjs` 才能直接 import `.ts`。

`quality.ts` 的路径调用固定为：

```ts
export const qualityApi = {
  createAssessment: (body, idempotencyKey) =>
    client.POST('/v1/assessments', {
      body,
      params: { header: { 'Idempotency-Key': idempotencyKey } },
    }).then(bodyOrThrow),
  getOperation: (id) =>
    client.GET('/v1/operations/{id}', {
      params: { path: { id } },
    }).then(dataOrThrow),
  getCurrentReport: async () => {
    const result = await client.GET('/v1/reports/current')
    if (result.response.status === 404) return null
    return dataOrThrow(result)
  },
  getReport: (id) =>
    client.GET('/v1/reports/{id}', {
      params: { path: { id } },
    }).then(dataOrThrow),
  getPlanSet: (id) =>
    client.GET('/v1/plan-sets/{id}', {
      params: { path: { id } },
    }).then(dataOrThrow),
  getExecution: (id) =>
    client.GET('/v1/executions/{id}', {
      params: { path: { id } },
    }).then(dataOrThrow),
}
```

> **契约修正（2026-09-13）**：`Idempotency-Key` / `If-Match` 在 `contracts/openapi.yaml` 里是
> 声明为 required 的 header 参数，生成类型因此要求它们出现在 `params.header`，而不是请求选项顶层
> 的 `headers`——顶层 `headers` 是 openapi-fetch 的自由字典，不满足 schema 的必填约束。所有带幂等键
> 的方法（createAssessment / createPlanSet / createRenderRun / selectPlanSet / createExecution /
> createExecutionEvent / createGenerationFeedback / createExecutionFeedback /
> mediaUpload.createIntent / mediaUpload.completeIntent）一律写成
> `params: { path: {...}, header: { 'Idempotency-Key': ..., 'If-Match': ... } }`。
> `client.use(middleware)` 而不是 `createGeneratedApiClient({ middleware })`：0.17 的
> `ClientOptions` 不含 `middleware`。

其余锁定方法逐一使用生成 path；禁止字符串拼接 URL。`taro-fetch.ts` 返回具有 `status`、`ok`、`headers.get()`、`json()`、`text()`、`blob()` 的 Fetch-compatible response。401 重登在 `client.ts` 单飞并且只重放一次；`/v1/auth/*` 的 401 不触发重登。

`media-upload.ts` 固定事务为：

```ts
export type CreateIntentInput = {
  purpose: 'face' | 'side' | 'body' | 'feedback'
  mime_type: 'image/jpeg' | 'image/png'
  byte_size: number
  sha256: string
}

export interface MediaUploadPort {
  createIntent(input: CreateIntentInput): Promise<UploadIntent>
  putObject(intent: UploadIntent, filePath: string): Promise<void>
  completeIntent(intentId: string): Promise<UploadedMediaAsset>
}

export async function uploadMedia(
  port: MediaUploadPort,
  file: LocalImageFile,
  purpose: 'face' | 'side' | 'body' | 'feedback',
): Promise<UploadedMediaAsset> {
  const intent = await port.createIntent({
    purpose,
    mime_type: file.mimeType,
    byte_size: file.byteSize,
    sha256: file.sha256,
  })
  await port.putObject(intent, file.path)
  return port.completeIntent(intent.id)
}
```

> **契约修正（2026-09-13）**：以 `contracts/openapi.yaml` 为准，本文档原先的两处描述作废。
> ① `CreateUploadIntentRequest` 只有 `purpose / mime_type / byte_size / sha256`，没有 `file_name`：
> 文件名不进后端，`LocalImageFile.name` 只用于本地排查。
> ② `POST /v1/media/upload-intents/{id}/complete` 返回的是 `MediaAsset`
> （`id / purpose / mime_type / byte_size / sha256 / state: 'ready' / created_at`），构造不出
> `DisplayMedia`（后者需要 `asset_id + url + source_kind + display_label`）。要展示图片，先拿
> `MediaAsset.id` 去报告/方案集的媒体接口换 `DisplayMedia`。
> ③ 命名：旧的 `packages/core/src/types/index.ts` 里仍有一个形状完全不同的 `MediaAsset`
> （`kind / url / demo`），它经由 `export *` 出口，会被显式导出的同名类型盖掉，从而弄坏
> `src/pages/capture/index.tsx`。因此在 `Task 12` 删掉旧类型之前，核心桶导出为
> `MediaAsset as UploadedMediaAsset`，调用方使用 `UploadedMediaAsset`；Task 12 完成后改回原名。

上传 intent 创建失败、COS 直传失败、complete 失败都不得创建本地假 Asset。

- [ ] **Step 4: Verify API tests and types**

Run:

```bash
node --test --experimental-strip-types apps/miniapp/tests/api-result.test.mjs
pnpm --filter @zsm/miniapp typecheck
pnpm --filter @zsm/miniapp exec tsc --noEmit 2>&1 | grep '^src/app/api'   # 期望无输出
```

Expected: tests PASS；typecheck 在 Task 2 新增/修改的文件上零错误。

> **验收口径修正（2026-09-13）**：`pnpm --filter @zsm/miniapp typecheck` 目前不可能整体 PASS，也
> **不是** Task 2 能修好的：`src/pages/{home,report,profile}/index.tsx` 共 44 个错误是上一个阶段
> 的既有存量——核心桶早就用 `api/types.ts` 的显式导出盖掉了 `types/index.ts` 里的同名
> `Report` / `HomeBootstrap`，而这三个页面还在按旧字段（`active_tasks`、`current_image_url`、
> `recent_plan`、`provider_version`、`Finding.detail`…）读取。它们是 Task 11（服务端驱动的首页/我的）
> 与 Task 12（删除兼容路径）的清理对象。
> 同理 `rg "/v1/tasks|createApiEndpoints" apps/miniapp/src/app packages/core/src` 在 Task 12 删除
> `packages/core/src/api/endpoints.ts` 之前必然命中。Task 2 只保证：
> `apps/miniapp/src/app` 下零匹配，且 Task 2 的文件不引入新错误。

- [ ] **Step 5: Commit the app API boundary**

```bash
git add apps/miniapp/package.json apps/miniapp/src/app/api apps/miniapp/tests/api-result.test.mjs \
  packages/core/src/index.ts packages/core/src/api/types.ts
git commit -m "feat(miniapp): add generated api boundary"
```

> **顺序修正（2026-09-13）**：原步骤里的 `git add apps/miniapp/src/services/api.ts` 是删除动作，推迟到
> Task 12。此刻还有 18 个文件（含 Task 12 归属的 8 个外围页面）从旧客户端导入，Task 2 到 Task 11
> 之间每一步都要能过 typecheck，因此 Task 2 保持纯新增。

### Task 3: Add Resource-Level Cache and Remove Business Storage

**Files:**
- Create: `apps/miniapp/src/app/cache/resource-cache.ts`
- Create: `apps/miniapp/src/app/cache/use-resource.ts`
- Create: `apps/miniapp/tests/resource-cache.test.mjs`
- Modify: `apps/miniapp/src/services/storage.ts`
- Modify: `apps/miniapp/src/app.ts`

**Interfaces:**
- Consumes: immutable resources carrying `id`; `DisplayMedia.asset_id + url`.
- Produces: `resourceKey`, `mediaKey`, `resourceCache.read/write/remove/subscribe/revalidate`, `useResource`.

- [ ] **Step 1: Write failing cache partition and invalidation tests**

```js
// apps/miniapp/tests/resource-cache.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import {
  createResourceCache,
  mediaKey,
  resourceKey,
} from '../src/app/cache/resource-cache.ts'

test('resources are partitioned by type and resource id', () => {
  const cache = createResourceCache()
  cache.write(resourceKey('report', 'r1'), { id: 'r1' })
  cache.write(resourceKey('plan-set', 'p1'), { id: 'p1' })
  assert.deepEqual(cache.read(resourceKey('report', 'r1')), { id: 'r1' })
  assert.equal(cache.read(resourceKey('report', 'p1')), undefined)
})

test('media cache includes asset id and signed url', () => {
  assert.notEqual(
    mediaKey({ asset_id: 'a1', url: 'https://cdn/a1.jpg?sig=one' }),
    mediaKey({ asset_id: 'a1', url: 'https://cdn/a1.jpg?sig=two' }),
  )
  assert.notEqual(
    mediaKey({ asset_id: 'a1', url: 'https://cdn/a.jpg' }),
    mediaKey({ asset_id: 'a2', url: 'https://cdn/a.jpg' }),
  )
})

test('invalid server refresh replaces cached resource instead of pinning old media', async () => {
  const cache = createResourceCache()
  const key = resourceKey('plan-set', 'p1')
  cache.write(key, { id: 'p1', plans: [{ id: 'v1', render: { media: { asset_id: 'old' } } }] })
  await cache.revalidate(key, async () => ({ id: 'p1', plans: [{ id: 'v1', render: { media: null } }] }))
  assert.equal(cache.read(key).plans[0].render.media, null)
})
```

- [ ] **Step 2: Run the focused test**

Run: `node --test --experimental-strip-types apps/miniapp/tests/resource-cache.test.mjs`

Expected: FAIL with missing `resource-cache.ts`.

- [ ] **Step 3: Implement cache semantics and shrink Storage**

```ts
export type ResourceKind =
  | 'report'
  | 'plan-set'
  | 'operation'
  | 'selection'
  | 'execution'
  | 'home'

export function resourceKey(kind: ResourceKind, id: string): string {
  return `${kind}:${id}`
}

export function mediaKey(media: Pick<DisplayMedia, 'asset_id' | 'url'>): string {
  return `media:${media.asset_id}:${media.url}`
}

export function createResourceCache() {
  const values = new Map<string, unknown>()
  const listeners = new Map<string, Set<() => void>>()
  const inflight = new Map<string, Promise<unknown>>()

  const publish = (key: string) => {
    listeners.get(key)?.forEach((listener) => listener())
  }

  return {
    read<T>(key: string): T | undefined {
      return values.get(key) as T | undefined
    },
    write<T>(key: string, value: T): T {
      values.set(key, value)
      publish(key)
      return value
    },
    remove(key: string): void {
      values.delete(key)
      publish(key)
    },
    subscribe(key: string, listener: () => void): () => void {
      const set = listeners.get(key) ?? new Set()
      set.add(listener)
      listeners.set(key, set)
      return () => set.delete(listener)
    },
    async revalidate<T>(key: string, load: () => Promise<T>): Promise<T> {
      const existing = inflight.get(key) as Promise<T> | undefined
      if (existing) return existing
      const request = load()
        .then((value) => {
          values.set(key, value)
          publish(key)
          return value
        })
        .finally(() => inflight.delete(key))
      inflight.set(key, request)
      return request
    },
  }
}
```

`storage.ts` 的 key 必须精确缩减为：

```ts
export const STORAGE_KEYS = {
  token: 'zsm_token',
  uiSchemaVersion: 'zsm_ui_schema_version',
  compareHint: 'zsm_compare_hint',
  city: 'zsm_city',
  openCreditSheet: 'zsm_open_credit_sheet',
} as const
```

Task 3 实际做的是：**新增** `uiSchemaVersion` 与 `compareHint`，并加 `syncUiSchemaVersion()`
（版本不匹配只清 UI 偏好三个 key，不迁移任何业务值），`app.ts` 在 `useLaunch` 里调用它。

> **顺序修正（2026-09-13）**：删除 reportId、planId、savedPlanId、activeTask*、outfit/purchase
> session、last diagnosis、sceneBrief、scenePending、advisorConversationId 这些 key **推迟到
> Task 12**。此刻有 22 个文件在读写 `STORAGE_KEYS`，其中 8 个外围页面归 Task 12、`home/report/
> profile` 归 Task 11；在这里删 key，Task 3 到 Task 10 每一步的 typecheck 都会红一片，与本步骤
> 自己要求的 "typecheck PASS" 直接冲突。Task 3 保持加法。

- [ ] **Step 4: Verify cache and forbidden key scans**

Run:

```bash
node --test --experimental-strip-types apps/miniapp/tests/resource-cache.test.mjs
pnpm --filter @zsm/miniapp typecheck
rg "reportId|planId|activeTask|sceneBrief|scenePending|advisorConversationId" apps/miniapp/src/services/storage.ts
```

Expected: tests PASS（含并发合并与订阅两条补充用例）；typecheck 在 Task 3 触碰的文件上零错误；
最后一个 `rg` 在 Task 12 删 key 之前仍会命中（见上面的顺序修正），Task 3 只需保证没有任何**新增**
业务 key。

- [ ] **Step 5: Commit cache/storage boundary**

```bash
git add apps/miniapp/src/app/cache apps/miniapp/src/services/storage.ts \
  apps/miniapp/src/app.ts apps/miniapp/tests/resource-cache.test.mjs
git commit -m "refactor(miniapp): make server state authoritative"
```

> **测试脚本修正（2026-09-13）**：`apps/miniapp/package.json` 的 `test` 改成
> `node --test --experimental-strip-types tests/*.test.mjs`。Node 22.12 把裸目录参数当模块路径解析
> （`ERR_MODULE_NOT_FOUND: Cannot find module .../tests`），Task 2 里写下的 `tests/` 实际跑不起来。

### Task 4: Create the Only Operation Polling Path

**Files:**
- Create: `packages/core/src/operations/polling.ts`
- Replace: `packages/core/tests/polling.test.mjs`
- Create: `apps/miniapp/src/app/operations/use-operation-polling.ts`
- Create: `apps/miniapp/src/app/operations/operation-view.ts`
- Create: `apps/miniapp/tests/operation-view.test.mjs`
- Modify: `packages/core/src/index.ts`
- Modify: `apps/miniapp/src/app/api/quality.ts`
- Create: `packages/core/tests/display-progress.test.mjs`
- Create: `packages/core/tests/events.test.mjs`
- Create: `packages/core/tests/legacy-task-polling.test.mjs`
- Delete: `packages/core/src/hooks/useTaskPolling.ts`
- Delete: `apps/miniapp/src/hooks/use-stable-polling.ts`

> **顺序修正（2026-09-13）**：两处 Delete 推迟到 Task 12。`useTaskPolling` 仍被
> `packages/core/src/index.ts` 与 `apps/miniapp/src/pages/plans/index.tsx` 引用，
> `use-stable-polling` 仍被 `apps/miniapp/src/pages/analysis/index.tsx` 引用；这些页面要到
> Task 5–12 才重写。Task 4 只做加法：新边界可用，旧路径暂时并存。
> 相应地，`packages/core/src/hooks/useTaskPolling.ts` 只做一处无行为变化的改动——
> 它自己的 `SubscribeVisibility` 声明改为从 `operations/polling.ts` 引入再 re-export，
> 让这个类型只有一个定义（该文件本就随 Task 12 一起消失，不留清理债）。

> **文件清单修正（2026-09-13）**：原清单缺两处必需品。
> 一、`apps/miniapp/src/app/api/quality.ts` 要补 `listOperations(ids)`——
> 契约里 `GET /v1/operations` 的 `ids` 是**逗号分隔字符串**而非数组，
> 由 API 边界做 join，页面不拼 URL。
> 二、`packages/core/tests/polling.test.mjs` 原本是个杂烩文件，里面同时放着
> display-progress、events 与旧任务控制器三组无关测试。直接 Replace 会顺手删掉这些覆盖，
> 所以先把它们原样搬进 `display-progress.test.mjs`、`events.test.mjs` 和
> `legacy-task-polling.test.mjs`，再重写 polling 测试。
> 其中 `legacy-task-polling.test.mjs` 保的是**仍在服役**的代码：`useTaskPolling` /
> `POLL_INTERVALS` 因为删除推迟而还活着，还被 `pages/plans` 引用，删测试就得先删代码——
> 两件事一起在 Task 12 做，该文件届时与 `useTaskPolling.ts` 一并删除。

**Interfaces:**
- Consumes: `Operation`, `qualityApi.getOperation`, Taro page visibility subscription.
- Produces: `createOperationPolling`, `useOperationPolling`, `operationView`, `MAX_OPERATION_FETCH_FAILURES = 5`, `OPERATION_POLL_INTERVAL_MS = 1000`。

- [ ] **Step 1: Replace Task tests with Operation lifecycle tests**

```js
// packages/core/tests/polling.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import {
  createOperationPolling,
  MAX_OPERATION_FETCH_FAILURES,
} from '../src/operations/polling.ts'

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms))

test('operation polling stops on every public terminal status', async () => {
  for (const terminal of ['succeeded', 'failed', 'cancelled', 'superseded']) {
    const seen = []
    const handle = createOperationPolling({
      fetcher: async () => {
        const status = seen.length === 0 ? 'running' : terminal
        seen.push(status)
        return [{ id: terminal, status }]
      },
      intervalMs: 1,
    })
    await sleep(20)
    handle.stop()
    assert.deepEqual(seen, ['running', terminal])
  }
})

test('page hide pauses, show refreshes immediately, five failures stop', async () => {
  let visibility
  let calls = 0
  let failed = 0
  const handle = createOperationPolling({
    fetcher: async () => {
      calls += 1
      throw new Error('offline')
    },
    intervalMs: 1,
    subscribeVisibility: (listener) => {
      visibility = listener
      return () => {
        visibility = undefined
      }
    },
    onFailed: () => {
      failed += 1
    },
  })
  visibility(false)
  const pausedAt = calls
  await sleep(10)
  assert.equal(calls, pausedAt)
  visibility(true)
  await sleep(30)
  assert.equal(calls, MAX_OPERATION_FETCH_FAILURES)
  assert.equal(failed, 1)
  handle.stop()
  assert.equal(visibility, undefined)
})
```

- [ ] **Step 2: Run and verify old Task controller fails the contract**

Run: `node --test --experimental-strip-types packages/core/tests/polling.test.mjs`

Expected: FAIL with missing `createOperationPolling`.

- [ ] **Step 3: Implement controller, view projection and React wrapper**

Controller terminal predicate:

```ts
const TERMINAL = new Set([
  'succeeded',
  'failed',
  'cancelled',
  'superseded',
])

export const MAX_OPERATION_FETCH_FAILURES = 5
export const OPERATION_POLL_INTERVAL_MS = 1000

export function isOperationSettled(status: Operation['status']): boolean {
  return TERMINAL.has(status)
}

export interface OperationPollingOptions {
  fetcher: () => Promise<readonly Operation[]>
  intervalMs: number
  enabled?: boolean
  subscribeVisibility?: SubscribeVisibility
  onSettled?: (operations: readonly Operation[]) => void
  onFailed?: (errors: readonly unknown[]) => void
}
```

`createOperationPolling` 只有在返回数组非空且每个 Operation 都是终态时停止；一项成功、一项仍 running 时继续。`useOperationPolling` 固定签名：

```ts
export interface UseOperationPollingOptions {
  operationIds: readonly string[]
  enabled?: boolean
  onSettled?: (operations: readonly Operation[]) => void
  onFetchFailure?: (errors: readonly unknown[]) => void
}

export function useOperationPolling(
  options: UseOperationPollingOptions,
): {
  operations: readonly Operation[]
  refresh: () => Promise<void>
  stop: () => void
}
```

Hook 用 `useRef` 保持唯一 handle；一个 ID 调 `getOperation`，多个 ID 调 `listOperations`，响应逐个回写 `resourceKey('operation', operation.id)`；`useDidHide` 只通过 `subscribeVisibility(false)` 暂停，不在页面再调用 `stop()`；`useDidShow` 恢复后立即拉取；卸载才 `stop()`。排序后的 operation ID 集合改变时先停旧 handle，再创建新 handle。Assessment、Planning 和 Render 传单元素数组，首页传 `active_operations.map(operation => operation.id)`；禁止在 `.map()` 中调用 Hook。

`operationView()` 固定投影：

```ts
export type OperationView =
  | { kind: 'idle' }
  | { kind: 'working'; progress: number; message: string; retrying: boolean }
  | { kind: 'succeeded'; resultType: string; resultId: string }
  | { kind: 'failed'; message: string; retryable: boolean; requestId: string }

export function operationView(operation: Operation | null): OperationView {
  if (!operation) return { kind: 'idle' }
  if (
    operation.status === 'accepted' ||
    operation.status === 'running' ||
    operation.status === 'retrying'
  ) {
    return {
      kind: 'working',
      progress: Math.min(100, Math.max(0, operation.progress_bps / 100)),
      message: operation.public_message,
      retrying: operation.status === 'retrying',
    }
  }
  if (operation.status === 'succeeded') {
    return {
      kind: 'succeeded',
      resultType: operation.result_type ?? '',
      resultId: operation.result_id ?? '',
    }
  }
  return {
    kind: 'failed',
    message: operation.public_message,
    retryable: operation.retryable,
    requestId: operation.trace_id ?? '',
  }
}
```

> **字段名修正（2026-09-13）**：上面这段投影的联合类型声明的是 `requestId`，函数体却返回 `traceId`，
> 两处对不上。以联合类型为准：返回 `requestId`，取值来自 `operation.trace_id`。
> 依据是 `contracts/openapi.yaml` 对 `trace_id` 的说明——它与错误体的 `request_id` 是同一个值
> （即 `X-Request-ID`）。对用户和页面统一叫 `requestId`，不让契约里的内部字段名漏进 UI。

> **签名修正（2026-09-13）**：`OperationPollingOptions.intervalMs` 实际实现为可选
> （缺省取 `OPERATION_POLL_INTERVAL_MS`），并额外提供 `onUpdate`——每一轮成功响应都会回调，
> 用于把"还在跑"的进度也画出来；只有 `onSettled` 的话页面在终态前拿不到任何更新。
> `useOperationPolling` 的返回类型为 `operations: Operation[]`（非 `readonly`）：调用方要能直接 `.map()` 渲染。

- [ ] **Step 4: Verify behavior and uniqueness**

Run:

```bash
node --test --experimental-strip-types packages/core/tests/polling.test.mjs
node --test --experimental-strip-types apps/miniapp/tests/operation-view.test.mjs
rg "useTaskPolling|createTaskPolling|useStablePolling|setInterval|setTimeout" apps/miniapp/src/features apps/miniapp/src/pages
```

Expected: both tests PASS；最后一个 `rg` 无轮询匹配，允许与 UI 动画有关且在代码注释中标明用途的 `setTimeout`。

> **验收口径修正（2026-09-13）**：那条 `rg` 现在还不可能干净。`apps/miniapp/src/pages` 下仍有
> `pages/plans/index.tsx`（`useTaskPolling` ×2）、`pages/analysis/index.tsx`（`useStablePolling`）、
> `pages/home/index.tsx`（裸 `setInterval`/`setTimeout`）——它们是 Task 5–12 的重写对象，
> 旧路径要到 Task 12 才算物理清掉。Task 4 能守住的是**新边界自身**不出现第二套轮询：
>
> ```bash
> rg "useTaskPolling|createTaskPolling|useStablePolling|setInterval|/v1/tasks|createApiEndpoints" apps/miniapp/src/app
> ```
>
> Expected: 无匹配（已核实）。另需 `pnpm --filter @zsm/core typecheck` PASS；
> `pnpm --filter @zsm/miniapp typecheck` 的错误数不得高于 Task 3 结束时的 44 条
> （全部落在 `pages/{home,profile,report}` 三个待重写页面，见 Task 2 的验收口径修正）。

- [ ] **Step 5: Commit polling boundary**

```bash
git add packages/core/src/operations/polling.ts packages/core/tests/polling.test.mjs \
  packages/core/tests/display-progress.test.mjs packages/core/tests/events.test.mjs \
  packages/core/tests/legacy-task-polling.test.mjs \
  packages/core/src/index.ts packages/core/src/hooks/useTaskPolling.ts \
  apps/miniapp/src/app/operations apps/miniapp/src/app/api/quality.ts \
  apps/miniapp/tests/operation-view.test.mjs
git commit -m "refactor(miniapp): poll public operations only"
```

> **暂存清单修正（2026-09-13）**：原清单里的 `apps/miniapp/src/hooks/use-stable-polling.ts`
> 是删除动作，随上面的顺序修正一起推迟到 Task 12，这里不再 `git add`。
> 新增 `apps/miniapp/src/app/api/quality.ts`（`listOperations`）、两个拆出来的测试文件，
> 以及 `packages/core/src/hooks/useTaskPolling.ts` 的唯一一处改动（类型单源）。

### Task 5: Enforce Strong Image Source and No Pinned Old Image

**Files:**
- Create: `packages/core/src/media/display.ts`
- Create: `packages/core/tests/display.test.mjs`
- Modify: `packages/core/tests/truth.test.mjs`
- Create: `apps/miniapp/src/components/source-image/index.tsx`
- Create: `apps/miniapp/src/components/source-image/index.scss`
- Create: `apps/miniapp/src/components/source-image/index.config.ts`
- Modify: `packages/core/src/copy/zh.ts`
- Modify: `packages/core/src/index.ts`
- Modify: `packages/core/src/media/truth.ts`
- Delete: `apps/miniapp/src/components/example-image/**`（推迟到 Task 12，见下）

**Interfaces:**
- Consumes: generated `DisplayMedia`.
- Produces: `projectDisplayMedia`, `DisplayImage`, `SourceImage`。

> **测试文件修正（2026-09-13）**：新用例写进**新建**的 `packages/core/tests/display.test.mjs`，
> 不再 `Replace: packages/core/tests/truth.test.mjs`。原步骤会把 `truth.test.mjs` 里另外
> 7 条覆盖**保留函数**的用例（`lookImage` / `userImage` / `exampleImage` / `isDisplayableImage`）
> 一起清掉，而 Task 5 只删 `shippedAsset` 一个函数。`truth.test.mjs` 因此只删掉 `shippedAsset`
> 那一条，其余 7 条原样留下（已核实 7/7 PASS）；新投影的 4 条单独成文件。

> **夹具日期修正（2026-09-13）**：原夹具写死 `url_expires_at: '2026-09-12T09:00:00Z'`，
> 到执行当天（2026-09-13）已经过期，`projectDisplayMedia` 对**每一条**用例都返回 null，
> 整组用例会静默失效（第 1、2 条直接抛 `TypeError: Cannot read properties of null`）。
> 改成远未来常量 `'2099-01-01T00:00:00Z'`；过期路径由第 3 条显式传 `now` + 过去时间戳覆盖，
> 不再依赖"写夹具的那一天"。

> **字段名修正（2026-09-13）**：`display_label !== badge → return null` 这条校验删掉。
> 它与本任务**自己的**用例矛盾：第 2 条的 `media({ source_kind: 'demo_example' })` 用的是默认
> `display_label: '风格参考'`，而派生 badge 是 `'效果示例'`，该分支会返回 null 并让断言抛错。
> 更根本的是它把角标绑在**服务端下发的字符串**上——而红线要求角标只认类型化的 `source_kind`，
> 文案单源在 `copy/zh.ts`。角标一律由 `source_kind` 派生，不读 `display_label`。

- [ ] **Step 1: Write failing source-truth tests**

```js
// packages/core/tests/display.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { projectDisplayMedia } from '../src/media/display.ts'

const media = (patch = {}) => ({
  asset_id: 'asset-1',
  url: 'https://cdn.example/asset-1.jpg?sig=one',
  url_expires_at: '2026-09-12T09:00:00Z',
  mime_type: 'image/jpeg',
  source_kind: 'generated_preview',
  display_label: '风格参考',
  ...patch,
})

test('source kind, not provider or url, controls the badge', () => {
  assert.deepEqual(projectDisplayMedia(media()), {
    key: 'asset-1:https://cdn.example/asset-1.jpg?sig=one',
    src: 'https://cdn.example/asset-1.jpg?sig=one',
    badge: '风格参考',
    soften: false,
    sourceKind: 'generated_preview',
  })
  assert.equal(
    projectDisplayMedia(
      media({ source_kind: 'demo_example', display_label: '效果示例' }),
    ).badge,
    '效果示例',
  )
})

test('bundled and demo are softened; generated preview is not', () => {
  assert.equal(projectDisplayMedia(media({ source_kind: 'bundled_reference' })).soften, true)
  assert.equal(projectDisplayMedia(media({ source_kind: 'demo_example' })).soften, true)
  assert.equal(projectDisplayMedia(media({ source_kind: 'generated_preview' })).soften, false)
})

test('invalid, expired and webp media return null without fallback', () => {
  assert.equal(projectDisplayMedia(media({ url: '' })), null)
  assert.equal(projectDisplayMedia(media({ mime_type: 'image/webp' })), null)
  assert.equal(projectDisplayMedia(media({ url: 'https://cdn.example/a.webp' })), null)
  assert.equal(
    projectDisplayMedia(media({ url_expires_at: '2020-01-01T00:00:00Z' }), Date.parse('2026-09-12T08:00:00Z')),
    null,
  )
})

test('generated, bundled and demo assets must be jpeg', () => {
  assert.equal(projectDisplayMedia(media({ mime_type: 'image/png' })), null)
  assert.ok(
    projectDisplayMedia(
      media({
        source_kind: 'user_original',
        display_label: '原本',
        mime_type: 'image/png',
      }),
    ),
  )
})
```

- [ ] **Step 2: Run source tests**

Run: `node --test --experimental-strip-types packages/core/tests/display.test.mjs`

Expected: FAIL with missing `media/display.ts`.

- [ ] **Step 3: Implement pure projection and pure rendering**

```ts
export interface DisplayImage {
  key: string
  src: string
  badge: '原本' | '风格参考' | '效果示例'
  soften: boolean
  sourceKind: DisplayMedia['source_kind']
}

export function projectDisplayMedia(
  media: DisplayMedia | null | undefined,
  now = Date.now(),
): DisplayImage | null {
  if (
    !media?.asset_id ||
    !(
      media.url.startsWith('https://') ||
      media.url.startsWith('wxfile://') ||
      media.url.startsWith('file://')
    )
  ) return null
  if (/\.webp(?:\?|$)/i.test(media.url)) return null
  if (Date.parse(media.url_expires_at) <= now) return null
  const generated =
    media.source_kind === 'generated_preview' ||
    media.source_kind === 'bundled_reference' ||
    media.source_kind === 'demo_example'
  if (generated && media.mime_type !== 'image/jpeg') return null
  if (
    media.source_kind === 'user_original' &&
    media.mime_type !== 'image/jpeg' &&
    media.mime_type !== 'image/png'
  ) return null
  const badge =
    media.source_kind === 'demo_example'
      ? '效果示例'
      : media.source_kind === 'user_original'
        ? '原本'
        : '风格参考'
  return {
    key: `${media.asset_id}:${media.url}`,
    src: media.url,
    badge,
    soften:
      media.source_kind === 'demo_example' ||
      media.source_kind === 'bundled_reference',
    sourceKind: media.source_kind,
  }
}
```

`SourceImage` 每次 render 只投影当前 `media` props，不用 `useRef` 保存旧 URL。对产品静态参考位，使用判别参数 `{ reference: { slug, variant } }` 调用 `exampleImage`，并强制“风格参考”角标及 `.example-badge + .example-soft`；该参数不能与 `media` 同时出现。投影为空时渲染可见空态和调用者传入的动作；`demo_example`、`bundled_reference` 同时挂 `.example-badge` 与 `.example-soft`；生成图 badge 是“风格参考”，Demo 是“效果示例”。删除 `provider_version.startsWith('demo')`、`isBundledAsset(url)`、`generated_image_url || image_url`。`media/truth.ts` 保留严格 `lookImage`、`userImage` 和唯一内置入口 `exampleImage`，但删除把旧 `.png/.webp` 自动改写为 `.jpg` 的 `shippedAsset` 兼容行为。

> **顺序修正（2026-09-13）**：`example-image/**` 三处 Delete 推迟到 Task 12。
> 当前有 12 个文件 import `ExampleImage`（`pages/{profile,home,report,plan,capture,plans}` 与
> `packages/{tools/lab,tools/hair,tools/purchase,tools/outfit,life/share,life/today}`），
> 它们分别是 Task 6/7/11/12 的重写对象；Task 5 删掉组件会让这些页面当场编译不过。
> 同理 `isBundledAsset` 这一版**保留**（改成不依赖 `shippedAsset` 的纯路径正则），
> 否则它的 4 个存活 import 者会一起断。它的去留随 Task 12 与调用方一同处理。

> **文件清单修正（2026-09-13）**：`Modify: apps/miniapp/src/services/local-looks.ts` 实际**无需改动**。
> 该文件已经是 `exampleImage` 的小程序 JPEG resolver，且已在 `apps/miniapp/src/app.ts` 里通过
> `setLocalLooksResolver(localLooksResolver)` 接好，符合 File Map 对它的约束
> （"继续作为 exampleImage 的小程序 JPEG resolver，不承载业务结果 fallback"）。此处不改动，
> 只登记为已核对。（它在 `services/api.ts` 里的另一处接线随 Task 12 一起删。）

- [ ] **Step 4: Verify truth and no-pin scan**

Run:

```bash
node --test --experimental-strip-types packages/core/tests/display.test.mjs \
  packages/core/tests/truth.test.mjs
pnpm --filter @zsm/core typecheck && pnpm --filter @zsm/miniapp typecheck
rg "pinnedUrl|PinnedImage|provider_version.*demo|isBundledAsset|generated_image_url \\|\\| image_url" apps/miniapp/src packages/core/src
```

Expected: tests PASS；typecheck PASS；最后一个 `rg` 无匹配。

> **验收口径修正（2026-09-13）**：最后那条 `rg` 现在还不可能干净，Task 5 也**不该**让它干净。
> 执行时（2026-09-13）实际命中：
>
> - `packages/core/src/media/truth.ts:7,95`、`packages/core/src/index.ts:60` —— 保留的 `isBundledAsset` 定义与 barrel 导出（见上面的顺序修正）；
> - `apps/miniapp/src/components/example-image/index.tsx`（多处）—— Task 12 才删；
> - `apps/miniapp/src/pages/report/index.tsx`（5 处）、`pages/home/index.tsx`（2 处）、`pages/profile/index.tsx`（3 处）—— Task 7 / Task 11 的重写对象；
> - `apps/miniapp/src/packages/tools/pages/{hair,purchase,outfit}/index.tsx` —— **这三个包根本不在本计划的 File Map 里**，
>   计划全程不会碰它们，`provider_version.startsWith('demo')` 会一直留到计划结束（见文末 Execution Notes 的遗留项）。
>
> Task 5 能守住的是**新边界自身**不出现旧判据：
>
> ```bash
> rg "provider_version|isBundledAsset|generated_image_url|display_label" apps/miniapp/src/components/source-image
> ```
>
> Expected: 无匹配（已核实；`source-image` 只认 `projectDisplayMedia` 与 `source_kind`）。
> 另需 `pnpm --filter @zsm/core test` 与 `pnpm --filter @zsm/miniapp test` 全绿，
> 且 `pnpm --filter @zsm/miniapp typecheck` 错误数**不超过 44 条**且仍全部落在
> `pages/{home,profile,report}/index.tsx`（Task 5 删除 `shippedAsset` 未新增任何错误，已核实）。

- [ ] **Step 5: Commit media truth boundary**

```bash
git add packages/core/src/media packages/core/tests/display.test.mjs \
  packages/core/tests/truth.test.mjs packages/core/src/copy/zh.ts \
  packages/core/src/index.ts apps/miniapp/src/components/source-image
git commit -m "refactor(media): enforce typed image provenance"
```

> **暂存清单修正（2026-09-13）**：原清单两处要改——去掉
> `apps/miniapp/src/components/example-image`（删除推迟到 Task 12，见顺序修正）与
> `apps/miniapp/src/services/local-looks.ts`（无需改动，见文件清单修正）；
> 补上新建的 `packages/core/tests/display.test.mjs`（`packages/core/tests/truth.test.mjs`
> 因删掉 `shippedAsset` 一条用例仍需暂存）。

### Task 6: Rebuild Three-Photo Capture

**Files:**
- Create: `apps/miniapp/src/features/capture/model.ts`
- Create: `apps/miniapp/src/features/capture/CaptureScreen.tsx`
- Create: `apps/miniapp/src/features/capture/index.scss`
- Create: `apps/miniapp/tests/capture-model.test.mjs`
- Modify: `apps/miniapp/src/pages/capture/index.tsx`
- Delete: `apps/miniapp/src/pages/capture/index.scss`

**Interfaces:**
- Consumes: `uploadMedia`, `qualityApi.createDemoMedia`, `qualityApi.createAssessment`, optional profile snapshot.
- Produces: `CaptureScreen`, `captureReady`, `toAssessmentInput`; navigation query `assessment_id` + `operation_id`。

> **文件清单修正（2026-09-13）**：原清单缺一个必需品，且顶部 Interfaces 有一条要作废。
>
> - 补 `Create: apps/miniapp/src/features/capture/local-file.ts` —— 微信本地临时文件 →
>   `LocalImageFile` 的适配层（`getFileSystemManager().getFileInfo({ digestAlgorithm: 'sha256' })`
>   + MIME 判定）。服务端 `createIntent` 只收 64 位小写 hex 且 `completeUploadIntent` 会拿存储侧
>   元数据对账，所以 sha256 必须在客户端真算；这段是平台 API 调用，放不进纯逻辑的 `model.ts`，
>   也放不进 JSX 的 `CaptureScreen.tsx`。
> - Interfaces 里的「optional profile snapshot」**不消费**：`CreateAssessmentRequest` 是
>   `additionalProperties: false` 且只有 `photos`，服务端自己快照档案
>   （`service/assessment/service.go` 的 `s.profiles.Snapshot(ctx, cmd.UserID)`）。
>   `CaptureScreen` 因此是**无 props** 组件。
> - 实际还额外消费 `mediaUpload`（端口常量，在 `apps/miniapp/src/app/api/client.ts:141`，
>   **不在** `app/api/media-upload.ts`——后者只导出类型与 `uploadMedia` 函数）。

- [ ] **Step 1: Write failing capture model tests**

```js
// apps/miniapp/tests/capture-model.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import {
  captureReady,
  toAssessmentInput,
} from '../src/features/capture/model.ts'

const display = (id, kind = 'user_original') => ({
  asset_id: id,
  url: `https://cdn.example/${id}.jpg`,
  url_expires_at: '2026-09-13T00:00:00Z',
  mime_type: 'image/jpeg',
  source_kind: kind,
  display_label: kind === 'demo_example' ? '效果示例' : '原本',
})

test('requires exactly one distinct face, side and body asset', () => {
  assert.equal(
    captureReady({ face: display('f'), side: display('s'), body: display('b') }),
    true,
  )
  assert.equal(captureReady({ face: display('f'), side: display('s') }), false)
  assert.equal(
    captureReady({ face: display('same'), side: display('same'), body: display('b') }),
    false,
  )
})

test('assessment input preserves explicit photo roles and lets the server snapshot profile', () => {
  assert.deepEqual(
    toAssessmentInput({ face: display('f'), side: display('s'), body: display('b') }),
    {
      photos: {
        face_asset_id: 'f',
        side_asset_id: 's',
        body_asset_id: 'b',
      },
    },
  )
})
```

- [ ] **Step 2: Run capture test**

Run: `node --test --experimental-strip-types apps/miniapp/tests/capture-model.test.mjs`

Expected: FAIL with missing `features/capture/model.ts`.

> **夹具日期修正（2026-09-13）**：上面夹具写死 `url_expires_at: '2026-09-13T00:00:00Z'`，
> 而 Step 2 当天就是 2026-09-13 —— 这不是"今天"，是零点，当天下午跑就已经过期。
> 实际实现把夹具日期改成远未来常量 `'2099-01-01T00:00:00Z'`。
> 理由与 Task 5 的同类修正一致：capture 模型现在不读 `url_expires_at`，但哪天有人把夹具
> 喂进 `projectDisplayMedia`，写死的近期日期会在某天悄悄变红。
> 同时在原 2 条之外补了 4 条用例：缺槽位时**拒绝伪造 id**、`ready` 槽必须带 media 而
> 非 `ready` 槽不得声称带、重拍失败必须让提交重新变红、幂等键只由三张图的 asset id 决定
> 而与时间无关。共 6 条，全部落在新建的 `apps/miniapp/tests/capture-model.test.mjs`。

- [ ] **Step 3: Implement capture states and submit flow**

Capture slots use stable role key；每槽状态只允许 `empty | selecting | hashing | uploading | ready | failed`。选择 JPEG/PNG 后计算 SHA-256，按 Task 2 的 Upload Intent 事务上传；失败槽保留错误文案和“重试 / 换一张”。Demo 按三个 role 分别请求 `createDemoMedia`，返回必须是 `source_kind: demo_example`，否则拒绝落槽。

提交固定流程：

```ts
const accepted = await qualityApi.createAssessment(
  toAssessmentInput(slots, profileSnapshot),
  createIdempotencyKey('assessment'),
)
resourceCache.write(resourceKey('operation', accepted.operation.id), accepted.operation)
await Taro.redirectTo({
  url:
    `/pages/analysis/index?assessment_id=${encodeURIComponent(accepted.data.id)}` +
    `&operation_id=${encodeURIComponent(accepted.operation.id)}`,
})
```

不写 Storage。`CaptureScreen` 的 placeholder 是拍摄姿势指导 PNG，不是结果 fallback；Demo 照片必须显示“效果示例”。所有样式迁到 Feature SCSS 并将裸 px 转为 rpx。

> **提交入参修正（2026-09-13）**：上面的 `toAssessmentInput(slots, profileSnapshot)` 是两参，
> 实际实现为**单参** `toAssessmentInput(photos)`。两处依据：
> `CreateAssessmentRequest` 是 `additionalProperties: false` 且只有 `photos` 字段，
> 多传档案字段会被契约拒；服务端在受理时自己快照档案。
> 与 Step 1 的用例标题「lets the server snapshot profile」一致——**本地不传档案**才是那条用例的原意。
> 入参用的是 `photosByRole(slots)` 的结果（只取 `phase === 'ready' && media` 的槽），不是 `slots`。

> **幂等键修正（2026-09-13）**：`createIdempotencyKey('assessment')` **不存在**——
> 那是旧 `services/task-utils.ts` 里的东西，Task 12 要删。实际实现是 `model.ts` 里一个
> 纯函数、**不含时间**：`` `assessment:${face}:${side}:${body}` ``（三个 asset id）。
> 同样的三张图 ⇒ 同样的键，所以重试不会在服务端建出第二份档案；
> 反过来，任一张换图必然换键，也就必然是一次新建档。

> **状态机与本地预览修正（2026-09-13）**：原文只说"每槽状态只允许
> `empty | selecting | hashing | uploading | ready | failed`"，执行时补了三条这会踩到的事实：
>
> - **槽位图只能来自本地临时文件**。上传成功后服务端只回 asset id —— `MediaAsset` 没有 URL，
>   契约里也没有媒体读取接口。所以 `slotMedia()` 渲染本地路径，并**显式声明**
>   `source_kind: 'user_original'`（角标「原本」，不弱化）。这是**声明**，不是从路径/后缀/来源
>   推断出来的，符合红线。
>   已知边界：微信开发者工具的 `chooseMedia` 返回 `http://tmp/...`，`projectDisplayMedia`
>   只认 `https` / `wxfile` / `file`，于是开发者工具里槽位落到空态（真机是 `wxfile://`，正常）。
>   **不放宽协议白名单**去迁就它——那条白名单是挡"服务端塞个 http 图进来"的。
> - **取消选择要能回退**：`chooseMedia` 取消时，老照片还在就回 `ready`，没有才回 `empty`；
>   否则点一下取消就把已拍的照片弄丢了。
> - **批量补拍不能读 state**：`ingest()` 返回 `Promise<boolean>`，`batchFill` / `useDemoPhotos`
>   用本地 outcomes 判断，避免 `await` 之后读到上一轮的 `slots`。
>
> 另外原文的「placeholder 是拍摄姿势指导 PNG」按**普通 `<Image>`** 实现，**不经**
> `SourceImage` —— 后者是内容图的投影器，给姿势示意图套「风格参考」角标会让人以为那是别人拍的效果。

> **未做的三件事（2026-09-13）**：原文没写、但旧实现里有，本次**有意不做**：
>
> - **不收 `?scene`**：契约里 assessment 没有场景字段，Storage 又禁止写业务态。
>   拍摄页的 `index.tsx` 现在完全不读 query。**场景交接归 Task 8**（`pages/scene/index.tsx`
>   在 Task 8 的文件清单里），不是漏做。
> - **不放补充资料表单**：身高/身份/预算的编辑入口归 `pages/profile`，不在拍摄页再造一份。
> - **不做"检测到已有分析就跳走"的门禁**：那依赖本地 Storage 里的任务 id，而恢复能力归分析页自己。
>
> 另：`POST /v1/assessments` 的响应只有 **202** / 400 / 401 / 409 / 429 / 500，**没有 402**，
> 所以建阶段正确地把 `handleBillingError` 去掉了（也就顺带断开了对 Task 12 删除目标
> `services/api.ts` 的依赖）。

> **文案与门禁修正（2026-09-13）**：两处计划外的必要改动。
>
> - `packages/core/src/copy/zh.ts` 新增 `CAPTURE_COPY` 与 `captureSelectedText` /
>   `captureFillMissingText`（文案只放 `zh.ts` 是硬约束），barrel `packages/core/src/index.ts` 同步。
> - `apps/miniapp/scripts/check.mjs` 的门禁 #2 **收紧**（不是放宽）：页面自带 `index.scss`
>   或整页迁进 `features/<x>` 时，要求**那个 feature 目录里真的有 `index.scss`**。
>   原门禁只看"有没有 import"，会放过 feature 里根本没有样式的页面——那等于页面裸奔；
>   而本任务的 File Map 正要删掉已迁移页面的 `index.scss`，所以这条必须补上。

- [ ] **Step 4: Verify capture**

Run:

```bash
node --test --experimental-strip-types apps/miniapp/tests/capture-model.test.mjs
pnpm --filter @zsm/miniapp typecheck
node apps/miniapp/scripts/check.mjs
```

Expected: tests PASS；typecheck PASS；静态门禁无 WebP、裸 px、下标 key 或业务 Storage 写入。

> **验收口径修正（2026-09-13）**：`pnpm --filter @zsm/miniapp typecheck` 仍**不可能 PASS**，
> 与 Task 2 / Task 5 的同一条修正一致：基线是 **44 条**，全部落在
> `pages/{home(28),report(11),profile(5)}/index.tsx`（Task 11 的重写对象）。
> Task 6 的验收口径是「**错误数不超过 44 条且仍全部落在那三个文件**」。
> 执行时（2026-09-13）实测：**44 条，零新增**——`src/features/capture/**` 干净。
> 另：`node apps/miniapp/scripts/check.mjs` 输出「dist 不存在：跳过 3/4 的 dist 侧检查」，
> 这是**预期**，dist 侧门禁要等 `build:weapp` 之后（Task 15）。

- [ ] **Step 5: Commit capture feature**

```bash
git add apps/miniapp/src/features/capture apps/miniapp/src/pages/capture \
  apps/miniapp/tests/capture-model.test.mjs
git commit -m "feat(miniapp): rebuild three-photo capture"
```

> **暂存清单修正（2026-09-13）**：原清单只覆盖了新建的 feature 目录、页面目录与测试，
> 但本任务实际还改了三个文件、删了一个文件，都要一起进这个提交：
>
> - 补 `packages/core/src/copy/zh.ts` 与 `packages/core/src/index.ts`（新增 `CAPTURE_COPY`，见文案与门禁修正）；
> - 补 `apps/miniapp/src/app/api/quality.ts`（新增 `createDemoMedia` + `DemoMediaRole`，
>   并校验返回必须是 `source_kind: 'demo_example'`）；
> - 补 `apps/miniapp/scripts/check.mjs`（门禁 #2 收紧，见文案与门禁修正）；
> - `apps/miniapp/src/pages/capture/index.scss` 的删除要**显式暂存**——
>   原清单的 `git add apps/miniapp/src/pages/capture` 在删除已 `git rm` 之后不会把删除记进索引。
>
> 建议先 `git status` 核对，确认没有把计划执行前就存在的未跟踪文件带进来。

### Task 7: Rebuild Assessment and Evidence-Bound Report

**Files:**
- Create: `apps/miniapp/src/features/assessment/model.ts`
- Create: `apps/miniapp/src/features/assessment/AssessmentScreen.tsx`
- Create: `apps/miniapp/src/features/assessment/index.scss`
- Create: `apps/miniapp/src/features/report/model.ts`
- Create: `apps/miniapp/src/features/report/ReportScreen.tsx`
- Create: `apps/miniapp/src/features/report/index.scss`
- Create: `apps/miniapp/tests/report-model.test.mjs`
- Modify: `apps/miniapp/src/pages/analysis/index.tsx`
- Modify: `apps/miniapp/src/pages/report/index.tsx`
- Delete: `apps/miniapp/src/pages/analysis/index.scss`
- Delete: `apps/miniapp/src/pages/report/index.scss`

**Interfaces:**
- Consumes: route `operation_id`, `assessment_id`, optional report route `id`; `useOperationPolling`; `qualityApi.getCurrentReport/getReport`。
- Produces: `AssessmentScreen`, `ReportScreen`, `reportPhotoForFinding`, `reportRouteAfterOperation`。

- [ ] **Step 1: Write failing report evidence and current-pointer tests**

```js
// apps/miniapp/tests/report-model.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import {
  reportPhotoForFinding,
  reportRouteAfterOperation,
} from '../src/features/report/model.ts'

const report = {
  id: 'r1',
  photo_set_id: 'photos-1',
  source_media: {
    face: { item_id: 'item-face', media: { asset_id: 'face-1' } },
    side: { item_id: 'item-side', media: { asset_id: 'side-1' } },
    body: { item_id: 'item-body', media: { asset_id: 'body-1' } },
  },
}

test('finding always resolves its explicit source role', () => {
  assert.equal(
    reportPhotoForFinding(report, { source_photo: { item_id: 'item-side', role: 'side' } }).asset_id,
    'side-1',
  )
})

test('missing evidence does not fall back to body or bundled media', () => {
  assert.equal(
    reportPhotoForFinding(
      { ...report, source_media: { ...report.source_media, face: null } },
      { source_photo: { item_id: 'item-face', role: 'face' } },
    ),
    null,
  )
})

test('successful assessment uses operation result id', () => {
  assert.equal(
    reportRouteAfterOperation({
      status: 'succeeded',
      result_type: 'report',
      result_id: 'r9',
    }),
    '/pages/report/index?id=r9',
  )
})
```

- [ ] **Step 2: Run report model test**

Run: `node --test --experimental-strip-types apps/miniapp/tests/report-model.test.mjs`

Expected: FAIL with missing `features/report/model.ts`.

- [ ] **Step 3: Implement explicit assessment/report states**

`AssessmentScreen` 使用 `operationView`：

- `accepted/running/retrying`: 显示服务端进度与文案；照片来自 assessment 返回的强类型 source media。
- `succeeded`: 只按 `operation.result_type === 'report'` 和 `result_id` 跳报告。
- `failed`: 内容不渲染，显示公开文案、request ID；可重试时“重新发起”，照片质量拒绝时“重新拍摄”。
- `cancelled/superseded`: 显示“这次分析已结束”，动作“返回首页”。
- 连续五次拉取错误: 显示网络失败和“重试”，不伪造业务失败。

`ReportScreen` 加载规则：

```ts
const report = routeReportId
  ? await qualityApi.getReport(routeReportId)
  : await qualityApi.getCurrentReport()
```

没有 current report 显示“还没有形象报告 / 重新拍摄”。Findings 使用 `finding.id` 作 key，展示 `visible_observation` 与 `recommendation`，不显示 confidence 和评分。锚点使用 `anchor.{x,y,w,h}`；source media 缺失时该照片区域显示空态，不用 body 图或内置图代替。

“查看方案”调用 `createPlanSet`，body 含当前 `report.id`、scene=`general`、完整默认 GeneralBrief `{focus:'balanced',preparation:'closet',impression:'natural'}`；返回后按 `plan_set_id + operation_id` 导航方案页，不写 Storage。

- [ ] **Step 4: Verify assessment/report**

Run:

```bash
node --test --experimental-strip-types apps/miniapp/tests/report-model.test.mjs
pnpm --filter @zsm/miniapp typecheck
rg "severity|confidence|provider_version|STORAGE_KEYS\\.report" apps/miniapp/src/features/assessment apps/miniapp/src/features/report
```

Expected: tests PASS；typecheck PASS；最后一个 `rg` 无匹配。

- [ ] **Step 5: Commit assessment/report**

```bash
git add apps/miniapp/src/features/assessment apps/miniapp/src/features/report \
  apps/miniapp/src/pages/analysis apps/miniapp/src/pages/report \
  apps/miniapp/tests/report-model.test.mjs
git commit -m "feat(miniapp): show trusted evidence report"
```

> **执行修正（Task 7 实施时记录，按索引的冲突规则改本计划）：**
>
> 1. **评估照片不在契约里。** 计划原文"照片来自 assessment 返回的强类型 source media"与冻结契约矛盾：`Assessment` 只回 `{id, photo_set_id, state, created_at}`，没有 `GET /v1/assessments/{id}`、没有照片集接口、没有按 id 读媒体接口。落地改为：提交时由 `features/assessment/start.ts#submitAssessment` 把客户端手里那三张 `PhotosByRole` 写进内存 `resourceCache`（键 `assessment-photos:<assessment_id>`），进度页按路由参数取；冷缓存渲染空槽，绝不找别图顶上。只进内存不进 Storage。
> 2. **operationView 扩展。** working 分支补 `stageCode`（页面按服务端 `stage_code` 查文案/步骤表）；新增 `{kind:'ended', reason:'cancelled'|'superseded'}`——契约把这两个状态放在 NonFailedOperation 一侧，没有错误与 trace_id，不出现"重试"承诺。
> 3. **进度只认服务端。** 删除 `createDisplayProgress` 补间（按 ~9%/s 爬升的数字服务端没说过）、按时间推进的 14 档 `ANALYSIS_STAGE_TIMELINE` + `analysisTimelineText`（客户端编的文案，已从 core 与桶文件删除）、6 分钟本地 stuck 守卫（客户端不伪造业务失败，超时归服务端判断）。
> 4. **失败动作只按 `retryable` 分流，不看 `error_code`**；"重新发起"必须换新幂等键（`assessmentRetryKey`，带尝试序号）——首提的键已被服务端和那次失败绑定，重放只会拿回同一份失败。
> 5. **useOperationPolling 增加 `restartKey`**：连续 5 次拉取失败后控制器自行 stop（此后 `refresh()` 是空操作），页面重试需要显式重装。
> 6. **"查看方案"用内存交接条而非路由参数**（`app/plan-set-handoff.ts`，取走即清）：方案页在 tabBar 里，`switchTab` 不接受 query，`reportPlanSetRoute` 改为 `reportPlanSetHandoff`。
> 7. **提交清单比计划 Step 5 的 `git add` 宽**：另含 `packages/core/src/copy/zh.ts`、`packages/core/src/index.ts`、`apps/miniapp/src/app/operations/*`、`apps/miniapp/src/app/cache/resource-cache.ts`、`apps/miniapp/src/features/capture/*`、`apps/miniapp/src/app/plan-set-handoff.ts`、`apps/miniapp/tests/operation-view.test.mjs`、`apps/miniapp/tests/assessment-model.test.mjs`。

### Task 8: Rebuild Planning with Partial Readiness

**Files:**
- Create: `apps/miniapp/src/features/planning/model.ts`
- Create: `apps/miniapp/src/features/planning/PlansScreen.tsx`
- Create: `apps/miniapp/src/features/planning/PlanDetailScreen.tsx`
- Create: `apps/miniapp/src/features/planning/SceneBriefScreen.tsx`
- Create: `apps/miniapp/src/features/planning/index.scss`
- Create: `apps/miniapp/src/components/render-state/index.tsx`
- Create: `apps/miniapp/src/components/render-state/index.scss`
- Create: `apps/miniapp/src/components/render-state/index.config.ts`
- Create: `apps/miniapp/tests/planning-model.test.mjs`
- Modify: `apps/miniapp/src/pages/scene/index.tsx`
- Modify: `apps/miniapp/src/pages/plans/index.tsx`
- Modify: `apps/miniapp/src/pages/plan/index.tsx`
- Delete: `apps/miniapp/src/pages/scene/index.scss`
- Delete: `apps/miniapp/src/pages/plans/index.scss`
- Delete: `apps/miniapp/src/pages/plan/index.scss`

**Interfaces:**
- Consumes: `PlanSet`, bound `Report`, `useOperationPolling`, resource cache, source media.
- Produces: `planSetView`, `variantRenderView`, `boundBodyMedia`, `PlansScreen`, `PlanDetailScreen`, `SceneBriefScreen`。

- [ ] **Step 1: Write failing state and binding tests**

```js
// apps/miniapp/tests/planning-model.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import {
  boundBodyMedia,
  planSetView,
  variantRenderView,
} from '../src/features/planning/model.ts'

test('all plan-set states remain explicit', () => {
  assert.equal(planSetView({ state: 'planning', plans: [] }).kind, 'planning')
  assert.equal(planSetView({ state: 'rendering', plans: [{ id: 'v1' }] }).kind, 'rendering')
  assert.equal(planSetView({ state: 'ready', plans: [{ id: 'v1' }] }).kind, 'ready')
  assert.equal(planSetView({ state: 'ready_partial', plans: [{ id: 'v1' }] }).kind, 'ready_partial')
  assert.equal(planSetView({ state: 'failed', plans: [] }).kind, 'failed')
})

test('single render failure does not hide text or ready siblings', () => {
  assert.deepEqual(
    variantRenderView({
      id: 'v2',
      content: { name: '暖意' },
      render: { state: 'failed', retryable: true, media: null },
    }),
    {
      kind: 'failed',
      textAvailable: true,
      retryable: true,
      media: null,
    },
  )
})

test('comparison body is from the plan-set bound report only', () => {
  const planSet = { id: 'ps1', report_id: 'r-old' }
  const bound = {
    id: 'r-old',
    source_media: { body: { item_id: 'item-old', media: { asset_id: 'body-old' } } },
  }
  assert.equal(boundBodyMedia(planSet, bound).asset_id, 'body-old')
  assert.throws(
    () => boundBodyMedia(planSet, { id: 'r-current', source_media: { body: { item_id: 'item-new', media: { asset_id: 'body-new' } } } }),
    /report binding mismatch/,
  )
})
```

- [ ] **Step 2: Run planning model test**

Run: `node --test --experimental-strip-types apps/miniapp/tests/planning-model.test.mjs`

Expected: FAIL with missing `features/planning/model.ts`.

- [ ] **Step 3: Implement progressive PlanSet UI**

`PlansScreen` 接受 `planSetId?: string` 和 `operationId?: string`。无 ID 的 Tab 入口先调用 `getCurrentReport()`，再 `listPlanSets(report_id, scene)`；没有 Report 显示建档动作。场景 Brief 只在 React state 和 POST body 中存在，不写 Storage；修改答案创建新 PlanSet。

PlanSet 状态行为固定：

- `planning`: 整屏 Operation 状态，无虚构方案卡。
- `rendering`: 三套文字方案立即可读；每套 RenderState 独立显示 queued/generating/checking/ready/failed/unavailable。
- `ready`: 三套文字和三套 published media 可见。
- `ready_partial`: 所有文字可见；成功图可见；失败图区域显示失败/重试；不可用显示“当前没有可用的同能力生成服务”，不显示内置图。
- `failed`: 若尚无文字，整屏失败；若服务端错误响应仍包含已发布文字则按 `ready_partial` 展示，不能由客户端拼接旧缓存。

后台刷新使用 `resourceCache.revalidate(resourceKey('plan-set', id), getPlanSet)`；刷新期间继续显示同一 PlanSet cache；响应到达后整体替换。每套 `key={variant.id}`。

单套重试固定调用：

```ts
const accepted = await qualityApi.createRenderRun(
  variant.id,
  createIdempotencyKey(`render:${variant.id}`),
)
resourceCache.write(resourceKey('operation', accepted.operation.id), accepted.operation)
await refreshPlanSet()
```

`PlanDetailScreen` 的对比左图严格调用 `boundBodyMedia(planSet, boundReport)`；右图只使用 `variant.render.media`。render 不是 ready 时展示文字步骤与 RenderState，不显示旧图。`PlanStep.details` 按 `category` switch 收窄，禁止 `as { hair_spec?: ... }`。

- [ ] **Step 4: Verify planning**

Run:

```bash
node --test --experimental-strip-types apps/miniapp/tests/planning-model.test.mjs
pnpm --filter @zsm/miniapp typecheck
node apps/miniapp/scripts/check.mjs
```

Expected: tests PASS；typecheck PASS；门禁 PASS；fixture 覆盖 planning、rendering、ready、ready_partial、failed 和六个 render 子状态。

- [ ] **Step 5: Commit planning feature**

```bash
git add apps/miniapp/src/features/planning apps/miniapp/src/components/render-state \
  apps/miniapp/src/pages/scene apps/miniapp/src/pages/plans apps/miniapp/src/pages/plan \
  apps/miniapp/tests/planning-model.test.mjs
git commit -m "feat(miniapp): render plan sets progressively"
```

> **执行修正（Task 8 实施时记录）：**
>
> 1. **夹具与契约的字段名**：计划测试片段用 `plans:`，契约 `PlanSet` 是 `variants:`——测试与实现一律按契约。
> 2. **`ready` 缺 `media` 的服务端违约**按"可重试失败"呈现（不渲染空图、不假装正常），`variantRenderView` 有测试钉住；`failed` 但服务端仍带已发布文字时提升为 `ready_partial`，这个判定放 `planSetView` 纯函数里。
> 3. **`createIdempotencyKey` 是本任务新建的助手**（planning/model.ts，时间戳+序号，每次调用不同）：重试一把渲染必须换新幂等键，旧键重放只会拿回同一份失败（同 Task 7 结论）。
> 4. **Tab 入口的"分析中"判定**计划未给来源：实现用 `getHomeBootstrap().active_operations`（公开契约）找进行中的 assessment operation，空态「查看分析进度」直接带 `operation_id` 跳分析页。
> 5. **方案 tab 不再放对比滑杆**（旧页行为）：对比归 `PlanDetailScreen`，左图严格 `boundBodyMedia`（绑定不符抛错，测试覆盖）。
> 6. **渲染刷新盯 operation**：受理 operation（planning 态，只能来自交接条/受理响应）+ 各套 `render.operation_id`；全部终态 → `resourceCache.revalidate` 整体替换，不做字段级拼接。
> 7. **Brief 文案表进 `SCENE_BRIEF_COPY`**，选项 value 即契约枚举（snake_case：`three_days`/`key_piece`/`bridal_party`/`dress_code`/`city_walk`/`air_conditioned`，替换旧页 kebab-case）；`sceneBriefRequest` 按表校验后才做唯一一处类型断言，缺答/表外值返回 null 不发请求。
> 8. **`PLAN_DETAIL_COPY.cta`（生成清单）暂不渲染**——选择/执行动作与清单页接线归 Task 9。
> 9. **提交清单比计划宽**：另含 `packages/core/src/copy/zh.ts`、`packages/core/src/index.ts`（PLANNING_COPY/SCENE_BRIEF_COPY/planSlotLabel/PLAN_DETAIL_COPY 扩展）。

### Task 9: Separate Selection and Execution

**Files:**
- Create: `apps/miniapp/src/features/execution/model.ts`
- Create: `apps/miniapp/src/features/execution/ExecutionScreen.tsx`
- Create: `apps/miniapp/src/features/execution/index.scss`
- Create: `apps/miniapp/tests/execution-model.test.mjs`
- Modify: `apps/miniapp/src/pages/checklist/index.tsx`
- Delete: `apps/miniapp/src/pages/checklist/index.scss`

**Interfaces:**
- Consumes: `putPlanSetSelection`, `createSelectionExecution`, `getExecution`, `createExecutionEvent`, ETag/version.
- Produces: `selectAndCreateExecution`, `applyExecutionEvent`, `canSubmitExecutionFeedback`, `ExecutionScreen`。

- [ ] **Step 1: Write failing execution snapshot/idempotency tests**

```js
// apps/miniapp/tests/execution-model.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import {
  canSubmitExecutionFeedback,
  executionEventBody,
  replaceExecutionFromServer,
} from '../src/features/execution/model.ts'

test('execution event keeps the same client id across retry', () => {
  const first = executionEventBody('step-1', true, 'event-1')
  const retry = executionEventBody('step-1', true, 'event-1')
  assert.deepEqual(first, retry)
  assert.equal(first.client_event_id, 'event-1')
})

test('server execution replaces optimistic state', () => {
  const optimistic = { id: 'e1', version: 1, steps: [{ id: 's1', completed: true }] }
  const server = { id: 'e1', version: 2, steps: [{ id: 's1', completed: false }] }
  assert.deepEqual(replaceExecutionFromServer(optimistic, server), server)
})

test('execution feedback opens only after completion', () => {
  assert.equal(canSubmitExecutionFeedback({ state: 'active' }), false)
  assert.equal(canSubmitExecutionFeedback({ state: 'completed' }), true)
})
```

- [ ] **Step 2: Run execution model test**

Run: `node --test --experimental-strip-types apps/miniapp/tests/execution-model.test.mjs`

Expected: FAIL with missing `features/execution/model.ts`.

- [ ] **Step 3: Implement selection and execution resources**

Plan CTA:

```ts
const selection = await qualityApi.selectPlanSet(planSet.id, {
  plan_variant_id: variant.id,
  render_publication_id: variant.render.publication_id ?? null,
})
const execution = await qualityApi.createExecution(
  selection.id,
  createIdempotencyKey(`execution:${selection.id}`),
)
resourceCache.write(resourceKey('selection', selection.id), selection)
resourceCache.write(resourceKey('execution', execution.id), execution)
await Taro.navigateTo({
  url: `/pages/checklist/index?execution_id=${encodeURIComponent(execution.id)}`,
})
```

`ExecutionScreen` 只读 execution snapshot steps，不再请求 Plan checklist。点击步骤时生成一次 `client_event_id`，乐观更新当前 execution cache；请求携带 `If-Match: "<version>"`。成功用服务端完整 Execution 替换；409/412 先 `getExecution` 覆盖，再提示用户重试；网络失败回滚并保留同一个 client_event_id 供显式重试。进度条件使用 `items.length > 0`，不得用 `{done && ...}`。

- [ ] **Step 4: Verify execution**

Run:

```bash
node --test --experimental-strip-types apps/miniapp/tests/execution-model.test.mjs
pnpm --filter @zsm/miniapp typecheck
rg "getChecklist|updateChecklistItem|STORAGE_KEYS\\.plan" apps/miniapp/src/features/execution apps/miniapp/src/pages/checklist
```

Expected: tests PASS；typecheck PASS；最后一个 `rg` 无匹配。

- [ ] **Step 5: Commit execution**

```bash
git add apps/miniapp/src/features/execution apps/miniapp/src/pages/checklist \
  apps/miniapp/tests/execution-model.test.mjs
git commit -m "feat(miniapp): execute immutable plan snapshots"
```

> **执行修正（Task 9 实施时记录）：**
>
> 1. **`executionEventBody` 的 `occurred_at` 是显式参数**：计划片段只传 3 个参数还想两次调用 deepEqual，真实时钟下不可能成立；契约要求事件带 occurred_at（±24h 内），测试显式传同一时刻。
> 2. **`isEventConflict` 判定错误实例**（`PublicApiError.statusCode` 为 409/412），不是裸状态码——网络失败没有状态码，天然不是冲突。
> 3. **version/state 服务端独有**：`toggleStepLocal` 不推 version、不改 state（服务端状态机按事件推进）；乐观状态被服务端响应整体替换，无字段级合并。
> 4. **无需单独发 `started` 事件**：服务端状态机 `planned + step_completed → active`（apps/server/internal/domain/execution.go ValidateTransition），首次勾选自动激活。
> 5. **"完成执行"是显式事件**：服务端只在收到 `completed` 事件且全部步骤已完成时才推进 completed，所以清单页在全部勾完后给「完成执行」按钮，completed 后反馈入口才打开（`canSubmitExecutionFeedback`）。
> 6. **幂等键生成上移 `app/keys.ts`**（createIdempotencyKey + createClientEventId 唯一来源，planning/model 保留再导出）；事件的 Idempotency-Key 用 `event:${clientEventId}`——同一次逻辑事件的网络重试共用，这正是幂等层想要的。
> 7. **提交清单比计划宽**：另含 `apps/miniapp/src/app/keys.ts`（新）、`apps/miniapp/src/features/planning/PlanDetailScreen.tsx`（"选这套"CTA 接线）、`packages/core/src/copy/zh.ts`、`packages/core/src/index.ts`（CHECKLIST_COPY 扩展、PLANS_COPY 恢复导出）。
> 8. **清单页 CTA 导航带 `execution_id`**：执行反馈页归 Task 10 接线；本提交内 feedback 页仍是旧实现，Task 10 立即重建。

### Task 10: Add Generation and Execution Feedback

**Files:**
- Create: `apps/miniapp/src/features/feedback/model.ts`
- Create: `apps/miniapp/src/features/feedback/GenerationFeedback.tsx`
- Create: `apps/miniapp/src/features/feedback/ExecutionFeedbackScreen.tsx`
- Create: `apps/miniapp/src/features/feedback/index.scss`
- Create: `apps/miniapp/tests/feedback-model.test.mjs`
- Modify: `apps/miniapp/src/features/planning/PlanDetailScreen.tsx`
- Modify: `apps/miniapp/src/pages/feedback/index.tsx`
- Delete: `apps/miniapp/src/pages/feedback/index.scss`

**Interfaces:**
- Consumes: exact viewed `publication_id`、`execution_id` and optional feedback media. RenderRun/Candidate/Selection/PlanSet chain is derived by the server.
- Produces: `generationFeedbackBody`, `executionFeedbackBody`, `GenerationFeedback`, `ExecutionFeedbackScreen`。

- [ ] **Step 1: Write failing feedback association tests**

```js
// apps/miniapp/tests/feedback-model.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import {
  executionFeedbackBody,
  generationFeedbackBody,
} from '../src/features/feedback/model.ts'

test('generation feedback binds the publication the user saw', () => {
  assert.deepEqual(
    generationFeedbackBody(
      { publication_id: 'publication-1' },
      ['不像本人', '发型不符'],
      '',
      null,
    ),
    {
      publication_id: 'publication-1',
      tags: ['不像本人', '发型不符'],
      comment: '',
      media_asset_id: null,
    },
  )
})

test('execution feedback remains valid without a photo', () => {
  assert.deepEqual(
    executionFeedbackBody(
      { execution_id: 'e1', selection_id: 's1', plan_set_id: 'ps1' },
      ['容易执行', '希望保留'],
      '发型很省时间',
      null,
    ).media_asset_id,
    null,
  )
})
```

- [ ] **Step 2: Run feedback model test**

Run: `node --test --experimental-strip-types apps/miniapp/tests/feedback-model.test.mjs`

Expected: FAIL with missing `features/feedback/model.ts`.

- [ ] **Step 3: Implement both feedback flows**

Generation tags 固定为“不像本人 / 发型不符 / 妆容不符 / 穿搭不符 / 肢体异常 / 不够自然”；入口在 ready publication 的方案详情。若 render 未发布，不显示质量反馈入口。

Execution tags 固定为“容易执行 / 太正式 / 太复杂 / 颜色不喜欢 / 希望保留”；只有 Execution completed 时可提交。实拍可选。若用户选择了实拍但上传失败：

1. 保留已选标签和文字；
2. 显示“照片没有上传成功，可以重试上传，或先提交文字和标签”；
3. “先提交文字和标签”用 `media_asset_id: null` 请求；
4. 不因图片失败阻止反馈保存。

成功文案使用服务端 acknowledgement 中明确保存的 preference summary，不写“AI 已经学会”。下一次 Planning 由服务端读取 feedback memory，客户端不拼 Prompt。

- [ ] **Step 4: Verify feedback**

Run:

```bash
node --test --experimental-strip-types apps/miniapp/tests/feedback-model.test.mjs
pnpm --filter @zsm/miniapp typecheck
rg "先上传今天的实拍|provider|model|已经学会" apps/miniapp/src/features/feedback
```

Expected: tests PASS；typecheck PASS；最后一个 `rg` 无匹配。

- [ ] **Step 5: Commit feedback**

```bash
git add apps/miniapp/src/features/feedback apps/miniapp/src/features/planning/PlanDetailScreen.tsx \
  apps/miniapp/src/pages/feedback apps/miniapp/tests/feedback-model.test.mjs
git commit -m "feat(miniapp): close both feedback loops"
```

> **执行修正（Task 10 实施时记录）：**
>
> 1. **计划测试片段期望 `media_asset_id: null` 与中文标签直传，均与冻结契约冲突**：契约里 `media_asset_id` 是 uuid 字符串（不可空、additionalProperties: false），tags 是枚举数组。落地：无照片时**整段省略** `media_asset_id`（服务端注释原文"上传失败就省略 media_asset_id"）；中文只活在界面，`generationFeedbackBody/executionFeedbackBody` 收发契约枚举，`comment` 为空同样省略。测试按契约重写。
> 2. **rg 门禁按字面自相矛盾**：`rg "…|model|…"` 会命中计划自己要求的 `./model` import 路径。按意图核验：除 import 行外零命中（`先上传今天的实拍`、`已经学会`、独立词 provider/model 均未出现在任何文案或逻辑里）。
> 3. **生成反馈入口做成 BottomSheet**（挂在方案详情 CTA 区，`publication_id` 为 null 时入口整体不渲染）；执行反馈是 feedback 页本体，route 只带 `execution_id`，bound 的 selection/plan_set 由服务端从执行追责链反推（`ExecutionFeedback` 响应自带 selection_id/plan_set_id）。
> 4. **`StructuredPreference` 未接 UI**：计划正文未要求偏好 chips，标签已承载服务端记忆语义（YAGNI）；成功文案只用 `FEEDBACK_ACK_COPY` 按服务端 `acknowledgement_code` 映射，不写"已经学会"。
> 5. **提交清单比计划宽**：另含 `packages/core/src/copy/zh.ts`、`packages/core/src/index.ts`（GENERATION/EXECUTION_FEEDBACK_TAGS、FEEDBACK_SCREEN_COPY）、`apps/miniapp/src/features/planning/index.scss`（反馈入口样式）。

### Task 11: Make Home/Profile Server-Driven and Pages Thin

**Files:**
- Create: `apps/miniapp/src/features/report/CurrentReportEntry.tsx`
- Create: `apps/miniapp/src/features/planning/RecentPlanSetEntry.tsx`
- Create: `apps/miniapp/tests/architecture.test.mjs`
- Modify: `apps/miniapp/src/pages/home/index.tsx`
- Modify: `apps/miniapp/src/pages/profile/index.tsx`
- Modify: all eight main-loop page files listed in File Map
- Delete: `apps/miniapp/src/services/task-utils.ts`

**Interfaces:**
- Consumes: `getHomeBootstrap`, `active_operations`, `current_report`, `recent_plan_set`, `useOperationPolling`。
- Produces: thin routes of at most route parsing plus one Feature Screen render; cached tab refresh with no duplicate first show.

- [ ] **Step 1: Write failing architecture tests**

```js
// apps/miniapp/tests/architecture.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'

const root = new URL('../src/', import.meta.url)
const routeNames = [
  'capture',
  'analysis',
  'report',
  'scene',
  'plans',
  'plan',
  'checklist',
  'feedback',
]

test('main-loop pages are thin and do not own api, storage or polling', () => {
  for (const name of routeNames) {
    const source = readFileSync(new URL(`pages/${name}/index.tsx`, root), 'utf8')
    assert.ok(source.split('\n').length <= 24, `${name} page is not thin`)
    assert.doesNotMatch(source, /qualityApi|resourceCache|Storage|Polling/)
    assert.match(source, /features\//)
  }
})

test('only the operation wrapper may create polling', () => {
  const allowed = 'app/operations/use-operation-polling.ts'
  const scan = (dir) =>
    readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
      const path = join(dir, entry.name)
      return entry.isDirectory() ? scan(path) : [path]
    })
  for (const file of scan(new URL('.', root)).filter((path) => /\.(ts|tsx)$/.test(path))) {
    const source = readFileSync(file, 'utf8')
    if (file.endsWith(allowed)) continue
    assert.doesNotMatch(source, /createOperationPolling|useTaskPolling|createTaskPolling/)
  }
})

test('quality loop contains no persisted business identifiers', () => {
  const storage = readFileSync(new URL('services/storage.ts', root), 'utf8')
  assert.doesNotMatch(storage, /reportId|planId|taskId|operationId|executionId/)
})
```

- [ ] **Step 2: Run architecture tests**

Run: `node --test --experimental-strip-types apps/miniapp/tests/architecture.test.mjs`

Expected: FAIL because current pages directly import API/Storage/polling and exceed 24 lines.

- [ ] **Step 3: Thin routes and replace Home Task behavior**

Thin page example:

```tsx
import { useRouter } from '@tarojs/taro'
import AssessmentScreen from '../../features/assessment/AssessmentScreen'

export default function AnalysisPage() {
  const { params } = useRouter()
  return (
    <AssessmentScreen
      assessmentId={params.assessment_id ?? ''}
      operationId={params.operation_id ?? ''}
    />
  )
}
```

Home/Profile 不再读取 Task 或业务 Storage。`HomeBootstrap` 使用：

```ts
type HomeBootstrapContract = {
  current_report: ReportSummary | null
  recent_plan_set: PlanSetSummary | null
  active_execution: ExecutionSummary | null
  active_operations: Operation[]
}
```

Tab 首次挂载时：若 cache 有 `home:current` 立即渲染；没有则加载。`useDidShow` 用 `firstShowRef` 跳过首次重复加载；后续 show 调 `revalidate`，不先 set null。存在 active operations 时，调用一次 `useOperationPolling({ operationIds: active_operations.map(operation => operation.id) })`；Operation 终态后只 revalidate home。报告入口直接用 bootstrap `current_report.id` 路由，方案入口用 `recent_plan_set.id`，不落 Storage。

- [ ] **Step 4: Verify thin pages and tab behavior**

Run:

```bash
node --test --experimental-strip-types apps/miniapp/tests/architecture.test.mjs
pnpm --filter @zsm/miniapp typecheck
rg "/v1/tasks|active_tasks|Task" apps/miniapp/src/pages apps/miniapp/src/features
```

Expected: architecture tests PASS；typecheck PASS；最后一个 `rg` 无匹配。

- [ ] **Step 5: Commit route/home boundary**

```bash
git add apps/miniapp/src/pages apps/miniapp/src/features/report/CurrentReportEntry.tsx \
  apps/miniapp/src/features/planning/RecentPlanSetEntry.tsx \
  apps/miniapp/src/services/task-utils.ts apps/miniapp/tests/architecture.test.mjs
git commit -m "refactor(miniapp): keep routes thin and server-driven"
```

> **执行修正（Task 11 实施时记录）：**
>
> 1. **计划里的 `HomeBootstrapContract`（current_report/recent_plan_set/active_execution）与冻结契约不符**：契约是 `{profile_summary?, report?, plan_set?, today_plan?, active_operations, billing?}`，没有 active_execution。实现按契约取字段：报告入口用 `report.id`、方案入口用 `plan_set.id`、进行中操作从 `active_operations`（kind + 非终态）推导。
> 2. **计划原文的 storage 断言（storage.ts 全文不得含 reportId/planId/…）与执行顺序矛盾**：外围页（Task 12 迁移）此刻仍读 `STORAGE_KEYS.reportId`。落地：本任务断言「谁在读」——八页 + app.ts 不得读写业务 key；已无读者的 key（planId/savedPlanId/activeTaskAnalysis/activeTaskPlanLook/scene*）本任务即删，reportId 与外围 key 留待后续任务随外围迁移删除。轮询断言同理：先扫 pages/features/app，外围迁移后纳入全量。
> 3. **`useDidShow` 跳过首次**：仓库已有 `useShowOnce`/`firstShowRef` 模式，首页与我的页用 firstShowRef 实现；缓存键 `resourceKey('home','current')` 种子直出 + revalidate 对账，不先 set null。
> 4. **缺参守卫从页面下沉到 feature**：计划的薄页示例（useRouter 解构 + 直接渲染）没有 AppHeader/pageClass 外壳；仓库外壳约定（AppHeader 占位 + usePageClass）必须保留，八页 ≤24 行（含空行与结尾换行，`split('\n')` 计数）后，无 id 的跳转逻辑放进对应 feature screen。
> 5. **`deleteMyData` 补进 qualityApi**（DELETE /v1/me/data）——「删除我的数据」流程保留，清的本地内容只剩 UI 偏好。
> 6. **提交清单比计划宽**：另含 `apps/miniapp/src/app.ts`（去 globalData 业务 id）、`apps/miniapp/src/services/storage.ts`（删无读者 key）、`apps/miniapp/src/app/api/quality.ts`、八个 feature 侧守卫改动、`packages/core/src/copy/zh.ts`、`packages/core/src/index.ts`。两个包 typecheck 至此 0 错误（原 33 个遗留页错误随 home/profile 重写消除）。

### Task 12: Remove Remaining Business-State Compatibility Paths

**Files:**
- Modify: `apps/miniapp/src/packages/tools/pages/hair/index.tsx`
- Modify: `apps/miniapp/src/packages/tools/pages/outfit/index.tsx`
- Modify: `apps/miniapp/src/packages/tools/pages/purchase/index.tsx`
- Modify: `apps/miniapp/src/packages/tools/pages/lab/index.tsx`
- Modify: `apps/miniapp/src/packages/life/pages/today/index.tsx`
- Modify: `apps/miniapp/src/packages/life/pages/wardrobe/index.tsx`
- Modify: `apps/miniapp/src/packages/life/pages/advisor/index.tsx`
- Modify: `apps/miniapp/src/packages/life/pages/share/index.tsx`
- Modify: `apps/miniapp/src/services/outfit-session.ts`
- Modify: `apps/miniapp/src/services/purchase-session.ts`
- Delete: `packages/core/src/api/endpoints.ts`
- Delete: `packages/core/src/types/index.ts`

**Interfaces:**
- Consumes: generated OpenAPI client for retained peripheral endpoints and server-side current readers.
- Produces: compile-clean miniapp with one API client and no hidden old report/plan/task compatibility path.

- [ ] **Step 1: Extend architecture test to reject old imports**

```js
test('no miniapp module imports old api or hand-written dto surfaces', () => {
  for (const file of scan(new URL('.', root)).filter((path) => /\.(ts|tsx)$/.test(path))) {
    const source = readFileSync(file, 'utf8')
    assert.doesNotMatch(source, /services\/api/)
    assert.doesNotMatch(source, /API_PATHS|createApiEndpoints/)
    assert.doesNotMatch(source, /getTask|getTasks|active_tasks/)
  }
})
```

- [ ] **Step 2: Run test and capture all remaining old call sites**

Run:

```bash
node --test --experimental-strip-types apps/miniapp/tests/architecture.test.mjs
rg "services/api|createApiEndpoints|getTask|getTasks|STORAGE_KEYS\\.(reportId|planId|activeTask)" apps/miniapp/src
```

Expected: FAIL and list every retained peripheral call site before migration.

- [ ] **Step 3: Move peripherals directly to generated API**

每个外围页面直接使用 `app/api` 中由 generated paths 约束的对应窄函数。Hair/Today 仍有异步生成时，服务端响应必须改为公开 Operation，并复用唯一 `useOperationPolling`；Outfit/Purchase 同步结果存在页面 React state，复访从服务端 current/latest endpoint 读取，不写 session Storage；Advisor conversation ID 从服务端返回或路由参数读取，不写 Storage。

此任务不重设计外围 UI、不把外围功能接入质量主链写模型，只移除编译所需的旧 API/DTO/Storage/Task 路径。迁移后删除 `services/outfit-session.ts`、`services/purchase-session.ts`；如果文件已无引用则直接删除，不保留转发函数。

- [ ] **Step 4: Verify no compatibility layer remains**

Run:

```bash
node --test --experimental-strip-types apps/miniapp/tests/architecture.test.mjs
pnpm typecheck
rg "services/api|createApiEndpoints|API_PATHS|getTask|getTasks|active_tasks|useTaskPolling|createTaskPolling" apps/miniapp/src packages/core/src
```

Expected: tests PASS；workspace typecheck PASS；最后一个 `rg` 无匹配。

- [ ] **Step 5: Commit compatibility deletion**

```bash
git add apps/miniapp/src/packages apps/miniapp/src/services \
  packages/core/src/api/endpoints.ts packages/core/src/types/index.ts \
  apps/miniapp/tests/architecture.test.mjs
git commit -m "refactor(miniapp): delete old client state paths"
```

> **执行修正（Task 12 实施时记录）：**
>
> 1. **外围数据路径迁移清单**：hair/today 的异步生成本就是契约里的受理信封（`HairPreviewAccepted`/`TodayPlanAccepted` + 公开 Operation），页面改用 `useOperationPolling`；outfit/purchase 是同步诊断，结果只活在 React state、复访走 `GET /diagnostics/latest`；advisor 会话归服务端「当前会话」，share 撤销/查看走 `share_ref`。恢复逻辑里所有「本地引用丢失」分支随之删除（服务端就是引用）。
> 2. **外围媒体迁移到 upload-intents 三步流**：老 `POST /v1/media` 不在新契约里；purpose 用契约枚举（outfit→body、purchase/单品→wardrobe）。Demo 媒体只允许 face/side/body（purchase/outfit 的示例图用 body），返回的 `asset_id` 直接作为 `media_id`。
> 3. **`CreateWardrobeOutfit` 契约不再收 context**（服务端自行快照），衣橱页去掉 getTodayContext 预取。
> 4. **删除范围比计划列表宽（计划正文允许："如果文件已无引用则直接删除"）**：`services/api.ts`、`services/http.ts`（唯一消费者是 api.ts）、`components/profile-sheet`（无消费者）、`components/example-image/**`（全部消费页已迁 SourceImage）、`hooks/use-stable-polling.ts`、`packages/core/src/hooks/useTaskPolling.ts` + `tests/legacy-task-polling.test.mjs`、client.test.mjs 中针对已删端点门面的 3 个用例。core 桶文件相应清理；`EventInput` 就地内联进 events.ts（旧 types 里只有它还被引用）。
> 5. **埋点发送改由 app.ts 用唯一客户端接线**（原来藏在 services/api.ts 的副作用里），`setDefaultEventSender` → `POST /v1/events`。
> 6. **`apps/mobile` 不在本 stage 的验证范围**：它仍消费旧 core 面（Analysis/Plan/POLL_INTERVALS 等），本任务删除后 mobile 的 tsc 会红——mobile 归 Peripherals/Cutover 阶段迁移。本 stage 的 typecheck 验证口径是 `@zsm/miniapp` + `@zsm/core`（两者 0 错误）。
> 7. **`createIdempotencyKey` 全部收口在 app/api 层**（createHairPreview/createTodayPlan 的受理键、账单 sync 键），页面不再手搓键。

### Task 13: Strengthen Miniapp Static Gates

**Files:**
- Modify: `apps/miniapp/scripts/check.mjs`
- Create: `apps/miniapp/scripts/check.test.mjs`
- Create: `apps/miniapp/src/components/operation-status/index.tsx`
- Create: `apps/miniapp/src/components/operation-status/index.scss`
- Create: `apps/miniapp/src/components/operation-status/index.config.ts`
- Create config files for existing components lacking them:
  - `apps/miniapp/src/components/app-header/index.config.ts`
  - `apps/miniapp/src/components/compare-slider/index.config.ts`
  - `apps/miniapp/src/components/compare-toggle/index.config.ts`
  - `apps/miniapp/src/components/empty-state/index.config.ts`
  - `apps/miniapp/src/components/error-state/index.config.ts`
  - `apps/miniapp/src/components/image-viewer/index.config.ts`
  - `apps/miniapp/src/components/photo-annotation/index.config.ts`
  - `apps/miniapp/src/components/pill/index.config.ts`
  - `apps/miniapp/src/components/primary-button/index.config.ts`
  - `apps/miniapp/src/components/section-header/index.config.ts`
  - `apps/miniapp/src/components/skeleton/index.config.ts`
  - `apps/miniapp/src/components/text-link/index.config.ts`

**Interfaces:**
- Consumes: source tree and app config.
- Produces: deterministic static gate covering all miniapp hard rules.

- [ ] **Step 1: Add fixture files to prove each gate fails**

在 `apps/miniapp/scripts/fixtures/invalid/` 创建运行时临时 fixture 的测试逻辑，不把非法 `.scss/.tsx` 放入 `src`。检查函数导出为 `checkSourceFile(path, source)` 并由 Node test 调用：

```js
test('static rules reject forbidden miniapp patterns', () => {
  assert.deepEqual(
    checkSourceFile('feature.scss', '.x { width: 10px; }'),
    ['scss 裸 px（须 rpx）: feature.scss:1'],
  )
  assert.deepEqual(
    checkSourceFile('feature.tsx', '{count && <View />}'),
    ['条件渲染可能渲染 0: feature.tsx:1'],
  )
  assert.deepEqual(
    checkSourceFile('feature.tsx', 'items.map((item, index) => <View key={index} />)'),
    ['列表禁止下标 key: feature.tsx:1'],
  )
  assert.deepEqual(
    checkSourceFile('feature.tsx', "const x = '/assets/a.webp'"),
    ['禁止 WebP: feature.tsx:1'],
  )
})
```

- [ ] **Step 2: Run the new gate tests**

Run: `node --test apps/miniapp/scripts/check.test.mjs`

Expected: FAIL because `checkSourceFile` and the new rules do not exist.

- [ ] **Step 3: Implement all hard gates**

`check.mjs` 增加：

- 所有 `src/**/*.scss` 与 JSX style 字符串禁止裸 px。
- 所有 source/assets/dist 禁 `.webp`，包内照片目录只允许 `.jpg/.jpeg`，tabBar/icon 只允许 `.png`。
- `components/**/index.tsx` 必须有同目录 `index.config.ts` 且包含 `styleIsolation: 'apply-shared'`。
- 拒绝 `{count && <...>}`、`{items.length && <...>}` 和数值表达式直接 `&&` JSX。
- 拒绝 `key={index}`、`key={i}`、`key={idx}`。
- `pages/*/index.tsx` 主闭环页面不得 import app API、cache、operations、Storage。
- 除唯一 hook 文件外拒绝 `createOperationPolling`；全仓拒绝 Task polling API。
- 拒绝 `pinnedUrl`、`PinnedImage`、`provider_version.startsWith('demo')`、`isBundledAsset`。
- 拒绝业务 Storage key 名。
- 保留现有主包体积 1.6MB、页面三件套、资产存在性、http 图片本地化检查。

所有新增和既有 custom component config 内容统一为：

```ts
export default defineComponentConfig({
  styleIsolation: 'apply-shared',
})
```

- [ ] **Step 4: Run static gates**

Run:

```bash
node --test apps/miniapp/scripts/check.test.mjs
pnpm --filter @zsm/miniapp build:weapp
node apps/miniapp/scripts/check.mjs
```

Expected: fixture tests PASS；build PASS；`[check] ✅ miniapp 静态门禁通过`。

- [ ] **Step 5: Commit static gates**

```bash
git add apps/miniapp/scripts apps/miniapp/src/components
git commit -m "test(miniapp): enforce quality loop invariants"
```

> **执行修正（Task 13 实施时记录）：**
>
> 1. **组件 config 保持现有约定**（`export default { styleIsolation: 'apply-shared', virtualHost: true }` 纯对象）：Taro 没有 `defineComponentConfig`，计划片段里的写法不可编译；门禁只校验 config 含 `styleIsolation: 'apply-shared'`。
> 2. **数值 `&&` JSX 的判定按命名/成员启发式**（`.length`/`.size` 成员、count/total/num/…/done/remain 结尾的标识符、`> 0` 比较式），纯布尔（`ready && …`）不拦——测试双向都有钉。
> 3. **首跑真实 build 暴露两处遗留**：`assets/capture/{face,side,body}.png`（233×424 无 alpha 的照片式指引图，共 ~390KB）转 jpg（→60KB），主包 1.62MB → 1.30MB；tabBar 图标按微信开发者工具的根目录查找习惯补到 `apps/miniapp/assets/tabbar/`（构建产物之外的静态拷贝）。`checkSourceFile` 抽取后，`check.mjs` 用 `import.meta.url` 守卫，被 node:test 导入时只出规则不出副作用。
> 4. **operation-status 组件**承接 OperationView 的 working/failed/ended 三态通用渲染；succeeded/idle 不渲染（终态去向是页面职责）。

### Task 14: Add the Automated Quality-Loop Fixture

**Files:**
- Create: `apps/miniapp/qa/fixtures/quality-loop.ts`
- Create: `apps/miniapp/qa/quality-loop.fixture.test.mjs`
- Modify: `apps/miniapp/package.json`
- Modify: `packages/core/src/copy/zh.ts`

**Interfaces:**
- Consumes: 64 deterministic front-end source/state fixtures; no live Provider and no physical device.
- Produces: automated regression proof for source labels, Operation lifecycle, cache partitions, current-report binding and partial readiness.

- [ ] **Step 1: Write the failing fixture matrix test**

```js
// apps/miniapp/qa/quality-loop.fixture.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { QUALITY_LOOP_FIXTURES } from './fixtures/quality-loop.ts'
import { projectDisplayMedia } from '../../../packages/core/src/media/display.ts'
import {
  planSetView,
  variantRenderView,
} from '../src/features/planning/model.ts'
import { operationView } from '../src/app/operations/operation-view.ts'

function projectState(fixture) {
  switch (fixture.stateDomain) {
    case 'plan-set':
      return planSetView(fixture.stateInput)
    case 'render':
      return variantRenderView(fixture.stateInput)
    case 'operation':
      return operationView(fixture.stateInput)
    default:
      throw new Error(`unknown state domain: ${fixture.stateDomain}`)
  }
}

test('fixture set contains 64 explicit source/state cases', () => {
  assert.equal(QUALITY_LOOP_FIXTURES.length, 64)
  assert.equal(new Set(QUALITY_LOOP_FIXTURES.map((item) => item.id)).size, 64)
})

test('every fixture has deterministic media and plan-set projection', () => {
  for (const fixture of QUALITY_LOOP_FIXTURES) {
    assert.deepEqual(
      projectDisplayMedia(fixture.media, fixture.now),
      fixture.expectedMedia,
      fixture.id,
    )
    assert.deepEqual(
      projectState(fixture),
      fixture.expectedState,
      fixture.id,
    )
  }
})
```

- [ ] **Step 2: Run fixture test**

Run: `node --test --experimental-strip-types apps/miniapp/qa/quality-loop.fixture.test.mjs`

Expected: FAIL with missing fixture module.

- [ ] **Step 3: Add the full deterministic matrix**

64 fixtures 是精确的 `4 个来源 × 16 个公开 UI 状态`：

- 来源：`user_original`、`generated_preview`、`bundled_reference`、`demo_example`。
- PlanSet：`planning`、`rendering`、`ready`、`ready_partial`、`failed`。
- Render：`queued`、`generating`、`checking`、`ready`、`failed`、`unavailable`。
- Operation：`accepted`、`running`、`retrying`、`succeeded`、`failed`。

fixture 文件用下面的确定性构造；每个 ID 都是 `<source-id>:<state-id>`，因此正好 64 个唯一、可定位用例：

```ts
const NOW = Date.parse('2026-09-12T08:00:00Z')

const SOURCE_CASES = [
  {
    id: 'user',
    media: {
      asset_id: 'asset-user',
      url: 'https://cdn.example/user.jpg',
      url_expires_at: '2026-09-13T08:00:00Z',
      mime_type: 'image/jpeg',
      source_kind: 'user_original',
      display_label: '原本',
    },
    expected: { badge: '原本', soften: false, sourceKind: 'user_original' },
  },
  {
    id: 'generated',
    media: {
      asset_id: 'asset-generated',
      url: 'https://cdn.example/generated.jpg',
      url_expires_at: '2026-09-13T08:00:00Z',
      mime_type: 'image/jpeg',
      source_kind: 'generated_preview',
      display_label: '风格参考',
    },
    expected: { badge: '风格参考', soften: false, sourceKind: 'generated_preview' },
  },
  {
    id: 'bundled',
    media: {
      asset_id: 'asset-bundled',
      url: 'https://cdn.example/bundled.jpg',
      url_expires_at: '2026-09-13T08:00:00Z',
      mime_type: 'image/jpeg',
      source_kind: 'bundled_reference',
      display_label: '风格参考',
    },
    expected: { badge: '风格参考', soften: true, sourceKind: 'bundled_reference' },
  },
  {
    id: 'demo',
    media: {
      asset_id: 'asset-demo',
      url: 'https://cdn.example/demo.jpg',
      url_expires_at: '2026-09-13T08:00:00Z',
      mime_type: 'image/jpeg',
      source_kind: 'demo_example',
      display_label: '效果示例',
    },
    expected: { badge: '效果示例', soften: true, sourceKind: 'demo_example' },
  },
] as const

const STATE_CASES = [
  { id: 'plan-planning', domain: 'plan-set', input: { state: 'planning', plans: [] }, expected: { kind: 'planning' } },
  { id: 'plan-rendering', domain: 'plan-set', input: { state: 'rendering', plans: [{ id: 'v1' }] }, expected: { kind: 'rendering' } },
  { id: 'plan-ready', domain: 'plan-set', input: { state: 'ready', plans: [{ id: 'v1' }] }, expected: { kind: 'ready' } },
  { id: 'plan-ready-partial', domain: 'plan-set', input: { state: 'ready_partial', plans: [{ id: 'v1' }] }, expected: { kind: 'ready_partial' } },
  { id: 'plan-failed', domain: 'plan-set', input: { state: 'failed', plans: [] }, expected: { kind: 'failed' } },
  { id: 'render-queued', domain: 'render', input: { id: 'v1', content: {}, render: { state: 'queued', retryable: false, media: null } }, expected: { kind: 'queued', textAvailable: true, retryable: false, media: null } },
  { id: 'render-generating', domain: 'render', input: { id: 'v1', content: {}, render: { state: 'generating', retryable: false, media: null } }, expected: { kind: 'generating', textAvailable: true, retryable: false, media: null } },
  { id: 'render-checking', domain: 'render', input: { id: 'v1', content: {}, render: { state: 'checking', retryable: false, media: null } }, expected: { kind: 'checking', textAvailable: true, retryable: false, media: null } },
  { id: 'render-ready', domain: 'render', input: { id: 'v1', content: {}, render: { state: 'ready', retryable: false, media: { asset_id: 'a1' } } }, expected: { kind: 'ready', textAvailable: true, retryable: false, media: { asset_id: 'a1' } } },
  { id: 'render-failed', domain: 'render', input: { id: 'v1', content: {}, render: { state: 'failed', retryable: true, media: null } }, expected: { kind: 'failed', textAvailable: true, retryable: true, media: null } },
  { id: 'render-unavailable', domain: 'render', input: { id: 'v1', content: {}, render: { state: 'unavailable', retryable: false, media: null } }, expected: { kind: 'unavailable', textAvailable: true, retryable: false, media: null } },
  { id: 'operation-accepted', domain: 'operation', input: { status: 'accepted', progress_bps: 0, public_message: '已受理' }, expected: { kind: 'working', progress: 0, message: '已受理', retrying: false } },
  { id: 'operation-running', domain: 'operation', input: { status: 'running', progress_bps: 5000, public_message: '生成中' }, expected: { kind: 'working', progress: 50, message: '生成中', retrying: false } },
  { id: 'operation-retrying', domain: 'operation', input: { status: 'retrying', progress_bps: 6000, public_message: '正在重试' }, expected: { kind: 'working', progress: 60, message: '正在重试', retrying: true } },
  { id: 'operation-succeeded', domain: 'operation', input: { status: 'succeeded', result_type: 'plan_set', result_id: 'ps1' }, expected: { kind: 'succeeded', resultType: 'plan_set', resultId: 'ps1' } },
  { id: 'operation-failed', domain: 'operation', input: { status: 'failed', public_message: '没有生成成功', retryable: true, trace_id: 'trace-1' }, expected: { kind: 'failed', message: '没有生成成功', retryable: true, traceId: 'trace-1' } },
] as const

export const QUALITY_LOOP_FIXTURES = SOURCE_CASES.flatMap((source) =>
  STATE_CASES.map((state) => ({
    id: `${source.id}:${state.id}`,
    now: NOW,
    media: source.media,
    expectedMedia: {
      key: `${source.media.asset_id}:${source.media.url}`,
      src: source.media.url,
      ...source.expected,
    },
    stateDomain: state.domain,
    stateInput: state.input,
    expectedState: state.expected,
  })),
)
```

过期、空 URL、WebP、错误 label、generated PNG、report mismatch、null replacement 和失败 revalidate 已分别由 Tasks 3、5、8 的 focused tests 覆盖，不能从这些测试删除。

`package.json` scripts：

```json
{
  "scripts": {
    "test": "node --test --experimental-strip-types tests/*.test.mjs",
    "qa": "node --test --experimental-strip-types qa/*.test.mjs",
    "check": "node scripts/check.mjs"
  }
}
```

- [ ] **Step 4: Run feature QA**

Run:

```bash
pnpm --filter @zsm/miniapp test
pnpm --filter @zsm/miniapp qa
pnpm --filter @zsm/core test
```

Expected: all tests PASS；fixture test reports 64 unique cases。

- [ ] **Step 5: Commit automated QA**

```bash
git add apps/miniapp/qa apps/miniapp/package.json packages/core/src/copy/zh.ts
git commit -m "test(miniapp): cover quality loop state matrix"
```

> **执行修正（Task 14 实施时记录）：**
>
> 1. **fixture 输入按冻结契约形状**：plan-set 用 `{state, variants}`（非计划的 `plans`）、render 用完整 `RenderStatusView`、operation 补契约必填字段；期望值按本仓实现（working 视图带 `stageCode`；失败视图字段名是 `requestId`——契约 `trace_id` 对用户的统一叫法，见 operation-view 注释）。
> 2. **render 的 `textAvailable: true` 由 `steps:[{id:'s1'}]` 给出**；PlanSet failed 用 `variants: []`（无已发布文字 → 整屏失败），不与 Task 8 的 ready_partial 提升用例重复。
> 3. **删除 `qa/home-visual-contract.test.mjs`**：它钉死的是 Task 11 已按计划重建前的首页实现（ExampleImage 大卡、SCENES 网格、步骤区样式），与"Home 服务端驱动 + ExampleImage 全仓删除"直接冲突；`qa/visual-gates.md` 原则文档保留。计划文件清单里的 `packages/core/src/copy/zh.ts` 本任务无改动需求，未动。

### Task 15: Final Verification and Commit Audit

**Files:**
- Verify only; do not create release notes, screenshots, device checklists or submission material.

**Interfaces:**
- Consumes: all previous task commits.
- Produces: machine-verifiable handoff and clean scope audit.

- [ ] **Step 1: Verify generated contract has no drift**

Run:

```bash
pnpm --filter @zsm/core api:check
node contracts/scripts/check-sync.mjs
```

Expected: both commands exit 0；required 20 operationId present；Task client endpoint absent。

- [ ] **Step 2: Run focused tests**

Run:

```bash
pnpm --filter @zsm/core test
pnpm --filter @zsm/miniapp test
pnpm --filter @zsm/miniapp qa
node --test apps/miniapp/scripts/check.test.mjs
```

Expected: all test files PASS；64-fixture assertion PASS；no skipped test。

- [ ] **Step 3: Run repository gates required by AGENTS.md**

Run:

```bash
pnpm typecheck && pnpm lint && make design-build
node apps/miniapp/scripts/check.mjs
make server-vet && make server-test
```

Expected: every command exits 0；design generated files have no drift；miniapp gate prints success；server tests remain green even though this plan does not change server business code。

- [ ] **Step 4: Audit forbidden patterns and scope**

Run:

```bash
rg "/v1/tasks|active_tasks|useTaskPolling|createTaskPolling|useStablePolling|API_PATHS|createApiEndpoints|pinnedUrl|PinnedImage|provider_version.*demo|STORAGE_KEYS\\.(reportId|planId|activeTask)" \
  apps/miniapp/src packages/core/src
rg "\\.webp" apps/miniapp/src apps/miniapp/dist
git diff --check
git status --short
```

Expected: both `rg` commands exit 1 with no output；`git diff --check` exits 0；status only shows files from Tasks 1–14 plus the pre-existing unrelated untracked files, with no change under `appearance-coach-prototype/`。

- [ ] **Step 5: Review commit boundaries without creating another commit**

Run:

```bash
git log --oneline --reverse HEAD~14..HEAD
git diff --stat HEAD~14..HEAD
```

Expected: exactly 14 implementation commits matching Tasks 1–14；Task 15 creates no commit。不要执行真机任务、不要准备微信提审材料、不要提交本计划执行之外的文件。

> **执行修正（Task 15 实施时记录）：**
>
> 1. **契约无漂移**：`api:check`（redocly）通过；`contracts/scripts/check-sync.mjs` 报 63 个 operationId 一致；`contract.type-test.ts` 的 `HasNoLegacyTasks`/`HasNoLegacyAnalyses` 断言 `/v1/tasks`、`/v1/analyses` 已从 generated client 消失。
> 2. **测试全绿**：core 43、miniapp 67、miniapp qa 2（含 64-fixture 断言）、gate 5，均 0 fail 0 skip。
> 3. **仓库门禁**：`make design-build` 无漂移；`node scripts/check.mjs` 主包 1.30MB 通过；`make server-vet` 通过；`make server-test` 需注入 `TEST_DATABASE_URL`（指向 `127.0.0.1:55432`）后 22 个包全绿——本计划不改服务端业务码，红仅因本地 postgres 未注入 URL。
> 4. **禁用模式审计**：`apps/miniapp/src` 与 `packages/core/src` 零命中；唯一 `/v1/tasks` 命中是 generated type-test 里**断言其不存在**的一行；唯一 `.webp` 命中是 `dist/common.js` 里 URL 重写器的拒绝正则（`\.webp(?:\?|$)/i.test`），src/dist 实际 .webp 文件数为 0。`git diff --check` 干净，`git status` 干净。
> 5. **提交边界（与计划"14 提交"预期有一处出入）**：Tasks 2–14 各有独立提交（`402e90e`…`5762fc2`，13 个）。Task 1「生成核心客户端」的产出（`generated/schema.ts` + `contract.type-test.ts` + check-sync）由更早的 core-foundation 阶段提交 `704babd fix(core): tighten frozen contract and lint gate` 交付，本分支没有计划书写的那个独立 Task 1 提交消息。Task 15 本身不产生提交。

## Execution Notes

- Task 1 依赖服务端团队认可新 `/v1` OpenAPI，但不依赖服务端实现完成；Tasks 2–5 可用 fake Fetch 与 fixtures 独立完成。
- Tasks 6–10 每个 Feature 都通过窄 API、cache 与 Operation 接口工作，可分别 code review。
- Task 11 才切换所有主闭环路由，避免中间提交留下页面直接访问已删除模块。
- Task 12 是删除旧路径的原子提交；它不是兼容期。该提交完成后仓库只保留 generated client。
- Task 13 将产品硬规则变为静态门禁，后续页面改动自动继承。
- Task 14 使用纯 fixture，不调用真实 AI，不要求真机，不生成提审截图。
- 执行时若服务端 endpoint 尚未可用，前端 E2E 可以保持 fake HTTP fixture；不得因此增加旧 endpoint fallback、双读、Storage 恢复或临时 Demo 注入。
