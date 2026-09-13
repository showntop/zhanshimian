// 报告页外壳：页面只负责外壳（.page 内边距 / 导航 / 路由参数），
// 报告加载、证据标注、查看方案全在 features/report。
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

  useLoad((options) => {
    setReportId(options?.id ?? '')
  })

  return (
    <View className={pageClass}>
      <AppHeader title={REPORT_COPY.title} back={false} />
      {/* 没有 id 就取「当前报告」——空 id 不拦截，交给 ReportScreen 呈现空态 */}
      <ReportScreen reportId={reportId || undefined} />
    </View>
  )
}
