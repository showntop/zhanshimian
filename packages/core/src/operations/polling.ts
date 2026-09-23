// 公开 Operation 的轮询控制器（框架中立，零 React 依赖）。
//
// 与已退役的任务控制器不同：终态集合来自公开 Operation 契约，一次可以拿多个
// Operation，只有"非空且全部终态"才停；失败计数被任意一次成功清零。
import type { Operation } from '../api/types.ts'

/** 公开 Operation 的四个终态；accepted/running/retrying 都还在动 */
const TERMINAL_STATUSES = ['succeeded', 'failed', 'cancelled', 'superseded'] as const

const TERMINAL: ReadonlySet<string> = new Set<string>(TERMINAL_STATUSES)

/** 连续拉取失败上限，达到即回调 onFailed 并停止 */
export const MAX_OPERATION_FETCH_FAILURES = 5

/** 唯一的轮询间隔：页面不再各自挑数字 */
export const OPERATION_POLL_INTERVAL_MS = 1000

/** 平台注入的可见性订阅：false=不可见（暂停），true=可见（恢复）；返回取消函数 */
export type SubscribeVisibility = (listener: (visible: boolean) => void) => () => void

export function isOperationSettled(status: Operation['status']): boolean {
  return TERMINAL.has(status)
}

/**
 * 一轮响应是否代表"整批都结束了"。空数组不算：可能是刚受理还没落库，
 * 当成终态会让页面停在空白上。
 */
export function areOperationsSettled(operations: readonly Operation[]): boolean {
  return operations.length > 0 && operations.every((operation) => isOperationSettled(operation.status))
}

export interface OperationPollingOptions {
  fetcher: () => Promise<readonly Operation[]>
  /** 默认 OPERATION_POLL_INTERVAL_MS */
  intervalMs?: number
  /** 默认 true；false 时不启动，只能靠 refresh() 手动拉 */
  enabled?: boolean
  subscribeVisibility?: SubscribeVisibility
  /** 每一轮成功响应都回调（含未终态的那些） */
  onUpdate?: (operations: readonly Operation[]) => void
  /** 整批终态时回调一次，随后停止 */
  onSettled?: (operations: readonly Operation[]) => void
  /** 连续失败达到上限时回调一次，随后停止 */
  onFailed?: (errors: readonly unknown[]) => void
}

export interface OperationPollingHandle {
  stop(): void
  /** 立即拉取一次（失败计数照常累加） */
  refresh(): Promise<void>
}

export function createOperationPolling(options: OperationPollingOptions): OperationPollingHandle {
  const intervalMs = options.intervalMs ?? OPERATION_POLL_INTERVAL_MS
  const errors: unknown[] = []
  let timer: ReturnType<typeof setTimeout> | null = null
  let stopped = false
  let visible = true
  let pending: Promise<void> | null = null
  let unsubscribe: (() => void) | null = null

  const clearTimer = (): void => {
    if (timer === null) return
    clearTimeout(timer)
    timer = null
  }

  const stop = (): void => {
    stopped = true
    clearTimer()
    if (unsubscribe) {
      unsubscribe()
      unsubscribe = null
    }
  }

  const schedule = (): void => {
    if (stopped || !visible || timer !== null) return
    timer = setTimeout(() => {
      timer = null
      void tick()
    }, intervalMs)
  }

  async function tick(): Promise<void> {
    if (stopped) return
    try {
      const operations = await options.fetcher()
      if (stopped) return
      errors.length = 0
      options.onUpdate?.(operations)
      if (areOperationsSettled(operations)) {
        stop()
        options.onSettled?.(operations)
        return
      }
    } catch (error) {
      if (stopped) return
      errors.push(error)
      if (errors.length >= MAX_OPERATION_FETCH_FAILURES) {
        const snapshot = [...errors]
        stop()
        options.onFailed?.(snapshot)
        return
      }
    }
    schedule()
  }

  /** 同一时刻只允许一轮在飞：refresh 与定时器撞上时共用同一个 Promise */
  const runTick = (): Promise<void> => {
    if (pending) return pending
    pending = tick().finally(() => {
      pending = null
    })
    return pending
  }

  const setVisible = (next: boolean): void => {
    if (next === visible) return
    visible = next
    if (visible) {
      // 回到前台立刻补一次，不让用户盯着上一轮的旧进度
      if (!stopped) void runTick()
    } else {
      clearTimer()
    }
  }

  if (options.subscribeVisibility) {
    unsubscribe = options.subscribeVisibility(setVisible)
  }

  if (options.enabled !== false) {
    void runTick()
  }

  return {
    stop,
    refresh(): Promise<void> {
      if (stopped) return Promise.resolve()
      return runTick()
    },
  }
}
