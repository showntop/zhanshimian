// 报告页外壳：加载、证据标注、查看方案全在 features/report；无 id 取「当前报告」。
import { useState } from 'react'
import { useLoad } from '@tarojs/taro'
import { View } from '@tarojs/components'
import { REPORT_COPY } from '@zsm/core'
import { usePageClass } from '../../hooks/use-page-visibility'
import AppHeader from '../../components/app-header'
import ReportScreen from '../../features/report/ReportScreen'

export default function Report() {
  const pageClass = usePageClass(true)
  const [reportId, setReportId] = useState('')

  useLoad((options) => { setReportId(options?.id ?? '') })

  return (
    <View className={pageClass}>
      <AppHeader title={REPORT_COPY.title} back={false} />
      <ReportScreen reportId={reportId || undefined} />
    </View>
  )
}
