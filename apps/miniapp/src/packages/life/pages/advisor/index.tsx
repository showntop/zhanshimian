import { View } from '@tarojs/components'
import AppHeader from '@/components/app-header'
import EmptyState from '@/components/empty-state'
import './index.scss'

export default function Advisor() {
  return (
    <View className="page">
      <AppHeader title="私人顾问" />
      <EmptyState title="私人顾问" description="对话式形象顾问（建设中）" />
    </View>
  )
}
