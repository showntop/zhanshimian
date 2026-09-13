// 分析进度页外壳：页面只负责外壳（.page 内边距 / 导航 / 路由参数），
// 进度、失败、恢复动作全在 features/assessment。
import { useState } from 'react'
import Taro, { useLoad } from '@tarojs/taro'
import { View } from '@tarojs/components'
import { ASSESSMENT_COPY } from '@zsm/core'
import { usePageClass } from '../../hooks/use-page-visibility'
import AppHeader from '../../components/app-header'
import AssessmentScreen from '../../features/assessment/AssessmentScreen'

export default function Analysis() {
  const pageClass = usePageClass(true)
  const [assessmentId, setAssessmentId] = useState('')
  const [operationId, setOperationId] = useState('')

  useLoad((options) => {
    setAssessmentId(options?.assessment_id ?? '')
    setOperationId(options?.operation_id ?? '')
  })

  // 没有 operation id 就没有可轮询的东西：这一页不猜「当前任务」，回首页重新走。
  // 旧的「Storage 里存当前任务 id」就是在这里长出来的，不再恢复它。
  if (!operationId) {
    void Taro.switchTab({ url: '/pages/home/index' })
    return <View className={pageClass} />
  }

  return (
    <View className={pageClass}>
      <AppHeader title={ASSESSMENT_COPY.headerTitle} back={false} />
      <AssessmentScreen assessmentId={assessmentId} operationId={operationId} />
    </View>
  )
}
