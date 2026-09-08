import { View } from '@tarojs/components'
import AppHeader from '@/components/app-header'
import EmptyState from '@/components/empty-state'
import './index.scss'

export default function Plans() {
  return (
    <View className="page">
      <AppHeader title="方案" />
      <EmptyState title="方案" description="三方案轮播与对比（建设中）" />
    </View>
  )
}
