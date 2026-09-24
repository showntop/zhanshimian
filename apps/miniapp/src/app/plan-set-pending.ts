// 受理在途回执的本地存取。回执只在「已受理、方案集还没发布」这一段窗口存在。
//
// 为什么必须落 Storage（与 plan-set-handoff 的分工）：交接条只对「刚才那次点击」
// 有意义，进程重启就该消失；而受理窗口可能跨越进程重启——用户提交后退出小程序，
// 任务还在服务端跑。这时内存交接条与受理归属 ref 全没了，方案 tab 只剩
// bootstrap 的 active_operations（不带场景、不带方案集 id），进度动画退化成
// 一行文字横幅、空态卡上的生成按钮也回来了。回执补的就是这份归属。
//
// 两条不可让步的规则：
// 1. 是否在途只认服务端（active_operations / operation 终态），回执不参与判断；
// 2. 回执带 TTL，过期的回执等于没有——不能让明天的冷启动看见今天的待办。
import { STORAGE_KEYS, readStorage, writeStorage } from '../services/storage'
import {
  parsePlanSetPending,
  serializePlanSetPending,
  type PlanSetPendingTicket,
} from '../features/planning/model'

/** 写回执：受理成功（202）时写，覆盖上一次的受理。 */
export function writePlanSetPending(ticket: Omit<PlanSetPendingTicket, 'at'>): void {
  writeStorage(STORAGE_KEYS.planSetPending, serializePlanSetPending(ticket))
}

/** 读回执：过期/损坏一律 null（调用方当「没有回执」处理，不报错）。 */
export function readPlanSetPending(): PlanSetPendingTicket | null {
  return parsePlanSetPending(readStorage(STORAGE_KEYS.planSetPending), Date.now())
}

/** 清回执：受理到终态（成功或失败）、200 复用已发布方案集时调用。 */
export function clearPlanSetPending(): void {
  writeStorage(STORAGE_KEYS.planSetPending, '')
}
