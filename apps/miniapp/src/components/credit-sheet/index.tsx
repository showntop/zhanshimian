import { useState } from 'react'
import { Text, View } from '@tarojs/components'
import Taro from '@tarojs/taro'
import { ApiError, BILLING_COPY, type BillingSKU, type BillingSummary } from '@zsm/core'
import PrimaryButton from '../primary-button'
import { formatPrice, purchaseSku, skuOffer } from '../../services/billing'
import './index.scss'

interface CreditSheetProps {
  billing: BillingSummary | null
  onPurchased: () => void
}

function badgeLabel(badge?: string): string {
  if (badge === 'featured') return BILLING_COPY.featured
  if (badge === 'value') return BILLING_COPY.bestValue
  return ''
}

function SkuRow({ sku, busySku, onBuy }: { sku: BillingSKU; busySku: string; onBuy: (id: string) => void }) {
  const offer = skuOffer(sku)
  const badge = badgeLabel(sku.badge)
  const featured = sku.badge === 'featured'
  return (
    <View className={`credit-sheet__sku ${featured ? 'credit-sheet__sku--featured' : ''}`}>
      <View className="credit-sheet__sku-main">
        <View className="credit-sheet__sku-head">
          <Text className="credit-sheet__sku-title">{sku.title}</Text>
          {badge ? <Text className="credit-sheet__badge">{badge}</Text> : null}
        </View>
        <View className="credit-sheet__prices">
          <Text className="credit-sheet__sku-price">{formatPrice(sku.price_fen)}</Text>
          {offer.original > 0 ? (
            <Text className="credit-sheet__sku-original">
              {BILLING_COPY.original} {formatPrice(offer.original)}
            </Text>
          ) : null}
        </View>
        {offer.saved > 0 ? (
          <Text className="credit-sheet__sku-deal">
            {BILLING_COPY.saved} {formatPrice(offer.saved)}
            {offer.zhe > 0 ? ` · ${offer.zhe}${BILLING_COPY.zhe}` : ''}
            {offer.perCredit > 0 ? ` · ${formatPrice(offer.perCredit)}${BILLING_COPY.perCredit}` : ''}
          </Text>
        ) : null}
      </View>
      <PrimaryButton
        text={busySku === sku.id ? BILLING_COPY.paying : BILLING_COPY.buyAction}
        loading={busySku === sku.id}
        disabled={Boolean(busySku) && busySku !== sku.id}
        onClick={() => onBuy(sku.id)}
      />
    </View>
  )
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
        skus.map((sku) => <SkuRow key={sku.id} sku={sku} busySku={busySku} onBuy={buy} />)
      ) : (
        <Text className="credit-sheet__balance">{BILLING_COPY.paymentUnavailable}</Text>
      )}
    </View>
  )
}
