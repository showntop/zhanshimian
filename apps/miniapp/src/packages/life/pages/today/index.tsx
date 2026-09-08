import { View } from '@tarojs/components'
import AppHeader from '@/components/app-header'
import EmptyState from '@/components/empty-state'
import './index.scss'

export default function Today() {
  return (
    <View className="page">
      <AppHeader title="今日造型" />
      <EmptyState title="今日造型" description="天气上下文每日方案（建设中）" />
    </View>
  )
}
