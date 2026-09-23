// 幂等键的唯一来源：每次调用都不相同的客户端键。
//
// 放在 app 层而不是某个 feature 里，因为"重试必须换新键"是全客户端一条规则：
// 建档重发、渲染重试、执行事件重放都靠它。同一把键在服务端的幂等层会被原样重放，
// 所以"重试一次失败的请求"永远是一笔新键；而"同一次点击的网络重试"沿用旧键。
let seq = 0

export function createIdempotencyKey(prefix: string): string {
  seq += 1
  return `${prefix}:${Date.now()}-${seq}`
}

/**
 * Execution 事件的 client_event_id。服务端按它去重：同一个逻辑勾选的所有重试
 * 必须带同一个 id，换一次 id 就可能被当成第二次勾选。
 */
export function createClientEventId(): string {
  return createIdempotencyKey('event')
}
