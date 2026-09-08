import { View } from '@tarojs/components'
import AppHeader from '@/components/app-header'
import EmptyState from '@/components/empty-state'
import './index.scss'

export default function Outfit() {
  return (
    <View className="page">
      <AppHeader title="穿搭诊断" />
      <EmptyState title="穿搭诊断" description="单图穿搭诊断（建设中）" />
    </View>
  )
}
