import { View } from '@tarojs/components'
import AppHeader from '@/components/app-header'
import EmptyState from '@/components/empty-state'
import './index.scss'

export default function Hair() {
  return (
    <View className="page">
      <AppHeader title="发型预览" />
      <EmptyState title="发型预览" description="AI 发型效果预览（建设中）" />
    </View>
  )
}
