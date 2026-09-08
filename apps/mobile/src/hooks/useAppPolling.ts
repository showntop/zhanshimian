import { useEffect } from 'react'
import { AppState } from 'react-native'
import { useTaskPolling, type TaskPollingOptions } from '@zsm/core'

/** RN 薄包装：页面卸载即停；App 进后台暂停。 */
export function useAppPolling<T>(options: TaskPollingOptions<T>): void {
  const { enabled = true, fetcher, intervalMs, isSettled, onDone, onFailed } = options

  useEffect(() => {
    if (!enabled) return
    const handle = useTaskPolling({
      fetcher,
      intervalMs,
      enabled: true,
      isSettled,
      onDone,
      onFailed,
      subscribeVisibility: (cb) => {
        const sub = AppState.addEventListener('change', (state) => cb(state === 'active'))
        return () => sub.remove()
      },
    })
    return () => handle.stop()
    // 仅跟随 enabled / interval，避免每次 render 重建轮询
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled, intervalMs])
}
