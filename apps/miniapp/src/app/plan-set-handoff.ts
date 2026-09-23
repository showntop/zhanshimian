// 「刚受理了哪一份方案集」的交接条：报告页写，方案页取。
//
// 为什么不是路由参数：方案页 `pages/plans/index` 在 tabBar 里，而 switchTab 不接受
// query（navigateTo/redirectTo 又禁止打开 tabBar 页）。所以 plan_set_id 与 operation_id
// 没法像分析页那样挂在 URL 上，只能走内存交一次手。
//
// 为什么不是 Storage：这两个 id 只对「刚才那次点击」有意义，是流程状态不是业务状态。
// 存进 Storage 的话，用户明天冷启动进方案 tab 会看到一份早就过期的待办；
// 内存里的交接条在进程重启后自然消失，正好是想要的语义。
//
// 取走即清（take 而不是 read）：方案页可能被反复切入切出，留着它会让每次进 tab
// 都跳回同一份方案集。
import { resourceCache } from './cache/resource-cache'

/** 报告页「查看方案」受理成功后要交给方案页的东西。 */
export interface PlanSetHandoff {
  planSetId: string
  /** 200 复用已发布方案集时为 null：没有任务在跑，方案页不必轮询。 */
  operationId: string | null
}

const HANDOFF_KEY = 'handoff:plan-set'

export function writePlanSetHandoff(handoff: PlanSetHandoff): void {
  resourceCache.write(HANDOFF_KEY, handoff)
}

export function takePlanSetHandoff(): PlanSetHandoff | null {
  const value = resourceCache.read<PlanSetHandoff>(HANDOFF_KEY)
  resourceCache.remove(HANDOFF_KEY)
  if (!value || typeof value.planSetId !== 'string' || !value.planSetId) return null
  return {
    planSetId: value.planSetId,
    operationId: typeof value.operationId === 'string' ? value.operationId : null,
  }
}
