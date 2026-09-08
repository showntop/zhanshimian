import { View } from '@tarojs/components'
import AppHeader from '@/components/app-header'
import EmptyState from '@/components/empty-state'
import './index.scss'

export default function Checklist() {
  return (
    <View className="page">
      <AppHeader title="执行清单" />
      <EmptyState title="执行清单" description="可勾选改造清单（建设中）" />
    </View>
  )
}
