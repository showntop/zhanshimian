import { View } from '@tarojs/components'
import AppHeader from '@/components/app-header'
import EmptyState from '@/components/empty-state'
import './index.scss'

export default function Profile() {
  return (
    <View className="page">
      <AppHeader title="我的" />
      <EmptyState title="我的" description="档案摘要与设置（建设中）" />
    </View>
  )
}
