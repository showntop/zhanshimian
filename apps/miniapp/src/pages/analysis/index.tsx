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
  const [ids, setIds] = useState({ assessmentId: '', operationId: '' })

  useLoad((options) => { setIds({ assessmentId: options?.assessment_id ?? '', operationId: options?.operation_id ?? '' }) })

  return (
    <View className={pageClass}>
      <AppHeader title={ASSESSMENT_COPY.headerTitle} back={false} />
      <AssessmentScreen assessmentId={ids.assessmentId} operationId={ids.operationId} />
    </View>
  )
}
