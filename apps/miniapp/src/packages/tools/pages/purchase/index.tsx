import { View } from '@tarojs/components'
import AppHeader from '@/components/app-header'
import EmptyState from '@/components/empty-state'
import './index.scss'

export default function Purchase() {
  return (
    <View className="page">
      <AppHeader title="购买判断" />
      <EmptyState title="购买判断" description="买前适配判断（建设中）" />
    </View>
  )
}
