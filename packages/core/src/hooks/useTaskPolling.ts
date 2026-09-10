// 统一任务轮询 —— 间隔常量单源（AGENTS.md：页面只能用这里的 useTaskPolling）。
// 框架中立（零依赖，无 React import）：Taro/RN 各自包一层薄 hook，
// 页面隐藏（visibility 订阅回调 visible=false）即暂停，恢复可见立即补一次拉取；
// 卸载时调用 handle.stop() 清理；连续失败 MAX_POLL_FAILURES 次触发 onFailed 并停止。

/** 各场景轮询间隔（ms）—— 唯一规范源 */
export const POLL_INTERVALS = {
  /** 分析页 tasks/{id} */
  analysis: 700,
  /** 方案页列表内嵌 look 任务 */
  planLook: 2500,
  /** 发型预览 */
  hairPreview: 900,
  /** 今日方案生成 */
  today: 3000,
  /** 首页任务横轨（批量） */
  homeTasks: 1500
} as const

export type PollScenario = keyof typeof POLL_INTERVALS

/** 连续拉取失败次数上限，达到即进失败态停轮询 */
export const MAX_POLL_FAILURES = 5

export type TaskStatusLike = 'queued' | 'processing' | 'completed' | 'failed' | string

/** 终态判定（纯函数）：completed/failed 即停 */
export function shouldStopPolling(status: TaskStatusLike): boolean {
  return status === 'completed' || status === 'failed'
}

/** 平台注入的可见性订阅：回调 false=不可见（暂停），true=可见（恢复）；返回取消函数 */
export type SubscribeVisibility = (callback: (visible: boolean) => void) => () => void

interface TaskLike {
  status?: TaskStatusLike
}

export interface TaskPollingOptions<T> {
  fetcher: () => Promise<T>
  intervalMs: number
  /** 默认 true；false 时不启动（包装层重建设备） */
  enabled?: boolean
  /** 终态判定；默认对带 status 字段的结果用 shouldStopPolling */
  isSettled?: (result: T) => boolean
  /** 终态（含 failed 任务态）回调：停止轮询并交出最后一次结果 */
  onDone?: (result: T) => void
  /** 连续拉取失败达到 MAX_POLL_FAILURES 时回调，随后停止 */
  onFailed?: (reason: 'fetch-errors', errors: unknown[]) => void
  subscribeVisibility?: SubscribeVisibility
}

export interface TaskPollingHandle {
  start(): void
  stop(): void
  /** 立即拉取一次（不重置失败计数语义之外的状态） */
  refresh(): Promise<void>
}

function defaultIsSettled<T>(result: T): boolean {
  const task = result as TaskLike | null | undefined
  return !!task && typeof task === 'object' && typeof task.status === 'string' && shouldStopPolling(task.status)
}

export function createTaskPolling<T>(options: TaskPollingOptions<T>): TaskPollingHandle {
  const { fetcher, intervalMs, enabled = true, isSettled = defaultIsSettled, onDone, onFailed, subscribeVisibility } = options

  let timer: ReturnType<typeof setTimeout> | null = null
  let unsubscribeVisibility: (() => void) | null = null
  let stopped = true
  let visible = true
  let failures = 0
  const errors: unknown[] = []
  let fetching = false

  const clearTimer = () => {
    if (timer !== null) {
      clearTimeout(timer)
      timer = null
    }
  }

  const run = async (): Promise<void> => {
    if (stopped || !visible || fetching) return
    fetching = true
    let outcome: { ok: true; value: T } | { ok: false; error: unknown }
    try {
      outcome = { ok: true, value: await fetcher() }
    } catch (error) {
      outcome = { ok: false, error }
    }
    fetching = false
    if (stopped) return

    if (!outcome.ok) {
      failures += 1
      errors.push(outcome.error)
      if (failures >= MAX_POLL_FAILURES) {
        stop()
        onFailed?.('fetch-errors', errors.slice(-MAX_POLL_FAILURES))
        return
      }
      schedule()
      return
    }

    failures = 0
    errors.length = 0
    if (isSettled(outcome.value)) {
      stop()
      onDone?.(outcome.value)
      return
    }
    schedule()
  }

  const schedule = () => {
    clearTimer()
    if (stopped || !visible) return
    timer = setTimeout(() => {
      void run()
    }, intervalMs)
  }

  const start = () => {
    if (!stopped) return
    stopped = false
    visible = true
    failures = 0
    errors.length = 0
    if (subscribeVisibility) {
      unsubscribeVisibility = subscribeVisibility((next) => {
        const wasVisible = visible
        visible = next
        if (stopped) return
        if (!next) {
          clearTimer()
        } else if (!wasVisible) {
          clearTimer()
          void run() // 恢复可见立即补一次拉取
        }
      })
    }
    void run() // 启动立即拉一次
  }

  const stop = () => {
    stopped = true
    clearTimer()
    if (unsubscribeVisibility) {
      unsubscribeVisibility()
      unsubscribeVisibility = null
    }
  }

  if (enabled) start()
  return { start, stop, refresh: run }
}

/** 兼容旧名：等同 createTaskPolling（框架中立控制器，非 React hook）。 */
export const useTaskPolling = createTaskPolling
