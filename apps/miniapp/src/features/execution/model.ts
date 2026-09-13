// 执行清单的纯逻辑：事件体、乐观勾选、服务端替换、反馈开关。
// 不碰 Taro、不碰网络；所有中文来自 @zsm/core。
//
// 执行是事件溯源的：客户端的每一次勾选都是一笔带 client_event_id 的事件，
// 带着当前 version 的强 ETag 发出去。乐观状态只在本地活着——服务端响应
// 一到就整体替换；version 冲突说明别处已经推进了，本地没有任何资格"合并"。
// 带 .ts 后缀：这两个值导入会被 node --test 直接加载，Node 的 ESM 解析不做后缀补全
// （同 features/report/model.ts 里的理由）。
import { PublicApiError } from '../../app/api/result.ts'
import { createIdempotencyKey } from '../../app/keys.ts'
import type {
  CreateExecutionEventRequest,
  Execution,
} from '@zsm/core'

export function createClientEventId(): string {
  return createIdempotencyKey('event')
}

/** version → 强 ETag。服务端只接受 `"3"` 这一形状（引号可省略，但我们不省）。 */
export function ifMatchVersion(version: number): string {
  return `"${version}"`
}

/** 勾选 / 取消勾选的事件体。同一次逻辑点击在重试间共用同一个 client_event_id。 */
export function executionEventBody(
  stepId: string,
  completed: boolean,
  clientEventId: string,
  occurredAt: string,
): CreateExecutionEventRequest {
  return {
    client_event_id: clientEventId,
    type: completed ? 'step_completed' : 'step_reopened',
    step_id: stepId,
    occurred_at: occurredAt,
  }
}

/** 全部完成后的收尾事件。completed 事件不带 step_id，服务端会拒绝带上的。 */
export function completedEventBody(clientEventId: string, occurredAt: string): CreateExecutionEventRequest {
  return {
    client_event_id: clientEventId,
    type: 'completed',
    occurred_at: occurredAt,
  }
}

/**
 * 乐观勾选：只动目标步骤，version 与 state 一概不碰——
 * 两者都是服务端拥有的事实，客户端提前推进等于伪造（冲突时必然对不上）。
 * 完成时补本地时间戳，重开时清掉。
 */
export function toggleStepLocal(execution: Execution, stepId: string): Execution {
  return {
    ...execution,
    steps: execution.steps.map((step) => {
      if (step.id !== stepId) return step
      const completed = !step.completed
      return {
        ...step,
        completed,
        completed_at: completed ? new Date().toISOString() : undefined,
      }
    }),
  }
}

/** 服务端响应整体替换乐观状态——没有任何字段级合并，服务端给的每一步都是事实。 */
export function replaceExecutionFromServer(_optimistic: Execution, server: Execution): Execution {
  return server
}

/** 409 / 412 是版本冲突：先拉最新覆盖本地，再让用户重试。其余都不是。 */
export function isEventConflict(error: unknown): boolean {
  return error instanceof PublicApiError && (error.statusCode === 409 || error.statusCode === 412)
}

/** 执行反馈只在 completed 后开放；planned/active/abandoned 都不行。 */
export function canSubmitExecutionFeedback(execution: Pick<Execution, 'state'>): boolean {
  return execution.state === 'completed'
}

/** 进度是否满：至少要有一步。空清单的 0/0 不是 100%。 */
export function allStepsDone(execution: Pick<Execution, 'steps'>): boolean {
  return execution.steps.length > 0 && execution.steps.every((step) => step.completed)
}

/** 网络层失败的统一判定：没有状态码的错误都算网络/服务不可达。 */
export function isNetworkFailure(error: unknown): boolean {
  return !(error instanceof PublicApiError)
}
