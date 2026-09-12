import { useState } from 'react'
import { Text, View } from '@tarojs/components'
import Taro from '@tarojs/taro'
import { ApiError, BILLING_COPY, type BillingSummary } from '@zsm/core'
import PrimaryButton from '../primary-button'
import { formatPrice, purchaseSku } from '../../services/billing'
import './index.scss'

interface CreditSheetProps {
  billing: BillingSummary | null
  onPurchased: () => void
}

export default function CreditSheet({ billing, onPurchased }: CreditSheetProps) {
  const [busySku, setBusySku] = useState('')
  const skus = billing?.skus ?? []

  const buy = async (skuId: string) => {
    if (!billing?.payment_enabled) {
      Taro.showToast({ title: BILLING_COPY.paymentUnavailable, icon: 'none' })
      return
    }
    setBusySku(skuId)
    try {
      const order = await purchaseSku(skuId)
      if (order.status === 'fulfilled') {
        Taro.showToast({ title: BILLING_COPY.buySuccess, icon: 'success' })
        onPurchased()
      } else {
        Taro.showToast({ title: BILLING_COPY.buyPending, icon: 'none' })
      }
    } catch (error) {
      if (error instanceof ApiError && error.code === 'payment_cancelled') {
        Taro.showToast({ title: BILLING_COPY.buyCancel, icon: 'none' })
      } else {
        Taro.showToast({ title: (error as Error).message || BILLING_COPY.paymentUnavailable, icon: 'none' })
      }
    } finally {
      setBusySku('')
    }
  }

  return (
    <View className="credit-sheet">
      <Text className="credit-sheet__balance">
        {BILLING_COPY.remaining} {billing?.credits ?? 0} {BILLING_COPY.packUnit}
      </Text>
      {skus.length > 0 ? (
        skus.map((sku) => (
          <View key={sku.id} className="credit-sheet__sku">
            <View className="credit-sheet__sku-main">
              <Text className="credit-sheet__sku-title">{sku.title}</Text>
              <Text className="credit-sheet__sku-price">{formatPrice(sku.price_fen)}</Text>
            </View>
            <PrimaryButton
              text={busySku === sku.id ? BILLING_COPY.paying : BILLING_COPY.buyAction}
              loading={busySku === sku.id}
              disabled={Boolean(busySku) && busySku !== sku.id}
              onClick={() => buy(sku.id)}
            />
          </View>
        ))
      ) : (
        <Text className="credit-sheet__balance">{BILLING_COPY.paymentUnavailable}</Text>
      )}
    </View>
  )
}
