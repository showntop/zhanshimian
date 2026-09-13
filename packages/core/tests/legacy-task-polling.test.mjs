// 旧任务轮询控制器的回归测试，从原 polling.test.mjs 原样搬来。
//
// 这套东西已退役（公开 Operation 轮询在 operations/polling.ts），但
// useTaskPolling / POLL_INTERVALS 现在仍被 packages/core/src/index.ts 与
// apps/miniapp/src/pages/plans 引用，Task 4 只做加法、推迟物理删除。
// 删除这些测试前必须先删掉被测代码——两件事一起在 Task 12 做，
// 所以本文件届时随 useTaskPolling.ts 一并删除。
import test from 'node:test'
import assert from 'node:assert/strict'
import {
  POLL_INTERVALS,
  MAX_POLL_FAILURES,
  shouldStopPolling,
  useTaskPolling,
} from '../src/index.ts'

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms))

test('shouldStopPolling：completed/failed 为终态', () => {
  assert.equal(shouldStopPolling('completed'), true)
  assert.equal(shouldStopPolling('failed'), true)
  assert.equal(shouldStopPolling('queued'), false)
  assert.equal(shouldStopPolling('processing'), false)
  assert.equal(shouldStopPolling(''), false)
})

test('POLL_INTERVALS 间隔常量单源（规范值）', () => {
  assert.deepEqual(POLL_INTERVALS, { analysis: 700, planLook: 2500, hairPreview: 900, today: 3000, homeTasks: 1500 })
  assert.equal(MAX_POLL_FAILURES, 5)
})

test('useTaskPolling：终态触发 onDone 并停止；不可见暂停、可见恢复', async () => {
  let visibilityCb = null
  const subscribeVisibility = (cb) => {
    visibilityCb = cb
    return () => {
      visibilityCb = null
    }
  }
  const statuses = ['processing', 'processing', 'processing', 'completed']
  let index = 0
  const fetched = []
  let done = null
  const handle = useTaskPolling({
    fetcher: async () => {
      const status = statuses[Math.min(index++, statuses.length - 1)]
      fetched.push(status)
      return { id: 't1', status }
    },
    intervalMs: 5,
    subscribeVisibility,
    onDone: (result) => {
      done = result
    }
  })
  try {
    await sleep(40)
    assert.equal(done.status, 'completed')
    assert.equal(fetched.length, 4)
  } finally {
    handle.stop()
  }
  assert.equal(visibilityCb, null) // 卸载清理：取消可见性订阅
})

test('useTaskPolling：连续失败 5 次触发 onFailed 停轮询', async () => {
  let calls = 0
  let failed = null
  const handle = useTaskPolling({
    fetcher: async () => {
      calls += 1
      throw new Error('boom')
    },
    intervalMs: 1,
    onFailed: (reason, errors) => {
      failed = { reason, errors }
    }
  })
  try {
    await sleep(80)
    assert.equal(calls, 5)
    assert.equal(failed.reason, 'fetch-errors')
    assert.equal(failed.errors.length, 5)
  } finally {
    handle.stop()
  }
})

test('useTaskPolling：成功会重置失败计数（间歇失败不误杀）', async () => {
  let calls = 0
  let failed = false
  const handle = useTaskPolling({
    fetcher: async () => {
      calls += 1
      // 偶数次失败、奇数次成功：连续失败永远只有 1 次
      if (calls % 2 === 0) throw new Error('flaky')
      return { status: 'processing' }
    },
    intervalMs: 2,
    onFailed: () => {
      failed = true
    }
  })
  try {
    await sleep(60)
    assert.equal(failed, false)
    assert.ok(calls >= 10)
  } finally {
    handle.stop()
  }
})
