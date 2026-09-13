// 埋点校验：事件名白名单格式、payload 上限、发送失败静默
import test from 'node:test'
import assert from 'node:assert/strict'
import {
  isValidEventName,
  eventPayloadBytes,
  createEventTracker,
  trackEvent,
  setDefaultEventSender,
} from '../src/index.ts'

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
