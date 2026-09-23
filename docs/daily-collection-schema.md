# 每日内容收藏（daily_collection）

## 它到底是什么

**一条建议，加上分类、素材、以及用户后来发生的事。**

不是"书签"。拆开看：

| 部分 | 是什么 | 例子 |
|---|---|---|
| **建议** | 那天推给他的那条内容（**完整副本**） | 「驼色是个陷阱 · 别贴脸穿」 |
| **分类** | 归到手册的哪一格 | 颜色 |
| **素材** | 这条建议带着的东西 | 4 个色值 / 一件衣橱里的大衣 / 一段视频 |
| **其它** | 用户侧产生的一切 | 试过了、留下了、自己的备注 |

## 设计要点

### 1. 引用与副本并存

- **引用**（`content_id` / `content_key`）—— 去重、反查、统计。
- **完整副本**（`contentSnapshot`）—— 展示用。

**副本是必需的**：内容池会迭代（文案改、素材换、甚至下架），
但用户手册里的东西不能跟着变。**他收下的是"当时的那条建议"。**

副本里存的是**已按该用户基因渲染后的文本**（`fit` 是函数，落库前必须先渲染成字符串）。

### 2. 素材用多态数组，不用固定字段

这是扩展性的核心。今天是色值和衣橱单品，明天可能是商品、方案、生成图。

```ts
type CollectionAsset =
  | { kind: 'colors';          colors: string[] }
  | { kind: 'media';           media_id: string; role: 'cover' | 'figure' | 'video' }
  | { kind: 'wardrobe_item';   item_id: string }    // 衣橱里的一件
  | { kind: 'wardrobe_outfit'; outfit_id: string }  // 穿过的一身
  | { kind: 'plan';            plan_id: string }    // 生成过的方案
  | { kind: 'product';         source: string; ref: string }
```

新增一种素材 = 加一个 union 分支，**不动表**。

### 3. 状态是一条建议的生命周期

`saved → tried → kept`（收下 → 试过 → 留下了）。

**"留下的"才是产品真正知道的东西**——这是资产兑现的方式。
不做连续天数、不做断签提醒。

### 4. 保留收下时的语境

`context` 记录当天天气、场合；`geneSnapshot` 记录当时的形象基因。
用途：回看"我当时为什么收这条"，以及判断哪些建议只在特定条件下成立。

---

## 分类

**命名原则：去掉"库"字，用两个字的常用词。** 不用"色卡/廓形"这类专业词。

| key | 名称 | 收什么 |
|---|---|---|
| `color` | 颜色 | 适合你的颜色、怎么配色 |
| `fit` | 版型 | 什么版型适合你的骨架 |
| `proportion` | 比例 | 怎么看着更高更顺 |
| `fabric` | 面料 | 厚度、垂感、光泽 |
| `occasion` | 场合 | 面试、约会穿什么 |
| `howto` | 技巧 | 卷袖、塞衣角这类动作 |
| `outfit` | 搭配 | 完整的一身 |
| `hair` | 发型 | 发型方向（2026-09-21 扩充） |
| `makeup` | 妆容 | 妆容要点（2026-09-21 扩充） |
| `accessory` | 配饰 | 鞋包首饰的选法与呼应（2026-09-21 扩充） |
| `general` | 综合 | 归不进其余各格的内容 |

**判归规则：按内容的决策变量归格，不按目的。** 选什么（颜色/版型/面料/发型/妆容/配饰）、怎么组合（搭配/比例）、何时何地（场合）、怎么做（技巧）。主体是头发就归发型——即使目的是修脸型；脸上用的颜色归妆容——衣服颜色靠近脸的归颜色；包「选什么体量」归配饰——「背在哪」归比例。

**分类默认由内容类型推导**，少数特例才手工覆盖——逐条手工分配必出错。

按钮统一用「**收下**」（分类名里"场合/技巧"组合成"收进我的场合"会拗口），
分类在手册里呈现，toast 提示"已收进 · 颜色"。

---

## 数据模型

```ts
export type CollectionCategory =
  | 'color' | 'fit' | 'proportion' | 'fabric' | 'occasion' | 'howto' | 'outfit'
  | 'hair' | 'makeup' | 'accessory' | 'general'

export type CollectionStatus = 'saved' | 'tried' | 'kept'

/** 内容副本：收下那一刻固化，内容池后续迭代不影响 */
export interface ContentSnapshot {
  id: string
  type: string
  topic: string
  lead: string
  /** 已按该用户形象基因渲染后的适配说明（不是函数） */
  fitText: string
  why: string
  visual: unknown      // ContentVisual 原样存
  category: CollectionCategory
}

export interface SavedContext {
  temperature?: number
  condition?: string
  dayType?: string
  season?: string
}

export interface CollectionAssetColors { kind: 'colors'; colors: string[] }
export interface CollectionAssetMedia {
  kind: 'media'
  media_id: string
  role: 'cover' | 'figure' | 'video'
}
export interface CollectionAssetItem   { kind: 'wardrobe_item';   item_id: string }
export interface CollectionAssetOutfit { kind: 'wardrobe_outfit'; outfit_id: string }
export interface CollectionAssetPlan   { kind: 'plan';            plan_id: string }
export interface CollectionAssetProduct {
  kind: 'product'
  source: string
  ref: string
}
export type CollectionAsset =
  | CollectionAssetColors
  | CollectionAssetMedia
  | CollectionAssetItem
  | CollectionAssetOutfit
  | CollectionAssetPlan
  | CollectionAssetProduct

export interface DailyCollection {
  id: string
  userId: string

  // ---- 内容引用 ----
  contentId: string
  contentKey: string

  // ---- 完整副本（展示用，内容迭代不影响）----
  contentSnapshot: ContentSnapshot

  // ---- 冗余展示字段（列表不必解析 snapshot）----
  title: string
  summary: string
  category: CollectionCategory

  // ---- 素材（多态数组）----
  assets: CollectionAsset[]

  // ---- 收下时的语境 ----
  context: SavedContext
  geneSnapshot?: Record<string, string>

  // ---- 用户侧 ----
  status: CollectionStatus
  note: string
  triedAt?: string
  keptAt?: string

  savedAt: string
  updatedAt: string
}
```

## 表结构

```sql
create table daily_collection (
  id               bigserial primary key,
  user_id          bigint       not null,

  -- 内容引用
  content_id       text         not null,
  content_key      text         not null,

  -- 完整副本（用户收下的那一刻固化）
  content_snapshot jsonb        not null,

  -- 冗余展示字段
  title            text         not null,
  summary          text         not null default '',
  category         text         not null,

  -- 素材 / 语境 / 用户侧
  assets           jsonb        not null default '[]'::jsonb,
  context          jsonb        not null default '{}'::jsonb,
  gene_snapshot    jsonb,
  status           text         not null default 'saved',
  note             text         not null default '',

  saved_at         timestamptz  not null default now(),
  updated_at       timestamptz  not null default now(),

  constraint daily_collection_user_content_key unique (user_id, content_key),
  constraint daily_collection_status_check
    check (status in ('saved','tried','kept'))
);

-- 打开手册某一格
create index daily_collection_user_category_idx on daily_collection (user_id, category, saved_at desc);
-- 手册时间线
create index daily_collection_user_saved_idx    on daily_collection (user_id, saved_at desc);
```

> `content_snapshot` 是 jsonb，不建索引——它只用于详情展示。
> 检索一律走 `category` / `saved_at` / `content_key`。

## 查询场景

| 场景 | 走哪个索引 |
|---|---|
| 打开手册某一格（"我的颜色"） | `(user_id, category, saved_at desc)` |
| 手册时间线 | `(user_id, saved_at desc)` |
| 选品去重 / 已收判断 | `unique (user_id, content_key)` |
| 各格子条数（补最薄的格） | 上面两个索引覆盖，或物化计数 |

## 扩展点（都不需要改表）

- 新增素材类型 → `CollectionAsset` 加 union 分支
- 新增分类 → `category` 加枚举值（配一个迁移默认值）
- 新增用户侧维度 → 塞 `context`，或加 jsonb 列
- 新增状态 → 改 check 约束（低频）

## 从本地迁移到服务端

当前客户端是本地 storage：

```ts
interface DailySave { key: string; asset: string; savedAt: string }
```

| 本地 | 服务端 |
|---|---|
| `key` | `content_key` |
| `asset` | `category`（做一次枚举映射） |
| `savedAt` | `saved_at` |
| — | `content_snapshot` 由客户端上报时一并提交 |

映射完直接上报，**用户无感**。本地记录保留为离线兜底。

## 与现有能力的接缝

| 素材 kind | 来源 |
|---|---|
| `wardrobe_item` / `wardrobe_outfit` | 已有 `WardrobeItem` / `WardrobeOutfit` |
| `plan` | 已有 `TodayPlan` / `PlanSet` |
| `media` | 已有 `SourceImage`（带 `source_kind` 标识） |
| `colors` | 由 `DailyContent.visual.spec` 的 swatch 项派生 |

素材一律**引用已有实体的 id**，不复制数据；
只有文字内容（副本）才复制——因为它必须固化。
