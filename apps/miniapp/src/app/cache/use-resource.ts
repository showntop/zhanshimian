// 页面读服务端资源的唯一方式：先吃缓存，再按需要拉一次，拉到的新值无条件覆盖旧的。
// 页面不得自己写"如果本地已经有图就不更新"这类判断——服务端说没有了就必须变没有。
import { useCallback, useEffect, useRef, useState } from 'react'
import { resourceCache } from './resource-cache'

export type ResourceState<T> = {
  data: T | undefined
  loading: boolean
  error: unknown
  /** 强制拉取；与同 key 的并发拉取合并成一次请求。 */
  refresh: () => Promise<void>
}

/**
 * @param key 资源键；传 null 表示"还没有 id，先别拉"（例如报告尚未生成）。
 * @param load 拉取函数；不传则只读缓存，由调用方另外触发刷新。
 */
export function useResource<T>(key: string | null, load?: () => Promise<T>): ResourceState<T> {
  const [data, setData] = useState<T | undefined>(() => (key ? resourceCache.read<T>(key) : undefined))
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<unknown>(null)

  // load 常写成行内箭头函数，放进依赖会让自动拉取每次渲染重跑；用 ref 保持最新实现，
  // 让 refresh 的依赖只剩 key。
  const loadRef = useRef(load)
  useEffect(() => {
    loadRef.current = load
  }, [load])

  useEffect(() => {
    if (!key) {
      setData(undefined)
      return
    }
    setData(resourceCache.read<T>(key))
    return resourceCache.subscribe(key, () => {
      setData(resourceCache.read<T>(key))
    })
  }, [key])

  const refresh = useCallback(async () => {
    const loader = loadRef.current
    if (!key || !loader) return
    setLoading(true)
    try {
      await resourceCache.revalidate(key, loader)
      setError(null)
    } catch (cause) {
      setError(cause)
    } finally {
      setLoading(false)
    }
  }, [key])

  useEffect(() => {
    if (!key || !loadRef.current) return
    if (resourceCache.read(key) !== undefined) return
    void refresh()
  }, [key, refresh])

  return { data, loading, error, refresh }
}
