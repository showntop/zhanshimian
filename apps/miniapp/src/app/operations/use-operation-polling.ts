// 页面唯一的 Operation 轮询入口。
// 客户端只认公开 Operation：一个 id 走 getOperation，多个 id 走 listOperations。
// 终态判定、失败上限、可见性暂停都在 core 的 createOperationPolling 里，
// 页面不再自己挑间隔，也不再自己数秒。
import { useCallback, useEffect, useRef, useState } from 'react'
import {
  createOperationPolling,
  type Operation,
  type OperationPollingHandle,
} from '@zsm/core'
import { qualityApi } from '../api/quality'
import { resourceCache, resourceKey } from '../cache/resource-cache'
import { usePageVisibility } from '../../hooks/use-page-visibility'

export interface UseOperationPollingOptions {
  /** 关注的操作 id；顺序不参与比较，重复项会被去掉 */
  operationIds: readonly string[]
  /** false 时不启动（例如页面还没拿到 id） */
  enabled?: boolean
  /** 整批进入终态时回调一次，随后轮询停止 */
  onSettled?: (operations: readonly Operation[]) => void
  /** 连续拉取失败达到上限时回调一次，随后轮询停止 */
  onFetchFailure?: (errors: readonly unknown[]) => void
}

export interface UseOperationPollingResult {
  /** 最近一轮拿到的整批 Operation；还没拿到时为空数组 */
  operations: Operation[]
  /** 立刻补拉一次（下拉刷新、失败重试用） */
  refresh: () => Promise<void>
  /** 提前收工；重复调用无副作用 */
  stop: () => void
}

/** 顺序无关的稳定 key：父组件重排数组不该把轮询整个重建 */
function idsKeyOf(ids: readonly string[]): string {
  return [...new Set(ids)].sort().join(',')
}

async function fetchOperations(ids: readonly string[]): Promise<Operation[]> {
  const only = ids.length === 1 ? ids[0] : undefined
  if (only !== undefined) return [await qualityApi.getOperation(only)]
  return qualityApi.listOperations(ids)
}

export function useOperationPolling(options: UseOperationPollingOptions): UseOperationPollingResult {
  const { operationIds, enabled = true } = options
  const idsKey = idsKeyOf(operationIds)
  const subscribeVisibility = usePageVisibility()
  const [operations, setOperations] = useState<Operation[]>([])

  // 调用方传进来的回调每次渲染都是新的箭头函数：放进 ref，
  // 否则它们会把轮询整个拆掉重建。
  const callbacksRef = useRef(options)
  callbacksRef.current = options

  // usePageVisibility 每次渲染都返回新函数（闭包里的 Set 是同一个），
  // 直接进依赖数组会让 effect 每帧重跑。
  const visibilityRef = useRef(subscribeVisibility)
  visibilityRef.current = subscribeVisibility

  const handleRef = useRef<OperationPollingHandle | null>(null)
  const ids = idsKey === '' ? [] : idsKey.split(',')

  useEffect(() => {
    if (!enabled || ids.length === 0) {
      setOperations([])
      return
    }

    // 换个页面再回来先拿上次的进度，不必从 0% 重新爬；
    // 换了另一批 id 则一律清空，绝不让上一批的进度留在屏幕上。
    setOperations(
      ids
        .map((id) => resourceCache.read<Operation>(resourceKey('operation', id)))
        .filter((operation): operation is Operation => Boolean(operation)),
    )

    const handle = createOperationPolling({
      fetcher: async () => {
        const batch = await fetchOperations(ids)
        // 服务端是唯一事实来源：每轮都覆盖回缓存，别的页面读同一份
        for (const operation of batch) {
          resourceCache.write(resourceKey('operation', operation.id), operation)
        }
        return batch
      },
      subscribeVisibility: visibilityRef.current,
      onUpdate: (batch) => setOperations([...batch]),
      onSettled: (batch) => {
        setOperations([...batch])
        callbacksRef.current.onSettled?.(batch)
      },
      onFailed: (errors) => callbacksRef.current.onFetchFailure?.(errors),
    })
    handleRef.current = handle

    return () => {
      handle.stop()
      if (handleRef.current === handle) handleRef.current = null
    }
    // idsKey 而不是 operationIds：数组字面量每次渲染都是新引用
  }, [idsKey, enabled])

  const refresh = useCallback(async (): Promise<void> => {
    await handleRef.current?.refresh()
  }, [])

  const stop = useCallback((): void => {
    handleRef.current?.stop()
  }, [])

  return { operations, refresh, stop }
}
