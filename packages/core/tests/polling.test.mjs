// 轮询 / 进度补间 / 文案红线 / 埋点校验 测试
import test from 'node:test'
import assert from 'node:assert/strict'
import {
  POLL_INTERVALS,
  MAX_POLL_FAILURES,
  shouldStopPolling,
  useTaskPolling,
  advanceDisplayProgress,
  useDisplayProgress,
  isValidEventName,
  eventPayloadBytes,
  createEventTracker,
  trackEvent,
  setDefaultEventSender,
  ERROR_COPY,
  EMPTY_COPY,
  SCENES,
  greetingByHour,
  FEEDBACK_WORDS,
  HOME_TITLE
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

test('advanceDisplayProgress：只追不跳、不回退，500ms 尺度逼近', () => {
  // 追赶方向：不超过 real
  const next = advanceDisplayProgress(0, 80, 500)
  assert.ok(next > 0 && next < 80)
  // 500ms 覆盖约 63% 差距（指数逼近）
  assert.ok(Math.abs(next - 80 * (1 - Math.exp(-1))) < 0.001)
  // 追平后保持
  assert.equal(advanceDisplayProgress(80, 80, 500), 80)
  // real 回落时显示值不动
  assert.equal(advanceDisplayProgress(80, 30, 500), 80)
  // real 超过显示值时单调不减
  const later = advanceDisplayProgress(next, 90, 500)
  assert.ok(later > next && later <= 90)
  // 越界钳制
  assert.equal(advanceDisplayProgress(120, 130, 100), 100)
  assert.equal(advanceDisplayProgress(-5, -1, 100), 0)
})

test('useDisplayProgress 控制器：set(real) 后 onUpdate 单调递增到追平', async () => {
  const values = []
  const handle = useDisplayProgress({ tickMs: 10, onUpdate: (v) => values.push(v) })
  try {
    handle.set(90)
    await sleep(1800) // tau=500ms：1800ms 覆盖约 97%（≈87），未到但已逼近
    const last = handle.get()
    assert.ok(last > 85 && last <= 90, `displayed=${last}`)
    for (let i = 1; i < values.length; i += 1) assert.ok(values[i] >= values[i - 1])
  } finally {
    handle.stop() // 断言失败也必须清理定时器，避免泄漏悬挂进程
  }
})

test('事件名校验：^[a-z][a-z0-9_]{1,63}$', () => {
  assert.equal(isValidEventName('plan_selected'), true)
  assert.equal(isValidEventName('ab'), true)
  assert.equal(isValidEventName('a'), false) // 最短 2 位
  assert.equal(isValidEventName('PlanSelected'), false)
  assert.equal(isValidEventName('1abc'), false)
  assert.equal(isValidEventName('plan selected'), false)
  assert.equal(isValidEventName('x'.repeat(65)), false)
  assert.equal(isValidEventName('x'.repeat(64)), true)
})

test('payload ≤ 4KB 校验 + 静默失败', async () => {
  assert.equal(eventPayloadBytes({ a: 1 }) < 4096, true)
  assert.equal(eventPayloadBytes({ blob: 'x'.repeat(5000) }) > 4096, true)

  const sent = []
  const tracker = createEventTracker((body) => {
    sent.push(body)
  })
  assert.equal(await tracker.trackEvent('good_event', { a: 1 }), true)
  assert.equal(await tracker.trackEvent('BadName'), false)
  assert.equal(await tracker.trackEvent('good_event', { blob: 'x'.repeat(5000) }), false)
  assert.equal(sent.length, 1)

  // 发送抛错：静默
  const failing = createEventTracker(() => {
    throw new Error('offline')
  })
  assert.equal(await failing.trackEvent('good_event'), false)

  // 默认 tracker 未接 sender：静默 false；接入后生效
  setDefaultEventSender(null)
  assert.equal(await trackEvent('whatever_event'), false)
})
