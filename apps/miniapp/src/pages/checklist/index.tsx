// 执行清单外壳：事件与进度全在 features/execution。
import { useState } from 'react'
import { useLoad } from '@tarojs/taro'
import { View } from '@tarojs/components'
import { CHECKLIST_COPY } from '@zsm/core'
import { usePageClass } from '../../hooks/use-page-visibility'
import AppHeader from '../../components/app-header'
import ExecutionScreen from '../../features/execution/ExecutionScreen'

export default function Checklist() {
  const pageClass = usePageClass(true)
  const [executionId, setExecutionId] = useState('')

  useLoad((options) => { setExecutionId(options?.execution_id ?? '') })

  return (
    <View className={pageClass}>
      <AppHeader title={CHECKLIST_COPY.title} back />
      <ExecutionScreen executionId={executionId} />
    </View>
  )
}
