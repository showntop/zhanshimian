// 报告页外壳：加载、证据标注、查看方案全在 features/report；无 id 取「当前报告」。
import { useState } from 'react'
import { useLoad } from '@tarojs/taro'
import { View } from '@tarojs/components'
import { REPORT_COPY } from '@zsm/core'
import { usePageShell } from '../../hooks/use-page-visibility'
import AppHeader from '../../components/app-header'
import ReportScreen from '../../features/report/ReportScreen'

export default function Report() {
  // ready 由 ReportScreen 在内容首次上屏时点亮（恒 true 会让 page--settled 提前把 fade-up 压掉）
  const [ready, setReady] = useState(false)
  const { pageClass, enter } = usePageShell(ready, '', 'report')
  const [reportId, setReportId] = useState('')
  useLoad((options) => { setReportId(options?.id ?? '') })

  return (
    <View className={pageClass}>
      <AppHeader title={REPORT_COPY.title} back />
      <ReportScreen reportId={reportId || undefined} onReady={() => setReady(true)} enter={enter} />
    </View>
  )
}
