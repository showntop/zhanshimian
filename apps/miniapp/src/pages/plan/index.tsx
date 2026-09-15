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
  // 路由参数在 useLoad 前是空的：未到位前不渲染 Screen，否则挂载效应会拿空 id 误判跳走。
  const [ids, setIds] = useState<{ planSetId: string; variantId: string } | null>(null)

  useLoad((options) => { setIds({ planSetId: options?.plan_set_id ?? '', variantId: options?.variant_id ?? '' }) })

  return (
    <View className={pageClass}>
      <AppHeader title={PLAN_DETAIL_COPY.title} back />
      {ids ? <PlanDetailScreen planSetId={ids.planSetId} variantId={ids.variantId} /> : null}
    </View>
  )
}
