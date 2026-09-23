// 计费错误的纯归类：不碰 Taro、不碰网络，node --test 直接加载。
//
// 为什么要单独一层：小程序里流通的错误类有两个，且没有继承关系——
// 接口错误一律是 app/api/result 的 PublicApiError（bodyOrThrow/dataOrThrow 抛的），
// core 的 ApiError 只来自 services/billing 自造的 payment_*（支付面板不可用/取消）。
// 用 instanceof 只认其中一个的代价已经实锤过：402  insufficient_credits 永远落不进
// 分支，「额度不足 → 打开购买层」整链失效。所以归类在这里一次做对，UI 侧只消费结果。
import { ApiError } from '@zsm/core'
// 带 .ts 后缀：node --test 直接加载本文件，Node 的 ESM 解析不做后缀补全
// （同 features/assessment/model.ts 里的理由）。
import { PublicApiError } from '../app/api/result.ts'

export type BillingErrorKind = 'insufficient_credits' | 'rate_limited' | 'payment_unavailable'

interface BillingErrorShape {
  code: string
  statusCode: number
  message: string
}

/** 两个错误类都认；都不认识就不是计费错误，返回 null 交给调用方按通用错误处理。 */
function shapeOf(error: unknown): BillingErrorShape | null {
  if (error instanceof PublicApiError) return error
  if (error instanceof ApiError) {
    return { code: error.code, statusCode: error.statusCode, message: error.message }
  }
  return null
}

/**
 * 错误 → 计费分支。code 优先、statusCode 兜底：
 * 服务端 402/429 即使换了错误码文案，分支也不该跟着歪。
 * `payment_cancelled` 刻意不在其中——取消支付不是失败，调用方（credit-sheet）自己认得它。
 */
export function billingErrorKind(error: unknown): BillingErrorKind | null {
  const shape = shapeOf(error)
  if (!shape) return null
  if (shape.code === 'insufficient_credits' || shape.statusCode === 402) return 'insufficient_credits'
  if (shape.code === 'rate_limited' || shape.statusCode === 429) return 'rate_limited'
  if (shape.code === 'payment_unavailable') return 'payment_unavailable'
  return null
}

/** 服务端愿意公开的那句话；没有就空串，由调用方回落到 BILLING_COPY 的本地文案。 */
export function billingErrorMessage(error: unknown): string {
  return shapeOf(error)?.message ?? ''
}
