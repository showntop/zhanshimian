import { View } from '@tarojs/components'
import AppHeader from '@/components/app-header'
import EmptyState from '@/components/empty-state'
import './index.scss'

export default function Feedback() {
  return (
    <View className="page">
      <AppHeader title="实际反馈" />
      <EmptyState title="实际反馈" description="实拍与感受反馈（建设中）" />
    </View>
  )
}
