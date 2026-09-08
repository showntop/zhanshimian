import { View } from '@tarojs/components'
import AppHeader from '@/components/app-header'
import EmptyState from '@/components/empty-state'
import './index.scss'

export default function Home() {
  return (
    <View className="page">
      <AppHeader title="首页" />
      <EmptyState title="首页" description="首页工作台（新用户/回访双状态、任务横轨、场景入口）（建设中）" />
    </View>
  )
}
