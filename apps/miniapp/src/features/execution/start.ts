// 「选这套」的完整事务：PUT Selection → POST Execution → 双双落缓存。
// 方案详情页的 CTA 是唯一调用方；清单页恢复入口只读缓存与服务端，不再走这条路。
import { resourceCache, resourceKey } from '../../app/cache/resource-cache'
import { createIdempotencyKey } from '../../app/keys'
import { qualityApi } from '../../app/api/quality'
import type { PlanSet, PlanVariant, Selection } from '@zsm/core'
import type { Execution } from '@zsm/core'

/**
 * 选中一套方案并开一份执行。
 *
 * Selection 记录"用户当时看到的是哪一个 publication"（render 没发布就没有），
 * 这是 Task 10 生成反馈追责链的锚点，客户端不猜、不补。
 * Execution 是不可变快照：创建之后清单只跟着事件走，不再回头请求方案。
 */
export async function selectAndCreateExecution(
  planSet: Pick<PlanSet, 'id'>,
  variant: PlanVariant,
): Promise<{ selection: Selection; execution: Execution }> {
  const selection = await qualityApi.selectPlanSet(
    planSet.id,
    {
      plan_variant_id: variant.id,
      ...(variant.render.publication_id ? { render_publication_id: variant.render.publication_id } : {}),
    },
    createIdempotencyKey(`selection:${planSet.id}:${variant.id}`),
  )
  const execution = await qualityApi.createExecution(
    selection.id,
    createIdempotencyKey(`execution:${selection.id}`),
  )
  resourceCache.write(resourceKey('selection', selection.id), selection)
  resourceCache.write(resourceKey('execution', execution.id), execution)
  return { selection, execution }
}
