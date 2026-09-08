import { View } from '@tarojs/components'
import AppHeader from '@/components/app-header'
import EmptyState from '@/components/empty-state'
import './index.scss'

export default function Report() {
  return (
    <View className="page">
      <AppHeader title="形象报告" />
      <EmptyState title="形象报告" description="当前形象与可提升点（建设中）" />
    </View>
  )
}
