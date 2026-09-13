// 方案详情外壳：页面只负责外壳与路由参数（plan_set_id + variant_id），
// 对比、步骤、渲染状态全在 features/planning。
import { useState } from 'react'
import Taro, { useLoad } from '@tarojs/taro'
import { View } from '@tarojs/components'
import { PLAN_DETAIL_COPY } from '@zsm/core'
import { usePageClass } from '../../hooks/use-page-visibility'
import AppHeader from '../../components/app-header'
import PlanDetailScreen from '../../features/planning/PlanDetailScreen'

export default function PlanDetail() {
  const pageClass = usePageClass(true)
  const [planSetId, setPlanSetId] = useState('')
  const [variantId, setVariantId] = useState('')

  useLoad((options) => {
    setPlanSetId(options?.plan_set_id ?? '')
    setVariantId(options?.variant_id ?? '')
  })

  // 没有完整参数就无法定位一套方案：回方案 tab，不猜「最近在看的那套」
  if (!planSetId || !variantId) {
    void Taro.switchTab({ url: '/pages/plans/index' })
    return <View className={pageClass} />
  }

  return (
    <View className={pageClass}>
      <AppHeader title={PLAN_DETAIL_COPY.title} back />
      <PlanDetailScreen planSetId={planSetId} variantId={variantId} />
    </View>
  )
}
