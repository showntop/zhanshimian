import { View } from '@tarojs/components'
import AppHeader from '@/components/app-header'
import EmptyState from '@/components/empty-state'
import './index.scss'

export default function Share() {
  return (
    <View className="page">
      <AppHeader title="分享卡" />
      <EmptyState title="分享卡" description="方案/今日分享（建设中）" />
    </View>
  )
}
