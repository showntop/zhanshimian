// 方案详情外壳：对比、步骤与渲染状态全在 features/planning。
import { useState } from 'react'
import { useLoad } from '@tarojs/taro'
import { View } from '@tarojs/components'
import { PLAN_DETAIL_COPY } from '@zsm/core'
import { usePageClass } from '../../hooks/use-page-visibility'
import AppHeader from '../../components/app-header'
import PlanDetailScreen from '../../features/planning/PlanDetailScreen'

export default function PlanDetail() {
  const pageClass = usePageClass(true)
  const [ids, setIds] = useState({ planSetId: '', variantId: '' })

  useLoad((options) => { setIds({ planSetId: options?.plan_set_id ?? '', variantId: options?.variant_id ?? '' }) })

  return (
    <View className={pageClass}>
      <AppHeader title={PLAN_DETAIL_COPY.title} back />
      <PlanDetailScreen planSetId={ids.planSetId} variantId={ids.variantId} />
    </View>
  )
}
