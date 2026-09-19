# 每日内容 · 小程序 + 服务端技术实现方案

> 前置结论（已对齐，不再重复论证）：
> ① 人工编辑生产**原子知识事实**，LLM 基于事实组装成每日建议（grounding 生成，非自由创作）；
> ② V1 用**实时生成**（用户触发 + 两段式 API + 揭晓动画），凌晨批任务推迟到有规模后；
> ③ 生成与推送之间由**自动校验**把关（黑名单 + 事实追溯），人工抽检取消；
> ④ 收藏 = 内容引用 + 完整副本 + 多态素材 + 生命周期状态（schema 见 `daily-collection-schema.md`）。

---

## 1. 系统总览

```
小程序                          服务端（Go）
──────                          ──────────
打开今日/首页
  │ POST /daily/prepare ──────→ 选品引擎（规则，毫秒级）
  │   ← { scenario, pickToken }    知识检索 + 基因匹配 + 历史去重 + 补薄格
  │ 播对应主题等待动画（循环段）
  │ POST /daily/generate ─────→ 生成管线
  │                                取回选品快照 → 组装 prompt
  │                                → AI 路由（capability: daily_content）
  │                                → 自动校验（黑名单/事实追溯/结构）
  │   ← 200 { source, content }    ├ 通过 → 落库 daily_content
  │ 动画落位 → 海报呈现            └ 失败/超时 → 兜底池（source:'fallback'）
  │ [收下] POST /daily/collection → 固化副本 + 素材引用
  │ 手册页 GET /daily/collection  → 按 category 分组返回
```

两条铁律：

- **generate 永远返回 200 + 内容**：`source: 'generated' | 'fallback'`。降级对客户端透明，客户端没有"生成失败"的错误分支，只有"内容来源"字段。鉴权等真正的错误才返回 4xx/5xx。
- **同一用户同一天只生成一次**：幂等键 `(user_id, gen_date)`，当天再次打开直接命中已生成内容。

---

## 2. 服务端设计

### 2.1 模块落位（对齐现有分层）

| 现有层 | 新增文件 | 职责 |
|---|---|---|
| `internal/domain/` | `daily.go` | KnowledgeFact / DailyContent / Collection 及枚举 |
| `internal/repository/` | `daily_knowledge.go` `daily_content.go` `daily_collection.go` | 三张表的数据访问 |
| `internal/service/daily/` | `prepare.go` `generate.go` `validate.go` `fallback.go` `collection.go` | 选品、生成管线、校验、降级、收藏 |
| `internal/httpapi/` | `daily.go` | 4 个 handler |
| `internal/database/migrations/` | `00XX_daily.sql` | 建表 |
| `contracts/openapi.yaml` | 追加 | 契约先行 |
| AI 路由表 | 加 `daily_content` capability | 模型主备/超时/预算全在路由配置 |

### 2.2 数据模型（4 张表）

```sql
-- ① 知识事实（人工编辑生产、顾问确认——整个系统的"原料"）
create table knowledge_fact (
  id          bigserial primary key,
  domain      text not null,        -- color/fit/proportion/fabric/occasion/howto
  fact        text not null,        -- "光泽面料反射光，模糊轮廓边界，放大视觉体积"
  boundary    text not null,        -- 适用边界："小骨架大面积光泽显壮；配饰级安全"
  gene_fit    jsonb not null default '{}'::jsonb,  -- 对哪些基因成立（复用 GeneCondition 结构）
  season      text[],
  source      text not null,        -- 出处：顾问确认 / 权威资料 / 编辑整理
  reviewed    boolean not null default false,      -- 未确认不参与生成
  created_at  timestamptz not null default now()
);
create index knowledge_fact_domain_idx on knowledge_fact (domain, reviewed);

-- ② 生成产物（每天每用户一条；兜底池条目也是同结构的行，user_id 为空表示公共兜底）
create table daily_content (
  id           bigserial primary key,
  user_id      bigint,                             -- null = 公共兜底池条目
  gen_date     date   not null,
  category     text   not null,
  topic        text   not null,
  lead         text   not null,
  fit_text     text   not null,
  why          text   not null,
  visual       jsonb  not null,                     -- ContentVisual，前端现有结构原样
  fact_ids     bigint[] not null,                   -- 事实追溯：本条用了哪些知识
  source       text   not null default 'generated', -- generated/fallback
  model_key    text,                                -- 生成用的模型（审计用）
  created_at   timestamptz not null default now(),
  unique (user_id, gen_date)
);
create index daily_content_user_idx on daily_content (user_id, gen_date desc);

-- ③ 生成审计（每次 LLM 调用一条，评估与回溯用）
create table generation_run (
  id           bigserial primary key,
  user_id      bigint,
  gen_date     date,
  fact_ids     bigint[],
  prompt_hash  text,
  output       jsonb,
  validation   jsonb,      -- {blacklist:[], factTrace:ok, structure:ok}
  outcome      text,       -- accepted/rejected/fallback
  model_key    text,
  latency_ms   int,
  cost_micro   bigint,     -- 复用 AI 路由现有成本记账
  created_at   timestamptz not null default now()
);

-- ④ 收藏（结构详见 daily-collection-schema.md，此处不重复）
-- daily_collection (content_snapshot jsonb, assets jsonb, status, note, ...)
```

`mock.ts` 的 19 条内容做两用：**反解成种子知识事实**（迁移种子脚本入库）+ **兜底池种子数据**。

### 2.3 API 契约

```
POST /api/v1/daily/prepare
  → 200 {
      "genDate": "2026-09-20",
      "pickToken": "< opaque >",
      "scenario": "color" | "fit" | ... | "outfit" | "fallback",
      "cacheHit": false          # true 表示当天已生成，客户端直接拉内容不播动画
    }

POST /api/v1/daily/generate   { "pickToken": "..." }
  → 200 {                       # 永远 200，见铁律
      "source": "generated" | "fallback",
      "content": {               # 结构与现有 DailyContent 对齐，前端类型零改动
        "id","type","topic","lead","fitText","why",
        "visual": { "modality", "spec", "alt" },
        "asset": "color",        # CollectionCategory
        "dedupeKey"
      }
    }

POST /api/v1/daily/collection          { contentId, note? }
DELETE /api/v1/daily/collection/:id
PATCH /api/v1/daily/collection/:id     { status?: 'tried'|'kept', note? }
GET  /api/v1/daily/collection          ?category=&limit=   # 手册按格拉取
GET  /api/v1/daily/collection/stats    # 7 个格子计数（选品补薄格也用它）
```

**pickToken**：prepare 把选品快照（知识条目 id、语境、历史摘要）暂存服务端 5 分钟（内存 LRU，多实例换 Redis），generate 凭 token 取回——保证两段看到同一个选品。不用签名回传：选品结果含用户历史摘要，不该下发给客户端再带回来。

### 2.4 生成管线（service/daily 核心）

**prepare（规则，目标 < 50ms）**

```
1. 幂等检查：daily_content 已有 (user, today) → cacheHit=true 直接返回
2. 知识检索：domain 候选集
     = reviewed=true
     × gene_fit 与该用户基因匹配
     × 季节/天气条件
     − seen（该用户近 30 天已推送的 fact_ids）
   权重：× (1 + 该 domain 在手册中的缺口)   ← 补最薄的格
3. 抽 3~5 条事实 + 1 个选题角度（角度枚举轮换，防同质化）
4. 组装选品快照 → 存 pickToken
```

V1 知识检索用**纯 SQL 结构化过滤**，不做向量。事实量 < 1000 时足够；V2 知识库大了再上 pgvector。

**generate（LLM，目标 P50 3s / P95 5s）**

```
1. 取选品快照；已生成 → 直接返回已有内容（幂等）
2. prompt 组装（见下）
3. AI 路由调用：capability=daily_content，结构化输出 json_object
4. 自动校验（§4）→ 通过：落库返回；失败：一次带修正提示的重试，再失败走兜底
5. LLM 超时 5s / 错误 → 直接兜底（不重试挤占用户等待时间）
```

**Prompt 骨架**（system + user 两段，知识条目逐条注入）：

```
[system]
你是形象顾问的内容编辑。基于给定的【知识事实】为用户组装今日一条建议。
规则：
- 每个事实性断言必须来自【知识事实】，禁止引入其中没有的品牌/价格/材质/商品
- 语气克制、肯定式，不评价身材外貌，不打分，不制造焦虑
- 禁用词：颜值、身材分、缺陷、百分位、显胖、显瘦之类评判（见禁则表）
- 输出 JSON：{ topic(≤12字), lead(≤40字), fit(≤60字，结合用户特征), why(≤40字), visual_hint }

[user]
知识事实：
  1. {fact} （边界：{boundary}，domain: {domain}）
  2. ...
用户特征：{肤色冷调 · 骨架小 · 肩窄 · 158cm}
今天：{18° 多云 · 周日 · 秋}
历史摘要：{近 7 天已推: ...；用户留下过的偏好: ...}
选题角度：{本轮轮换的角度}
```

`visual_hint` 映射到 `visual.spec`（swatch/compare/diagram 的参数），让每条生成内容天然带程序化视觉；`photo/video` 素材类内容 V1 只从兜底池出（素材未到位）。

### 2.5 AI 路由接入

新增 capability `daily_content`：

```json
{ "routes": { "daily_content": { "primary": "qwen-flash", "fallback": "qwen-plus" } } }
```

- 主选轻量模型（成本 ~$0.002/次），备选略强——**备选只在主模型超时/5xx 时用**，校验失败不换模型重试（失败原因是内容不是模型）。
- 每次调用走现有成本记账（cost_micro → generation_run）。
- 单用户每日生成预算硬上限 1 次（超限直接兜底），防刷。

### 2.6 限流、超时、降级链

| 层 | 值 | 说明 |
|---|---|---|
| generate 总超时 | 6s | 客户端动画硬超时对齐 |
| LLM 调用超时 | 5s | 留 1s 给校验与落库 |
| prepare 超时 | 2s | 纯 SQL，超了说明 DB 有问题，直接兜底 |
| 全局并发 | AI 路由既有机制 | 高峰 QPS 估算 < 6（10万 DAU），远低于配额 |
| 用户级 | 1 次/天 | 幂等键硬约束 |

降级链（每一级对客户端都是 `source:'fallback'`）：
```
校验失败(重试1次仍败) ─┐
LLM 超时/故障         ─┼→ 兜底池（user_id=null 的 daily_content，规则选品）
知识库为空/全未审      ─┘        └ 再失败 → 静态问候（永不空屏）
```

### 2.7 规模化演进（V2，现在不做）

用户量起来后加凌晨 warming：调度器分批写 `generation_task`，worker 池消费同一套 generate 逻辑（`FOR UPDATE SKIP LOCKED`，幂等键复用）。**API 与生成管线零改动**，只是把"用户触发生成"提前到"凌晨预生成"，白天打开全是 cacheHit。高频用户的当日内容提前就绪，高峰 QPS 顺势挪到凌晨。

---

## 3. 小程序端设计

### 3.1 页面状态机（今日页 / 首页共用）

```
进入
 → [loading] 调 prepare；同时按返回的 scenario 播等待动画循环段
    （cacheHit=true 则跳过动画直接拉内容）
 → [generating] 调 generate；动画继续循环（≤6s 硬超时）
    ├─ 200 → [settling] 动画落位（1.2s）→ [content] 海报呈现
    ├─ 200 fallback → 动画收尾 → 兜底海报（同结构，角标"今日精选"）
    └─ 网络错误 → [offline] 读本地缓存（最近一次内容）→ 无缓存再走静态问候
[content] 内：
    [收下] → POST collection（乐观更新 + 本地留底）→ toast「已收进 · {category}」
    重复进入 → cacheHit，直接 [content]
```

等待动画的实现载体（序列帧 / Lottie / CSS）**不锁死**，客户端只依赖两个契约：`scenario` 枚举决定播哪套；`settle` 时机由 generate 返回触发。序列帧规范建议：每场景两段（loop 可循环段 + settle 收尾段），750px 宽、透明底、12fps、WebP/APNG；若用 Lottie 则可参数化主色，体积更小且能复用海报配色变量。

### 3.2 现有代码改造点

| 现有模块 | 改造 | 保留 |
|---|---|---|
| `features/daily/use-daily-pick.ts` | 选品逻辑移到服务端；hook 改为调 prepare/generate 两段式，输出状态机信号（phase: waiting/settling/content/fallback） | 对外暴露的字段形状尽量不变（pick/bucketName/save），下游页面改动小 |
| `features/daily/saves.ts` | 本地 storage 降级为**离线缓存 + 乐观更新留底**；真源是 API。字段已对齐 daily_collection（category/snapshot/status） | readSaves/addSave 等 API 签名不变，内部改为"写穿"：先本地后网络 |
| `components/daily-poster/` | 数据源从 mock `DailyContent` 换成 API content——结构本就对齐，**组件基本零改动**；增加 `scenario` prop 驱动等待动画 | 视觉渲染（poster-visual 四类图形）、承载块、文案 clamp 全部保留 |
| `pages/today` | 接状态机；等待动画在这页完整呈现，首页可只用缩略形态 | |
| `packages/life/pages/handbook` | 数据源 `readSaves()` → `GET /collection`；**改读 snapshot 副本**（不再反查 MOCK_CONTENTS） | 分组逻辑、BUCKET_ORDER 已是新分类 |
| `packages/core/src/daily/mock.ts` | 退役路径：19 条 → 种子知识事实 + 兜底池种子（服务端迁移脚本）；core 里的类型/选品纯函数保留（兜底池复用 pickDaily） | |

### 3.3 离线与一致性

- 收藏操作失败不阻塞 UI：本地先记，恢复后重放（本地记录带 `pendingSync` 标记）。
- 服务端以 `(user_id, content_key)` 幂等去重，重放安全。
- 手册页拉取失败 → 显示本地缓存并标注"离线"。

---

## 4. 红线的执行方式（自动校验明细）

`validate.go` 三道闸，全部毫秒级：

1. **黑名单**：输出命中禁则词（评分/身材/缺陷/百分位/品牌/价格/竞品）→ 拒绝。禁则词表进配置，可热更。
2. **事实追溯**：输出的每条断言回链 fact_ids——V1 用简化法：生成时要求模型对每段输出标注引用的知识条目序号，校验"引用序号都在输入集内且非空"。完全语义级核查留给 eval 流程（`internal/eval` 已有基建）离线抽评。
3. **结构**：长度上限、必填字段、JSON 合法。

拦截 → 带错误原因重试一次 → 仍败走兜底 + `generation_run.outcome='rejected'` 留痕。**禁则词表与现有文案校验器（ValidateReportDraft）共用词源，两处不分裂。**

---

## 5. 分期

| 期 | 内容 | 出口标准 |
|---|---|---|
| **V1** | 4 张表 + 两段式 API + 生成管线 + 校验 + 兜底池；小程序接 API、状态机、揭晓动画；手册页读副本 | 真机全链路：打开→动画→内容→收下→手册可见；断网/超时走兜底不空屏 |
| **V1.5** | 知识库扩容（顾问确认流程上线）、generation_run 评估看板、kept/tried 状态 UI、选题角度扩充 | kept 率开始反馈进知识库迭代 |
| **V2** | 凌晨 warming 批任务、pgvector 知识检索、素材库扩充后开启 photo/video 生成内容 | 高峰 QPS 不再依赖实时 LLM 全量 |

## 6. 依赖与风险

| 依赖/风险 | 对策 |
|---|---|
| 知识事实的**专业确认**是第一瓶颈（没有事实就没有生成） | 先用现有 19 条反解出种子事实（~25 条）保 V1 跑通；顾问确认流程与扩容并行启动 |
| LLM 输出同质化 | 选题角度枚举轮换 + seen 去重 + 手册补薄格；generation_run 里监控输出聚类 |
| 实时生成的 P95 延迟 | 6s 硬超时兜底；兜底体验必须做到与正常无差（同结构同视觉） |
| 动画工作量（4~5 场景） | 先做兜底 + 颜色两个场景上线，其余按 scenario 逐步补，未覆盖场景回落兜底动画 |
| AI 配额被对话挤占 | daily_content 独立路由条目，独立预算 |
