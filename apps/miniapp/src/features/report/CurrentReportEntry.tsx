// 首页的「当前报告」入口：直接用 bootstrap 里的 report 渲染，
// 点击带 id 进报告页。不落任何 Storage。
import { Text, View } from '@tarojs/components'
import Taro from '@tarojs/taro'
import { HOME_COPY, REPORT_COPY } from '@zsm/core'
import type { Report } from '@zsm/core'

interface CurrentReportEntryProps {
  report: Report
}

export default function CurrentReportEntry({ report }: CurrentReportEntryProps) {
  return (
    <View
      className="report-entry pressable"
      onClick={() =>
        void Taro.navigateTo({
          url: `/pages/report/index?id=${encodeURIComponent(report.id)}`,
        })
      }
    >
      <Text className="report-entry__mark">{REPORT_COPY.currentMark}</Text>
      <Text className="report-entry__title">{report.priority_title}</Text>
      <Text className="report-entry__cta">{HOME_COPY.viewReport}</Text>
    </View>
  )
}
