// 反馈页外壳：页面只负责外壳与路由参数（execution_id），反馈流程全在 features/feedback。
import { useState } from 'react'
import Taro, { useLoad } from '@tarojs/taro'
import { View } from '@tarojs/components'
import { FEEDBACK_SCREEN_COPY } from '@zsm/core'
import { usePageClass } from '../../hooks/use-page-visibility'
import AppHeader from '../../components/app-header'
import ExecutionFeedbackScreen from '../../features/feedback/ExecutionFeedbackScreen'

export default function Feedback() {
  const pageClass = usePageClass(true)
  const [executionId, setExecutionId] = useState('')

  useLoad((options) => {
    setExecutionId(options?.execution_id ?? '')
  })

  // 反馈只对某一次具体的执行存在：没有 id 就回清单，不猜「刚完成的那次」
  if (!executionId) {
    void Taro.switchTab({ url: '/pages/plans/index' })
    return <View className={pageClass} />
  }

  return (
    <View className={pageClass}>
      <AppHeader title={FEEDBACK_SCREEN_COPY.executionTitle} back />
      <ExecutionFeedbackScreen executionId={executionId} />
    </View>
  )
}
