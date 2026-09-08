import { View } from '@tarojs/components'
import AppHeader from '@/components/app-header'
import EmptyState from '@/components/empty-state'
import './index.scss'

export default function ProfileSetup() {
  return (
    <View className="page">
      <AppHeader title="补充资料" />
      <EmptyState title="补充资料" description="身高/职业/预算（可跳过）（建设中）" />
    </View>
  )
}
