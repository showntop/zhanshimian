// 受理失败后的「换新键」标记，落 Storage（不是内存缓存）。
//
// 为什么必须持久：方案受理用固定幂等键，服务端 24h 内重放同一份结果——
// 用户重启小程序后点「重新生成」，内存标记已丢，同键重发只会把那份失败
// 原样端回来（202 重放），看起来就像「重新生成没用」。
//
// 标记没有 TTL：下一次受理成功（或复用到已发布集）由发起方清掉；期间多
// 换一次新键只是放弃一次幂等保护，不会出错。
import { readStorage, writeStorage } from '../services/storage'

const markerKey = (scene: string): string => `zsm_plan_set_retry_${scene}`

/** 受理终态失败时写：下一次发起必须换新幂等键。 */
export function markPlanSetRetry(scene: string): void {
  writeStorage(markerKey(scene), '1')
}

/** 发起前读：true 表示上一份受理失败过，必须换新键。 */
export function hasPlanSetRetryMark(scene: string): boolean {
  return readStorage(markerKey(scene)) === '1'
}

/** 新受理成功（或复用到已发布集）后由发起方清掉。 */
export function clearPlanSetRetryMark(scene: string): void {
  writeStorage(markerKey(scene), '')
}
