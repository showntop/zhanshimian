// 每日内容（Daily Content）契约。
//
// 定位：首页顶部「今天这一条」的内容源。它不依赖用户当天穿什么，
// 只依赖两类输入 —— 形象基因（稳定）+ 今日语境（每日变）。
// 这是「不臆想」的结构性保证：内容池里没有任何一条可以要求知道用户的当日穿搭。
//
// 与既有能力的关系：
//   Report      → 降级为 StyleGene 的来源（稳定特征），不再直接当内容源
//   TodayContext → 天气/日期/日程，已存在，直接复用
//   WardrobeItem/Outfit → 资产与穿着记录，阶段 3 接入
//   TodayPlan   → 本内容的「引导 action」产出的结果，不是并列模式

// ---------- 形象基因（稳定层） ----------
// 由服务端从 Report 派生；非用户当日状态，因此可用于每日推送而不构成臆想。
// 注意：这里只有「特征描述」，不含任何评分、体型判断或敏感属性。

export type SkinTone = 'cool' | 'warm' | 'neutral'
export type Contrast = 'high' | 'medium' | 'low'
export type ShoulderWidth = 'narrow' | 'medium' | 'broad'
export type FrameScale = 'small' | 'medium' | 'large'
export type VerticalRatio = 'long_torso' | 'balanced' | 'long_leg'
export type NeckLength = 'short' | 'medium' | 'long'
export type FaceShape = 'round' | 'oval' | 'square' | 'heart'
export type HeightBand = 'petite' | 'average' | 'tall'

export interface StyleGene {
  skinTone: SkinTone
  contrast: Contrast
  shoulder: ShoulderWidth
  frame: FrameScale
  vertical: VerticalRatio
  neck: NeckLength
  face: FaceShape
  heightBand: HeightBand
}

// ---------- 内容类型与载体 ----------
// 载体由内容类型决定，不是每条都配全套（那会变成成本黑洞）。

export type ContentType =
  | 'color'      // 色彩 / 色温
  | 'silhouette' // 版型 / 廓形
  | 'proportion' // 比例 / 长度 / 位置
  | 'fabric'     // 面料 / 厚度 / 光泽
  | 'item'       // 单品 / 趋势
  | 'occasion'   // 场合
  | 'howto'      // 动作教程
  | 'general'    // 综合：归不进七格的内容（服务端自报分类的兜底格）

export type Modality =
  | 'swatch'    // 色卡 / 色板（程序化，零成本）
  | 'diagram'   // 示意图 / 标注图（程序化，零成本）
  | 'compare'   // 对比图（程序化或编辑图）
  | 'photo'     // 单品图 / 编辑图（需图源）
  | 'video'     // 短视频 / 动图（动作演示才用）
  | 'generated' // AI 生成图（仅作引导 action 的结果，不进日常池）
  | 'poster'    // 海报式：多件素材错落摆放 + 出血大字铺底 + 一处不和谐点缀

// poster 的 spec 形状（其余 modality 的契约见各组件注释）：
//   { word: { main, sub?, rotate? },        // 出血大字，不承载信息，只提供氛围
//     layers: [{ image, x, y, w, rotate }], // 位置 / 宽度(rpx) / 旋转全在数据里
//     chip?: { text } }                     // 一处不和谐的点缀色，最多一个
// 位置数据化是为了：将来单件素材到位后，只改数据不改组件。

// ---------- 生效条件 ----------
// 内容池筛选时用于过滤。条件不匹配的内容当天不发。

export interface WeatherBand {
  minTemp?: number
  maxTemp?: number
  condition?: string[] // 晴 / 雨 / 雪 / 多云 / 风
}

export type Season = 'spring' | 'summer' | 'autumn' | 'winter'
export type DayType = 'weekday' | 'weekend' | 'holiday'

/** 内容声明它适用于哪些基因取值；省略该维度表示不挑人 */
export interface GeneCondition {
  skinTone?: SkinTone[]
  contrast?: Contrast[]
  shoulder?: ShoulderWidth[]
  frame?: FrameScale[]
  vertical?: VerticalRatio[]
  neck?: NeckLength[]
  face?: FaceShape[]
  heightBand?: HeightBand[]
}

// ---------- 资产归属 ----------
// 用户「收下」后归入手册的哪一格。
//
// 命名原则：去掉"库"字、用两个字的常用词，不用"色卡/廓形"这类专业词。
// 原命名（色卡 / 配色库 / 版型库…）有两个毛病：
//   ① "色卡"和"配色库"用户分不清；② "库"让它听上去像仓库，不像"属于我的东西"。
//
// 分类默认由内容类型推导（见 pick.ts），少数特例才手工覆盖 ——
// 逐条手工分配必出错（曾把讲材质的「全黑并不自动显瘦」归到了配色库）。

export type CollectionCategory =
  | 'color'      // 颜色：适合你的颜色、怎么配色
  | 'fit'        // 版型：什么版型适合你的骨架
  | 'proportion' // 比例：怎么看着更高更顺
  | 'fabric'     // 面料：厚度、垂感、光泽
  | 'occasion'   // 场合：面试、约会穿什么
  | 'howto'      // 技巧：卷袖、塞衣角这类动作
  | 'outfit'     // 搭配：完整的一身
  | 'general'    // 综合：归不进七格的内容

/**
 * 一条建议的生命周期：收下 → 试过 → 留下了。
 *
 * "留下了"是产品真正知道的东西 —— 这是资产兑现的方式。
 * 不做连续天数、不做断签提醒（红线）。
 */
export type CollectionStatus = 'saved' | 'tried' | 'kept'

// ---------- 内容主体 ----------

export interface ContentVisual {
  modality: Modality
  /** 程序化视觉的绘制参数；modality 为 photo/video 时为图源标识 */
  spec: Record<string, unknown>
  /** 无障碍/无图时的文字兜底 */
  alt: string
}

export interface DailyContent {
  id: string
  type: ContentType
  /** 选题（杂志式标题，通用内容，不个性化） */
  topic: string
  /** 导语 */
  lead: string
  /** 适配说明：唯一被个性化的部分，按形象基因渲染 */
  fit: (gene: StyleGene) => string
  /** 原理：认知增量所在，一句 */
  why: string
  visual: ContentVisual
  /** 生效条件 */
  gene?: GeneCondition
  weather?: WeatherBand
  season?: Season[]
  dayType?: DayType[]
  /** 归入手册哪一格。可由内容类型推导，见 categoryOfType */
  asset: CollectionCategory
  /** 去重键：同一 key 的内容不对同一用户重复发 */
  dedupeKey: string
  /** 来源：决定是否需要人工审 */
  source: 'programmatic' | 'editorial' | 'partner' | 'ai_draft'
  /** 形象顾问是否已确认。未确认的内容不得进生产池 */
  reviewed: boolean
}

// ---------- 内容池筛选输入 ----------
// TodayContext 已存在于服务端（date/city/condition/temperature/day_type/schedule），此处只取所需。

export interface DailyPickInput {
  gene: StyleGene
  temperature: number
  condition: string
  dayType: DayType
  season: Season
  /** 该用户已收过的 dedupeKey */
  seen: string[]
  /** 手册各格现有条数，越厚说明用户越关心这一格 */
  bucketCount: Record<CollectionCategory, number>
}

// ---------- 内容池筛选结果 ----------

export interface DailyPick {
  content: DailyContent
  /** 渲染后的适配说明 */
  fitText: string
  /** 命中原因，用于调试与埋点 */
  reason: string[]
}
