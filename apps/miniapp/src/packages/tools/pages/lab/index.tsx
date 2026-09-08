import { View } from '@tarojs/components'
import AppHeader from '@/components/app-header'
import EmptyState from '@/components/empty-state'
import './index.scss'

export default function Lab() {
  return (
    <View className="page">
      <AppHeader title="体验实验室" />
      <EmptyState title="体验实验室" description="AR/3D/试衣（M3 前占位）（建设中）" />
    </View>
  )
}
