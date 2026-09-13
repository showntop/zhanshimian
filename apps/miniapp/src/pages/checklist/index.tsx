// 执行清单外壳：页面只负责外壳与路由参数（execution_id），事件与进度全在 features/execution。
import { useState } from 'react'
import Taro, { useLoad } from '@tarojs/taro'
import { View } from '@tarojs/components'
import { CHECKLIST_COPY } from '@zsm/core'
import { usePageClass } from '../../hooks/use-page-visibility'
import AppHeader from '../../components/app-header'
import ExecutionScreen from '../../features/execution/ExecutionScreen'

export default function Checklist() {
  const pageClass = usePageClass(true)
  const [executionId, setExecutionId] = useState('')

  useLoad((options) => {
    setExecutionId(options?.execution_id ?? '')
  })

  // 没有 execution id 就没有快照可执行：回方案 tab，不猜「上次选的那套」
  if (!executionId) {
    void Taro.switchTab({ url: '/pages/plans/index' })
    return <View className={pageClass} />
  }

  return (
    <View className={pageClass}>
      <AppHeader title={CHECKLIST_COPY.title} back />
      <ExecutionScreen executionId={executionId} />
    </View>
  )
}
