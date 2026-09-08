import { View } from '@tarojs/components'
import AppHeader from '@/components/app-header'
import EmptyState from '@/components/empty-state'
import './index.scss'

export default function Plan() {
  return (
    <View className="page">
      <AppHeader title="方案详情" />
      <EmptyState title="方案详情" description="发型/妆容/穿搭与发型师参考卡（建设中）" />
    </View>
  )
}
