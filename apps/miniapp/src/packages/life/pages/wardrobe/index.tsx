import { View } from '@tarojs/components'
import AppHeader from '@/components/app-header'
import EmptyState from '@/components/empty-state'
import './index.scss'

export default function Wardrobe() {
  return (
    <View className="page">
      <AppHeader title="衣橱" />
      <EmptyState title="衣橱" description="轻量衣橱与搭配（建设中）" />
    </View>
  )
}
