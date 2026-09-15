// 执行清单的三条硬规则：同一个勾选动作在重试间共用同一个 client_event_id，
// 服务端返回永远整体替换乐观状态，反馈只在执行完成后开放。
import test from 'node:test'
import assert from 'node:assert/strict'
// 带 .ts 后缀：node --test 直接加载真实现，Node 的 ESM 解析不做后缀补全
import { PublicApiError } from '../src/app/api/result.ts'
import {
  canSubmitExecutionFeedback,
  completedEventBody,
  createClientEventId,
  createEventDraft,
  executionEventBody,
  isEventConflict,
  isNetworkFailure,
  replaceExecutionFromServer,
  toggleStepLocal,
  allStepsDone,
} from '../src/features/execution/model.ts'

const execution = {
  id: 'e1',
  selection_id: 's1',
  state: 'active',
  version: 7,
  steps: [
    { id: 's1', completed: false },
    { id: 's2', completed: true, completed_at: '2026-09-13T01:00:00Z' },
  ],
  created_at: '2026-09-13T00:00:00Z',
  updated_at: '2026-09-13T00:00:00Z',
}

test('execution event keeps the same client id across retry', () => {
  const first = executionEventBody('step-1', true, 'event-1', '2026-09-13T08:00:00Z')
  const retry = executionEventBody('step-1', true, 'event-1', '2026-09-13T08:00:00Z')
  assert.deepEqual(first, retry)
  assert.equal(first.client_event_id, 'event-1')
})

test('event draft freezes client id and occurred_at so a retry replays byte-identical body', () => {
  // 同一笔事件的所有重试共用同一个草稿：服务端幂等 fingerprint = method+path+body，
  // occurred_at 变一个字节，同一把 Idempotency-Key 就会被拦成 409 而不是重放
  const draft = createEventDraft()
  const first = executionEventBody('step-1', true, draft.clientEventId, draft.occurredAt)
  const retry = executionEventBody('step-1', true, draft.clientEventId, draft.occurredAt)
  assert.deepEqual(first, retry)

  const completedFirst = completedEventBody(draft.clientEventId, draft.occurredAt)
  const completedRetry = completedEventBody(draft.clientEventId, draft.occurredAt)
  assert.deepEqual(completedFirst, completedRetry)

  // 新的一次点击是新的一笔：草稿不允许复用到下一次点击
  const next = createEventDraft()
  assert.notEqual(next.clientEventId, draft.clientEventId)
})

test('network failure is status 0, not the absence of PublicApiError', () => {
  // taro-fetch 把超时/断网包成 PublicApiError(status=0)；400/500 是永久失败，
  // 重试同一笔必然再败，调用方必须走另一个分支
  assert.equal(isNetworkFailure(new PublicApiError('network_failed', '网络连接不上', 0, '', true)), true)
  assert.equal(isNetworkFailure(new PublicApiError('validation_error', 'occurred_at 不合法', 400, '', false)), false)
  assert.equal(isNetworkFailure(new PublicApiError('internal_error', '服务异常', 500, '', true)), false)
  assert.equal(isNetworkFailure(new PublicApiError('version_conflict', '冲突', 412, '', false)), false)
  // 非 PublicApiError（意料外异常）按网络失败处理：回滚保留草稿是最安全的分支
  assert.equal(isNetworkFailure(new Error('boom')), true)
  assert.equal(isNetworkFailure(null), true)
})

test('event body maps completion to step_completed and reopen to step_reopened', () => {
  assert.equal(
    executionEventBody('step-1', true, 'event-1', '2026-09-13T08:00:00Z').type,
    'step_completed',
  )
  assert.equal(
    executionEventBody('step-1', false, 'event-1', '2026-09-13T08:00:00Z').type,
    'step_reopened',
  )
  // 非 step 事件不带 step_id（服务端会拒绝带 step_id 的 completed 事件）
  const completed = completedEventBody('event-9', '2026-09-13T08:00:00Z')
  assert.equal(completed.type, 'completed')
  assert.equal('step_id' in completed, false)
})

test('optimistic toggle touches only the target step and never the server-owned version', () => {
  const next = toggleStepLocal(execution, 's1')
  assert.equal(next.steps[0].completed, true)
  assert.notEqual(next.steps[0].completed_at, undefined)
  assert.equal(next.steps[1].completed, true)
  assert.equal(next.version, 7, 'version is server-owned; optimistic state must not bump it')
  assert.equal(next.state, 'active', 'state transitions are the server call too')
  // 原对象不被改动
  assert.equal(execution.steps[0].completed, false)
  // 再勾回去：completed_at 清空
  const reopened = toggleStepLocal(next, 's1')
  assert.equal(reopened.steps[0].completed, false)
  assert.equal(reopened.steps[0].completed_at, undefined)
})

test('server execution replaces optimistic state', () => {
  const optimistic = { id: 'e1', version: 1, steps: [{ id: 's1', completed: true }] }
  const server = { id: 'e1', version: 2, steps: [{ id: 's1', completed: false }] }
  assert.deepEqual(replaceExecutionFromServer(optimistic, server), server)
})

test('execution feedback opens only after completion', () => {
  assert.equal(canSubmitExecutionFeedback({ state: 'planned' }), false)
  assert.equal(canSubmitExecutionFeedback({ state: 'active' }), false)
  assert.equal(canSubmitExecutionFeedback({ state: 'completed' }), true)
  assert.equal(canSubmitExecutionFeedback({ state: 'abandoned' }), false)
})

test('progress requires at least one step', () => {
  // 空清单不许显示"全部完成"：0/0 不是 100%
  assert.equal(allStepsDone({ steps: [] }), false)
  assert.equal(allStepsDone({ steps: [{ id: 'a', completed: true }] }), true)
  assert.equal(
    allStepsDone({ steps: [{ id: 'a', completed: true }, { id: 'b', completed: false }] }),
    false,
  )
})

test('only version conflicts are conflicts', () => {
  // 判定对象是真实的错误对象：网络失败没有状态码，天然不是冲突
  const conflict = (statusCode) =>
    new PublicApiError('version_conflict', '清单已被更新', statusCode, '', false)
  assert.equal(isEventConflict(conflict(409)), true)
  assert.equal(isEventConflict(conflict(412)), true)
  assert.equal(isEventConflict(conflict(500)), false)
  assert.equal(isEventConflict(new Error('network down')), false)
  assert.equal(isEventConflict(null), false)
})

test('client event ids are printable ascii within the server limit', () => {
  const id = createClientEventId()
  assert.ok(id.length > 0 && id.length <= 200)
  assert.match(id, /^[\x20-\x7e]+$/)
  assert.notEqual(id, createClientEventId())
})
