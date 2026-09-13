// 公开 Operation → 页面视图的唯一投影。
// 页面不得自己去看 status 字符串，也不得自己算进度百分比：这里钳好、命名好。
import type { Operation } from '@zsm/core'

export type OperationView =
  | { kind: 'idle' }
  | { kind: 'working'; progress: number; message: string; stageCode: string; retrying: boolean }
  | { kind: 'succeeded'; resultType: string; resultId: string }
  | { kind: 'failed'; message: string; retryable: boolean; requestId: string }
  // 取消与被取代不是失败：契约把 cancelled/superseded 放在 NonFailedOperation 一侧，
  // 它们没有错误、没有 trace_id，也不该出现"重试"这个承诺。
  | { kind: 'ended'; reason: 'cancelled' | 'superseded' }

export function operationView(operation: Operation | null | undefined): OperationView {
  if (!operation) return { kind: 'idle' }
  if (
    operation.status === 'accepted' ||
    operation.status === 'running' ||
    operation.status === 'retrying'
  ) {
    return {
      kind: 'working',
      // bps → 0–100 的百分比，越界一律钳住：进度条不接受超范围的输入。
      progress: Math.min(100, Math.max(0, operation.progress_bps / 100)),
      message: operation.public_message,
      // 阶段码原样带出：页面拿它查文案表，但判定仍然只认这个视图。
      stageCode: operation.stage_code,
      retrying: operation.status === 'retrying',
    }
  }
  if (operation.status === 'succeeded') {
    return {
      kind: 'succeeded',
      resultType: operation.result_type ?? '',
      resultId: operation.result_id ?? '',
    }
  }
  if (operation.status === 'cancelled' || operation.status === 'superseded') {
    return { kind: 'ended', reason: operation.status }
  }
  return {
    kind: 'failed',
    message: operation.public_message,
    retryable: operation.retryable,
    // 契约里 Operation 的追踪字段叫 trace_id，与错误体的 request_id 是同一个值
    // （见 openapi.yaml 的 X-Request-ID 说明）；对用户统一叫 requestId。
    requestId: operation.trace_id ?? '',
  }
}
