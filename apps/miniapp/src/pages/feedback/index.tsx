// 反馈页外壳：反馈流程全在 features/feedback。
import { useState } from 'react'
import { useLoad } from '@tarojs/taro'
import { View } from '@tarojs/components'
import { FEEDBACK_SCREEN_COPY } from '@zsm/core'
import { usePageClass } from '../../hooks/use-page-visibility'
import AppHeader from '../../components/app-header'
import ExecutionFeedbackScreen from '../../features/feedback/ExecutionFeedbackScreen'

export default function Feedback() {
  const pageClass = usePageClass(true)
  const [executionId, setExecutionId] = useState('')

  useLoad((options) => { setExecutionId(options?.execution_id ?? '') })

  return (
    <View className={pageClass}>
      <AppHeader title={FEEDBACK_SCREEN_COPY.executionTitle} back />
      <ExecutionFeedbackScreen executionId={executionId} />
    </View>
  )
}
