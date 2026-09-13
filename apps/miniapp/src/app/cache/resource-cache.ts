// 资源级缓存：服务端是唯一事实来源，缓存只做"同一次会话里少发一次请求"。
// key 必须带资源类型 + 资源 id，媒体的 key 还要带签名 URL——换签名的同一张图
// 是两条缓存，否则会拿着过期 URL 去渲染。
import type { DisplayMedia } from '@zsm/core'

export type ResourceKind =
  | 'report'
  | 'plan-set'
  | 'operation'
  | 'selection'
  | 'execution'
  | 'home'

export function resourceKey(kind: ResourceKind, id: string): string {
  return `${kind}:${id}`
}

export function mediaKey(media: Pick<DisplayMedia, 'asset_id' | 'url'>): string {
  return `media:${media.asset_id}:${media.url}`
}

export type ResourceCache = {
  read<T>(key: string): T | undefined
  write<T>(key: string, value: T): T
  remove(key: string): void
  subscribe(key: string, listener: () => void): () => void
  /** 拉取并覆盖。同一 key 的并发调用合并成一次请求；结果无条件覆盖旧值。 */
  revalidate<T>(key: string, load: () => Promise<T>): Promise<T>
}

export function createResourceCache(): ResourceCache {
  const values = new Map<string, unknown>()
  const listeners = new Map<string, Set<() => void>>()
  const inflight = new Map<string, Promise<unknown>>()

  const publish = (key: string): void => {
    const set = listeners.get(key)
    if (!set) return
    for (const listener of [...set]) listener()
  }

  return {
    read<T>(key: string): T | undefined {
      return values.get(key) as T | undefined
    },

    write<T>(key: string, value: T): T {
      values.set(key, value)
      publish(key)
      return value
    },

    remove(key: string): void {
      values.delete(key)
      publish(key)
    },

    subscribe(key: string, listener: () => void): () => void {
      const set = listeners.get(key) ?? new Set<() => void>()
      set.add(listener)
      listeners.set(key, set)
      return () => {
        set.delete(listener)
      }
    },

    async revalidate<T>(key: string, load: () => Promise<T>): Promise<T> {
      const existing = inflight.get(key) as Promise<T> | undefined
      if (existing) return existing
      const request = load()
        .then((value) => {
          // 直接落值而不是走 write()，避免在 await 之后再同步通知一遍订阅者。
          values.set(key, value)
          publish(key)
          return value
        })
        .finally(() => {
          inflight.delete(key)
        })
      inflight.set(key, request)
      return request
    },
  }
}

/** 全局单例：页面之间共享同一份缓存，避免每个页面各自拉一遍同一份资源。 */
export const resourceCache = createResourceCache()
