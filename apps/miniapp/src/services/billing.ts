import Taro from '@tarojs/taro'
import { ApiError, BILLING_COPY, type BillingOrder, type BillingSKU } from '@zsm/core'
import { peripherals } from '../app/api/peripherals'
import { billingErrorKind, billingErrorMessage } from './billing-error'
import { STORAGE_KEYS, writeStorage } from './storage'

export function isInsufficientCredits(error: unknown): boolean {
  return billingErrorKind(error) === 'insufficient_credits'
}

/**
 * 计费类错误的一次性处理；返回 true 表示已处理，调用方直接收工。
 * 错误类识别在 ./billing-error（接口错误是 PublicApiError，core ApiError 只认
 * 本文件自造的 payment_*）——曾用 instanceof 只判 core 类，402 分支整链失效。
 */
export function handleBillingError(error: unknown): boolean {
  const kind = billingErrorKind(error)
  if (kind === 'insufficient_credits') {
    writeStorage(STORAGE_KEYS.openCreditSheet, '1')
    Taro.showModal({
      title: BILLING_COPY.insufficientTitle,
      content: billingErrorMessage(error) || BILLING_COPY.insufficientBody,
      confirmText: BILLING_COPY.buyAction,
      success: (res) => {
        if (res.confirm) Taro.switchTab({ url: '/pages/profile/index' })
      },
    })
    return true
  }
  if (kind === 'rate_limited') {
    Taro.showToast({ title: billingErrorMessage(error) || BILLING_COPY.rateLimited, icon: 'none' })
    return true
  }
  if (kind === 'payment_unavailable') {
    Taro.showToast({ title: BILLING_COPY.paymentUnavailable, icon: 'none' })
    return true
  }
  return false
}

export async function purchaseSku(skuId: string): Promise<BillingOrder> {
  const login = await Taro.login()
  const order = await peripherals.createBillingOrder({ sku_id: skuId, code: login.code })
  await requestVirtualPayment(order)
  return peripherals.syncBillingOrder(order.id)
}

function requestVirtualPayment(order: BillingOrder): Promise<void> {
  return new Promise((resolve, reject) => {
    const wxPay = typeof wx !== 'undefined' ? wx.requestVirtualPayment : undefined
    const taroPay = (Taro as unknown as { requestVirtualPayment?: typeof wx.requestVirtualPayment }).requestVirtualPayment
    const pay = wxPay ?? taroPay
    if (!pay || !order.sign_data || !order.pay_sig) {
      reject(new ApiError({ code: 'payment_unavailable', message: BILLING_COPY.paymentUnavailable, statusCode: 503 }))
      return
    }
    pay({
      signData: order.sign_data,
      paySig: order.pay_sig,
      signature: order.signature || '',
      mode: order.mode || 'short_series_goods',
      success: () => resolve(),
      fail: (err) => {
        const message = err?.errMsg || ''
        if (message.includes('cancel') || message.includes('取消')) {
          reject(new ApiError({ code: 'payment_cancelled', message: BILLING_COPY.buyCancel, statusCode: 0 }))
          return
        }
        reject(new Error(message || BILLING_COPY.paymentUnavailable))
      },
    })
  })
}

export function formatPrice(fen: number): string {
  if (fen % 100 === 0) return `¥${fen / 100}`
  if (fen % 10 === 0) return `¥${(fen / 100).toFixed(1)}`
  return `¥${(fen / 100).toFixed(2)}`
}

export function skuOffer(sku: BillingSKU) {
  const original = sku.original_price_fen && sku.original_price_fen > sku.price_fen ? sku.original_price_fen : 0
  const saved = original > 0 ? original - sku.price_fen : 0
  const zhe = original > 0 ? Math.round((sku.price_fen / original) * 10) : 0
  return {
    original,
    saved,
    zhe: zhe > 0 && zhe < 10 ? zhe : 0,
    perCredit: sku.credits > 0 ? Math.round(sku.price_fen / sku.credits) : 0,
  }
}
