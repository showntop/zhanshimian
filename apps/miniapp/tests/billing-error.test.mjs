// 计费错误归类的红线：接口错误是 PublicApiError，core ApiError 只来自 billing 自造的
// payment_*；两个类没有继承关系，只认一个就有一半分支永远不可达（402 开购买层曾整链失效）。
import test from 'node:test'
import assert from 'node:assert/strict'
import { ApiError } from '@zsm/core'
import { PublicApiError } from '../src/app/api/result.ts'
import { billingErrorKind, billingErrorMessage } from '../src/services/billing-error.ts'

test('PublicApiError（接口层实际抛的类）落进全部三个计费分支', () => {
  assert.equal(
    billingErrorKind(new PublicApiError('insufficient_credits', '额度不足', 402, 'req-1', false)),
    'insufficient_credits',
  )
  // code 不认识但状态码在：服务端改文案/改码都不能把分支弄丢
  assert.equal(
    billingErrorKind(new PublicApiError('credits_exhausted', '额度不足', 402, '', false)),
    'insufficient_credits',
  )
  assert.equal(
    billingErrorKind(new PublicApiError('rate_limited', '今日额度已用完', 429, '', true)),
    'rate_limited',
  )
  assert.equal(
    billingErrorKind(new PublicApiError('too_many_requests', '稍后再试', 429, '', true)),
    'rate_limited',
  )
  assert.equal(
    billingErrorKind(new PublicApiError('payment_unavailable', '购买暂未开通', 503, '', false)),
    'payment_unavailable',
  )
})

test('core ApiError（billing 自造 payment_* 用的类）同样被认出', () => {
  assert.equal(
    billingErrorKind(new ApiError({ code: 'insufficient_credits', message: '额度不足', statusCode: 402 })),
    'insufficient_credits',
  )
  assert.equal(
    billingErrorKind(new ApiError({ code: 'payment_unavailable', message: '购买暂未开通', statusCode: 503 })),
    'payment_unavailable',
  )
})

test('payment_cancelled 不是计费失败：取消支付归调用方自己处理', () => {
  assert.equal(
    billingErrorKind(new ApiError({ code: 'payment_cancelled', message: '已取消支付', statusCode: 0 })),
    null,
  )
})

test('非计费错误一律 null，交给通用错误通道', () => {
  assert.equal(billingErrorKind(new PublicApiError('network_failed', '网络连接不上', 0, '', true)), null)
  assert.equal(billingErrorKind(new PublicApiError('capability_unavailable', '能力未开放', 503, '', false)), null)
  assert.equal(billingErrorKind(new Error('boom')), null)
  assert.equal(billingErrorKind('insufficient_credits'), null)
  assert.equal(billingErrorKind(undefined), null)
})

test('公开 message 两个类都取得出，取不到回落空串', () => {
  assert.equal(
    billingErrorMessage(new PublicApiError('insufficient_credits', '服务端说的话', 402, '', false)),
    '服务端说的话',
  )
  assert.equal(
    billingErrorMessage(new ApiError({ code: 'rate_limited', message: 'core 侧的话', statusCode: 429 })),
    'core 侧的话',
  )
  assert.equal(billingErrorMessage(new Error('boom')), '')
})

// 主线四条计费链（capture 提交 / scene 提交 / plans 生成与单套重试 / report 查看方案）
// 的 catch 契约：`if (handleBillingError(e)) return` 在前、通用 toast 在后。
// handleBillingError 的开关就是 billingErrorKind 非空——这里钉住各链真实会遇到的错误形状，
// 保证「402 → 购买引导」永远不会因为错误类/码的变动悄悄落回一句 toast。
test('调用点契约：402/429 被 billing 接管（不再落通用 toast）', () => {
  const handled = (error) => billingErrorKind(error) !== null
  // 服务端 402：code 可能是 insufficient_credits，也可能换名（statusCode 兜底）
  assert.ok(handled(new PublicApiError('insufficient_credits', '额度不足', 402, 'req-1', false)))
  assert.ok(handled(new PublicApiError('credits_exhausted', '', 402, '', false)))
  // 429 日额度
  assert.ok(handled(new PublicApiError('rate_limited', '今日额度已用完', 429, '', true)))
  // 402 的 message 会进购买引导弹层；空 message 时调用方回落 BILLING_COPY
  assert.equal(
    billingErrorMessage(new PublicApiError('insufficient_credits', '', 402, '', false)),
    '',
  )
})

test('调用点契约：非计费错误一律放行给各页现有 toast 分支', () => {
  const fallsThroughToToast = (error) => billingErrorKind(error) === null
  // 各链现有 toast 分支消费的错误形状：PublicApiError（取 message）或任意 Error
  assert.ok(fallsThroughToToast(new PublicApiError('request_failed', '请求没有成功，请重试', 500, '', true)))
  assert.ok(fallsThroughToToast(new PublicApiError('network_failed', '网络连接不上', 0, '', true)))
  assert.ok(fallsThroughToToast(new PublicApiError('capability_unavailable', '能力未开放', 503, '', false)))
  assert.ok(fallsThroughToToast(new Error('boom')))
})
