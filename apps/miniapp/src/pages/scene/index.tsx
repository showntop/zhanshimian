import { View } from '@tarojs/components'
import AppHeader from '@/components/app-header'
import EmptyState from '@/components/empty-state'
import './index.scss'

export default function Scene() {
  return (
    <View className="page">
      <AppHeader title="场合需求" />
      <EmptyState title="场合需求" description="面试/婚礼/约会/日常 四场景 Brief（建设中）" />
    </View>
  )
}
