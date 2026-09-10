// Taro 薄包装：core 的 createTaskPolling 是框架中立控制器（非 React hook），
// 在组件体里裸调会随每次 render 重建一个新轮询循环（循环倍增）。
// 这里整页生命周期只创建一个实例；fetcher/onDone 等配置经 ref 透传最新闭包；
// 页面不可见自动暂停（usePageVisibility 桥），恢复可见立即补拉一次，卸载即停。
import { useEffect, useRef } from 'react'
import { createTaskPolling, shouldStopPolling, type TaskPollingHandle, type TaskPollingOptions } from '@zsm/core'
import { usePageVisibility } from './use-page-visibility'

export function useStablePolling<T>(options: TaskPollingOptions<T>): TaskPollingHandle {
  const optionsRef = useRef(options)
  optionsRef.current = options
  const subscribeVisibility = usePageVisibility()
  const subscribeRef = useRef(subscribeVisibility)
  subscribeRef.current = subscribeVisibility

  const handleRef = useRef<TaskPollingHandle | null>(null)
  if (handleRef.current === null) {
    handleRef.current = createTaskPolling<T>({
      // 经 ref 调用最新配置：实例不随渲染重建，闭包也不过期
      fetcher: () => optionsRef.current.fetcher(),
      intervalMs: optionsRef.current.intervalMs,
      isSettled: (result) => {
        const custom = optionsRef.current.isSettled
        if (custom) return custom(result)
        const status = (result as { status?: unknown } | null | undefined)?.status
        return typeof status === 'string' && shouldStopPolling(status)
      },
      onDone: (result) => optionsRef.current.onDone?.(result),
      onFailed: (reason, errors) => optionsRef.current.onFailed?.(reason, errors),
      subscribeVisibility: (cb) => subscribeRef.current(cb),
      enabled: false // start/stop 由下方 effect 驱动
    })
  }

  useEffect(() => {
    if (!options.enabled) return
    handleRef.current?.start()
    return () => handleRef.current?.stop()
  }, [options.enabled])

  return handleRef.current
}
