import { View } from '@tarojs/components'
import AppHeader from '@/components/app-header'
import EmptyState from '@/components/empty-state'
import './index.scss'

export default function Analysis() {
  return (
    <View className="page">
      <AppHeader title="正在分析" />
      <EmptyState title="正在分析" description="异步分析进度与扫描动效（建设中）" />
    </View>
  )
}
