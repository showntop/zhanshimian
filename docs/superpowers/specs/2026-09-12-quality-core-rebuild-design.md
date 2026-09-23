# uplook 质量核心彻底重构设计

> 状态：待评审  
> 日期：2026-09-12  
> 范围：建档、报告、方案、本人效果图、执行与反馈主闭环  
> 前提：不保留历史业务数据，不兼容旧数据库、旧 API、旧本地缓存或旧任务

## 1. 结论

uplook 保持一个 Go 代码库、API/Worker 两个进程、一个 PostgreSQL 和一个私有 COS，不拆微服务，不引入 Kafka、Redis 队列、工作流引擎、Saga 或事件溯源。

本次重构不在现有 `analyses / reports / plans / tasks` 模型上继续修补，而是重新建立一条可追溯、可验收、可重试的质量链路：

```text
三图建档
  → 照片质量与同人检查
  → 有证据的形象报告
  → 报告 + 用户偏好 + 场景 Brief
  → 三套真正不同的结构化方案
  → 每套方案的 RenderSpec
  → 候选效果图
  → 身份 / 人体 / 构图 / 图文质量门禁
  → 合格后发布
  → 选择与执行
  → 质量反馈和执行反馈分别回流
```

核心设计决策：

- 报告、方案集、渲染规格、候选图都是不可变产物。
- 重新分析、修改场景、重新生成时创建新产物，不原地覆盖旧产物。
- 客户端只轮询公开 Operation，不读取内部任务。
- 内部仍使用 PostgreSQL `tasks + ClaimTask + SKIP LOCKED`。
- 图片模型输出先进入隔离候选区，质量通过后才成为可展示资产。
- 身份敏感能力只能在具备同等多图输入能力的模型之间切换。
- 前端不根据 URL、文件后缀或 Provider 名称推断图片来源和状态。
- 质量、速度、成本采用平衡策略：默认生成一个候选，质量失败后最多补生成一次，最多切换一次同能力模型。

## 2. 重构目标

### 2.1 业务目标

1. 报告里的每条判断都能回答“从哪张照片、哪个区域看到了什么”。
2. 通用、面试、婚礼、约会、日常和聚会方案都真正使用本人报告。
3. 三套方案在发型、妆容、穿搭、配色、正式度中至少有两个可验证差异。
4. 效果图优先保持本人身份、身体比例和原始构图，只修改方案明确要求的内容。
5. 不合格效果图不会被当作成功结果展示。
6. 用户反馈能够明确影响重新生成或下一轮方案。
7. 客服和研发可以用一个 trace ID 还原一次结果的输入版本、模型、参数、质量判断、成本和失败原因。

### 2.2 软件设计目标

- 单一职责：一个用例只负责一个业务动作。
- 接口隔离：删除全仓巨型 `Service` 和巨型 `Repository`。
- 依赖倒置：业务服务依赖窄接口，不依赖 PostgreSQL、COS 或具体模型。
- 开闭原则：新增模型、任务类型或质量检查通过注册扩展，不修改主循环。
- 不可变优先：AI 产物 append-only，重新生成产生新 ID。
- 显式状态：领域结果、公开 Operation 和内部 Task 各有单一职责。
- 失败关闭：身份和真实性无法确认时不发布，不用示例图冒充结果。
- 简单优先：不采用当前规模不需要的分布式架构。

### 2.3 非目标

- 不重写已稳定的微信登录、短信、支付签名、COS SDK 和天气适配器内部算法。
- 不把手机端作为首轮交付阻塞项；先完成小程序和服务端主闭环。
- 不做在线训练、自动微调或基于单次反馈实时修改模型。
- 不持久化人脸 embedding，不做颜值、身材、年龄或敏感属性评分。
- 不为旧数据编写迁移、回填、双写或兼容 DTO。

## 3. 成功标准

首轮发布必须达到：

- 照片类型和归属校验准确率 100%。
- Demo、内置参考、用户原图和生成图来源错配次数为 0。
- 报告敏感推断、颜值评分、身材评分次数为 0。
- 报告 finding 的证据关联完整率 100%，抽检证据支持率不低于 95%。
- 方案步骤 grounding 完整率 100%。
- 三套方案任意两套至少两个实质差异。
- full-look 单图 fallback 使用次数为 0。
- 严重身份漂移或明显人体结构错误被发布的比例低于 1%。
- 人审“像本人”通过率不低于 90%，稳定后提升到 95%。
- 明确承诺的发型、妆容、穿搭图文符合率不低于 85%。
- 旧 Task 覆盖新产物次数为 0。
- 相同幂等请求产生重复 AI 调用次数为 0。
- 图片来源、Operation、Provider invocation 和反馈关联完整率 100%。
- 照片检查 P95 不高于 12 秒。
- 报告 P95 不高于 90 秒。
- 方案文字 P95 不高于 60 秒。
- 第一张效果图 P95 目标不高于 120 秒；质量补生成路径允许到 240 秒。
- 小程序收到 WebP 生成图次数为 0。

## 4. 总体架构

```text
微信小程序 / 手机端
        │
        │ REST + OpenAPI 生成客户端
        ▼
Go API
  httpapi
    ↓
  service/{assessment, planning, rendering, execution, feedback}
    ↓
  repository 窄接口 / provider 能力接口 / storage 接口
        │
        ├── PostgreSQL：业务事实、Operation、Task、调用账本
        ├── 私有 COS：用户原图、隔离候选、已发布图片
        └── AI Runtime：能力路由、协议适配、质量策略

Go Worker
  ClaimTask(SKIP LOCKED)
    → 对应 Handler
    → Service Use Case
    → 外部调用在事务外
    → lease CAS + 目标 generation CAS 原子提交
```

部署规则：

- `cmd/api` 不在生产环境内嵌 Worker。
- `cmd/worker` 独立扩缩容。
- API 与 Worker 使用同一代码库和数据库。
- 不增加消息中间件。
- 按任务类型配置并发配额，避免图片生成阻塞分析和文本方案。

## 5. 代码边界

保留仓库规定的服务端依赖方向：

```text
httpapi → service → repository / provider / storage
```

但将横向巨型包拆成按业务能力组织的窄边界。

目标目录：

```text
apps/server/
  cmd/
    api/
    worker/
  internal/
    domain/
      media.go
      profile.go
      assessment.go
      planning.go
      rendering.go
      execution.go
      feedback.go
      operation.go
      billing.go
    httpapi/
      auth.go
      media.go
      assessments.go
      reports.go
      plan_sets.go
      renders.go
      operations.go
      executions.go
      feedback.go
      home.go
    service/
      assessment/
        service.go
        ports.go
        validation.go
      planning/
        service.go
        ports.go
        validation.go
      rendering/
        service.go
        ports.go
        policy.go
      execution/
        service.go
        ports.go
      feedback/
        service.go
        ports.go
      operation/
        service.go
      taskrunner/
        runner.go
        registry.go
        handlers.go
    repository/
      postgres/
        users.go
        media.go
        assessments.go
        planning.go
        rendering.go
        execution.go
        feedback.go
        operations.go
        tasks.go
        billing.go
    provider/
      ai/
        runtime.go
        router.go
        contracts.go
        structured.go
        image.go
        quality.go
        protocols/
      identity/
      weather/
      payment/
    storage/
      storage.go
      cos.go
      local.go
    bootstrap/
      api.go
      worker.go
  database/
    migrations/
      001_baseline.sql
```

边界规则：

- `httpapi` 只做鉴权、DTO 转换、请求校验和错误翻译。
- 每个 `service/*` 只暴露该领域用例，不共享全能 Service 结构体。
- 每个 Service 在自己的 `ports.go` 定义最小依赖接口。
- Postgres Adapter 可实现多个窄接口，但不得重新暴露巨型 Repository。
- 一个 Service 不直接更新另一个领域的表；跨领域读取通过窄 Reader。
- Task runner 不认识业务规则，只负责租约、调度和 Handler 注册。
- AI Runtime 不返回数据库实体，只返回 Provider DTO 和 InvocationMeta。
- `packages/core` 只保留跨端纯逻辑、生成的 API 类型和平台接口，不承载服务端业务规则。

## 6. 核心数据模型

### 6.1 全局规则

所有用户资源包含：

- `id uuid primary key`
- `user_id uuid not null`
- `created_at timestamptz not null`
- `unique(user_id, id)`

所有跨资源关系使用 `(user_id, parent_id)` 复合外键，数据库层阻止跨用户挂接。

其他规则：

- 数据库只保存 COS object key，不保存签名 URL。
- AI 产物使用不可变 object key，不覆盖已有对象。
- 用户上传只接受 JPEG/PNG；生成图发布前统一转 JPEG。
- `jsonb` 只保存有 Schema 版本的快照，不保存可以使用外键表达的关系。
- 报告、方案、RenderSpec、候选和质量结果禁止原地修改。
- 用户资料、当前指针、执行进度是少量允许更新的资源，必须带 version 做 CAS。

### 6.2 用户资料

`user_profiles`

- `user_id`
- `role`
- `height_cm`，仅用于用户主动提供的搭配上下文，不作为身体评价依据
- `budget`
- `preferences jsonb`
- `avoidances jsonb`
- `current_report_id`
- `version`
- `updated_at`

每次分析和规划都保存实际使用的 `profile_snapshot`，保证之后可以复现。

### 6.3 媒体资产

`media_assets`

- `id, user_id`
- `origin`: `user_upload | provider_output | demo | bundled_reference`
- `purpose`: `face | side | body | render_candidate | feedback | wardrobe`
- `object_key`
- `sha256`
- `mime_type`
- `byte_size`
- `width, height`
- `state`: `quarantined | ready | published | deleted`
- `display_kind`: `original | generated_reference | effect_example | style_reference`
- `provider_invocation_id nullable`
- `deleted_at`

约束：

- object key 全局唯一。
- Provider 输出必须关联 invocation。
- Demo 必须使用 `effect_example`。
- 候选图质量通过前只能是 `quarantined`。
- 小程序可展示的 Provider 输出必须是 JPEG。

上传采用 `upload_intents`：

- 客户端请求短期上传凭证。
- 客户端直传 COS。
- 服务端 `HEAD` 对象并验证 key、所有者、大小、MIME、hash 后创建 Asset。
- 不信任客户端“已上传”的单方声明。

### 6.4 三图照片集

`photo_sets`

- `id, user_id`
- `profile_snapshot jsonb`
- `content_hash`
- `schema_version`
- `created_at`

`photo_set_items`

- `photo_set_id`
- `role`: `face | side | body`
- `media_asset_id`

约束：

- 每个照片集恰好一张 face、side、body。
- 三张 Asset 必须属于同一用户。
- 同一 Asset 不能占两个角色。
- `content_hash` 相同时复用已有照片集，不重复分析。

不再使用 `media_ids uuid[]`。

### 6.5 分析与报告

`analysis_runs`

- `id, user_id`
- `photo_set_id`
- `operation_id`
- `input_hash`
- `analyzer_schema_version`
- `quality_policy_version`
- `provider_invocation_id`
- `outcome`: `published | rejected | failed`
- `report_id nullable`
- `created_at, finished_at`

`reports`

- `id, user_id`
- `photo_set_id`
- `profile_snapshot`
- `impression_tags`
- `priority_title`
- `priority_copy`
- `hero_asset_id`
- `schema_version`
- `content_hash`
- `provider_invocation_id`
- `quality_evaluation_id`
- `created_at`

`report_findings`

- `id, user_id, report_id`
- `category`: `hair | makeup | outfit | color`
- `priority`: `1 | 2 | 3`
- `label`
- `visible_observation`
- `recommendation`
- `source_photo_item_id`
- `anchor_x, anchor_y, anchor_w, anchor_h`
- `confidence`
- `position`

设计规则：

- finding 数量允许 3–6 条，不强迫模型编造固定四条。
- `priority` 表示建议先后，不表示人的严重程度。
- observation 只能陈述可见事实。
- recommendation 与 observation 分开存储。
- confidence 仅用于内部质量判断，不向用户展示为评分。
- 用户职业、预算和身高只能进入建议上下文，不能伪装成视觉结论。

### 6.6 方案集

`plan_sets`

- `id, user_id`
- `report_id`
- `profile_snapshot`
- `scene`
- `scene_brief jsonb`
- `brief_hash`
- `planner_schema_version`
- `style_rule_version`
- `provider_invocation_id`
- `quality_evaluation_id`
- `created_at`

`plan_variants`

- `id, user_id, plan_set_id`
- `slot`: `1 | 2 | 3`
- `key`: `sharp | warm | natural`
- `name`
- `descriptor`
- `rationale`
- `recommended`
- `outcome_tags`
- `difference_tags`

`plan_steps`

- `id, user_id, plan_variant_id`
- `category`: `hair | makeup | outfit`
- `action`: `keep | adjust`
- `title`
- `summary`
- `details jsonb`
- `position`

`plan_step_groundings`

- `plan_step_id`
- `source_type`: `report_finding | scene_answer | profile_preference | style_rule | feedback_memory`
- `source_id`
- `reason`

约束：

- 每个 PlanSet 恰好三套方案，恰好一套 recommended。
- 每套方案包含 hair、makeup、outfit；没有依据时使用 `keep`，不编造调整。
- 每一步至少一个 grounding。
- 任意两套在发型、妆容、穿搭、配色、正式度中至少两个维度不同。
- 相同 `report_id + scene + brief_hash + planner_version` 使用幂等结果。
- 修改场景答案时创建新 PlanSet，不更新旧 PlanSet。

### 6.7 RenderSpec

页面文案不直接进入图片模型。Planning 输出先经过确定性编译器生成 `render_specs`：

- `id, user_id`
- `plan_variant_id`
- `source_photo_set_id`
- `schema_version`
- `spec jsonb`
- `content_hash`
- `created_at`

`spec` 必须包含：

```json
{
  "identity": {
    "body_asset_id": "uuid",
    "face_asset_id": "uuid",
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
  "hair": {
    "action": "keep|adjust",
    "target": "具体目标",
    "intensity": "low|medium"
  },
  "makeup": {
    "action": "keep|adjust",
    "target": "具体目标",
    "intensity": "low|medium"
  },
  "outfit": {
    "action": "keep|adjust",
    "silhouette": "版型",
    "palette": ["颜色"],
    "layers": ["层次"],
    "avoid": ["禁止项"]
  },
  "output": {
    "mime_type": "image/jpeg",
    "aspect_policy": "preserve_body_source",
    "quality": "high"
  }
}
```

规则：

- 默认禁止 outpaint，不再为补齐脚部主动生成不存在的身体区域。
- 输出比例跟随 body 原图，不固定为 1024×1536。
- 正脸图只用于身份参考，body 图是构图和身体比例基准。
- RenderSpec 由服务端 Schema 校验后才可创建 RenderRun。
- 同一 RenderSpec 可以产生多个 RenderRun，但自身不变。

### 6.8 渲染、候选和发布

`render_heads`

- `plan_variant_id`
- `generation`
- `current_publication_id`
- `version`

这是渲染领域唯一必要的可变指针。

`render_runs`

- `id, user_id`
- `plan_variant_id`
- `render_spec_id`
- `generation`
- `operation_id`
- `candidate_limit`: 默认 1，质量失败后最多 2
- `routing_policy_version`
- `quality_policy_version`
- `outcome`: `published | unavailable | failed | superseded`
- `created_at, finished_at`

`render_candidates`

- `id, user_id`
- `render_run_id`
- `ordinal`: `1 | 2`
- `asset_id`
- `provider_invocation_id`
- `created_at`

`quality_evaluations`

- `id, user_id`
- `subject_type`: `report | plan_set | render_candidate`
- `subject_id`
- `policy_version`
- `decision`: `pass | retry | reject | error`
- `reason_codes`
- `internal_scores jsonb`
- `evaluator_invocation_id`
- `created_at`

`render_publications`

- `id, user_id`
- `plan_variant_id`
- `render_run_id`
- `candidate_id`
- `quality_evaluation_id`
- `generation`
- `created_at`

发布规则：

- Candidate 先存入隔离 COS 路径和 quarantined Asset。
- Gate 通过后，事务内把 Asset 标为 published、创建 Publication，并用 generation/version CAS 更新 RenderHead。
- 旧 generation 即使晚完成也只能标记 superseded，不能替换当前图片。
- Gate 失败且 candidate budget 未耗尽时创建第二候选任务。
- 第二候选仍失败时 RenderRun 失败，文字方案继续可用。
- 无可用同能力路由时返回 `unavailable`，不使用内置图或用户原图冒充。

### 6.9 选择、执行和反馈

`plan_selections`

- `id, user_id`
- `plan_set_id`
- `plan_variant_id`
- `render_publication_id nullable`
- `created_at`

选择是独立资源，不再写 `plans.selected_at`。

`executions`

- `id, user_id`
- `selection_id`
- `state`: `planned | active | completed | abandoned`
- `version`
- `started_at, completed_at`

`execution_steps`

- 创建 Execution 时复制所选方案步骤，形成不可变执行快照。
- 完成状态属于 Execution，不回写 Plan。

反馈拆成两类：

`generation_feedback`

- 关联 `render_run_id / candidate_id / publication_id`
- 标签包括：不像本人、发型不符、妆容不符、穿搭不符、肢体异常、不够自然
- 图片可选
- 用于重新生成、质量统计和路由评估

`execution_feedback`

- 关联 `execution_id / selection_id / plan_set_id`
- 标签包括：容易执行、太正式、太复杂、颜色不喜欢、希望保留
- 实拍图片可选
- 用于生成下一轮 `feedback_memory`

反馈不会直接在线训练。只有用户明确表达、经过规则归一化的偏好才写入 `user_profiles.preferences`；模型负反馈进入离线评测集。

## 7. Operation 与 Task

### 7.1 为什么分两层

- Operation 是客户端看到的业务操作，例如“正在生成报告”。
- Task 是 Worker 执行的内部步骤，例如“调用照片检查模型”。
- 一个 Operation 可以包含多个 Task。
- 客户端不能看到任务 payload、厂商名或内部错误。

### 7.2 Operation

`operations`

- `id, user_id`
- `kind`: `assessment | plan_set | render | execution_feedback`
- `subject_type, subject_id`
- `status`: `accepted | running | retrying | succeeded | failed | cancelled | superseded`
- `progress_bps`
- `stage_code`
- `public_message`
- `error_code`
- `trace_id`，失败终态必填，用于客服和研发追踪；与同步 HTTP 错误的 `request_id` 区分
- `retryable`
- `result_type, result_id`
- `created_at, updated_at, finished_at`

进度由服务端 stage 映射产生，客户端不自行推算。

### 7.3 Task

`tasks`

- `id, user_id, operation_id`
- `type`
- `subject_type, subject_id`
- `subject_generation`
- `payload_version`
- `payload jsonb`
- `dedupe_key`
- `status`: `queued | leased | retry_wait | succeeded | failed | cancelled | superseded`
- `priority`
- `attempt, max_attempts`
- `available_at`
- `lease_token`
- `lease_owner`
- `lease_expires_at`
- `heartbeat_at`
- `cancel_requested_at`
- `progress_bps`
- `stage_code`
- `error_class, error_code`
- `created_at, updated_at, finished_at`

规则：

- `unique(user_id, dedupe_key)`。
- Task 类型不使用数据库 CHECK 枚举；Handler Registry 是注册源。
- Claim 使用 `FOR UPDATE SKIP LOCKED`。
- Worker 必须 heartbeat，不再用固定 `locked_at` 猜僵尸任务。
- 外部 Provider 调用不持有数据库事务。
- 成功提交同时校验 Task lease token 和业务 generation。
- 重试预算只配置一处。
- 错误使用 `transient | throttled | permanent | quality_rejected | superseded` 类型，不通过字符串搜索判断。

## 8. AI 能力路由

能力拆分为：

- `photo_quality_check`
- `photo_identity_consistency`
- `appearance_analysis`
- `report_evidence_verification`
- `plan_set_generation`
- `plan_grounding_verification`
- `render_spec_compilation`
- `full_look_generation`
- `render_quality_evaluation`
- `hair_edit`
- `outfit_diagnosis`
- `purchase_diagnosis`
- `advisor_chat`
- `today_plan`

每个模型配置声明能力元数据：

- 支持的输入模态
- 最大输入图片数
- 是否支持多参考图
- 是否适合身份保持
- 输出格式和分辨率
- 超时
- 成本
- 数据保留策略

Router 流程：

1. 根据 Capability 和 RenderSpec 计算硬需求。
2. 过滤不满足输入数量、身份保持、格式和隐私要求的模型。
3. 在剩余模型中按平衡策略选择主模型。
4. 技术性失败时只切换到同能力模型一次。
5. 质量失败时把明确 reason code 传给第二候选生成。
6. 没有同能力模型时 fail closed。

禁止事项：

- `full_look_generation` 降级到只支持一张底图的模型。
- 生产自动切 Demo。
- 业务代码出现厂商或模型名。
- 将 Provider 的 `quality=high` 等无效参数当成质量保障。
- 不区分文生图和图片编辑参数适用范围。

## 9. 质量门禁

### 9.1 照片输入

硬门禁：

- 文件魔数与 MIME 一致。
- 仅 JPEG/PNG，可解码且无压缩炸弹。
- face、side、body 各一张。
- 单一真人主体，拒绝截图、插画、宠物和多人主导画面。
- 正脸完整、侧脸方向正确、全身照至少覆盖头部到小腿。
- 三张照片属于同一人的概率通过校准阈值。

质量灰区不对用户说“不是本人”，统一提示“照片差异较大，请重新拍摄确认”。

### 9.2 报告

顺序：

1. JSON Schema、枚举和长度校验。
2. 文案红线校验。
3. 每条 finding 必须有照片、区域和 visible observation。
4. 独立视觉核验器判断证据是否支持 observation。
5. 不支持的 finding 删除；少于最低数量时补生成一次。
6. 通过后发布 Report。

### 9.3 方案

门禁：

- 所有步骤有 grounding。
- 最高优先建议至少被一个步骤落实。
- 场景硬约束全部覆盖。
- 不编造衣橱、品牌、价格、材质或身体特征。
- 三套方案差异达标。
- 方案文字与报告不矛盾。
- 失败时最多补生成一次。

### 9.4 效果图

Candidate 生成后依次检查：

1. JPEG、尺寸、解码、单人、无水印、无文字。
2. 正脸与源图身份保持。
3. 头部、躯干、双臂、双腿和手部无明显结构异常。
4. 头脸未裁切，构图符合 RenderSpec。
5. 发型、妆容、穿搭逐项符合 RenderSpec。
6. 未要求的年龄感、肤色、体型、背景和首饰没有明显变化。

身份比较只在内存中短暂计算，不保存 embedding；数据库只记录 evaluator 版本、decision 和 reason code。内部质量分数不进入用户 API。

### 9.5 平衡模式重试

```text
Candidate 1
  ├─ pass → 发布
  ├─ 技术失败 → 同能力 fallback 一次
  └─ 质量失败 → 携带失败原因生成 Candidate 2
                      ├─ pass → 发布
                      └─ fail → 明确失败，保留文字方案
```

每套方案最多两个候选，最多一次模型切换。Task 自身的网络重试不能重新消耗候选预算。

## 10. API 设计

客户端和服务端同步重构，直接重新定义 `/v1`，不创建兼容 `/v2`。

全局约定：

- 成功：`{"data": ...}`。
- 异步创建：`202 {"data": <resource>, "operation": <operation-ref>}`。
- 错误：`{"error":{"code","message","request_id","retryable"}}`。
- 所有创建请求支持 `Idempotency-Key`。
- 可变资源更新使用 `If-Match` / ETag。
- 越权统一 404。
- API 不返回模型厂商、模型名、Provider 临时 URL或内部质量分数。

质量主链端点：

```text
POST   /v1/media/upload-intents
POST   /v1/media/upload-intents/{id}/complete

POST   /v1/assessments
GET    /v1/operations/{id}
GET    /v1/operations?ids=...

GET    /v1/reports/current
GET    /v1/reports/{id}

POST   /v1/plan-sets
GET    /v1/plan-sets/{id}
GET    /v1/plan-sets?report_id=&scene=

POST   /v1/plan-variants/{id}/render-runs
GET    /v1/render-runs/{id}

PUT    /v1/plan-sets/{id}/selection
POST   /v1/selections/{id}/executions
GET    /v1/executions/{id}
POST   /v1/executions/{id}/events

POST   /v1/generation-feedback
POST   /v1/execution-feedback

GET    /v1/home/bootstrap
```

关键响应不让客户端猜状态：

```json
{
  "id": "plan-set-id",
  "state": "planning|rendering|ready|ready_partial|failed",
  "plans": [
    {
      "id": "plan-id",
      "content": {},
      "render": {
        "state": "queued|generating|checking|ready|failed|unavailable",
        "retryable": true,
        "operation_id": "operation-id",
        "media": {
          "asset_id": "asset-id",
          "url": "short-lived-signed-url",
          "url_expires_at": "timestamp",
          "source_kind": "generated_preview",
          "display_label": "风格参考"
        }
      }
    }
  ]
}
```

来源类型固定为：

- `user_original`
- `generated_preview`
- `bundled_reference`
- `demo_example`

展示文案按产品决定：

- 用户原图：`原本`
- 生成图：`风格参考`
- 内置图：`风格参考`
- Demo：`效果示例`

虽然生成图和内置图展示文字一致，服务端 source_kind 必须不同，埋点、客服和缓存不得混用。

## 11. 小程序状态架构

目标目录：

```text
apps/miniapp/src/
  app/
    api/
    auth/
    cache/
    operations/
  features/
    capture/
    assessment/
    report/
    planning/
    execution/
    feedback/
  components/
  pages/
```

规则：

- `pages` 只负责路由和组合，不直接散落 API、Storage 和轮询代码。
- OpenAPI 生成请求/响应类型和 Client，删除手写端点和宽泛 `unknown`。
- 服务端是 Report、PlanSet、Operation 和当前状态的唯一事实源。
- 本地 Storage 只保存 token、轻量 UI 偏好和数据 Schema 版本，不保存 reportId/taskId 作为业务事实。
- 所有轮询使用唯一 React/Taro 包装 `useOperationPolling`。
- 包装内部只创建一个 Polling Handle，页面隐藏停止、恢复立即刷新、卸载清理。
- PlanSet Cache 以 `plan_set_id` 分区；Media Cache 以 `asset_id + url` 分区。
- URL 无效时显示空态，不显示上一张有效图片。
- 当前图严格取自 PlanSet 绑定 Report 的 body Asset。
- 已完成图片后台刷新时保持展示，不清空造成闪屏。
- 生成中、失败、Demo、参考图和真实生成结果都有明确状态组件。
- 错误态提供重试或返回动作。

方案页状态：

```text
loading
  → planning
  → rendering（每套独立进度）
  → ready / ready_partial
  → render_failed（单套可重试）
```

用户可以在某套图片失败时继续查看文字方案和其他合格方案；不能把内置图伪装成失败生成结果。

## 12. 执行、反馈和计费

### 12.1 执行

- 选择 Plan 时创建独立 Selection。
- 创建 Execution 时复制步骤，之后 Plan 不再影响执行清单。
- 清单事件用 `client_event_id` 幂等。
- 执行完成后才进入执行反馈。

### 12.2 反馈

- 生成质量反馈可以在方案详情直接提交，不强制上传实拍。
- 执行反馈可以附实拍，也不因图片失败阻止文字和标签保存。
- 每条反馈关联用户实际看到的 Asset、Publication 和生成版本。
- 页面承诺“下次会保留/调整”的内容必须进入下一次 Planning 输入。

### 12.3 计费

- 发起付费 Operation 时预占额度。
- Report 发布成功后结算分析额度。
- PlanSet 文字发布后结算方案额度。
- Render 只有合格 Candidate 发布后结算对应效果图额度。
- 系统内部的质量补生成不重复扣用户额度。
- Operation 最终失败、取消或 superseded 时自动退款。
- Billing Ledger 使用 Operation/Publication ID 作为幂等引用，不绑定易重试 Task ID。

## 13. 可观测性与生成账本

`provider_invocations`

- `id, user_id`
- `operation_id, task_id, attempt_no`
- `capability`
- `routing_config_version`
- `provider_key, model_key, protocol`
- `request_hash`
- `provider_request_id`
- `status`
- `input_tokens, output_tokens`
- `input_images, output_images`
- `estimated_cost_cny`
- `latency_ms`
- `error_class, error_code`
- `started_at, finished_at`

不得记录：

- API key
- 原始图片字节或 data URL
- 完整用户照片 URL
- 人脸 embedding
- 未脱敏手机号、OpenID

必须记录的指标：

- 每阶段成功率、重试率、最终失败率
- 各能力主模型和 fallback 命中率
- Candidate 2 触发原因
- 质量 Gate 各 reason code 分布
- P50/P95 时延
- 单个已发布结果成本
- 用户“像本人、自然、易执行”反馈率
- 报告到方案、方案到选择、选择到执行转化
- 来源错配、WebP、旧 Task CAS 冲突次数

所有用户可见失败返回 request/trace ID，便于客服定位；客户端不展示具体厂商和模型。

## 14. 安全、隐私与产品红线

- Repository 每次读写必须使用 `resource_id + user_id`；越权统一 404。
- 所有父子表使用复合租户外键。
- COS 默认私有，签名 URL 短期有效。
- 用户原图、生成候选、已发布图和反馈图分开对象前缀。
- 失败候选和未采用候选默认 30 天删除；临时上传 24 小时删除。
- 用户删除数据时，数据库事务写入对象 GC 队列，异步删除所有衍生资产。
- AI Provider 必须满足零训练或明确的数据保留策略。
- 人脸相似度只作为“是否保持同一人”的质量门禁，不用于识别、聚类或用户画像。
- 不存 beauty_score、body_score、年龄分、排名或百分位。
- 不输出医学、健康、族裔、性格和社会身份推断。
- Finding 使用建议优先级，不使用“缺陷严重度”。
- 生成结果保持“风格参考”，Demo 保持“效果示例”，不使用警示红。

## 15. 测试与评测体系

### 15.1 常规 CI

- Domain 纯函数单测。
- Service 用例测试：使用窄 Port fake，覆盖成功、幂等、取消、重试和 CAS。
- Postgres 集成测试：真实 PostgreSQL，覆盖复合外键、事务、SKIP LOCKED、lease 和并发。
- Provider Contract 测试：Mock HTTP 验证输入图片数量、顺序、参数和错误分类。
- HTTP 契约测试：OpenAPI 请求/响应和错误码。
- 小程序状态测试：Operation 生命周期、页面隐藏、缓存分区、图片来源和 URL 失效。
- 端到端测试：三图 → Report → PlanSet → Candidate → Gate → Selection → Execution → Feedback。
- 每个 Bug 必须有能在旧实现失败、在新实现通过的回归测试。

### 15.2 AI 金集

初始规模采用可维护的小型高质量金集：

- 60 个明确授权身份，共 180 张三视图。
- 120 个报告样本。
- 120 个场景 Brief，覆盖六个场景。
- 180 个三方案组。
- 至少 300 张跨模型和失败类型的效果图。
- 64 组前端来源/状态 fixture。
- 30 组反馈后再生成序列。

拆分：

- 开发集 60%
- 验证集 20%
- 锁定发布集 20%
- 同一身份不得跨分区

标注：

- 报告标注允许结论、禁止结论、证据照片和区域。
- 方案标注 grounding、场景符合、可执行性和三套差异。
- 图片分别标注身份保持、人体结构和图文一致。
- 用户本人只评价“像不像我、自然不自然、愿不愿使用”，不评价颜值。

外部模型评测不进入每次 PR CI；在 Staging 和夜间任务中运行，只有锁定集达标的路由版本才能发布。

### 15.3 A/B 规则

- 以用户为单位固定分桶。
- 每次只改变模型、Prompt/RenderSpec 编译器或质量策略中的一个因素。
- 被 Gate 拒绝的候选也必须入账，避免只看成功图。
- 主指标：方案选择、清单完成、实际采用、像本人、容易执行。
- 护栏：身份失败、人体错误、敏感推断、P95、成本和第二候选率。
- 新版本先离线通过，再按 5% → 25% → 50% → 100% 放量。

## 16. 删除和重建

直接删除：

- 现有 20 个历史迁移，改为一个全新 `001_baseline.sql`。
- `internal/domain/domain.go` 类型总仓。
- `repository.Repository` 巨型接口。
- 全能 `service.Service` 和 `ProviderOptions` 参数袋。
- `analyses.media_ids[]`。
- 可变 `plans + selected_at + generated_image_url`。
- `PUT plans` 同时创建、刷新和生成图片的混合语义。
- `generation_status + look_task.status` 双状态。
- 只按 plan ID 回写生成结果的接口。
- `LatestTasksByRef(refKey string)` 动态引用。
- `locked_at + 固定十分钟` 僵尸任务判断。
- 多处重复维护的重试次数。
- 根据错误字符串判断 quota/401/格式错误。
- 生产 Demo fallback 和 full-look 单图 fallback。
- 服务端发布 WebP 的路径。
- 前端根据 URL 或 Provider 前缀猜图片来源。
- ExampleImage 跨资源 pinned URL 回退。
- 页面裸调用框架中立 Polling Controller。
- 本地 Storage 中作为业务事实的 reportId/taskId。
- 反馈“承诺会学习”但下游从不读取的逻辑。

保留并重构：

- 微信、短信、Apple 登录和 Session 思路。
- COS、本地存储 Adapter。
- AI 能力路由思想和已有协议 Adapter 测试。
- PostgreSQL `SKIP LOCKED` 任务队列思想。
- Billing Ledger、退款和微信虚拟支付 Adapter。
- `@zsm/design` Token 单源。
- `@zsm/core` 的媒体真实性原则和平台无关轮询控制器。
- 首页 BFF 思路。
- Today、Wardrobe、Advisor、Hair、Outfit、Purchase 功能，但只能通过新 Report/Plan Reader 接入，不直接耦合质量核心表。

数据库重置流程：

1. 停止 API 和 Worker。
2. 删除并重建数据库或开发/预发 Volume。
3. 运行新的 baseline migration。
4. 部署 API 和独立 Worker。
5. 发布同步更新的小程序。
6. 不运行旧数据回填和兼容读取。

## 17. 实施阶段

### 阶段 0：冻结设计与契约

交付：

- 本设计评审通过。
- 新 OpenAPI `/v1` 契约。
- ADR 清单。
- 新 baseline schema。
- 质量指标和金集规范。

验收：

- 所有状态、错误、实体归属和异步操作都有唯一解释。
- 没有旧字段兼容项。

### 阶段 1：基础设施骨架

交付：

- 窄 Service/Port 结构。
- Media Asset 和 Upload Intent。
- Operation + Task lease/heartbeat/CAS。
- Provider Invocation 账本。
- 独立 Worker 部署。
- Idempotency 和 Billing reservation。

验收：

- 并发、租约过期、旧 Worker、重复请求集成测试通过。
- 任意任务结果都可通过 trace ID 还原。

### 阶段 2：建档与可信报告

交付：

- PhotoSet。
- 输入质量和同人检查。
- Analysis Run。
- Report/Finding 新模型。
- 证据核验 Gate。
- 新报告页 API 和小程序。

验收：

- 照片角色/归属 100%。
- 报告证据完整 100%，抽检支持率 ≥95%。
- 文案红线为 0。

### 阶段 3：方案系统

交付：

- 统一 PlanSet Generator。
- 六场景 Brief Schema。
- 三方案差异和 Grounding Gate。
- RenderSpec Compiler。
- 新方案列表和详情文字态。

验收：

- 场景方案全部读取 Report。
- Grounding 100%。
- 三套方案差异达标。

### 阶段 4：本人效果图与质量门禁

交付：

- RenderRun/Candidate/Publication。
- 多图能力路由。
- 身份、人体、构图、图文 Gate。
- Candidate 2 平衡策略。
- 生成图统一 JPEG。
- 方案页渐进式效果图状态。

验收：

- 单图 fallback 为 0。
- 严重坏图发布率 <1%。
- 像本人通过率 ≥90%。
- 第一张图 P95 ≤120 秒。

### 阶段 5：选择、执行、反馈和计费闭环

交付：

- Selection/Execution Snapshot。
- 幂等执行事件。
- Generation Feedback 和 Execution Feedback。
- Preference Memory。
- 按发布成功结算、失败自动退款。

验收：

- 反馈链路关联完整率 100%。
- 明确偏好在下一次方案中生效。
- 内部补生成不重复扣费。

### 阶段 6：首页和外围能力接入

交付：

- 新 Home Read Model。
- Today、Wardrobe、Advisor、Hair、Outfit、Purchase 通过窄 Reader 接入。
- Share Snapshot 使用已发布不可变 Asset。

验收：

- 外围模块不能写 Report、PlanSet 和 Render 表。
- 主闭环质量指标不因外围接入下降。

### 阶段 7：删除旧系统并发布

交付：

- 删除旧 Domain/Service/Repository/Provider 调用路径。
- 删除旧 OpenAPI、旧 migrations 和旧前端状态逻辑。
- 全量静态检查、单测、集成测试、E2E 和锁定金集。
- 数据库清空并按 baseline 上线。

验收：

- 仓库不存在新旧双路径。
- 生产只有独立 Worker。
- 所有提交前门禁通过。

## 18. 发布与停止条件

放量顺序：

```text
内部授权样本
  → Staging 金集
  → 5%
  → 25%
  → 50%
  → 100%
```

每档至少满足预定样本量，不以单日主观观察代替指标。

立即停止放量：

- 出现 Demo、内置图、用户图或生成图来源误标。
- 出现颜值/身材评分或敏感属性推断。
- 严重身份漂移或人体错误超过阈值。
- 旧 generation 成功覆盖新 generation。
- P95 持续超过目标两倍。
- Provider 出现未经批准的数据保留行为。

## 19. 架构决策记录

- ADR-001：模块化单体，不拆微服务。
- ADR-002：保留 `httpapi → service → repository/provider/storage`，删除巨型横向接口。
- ADR-003：AI 产物不可变，重新生成创建新资源。
- ADR-004：客户端 Operation 与内部 Task 分离。
- ADR-005：PostgreSQL Task Queue + lease + heartbeat + 双 CAS。
- ADR-006：RenderSpec 隔离页面文案与图片模型输入。
- ADR-007：候选先质检后发布。
- ADR-008：身份敏感能力只允许同能力 fallback。
- ADR-009：OpenAPI 是传输契约单源，生成前端类型和 Client。
- ADR-010：COS 只保存不可变对象，数据库只保存 object key。
- ADR-011：来源由服务端强类型表达，客户端不推断。
- ADR-012：选择和执行是独立资源，不修改方案。
- ADR-013：反馈先形成显式偏好和离线评测，不在线训练。
- ADR-014：不采用事件溯源；只对 AI 产物和执行事件使用 append-only。
- ADR-015：不做历史数据和逻辑兼容，使用全新 baseline 数据库。

## 20. 主要风险与控制

### 20.1 质量 Gate 误拒合格图片

控制：

- 先影子运行收集分布。
- 阈值由授权金集校准。
- 灰区进入第二候选，不直接永久失败。
- 保留人审抽样。

### 20.2 三套效果图成本过高

控制：

- 三套第一候选有界并行。
- 第二候选只为 Gate 失败的单套生成。
- 每用户和每模型设置并发配额。
- 以“每个已发布结果成本”而不是“每次调用成本”优化路由。

### 20.3 任务系统过度复杂

控制：

- 只实现 PostgreSQL lease、heartbeat、CAS 和 Handler Registry。
- 不引入 DAG 引擎；后继 Task 由 Handler 成功事务显式创建。
- Operation 进度使用固定 Stage，不从任意 Task 推导。

### 20.4 模块拆分变成样板代码

控制：

- 只拆质量主链五个 Service 域。
- 一个领域允许多个 Use Case 共用 Service。
- 只有存在替换或测试价值的依赖才定义接口。
- 不建立泛型 BaseRepository、通用事件总线或空目录层级。

### 20.5 外围功能拖慢主闭环

控制：

- 阶段 2–5 不改外围业务能力。
- 阶段 6 仅通过 Reader 接入。
- 外围模块不能成为 Report/Plan/Render 发布的前置依赖。

## 21. 设计自检

- 业务闭环从照片到反馈完整。
- 报告、方案和效果图均有独立质量门禁。
- 状态不存在“领域表与 Task 双写同一含义”。
- 不可变产物和少量可变指针边界明确。
- 历史数据、API 和本地缓存兼容明确排除。
- 没有引入微服务、事件溯源或通用工作流引擎。
- 所有 AI fallback 都受能力等价约束。
- 产品红线、图片真实性和小程序 JPEG 约束已覆盖。
- 每个实施阶段都有独立交付和验收标准。

## 22. 后续文档拆分

本设计批准后，再编写以下可执行计划：

1. 数据库、Operation/Task、Media 基础计划。
2. Assessment/Report 计划。
3. Planning/RenderSpec 计划。
4. Rendering/Quality 计划。
5. Miniapp 主闭环计划。
6. Execution/Feedback/Billing 计划。
7. 外围能力接入与旧系统删除计划。

每份计划独立测试、独立评审、独立提交，避免一个超大实现任务跨越整个系统。
