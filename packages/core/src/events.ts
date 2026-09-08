// 埋点 —— 名字与负载在端上先校验，静默失败（埋点永不影响业务流程）。
// 服务端端点：POST /v1/events（见 api/endpoints.ts 的 postEvent）。
import type { EventInput } from './types/index.ts'

/** 小写字母开头，仅小写字母/数字/下划线，总长 2–64 */
export const EVENT_NAME_PATTERN = /^[a-z][a-z0-9_]{1,63}$/

export const MAX_EVENT_PAYLOAD_BYTES = 4096

export function isValidEventName(name: unknown): name is string {
  return typeof name === 'string' && EVENT_NAME_PATTERN.test(name)
}

/** payload 序列化字节数；不可序列化返回 Infinity（即非法） */
export function eventPayloadBytes(payload: unknown): number {
  try {
    return new TextEncoder().encode(JSON.stringify(payload ?? {})).byteLength
  } catch {
    return Infinity
  }
}

export function isPayloadWithinLimit(payload: unknown): boolean {
  return eventPayloadBytes(payload) <= MAX_EVENT_PAYLOAD_BYTES
}

export type EventSender = (body: EventInput) => Promise<unknown> | unknown

export interface EventTracker {
  /** 校验并发送；任何失败静默吞掉。返回是否成功送达 */
  trackEvent(name: string, payload?: unknown): Promise<boolean>
}

export function createEventTracker(send: EventSender): EventTracker {
  return {
    async trackEvent(name: string, payload: unknown = {}): Promise<boolean> {
      try {
        if (!isValidEventName(name)) {
          console.warn('[events] 非法事件名，已丢弃', name)
          return false
        }
        if (!isPayloadWithinLimit(payload)) {
          console.warn('[events] payload 超过 4KB，已丢弃', name)
          return false
        }
        await send({ name, payload })
        return true
      } catch (cause) {
        console.warn('[events] 发送失败（静默）', name, cause)
        return false
      }
    }
  }
}

// 模块级默认 tracker：app 启动时用 setDefaultEventSender 接上 API 客户端；
// 未接入前 trackEvent 直接静默返回 false，不抛错。
let defaultSender: EventSender | null = null
let defaultTracker: EventTracker | null = null

export function setDefaultEventSender(send: EventSender): void {
  defaultSender = send
  defaultTracker = null
}

export function trackEvent(name: string, payload?: unknown): Promise<boolean> {
  // 未接入 sender：静默返回 false（不校验、不发网络、不抛错）
  if (!defaultSender) return Promise.resolve(false)
  if (!defaultTracker) defaultTracker = createEventTracker(defaultSender)
  return defaultTracker.trackEvent(name, payload)
}
