// 每日内容的动画表演：协议类型 + 收敛调度（纯逻辑，平台无关）。
//
// 契约见 contracts/openapi.yaml 的 DailyPresentation。三条纪律：
//
//   1. 客户端只是播放器。stage.kind 在能力注册表里查渲染器，查不到就回落
//      兜底形态——新增一种动画 = 注册一个 kind/form，这里与页面都不用改。
//   2. 服务端只给语义（讲什么、哪套是答案 target、哪套是反例 counter、节奏档位），
//      视觉空间（有几个变体、长什么样）在客户端的 form 里。两边靠语义值对接，
//      所以 resolve() 由 form 提供，本文件不假设任何分类的视觉形状。
//   3. 定格的那一套必须就是内容本身。调度器只负责「从乱到定」的过程，
//      终点恒等于 target，不是随机停在哪。
//
// 本文件不做任何渲染、不碰 DOM——小程序与手机端共用同一份时间轴。

export type MotionPhase = 'roam' | 'settle' | 'reveal'

export interface MotionAsset {
  url: string
  kind: 'lottie' | 'sprite' | 'still'
  frames?: number
  fps?: number
  w?: number
  h?: number
}

export interface MotionStage {
  phase: MotionPhase
  kind: string
  /** converge 用：形态渲染器名（变体空间由它定义） */
  form?: string
  params?: Record<string, unknown>
  asset?: MotionAsset
  duration_ms?: number
  repeat?: number
  label?: string
}

export interface MotionPresentation {
  version: number
  stages: MotionStage[]
}

// ---------- 巡游 ----------

export interface RoamTheme {
  theme: string
  form: string
  label: string
}

/** 巡游主题序列；解析不出来时返回空（客户端回落静态文案，不空屏） */
export function roamThemes(stage: MotionStage | undefined): RoamTheme[] {
  const themes = stage?.params?.themes
  if (!Array.isArray(themes)) return []
  const out: RoamTheme[] = []
  for (const item of themes) {
    if (!item || typeof item !== 'object') continue
    const raw = item as Record<string, unknown>
    if (typeof raw.form !== 'string' || raw.form === '') continue
    out.push({
      theme: typeof raw.theme === 'string' ? raw.theme : raw.form,
      form: raw.form,
      label: typeof raw.label === 'string' ? raw.label : '',
    })
  }
  return out
}

/** 每个主题的停留时长（ms） */
export function roamPerThemeMS(stage: MotionStage | undefined, fallback = 4000): number {
  const value = stage?.params?.per_ms
  return typeof value === 'number' && value > 0 ? value : fallback
}

// ---------- 序列帧揭晓 ----------

export interface FramesParams {
  /** 逐帧切换的图片地址（已按播放顺序排好） */
  urls: string[]
  /** 每帧停留（ms） */
  intervalMS: number
  /** 末帧定格后再停一拍才揭晓海报（ms）——「定住」要有分量 */
  holdMS: number
}

const DEFAULT_FRAME_INTERVAL_MS = 108
const DEFAULT_FRAME_HOLD_MS = 320

/**
 * 帧序列参数；解析不出（kind 不对 / urls 缺失）返回 null，
 * 播放器据此立即揭晓海报——素材问题不能变成黑屏。
 */
export function readFramesParams(stage: MotionStage | undefined): FramesParams | null {
  const params = stage?.params
  if (!params) return null
  const urls = Array.isArray(params.urls)
    ? (params.urls as unknown[]).filter((url): url is string => typeof url === 'string' && url !== '')
    : []
  if (urls.length === 0) return null
  const interval = params.interval_ms
  const hold = params.hold_ms
  return {
    urls,
    intervalMS: typeof interval === 'number' && interval > 0 ? interval : DEFAULT_FRAME_INTERVAL_MS,
    holdMS: typeof hold === 'number' && hold >= 0 ? hold : DEFAULT_FRAME_HOLD_MS,
  }
}

// ---------- 收敛 ----------

export interface ConvergeAxis {
  id: string
  /** 归一化进度到这一刻，该轴锁死在 target */
  brake_at: number
  target: unknown
  counter: unknown
}

export interface ConvergeParams {
  axes: ConvergeAxis[]
  /** 各收敛维度的权重（drift/blur/survive/palette），form 自行解释 */
  dims?: Record<string, number>
  tempo?: { steps: number; interval: number[] }
  brakes?: number[]
  overshoot?: boolean
  theatrical?: boolean
}

/** 一步的时间轴切片。播放器按 phase 决定这一拍怎么演。 */
export interface ConvergeStep {
  /** 相对收敛开始的毫秒 */
  at: number
  /** 每个轴的变体索引 */
  indices: number[]
  /** 0~1 收敛幅度：驱动残影/模糊/位移的权重 */
  amp: number
  phase: 'chaos' | 'hesitate' | 'overshoot' | 'lock'
}

/** 戏剧停顿：让长出来的时间是「有戏的」，不是单纯把间隔拉大 */
export const BRAKE_HOLD_MS = 95
export const HESITATE_MS = 150
export const OVERSHOOT_MS = 260
export const LOCK_HOLD_MS = 190

const DEFAULT_STEPS = 14
const DEFAULT_INTERVAL: [number, number] = [90, 300]

function clampIdx(index: number, count: number): number {
  const max = Math.max(0, count - 1)
  if (!Number.isFinite(index)) return 0
  return Math.min(max, Math.max(0, Math.round(index)))
}

/** 防御式解析：契约是外部输入，缺字段一律退回默认值而不是崩 */
export function readConvergeParams(stage: MotionStage | undefined): ConvergeParams {
  const params = stage?.params ?? {}
  const axes: ConvergeAxis[] = Array.isArray(params.axes)
    ? (params.axes as Array<Record<string, unknown>>)
        .filter((a) => a && typeof a.id === 'string')
        .map((a) => ({
          id: a.id as string,
          brake_at: typeof a.brake_at === 'number' ? a.brake_at : 1,
          target: a.target,
          counter: a.counter,
        }))
    : []
  const tempoRaw = params.tempo as { steps?: unknown; interval?: unknown } | undefined
  const interval = Array.isArray(tempoRaw?.interval)
    ? (tempoRaw?.interval as unknown[]).filter((v): v is number => typeof v === 'number')
    : []
  return {
    axes,
    dims: params.dims && typeof params.dims === 'object' ? (params.dims as Record<string, number>) : undefined,
    tempo: {
      steps: typeof tempoRaw?.steps === 'number' ? Math.max(2, Math.min(40, tempoRaw.steps)) : DEFAULT_STEPS,
      interval:
        interval.length >= 2 && typeof interval[0] === 'number' && typeof interval[1] === 'number'
          ? [interval[0], interval[1]]
          : DEFAULT_INTERVAL,
    },
    overshoot: params.overshoot !== false,
    theatrical: params.theatrical !== false,
  }
}

/**
 * 收敛时间轴：从 counter（反例）起跳，幅度与速度同时衰减，三个轴依次制动，
 * 最后过冲回弹并锁死在 target。
 *
 * @param counts 每个轴的变体数量（由 form 提供）
 * @param resolve 语义值 → 变体索引（由 form 提供）
 * @param rand 注入随机源，测试可复现
 */
export function planConverge(
  params: ConvergeParams,
  counts: number[],
  resolve: (axisId: string, value: unknown) => number,
  rand: () => number = Math.random,
): ConvergeStep[] {
  const axes = params.axes
  if (axes.length === 0) return []

  const tempo = params.tempo
  const steps = tempo?.steps ?? DEFAULT_STEPS
  const interval = tempo?.interval ?? DEFAULT_INTERVAL
  const iv0 = interval[0] ?? DEFAULT_INTERVAL[0]
  const iv1 = interval[1] ?? DEFAULT_INTERVAL[1]
  const targets = axes.map((axis, i) => clampIdx(resolve(axis.id, axis.target), counts[i] ?? 1))
  const braked = axes.map(() => false)
  const theatrical = params.theatrical !== false

  const out: ConvergeStep[] = []
  let at = 0
  let current = axes.map((axis, i) => clampIdx(resolve(axis.id, axis.counter), counts[i] ?? 1))

  for (let step = 0; step < steps; step++) {
    const p = steps === 1 ? 1 : step / (steps - 1)
    const amp = Math.pow(1 - p, 2)
    const interval = Math.round(iv0 + (iv1 - iv0) * p)

    current = current.map((_, i) => {
      const axis = axes[i]
      const target = targets[i] ?? 0
      if (!axis || p >= axis.brake_at) return target
      const count = Math.max(1, counts[i] ?? 1)
      const spread = Math.max(1, Math.round((count - 1) * amp))
      const offset = Math.round((rand() * 2 - 1) * spread)
      return clampIdx(target + offset, count)
    })
    out.push({ at, indices: current.slice(), amp, phase: 'chaos' })
    at += interval

    // 制动回响：某个轴刚锁死的那一拍停一下，让「咔」有回响
    if (theatrical) {
      let hit = false
      axes.forEach((axis, i) => {
        if (!braked[i] && p >= axis.brake_at) {
          braked[i] = true
          hit = true
        }
      })
      if (hit) at += BRAKE_HOLD_MS
    }
  }

  // 最后犹豫：像轮盘停在目标前晃一下
  if (theatrical) {
    out.push({ at, indices: targets.slice(), amp: 0.08, phase: 'hesitate' })
    at += HESITATE_MS
  }
  // 过冲：越过半格再回弹
  if (params.overshoot !== false) {
    const over = targets.map((v, i) => clampIdx(v + (rand() < 0.5 ? -1 : 1), counts[i] ?? 1))
    out.push({ at, indices: over, amp: 0, phase: 'overshoot' })
    at += OVERSHOOT_MS
  }
  out.push({ at, indices: targets.slice(), amp: 0, phase: 'lock' })
  return out
}

/**
 * 减动效（prefers-reduced-motion）：整条时间轴塌成一步——直接定格，
 * 元素保持可见，不做任何变换。
 */
export function reducedPlan(plan: ConvergeStep[]): ConvergeStep[] {
  const lock = plan.find((step) => step.phase === 'lock')
  if (!lock) return []
  return [{ at: 0, indices: lock.indices, amp: 0, phase: 'lock' }]
}

/** 整条收敛的时长（ms），播放器据此安排何时揭晓 */
export function planDuration(plan: ConvergeStep[]): number {
  const last = plan[plan.length - 1]
  if (!last) return 0
  return last.at + (last.phase === 'lock' ? LOCK_HOLD_MS : 0)
}
