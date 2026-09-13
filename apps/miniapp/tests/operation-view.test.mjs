// 公开 Operation 的页面投影：进度、终态与失败文案的唯一出口。
import test from 'node:test'
import assert from 'node:assert/strict'
import { operationView } from '../src/app/operations/operation-view.ts'

const base = {
  id: 'op-1',
  kind: 'render',
  subject_type: 'plan_variant',
  subject_id: 'v1',
  stage_code: 'rendering',
  public_message: '正在生成',
  retryable: false,
  created_at: '2026-09-13T00:00:00Z',
  updated_at: '2026-09-13T00:00:00Z',
}

test('no operation is the idle view', () => {
  assert.deepEqual(operationView(null), { kind: 'idle' })
  assert.deepEqual(operationView(undefined), { kind: 'idle' })
})

test('accepted, running and retrying all read as working', () => {
  for (const status of ['accepted', 'running', 'retrying']) {
    const view = operationView({ ...base, status, progress_bps: 4200 })
    assert.equal(view.kind, 'working')
    assert.equal(view.progress, 42)
    assert.equal(view.message, '正在生成')
    assert.equal(view.retrying, status === 'retrying')
  }
})

test('progress is clamped to 0-100 even if the server overshoots', () => {
  assert.equal(operationView({ ...base, status: 'running', progress_bps: 12345 }).progress, 100)
  assert.equal(operationView({ ...base, status: 'running', progress_bps: -7 }).progress, 0)
})

test('succeeded carries the result pointer, empty when the server omits it', () => {
  assert.deepEqual(
    operationView({ ...base, status: 'succeeded', result_type: 'report', result_id: 'r1' }),
    { kind: 'succeeded', resultType: 'report', resultId: 'r1' },
  )
  assert.deepEqual(operationView({ ...base, status: 'succeeded' }), {
    kind: 'succeeded',
    resultType: '',
    resultId: '',
  })
})

test('failed exposes the public message, retryability and request id only', () => {
  const view = operationView({
    ...base,
    status: 'failed',
    public_message: '这一套暂时没有生成成功',
    retryable: true,
    trace_id: 'req-9',
  })
  assert.deepEqual(view, {
    kind: 'failed',
    message: '这一套暂时没有生成成功',
    retryable: true,
    requestId: 'req-9',
  })
  // 失败视图不携带任何服务端内部字段（kind 是视图自己的判别字段，不在此列）
  for (const leaked of ['error_code', 'stage_code', 'subject_id', 'id', 'status']) {
    assert.equal(leaked in view, false, `${leaked} leaked into the failure view`)
  }
})

test('cancelled and superseded are failures without a retry promise', () => {
  for (const status of ['cancelled', 'superseded']) {
    const view = operationView({ ...base, status, public_message: '已取消', trace_id: 'req-1' })
    assert.equal(view.kind, 'failed')
    assert.equal(view.retryable, false)
    assert.equal(view.requestId, 'req-1')
  }
})
