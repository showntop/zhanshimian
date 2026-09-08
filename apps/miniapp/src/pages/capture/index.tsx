import { View } from '@tarojs/components'
import AppHeader from '@/components/app-header'
import EmptyState from '@/components/empty-state'
import './index.scss'

export default function Capture() {
  return (
    <View className="page">
      <AppHeader title="创建形象档案" />
      <EmptyState title="创建形象档案" description="三张自然光照片采集（建设中）" />
    </View>
  )
}
