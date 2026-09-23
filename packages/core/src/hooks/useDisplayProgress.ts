// 分析页显示进度补间 ——「显示进度追真实进度」双轨（§4.3 规约）：
// 服务端推真实 progress，前端 500ms 补间逼近，只追不跳、不回退。
// 可选时间推期（pace）：真实进度长时间停滞时（如大模型调用 1-4 分钟），
// 显示目标按经过时间缓慢推进并封顶，避免进度条长时间冻结；
// 终态 set(100) 后真实值接管，显示值平滑收尾到 100。
// 框架中立：纯函数 advanceDisplayProgress + 控制器 createDisplayProgress；
// React 包装层把 onUpdate 接到 setState。

/** 追赶时间常数（ms）：显示值以该时间尺度指数逼近真实值 */
export const PROGRESS_CATCH_UP_MS = 500

/** 时间推期参数：真实进度停滞时按经过时间推进显示目标 */
export interface DisplayPaceOptions {
  /** 推期封顶：显示目标最多推进到这里（必须 < 100，终态由真实值接管） */
  ceiling: number
  /** 推期时间常数（ms）：elapsed = tau 时约达 ceiling 的 63% */
  tauMs: number
}

export interface DisplayProgressOptions {
  catchUpMs?: number
  /** 补间 tick 间隔，默认 100ms */
  tickMs?: number
  /**
   * 追平速率上限（百分比/秒，0 = 不限）。真实进度大幅跳变时（如重进页面追到 72%），
   * 显示值按该速率平滑爬升而不是 2 秒内瞬移到位。
   */
  maxRatePerSecond?: number
  /** 时间推期（可选）：真实进度停滞时按经过时间缓慢推进显示目标，封顶不碰 100 */
  pace?: DisplayPaceOptions
  /**
   * 每次显示值更新回调（单调不减；未配置 pace 时恒在 [displayed, real] 区间内，
   * 配置 pace 后可能领先 real 但绝不越过 ceiling，终态以 real=100 收尾）
   */
  onUpdate?: (value: number) => void
  /** 定时器注入（测试用），默认 setTimeout */
  schedule?: (callback: () => void, ms: number) => () => void
}

/**
 * 纯函数：给定当前显示值、真实值与距上次推进的毫秒数，返回新的显示值。
 * - 指数逼近（时间常数 catchUpMs）：500ms 内覆盖约 63% 差距，平滑无跳变；
 * - 只追不跳：新值不会超过 real；
 * - 不回退：real 回落（异常场景）时显示值保持不动。
 */
export function advanceDisplayProgress(displayed: number, real: number, elapsedMs: number, catchUpMs: number = PROGRESS_CATCH_UP_MS): number {
  const safeReal = clampProgress(real)
  const safeDisplayed = clampProgress(displayed)
  if (safeReal <= safeDisplayed) return safeDisplayed
  if (!Number.isFinite(elapsedMs) || elapsedMs <= 0) return safeDisplayed
  const fraction = Math.min(1, elapsedMs / catchUpMs)
  const next = safeDisplayed + (safeReal - safeDisplayed) * (1 - Math.exp(-fraction))
  return Math.min(safeReal, next)
}

function clampProgress(value: number): number {
  if (!Number.isFinite(value)) return 0
  return Math.min(100, Math.max(0, value))
}

export interface DisplayProgressHandle {
  /** 服务端推进真实值（触发补间循环直到追平；终态传 100 让真实值接管收尾） */
  set(real: number): void
  /** 读取当前显示值 */
  get(): number
  /** 停止补间循环（页面卸载调用） */
  stop(): void
}

const defaultSchedule = (callback: () => void, ms: number): (() => void) => {
  const timer = setTimeout(callback, ms)
  return () => clearTimeout(timer)
}

/**
 * 框架中立控制器（**不是 React hook**，名字不带 use）：
 * 组件里直接每次 render 调用会重建闭包、进度归零。
 * React 页面必须整页生命周期只创建一个实例，并把 onUpdate 接到 setState。
 */
export function createDisplayProgress(options: DisplayProgressOptions = {}): DisplayProgressHandle {
  const {
    catchUpMs = PROGRESS_CATCH_UP_MS,
    tickMs = 100,
    maxRatePerSecond = 0,
    pace,
    onUpdate,
    schedule = defaultSchedule,
  } = options
  let displayed = 0
  let real = 0
  let startedAt: number | null = null
  let cancelTick: (() => void) | null = null
  let lastTickAt = Date.now()

  const emit = () => onUpdate?.(displayed)

  // 显示目标：真实进度与时间推期取大。推期只在真实进度停滞时领先，
  // 且绝不越过 ceiling；真实值一旦反超（含终态 100）立即接管。
  const targetAt = (now: number): number => {
    if (!pace || startedAt === null) return real
    const paced = pace.ceiling * (1 - Math.exp(-(now - startedAt) / pace.tauMs))
    return Math.max(real, Math.min(pace.ceiling, paced))
  }

  // 补间循环是否继续：还没追平真实值，或推期仍有推进空间
  // （-0.5：推期渐近封顶时浮点永不触顶，用半个百分点阈停掉空转心跳）。
  const canAdvance = (goal: number): boolean => {
    if (displayed < goal) return true
    if (!pace || startedAt === null) return false
    return real < pace.ceiling && displayed < pace.ceiling - 0.5
  }

  const tick = () => {
    cancelTick = null
    const now = Date.now()
    const elapsed = now - lastTickAt
    const goal = targetAt(now)
    let next = advanceDisplayProgress(displayed, goal, elapsed, catchUpMs)
    if (maxRatePerSecond > 0) {
      // 速率上限：大跳变时平滑爬升（不改变「只追不跳、不回退」契约）
      next = Math.min(next, displayed + (maxRatePerSecond * elapsed) / 1000)
    }
    lastTickAt = now
    if (next !== displayed) {
      displayed = next
      emit()
    }
    if (canAdvance(goal)) {
      cancelTick = schedule(tick, tickMs)
    }
  }

  return {
    set(nextReal: number) {
      real = clampProgress(nextReal)
      if (startedAt === null) startedAt = Date.now()
      lastTickAt = Date.now()
      if (canAdvance(targetAt(Date.now())) && !cancelTick) {
        cancelTick = schedule(tick, tickMs)
      }
    },
    get() {
      return displayed
    },
    stop() {
      if (cancelTick) {
        cancelTick()
        cancelTick = null
      }
    }
  }
}

/** 兼容旧名：等同 createDisplayProgress（框架中立控制器，非 React hook）。 */
export const useDisplayProgress = createDisplayProgress
