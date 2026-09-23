// 分析进度页外壳：进度、失败与恢复动作全在 features/assessment。
import { useState } from 'react'
import { useLoad } from '@tarojs/taro'
import { View } from '@tarojs/components'
import { ASSESSMENT_COPY } from '@zsm/core'
import { usePageClass } from '../../hooks/use-page-visibility'
import AppHeader from '../../components/app-header'
import AssessmentScreen from '../../features/assessment/AssessmentScreen'

export default function Analysis() {
  const pageClass = usePageClass(true)
  // 路由参数在 useLoad 前是空的：未到位前不渲染 Screen，否则挂载效应会拿空 id 误判跳走。
  const [ids, setIds] = useState<{ assessmentId: string; operationId: string } | null>(null)

  useLoad((options) => { setIds({ assessmentId: options?.assessment_id ?? '', operationId: options?.operation_id ?? '' }) })

  return (
    <View className={pageClass}>
      <AppHeader title={ASSESSMENT_COPY.headerTitle} back={false} />
      {ids ? <AssessmentScreen assessmentId={ids.assessmentId} operationId={ids.operationId} /> : null}
    </View>
  )
}
