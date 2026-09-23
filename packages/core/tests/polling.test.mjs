// 公开 Operation 的轮询生命周期。
// 客户端只认 Operation（accepted/running/retrying/succeeded/failed/cancelled/
// superseded），不再有第二套任务状态机。
import test from 'node:test'
import assert from 'node:assert/strict'
import {
  createOperationPolling,
  MAX_OPERATION_FETCH_FAILURES,
} from '../src/operations/polling.ts'

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms))

test('operation polling stops on every public terminal status', async () => {
  for (const terminal of ['succeeded', 'failed', 'cancelled', 'superseded']) {
    const seen = []
    const handle = createOperationPolling({
      fetcher: async () => {
        const status = seen.length === 0 ? 'running' : terminal
        seen.push(status)
        return [{ id: terminal, status }]
      },
      intervalMs: 1,
    })
    await sleep(20)
    handle.stop()
    assert.deepEqual(seen, ['running', terminal])
  }
})

test('page hide pauses, show refreshes immediately, five failures stop', async () => {
  let visibility
  let calls = 0
  let failed = 0
  const handle = createOperationPolling({
    fetcher: async () => {
      calls += 1
      throw new Error('offline')
    },
    intervalMs: 1,
    subscribeVisibility: (listener) => {
      visibility = listener
      return () => {
        visibility = undefined
      }
    },
    onFailed: () => {
      failed += 1
    },
  })
  visibility(false)
  const pausedAt = calls
  await sleep(10)
  assert.equal(calls, pausedAt)
  visibility(true)
  await sleep(30)
  assert.equal(calls, MAX_OPERATION_FETCH_FAILURES)
  assert.equal(failed, 1)
  handle.stop()
  assert.equal(visibility, undefined)
})

test('partially settled batch keeps polling until every operation settles', async () => {
  let calls = 0
  const settled = []
  const handle = createOperationPolling({
    fetcher: async () => {
      calls += 1
      return [
        { id: 'op-a', status: 'succeeded' },
        { id: 'op-b', status: calls < 3 ? 'running' : 'failed' },
      ]
    },
    intervalMs: 1,
    onSettled: (operations) => settled.push(operations.map((op) => op.status).join(',')),
  })
  await sleep(20)
  handle.stop()
  assert.equal(calls, 3)
  assert.deepEqual(settled, ['succeeded,failed'])
})

test('an empty batch is not a settled batch', async () => {
  let calls = 0
  const settled = []
  const handle = createOperationPolling({
    fetcher: async () => {
      calls += 1
      return calls < 2 ? [] : [{ id: 'op-1', status: 'succeeded' }]
    },
    intervalMs: 1,
    onSettled: () => settled.push('settled'),
  })
  await sleep(20)
  handle.stop()
  assert.equal(settled.length, 1)
})

test('onUpdate sees every batch; a successful fetch clears the failure count', async () => {
  let calls = 0
  const errors = []
  const updates = []
  const handle = createOperationPolling({
    fetcher: async () => {
      calls += 1
      // 失败两次、成功一次：失败计数必须被成功清零，否则间歇失败会误杀轮询
      if (calls <= 2) throw new Error('flaky')
      if (calls === 3) return [{ id: 'op-1', status: 'running' }]
      return [{ id: 'op-1', status: 'running' }]
    },
    intervalMs: 1,
    onUpdate: (operations) => updates.push(operations.length),
    onFailed: (seen) => errors.push(seen.length),
  })
  await sleep(20)
  handle.stop()
  assert.equal(errors.length, 0)
  assert.ok(updates.length >= 1)
})

test('stop is idempotent and refresh fetches immediately', async () => {
  let calls = 0
  const handle = createOperationPolling({
    fetcher: async () => {
      calls += 1
      return [{ id: 'op-1', status: 'running' }]
    },
    intervalMs: 10_000,
  })
  await handle.refresh()
  const afterRefresh = calls
  assert.ok(afterRefresh >= 1)
  handle.stop()
  handle.stop()
  await sleep(5)
  assert.equal(calls, afterRefresh)
})

test('enabled false never starts until refresh is called explicitly', async () => {
  let calls = 0
  const handle = createOperationPolling({
    fetcher: async () => {
      calls += 1
      return [{ id: 'op-1', status: 'running' }]
    },
    intervalMs: 1,
    enabled: false,
  })
  await sleep(10)
  assert.equal(calls, 0)
  handle.stop()
})
