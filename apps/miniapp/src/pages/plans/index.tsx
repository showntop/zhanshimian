// 方案 tab 外壳：页面只负责外壳（.page / 导航），方案集状态全在 features/planning。
// tab 页不接受 query（switchTab 限制），入口由 app/plan-set-handoff 交接或问服务端。
import { View } from '@tarojs/components'
import { usePageClass } from '../../hooks/use-page-visibility'
import AppHeader from '../../components/app-header'
import PlansScreen from '../../features/planning/PlansScreen'

export default function Plans() {
  const pageClass = usePageClass(true, 'page--tab')

  return (
    <View className={pageClass}>
      <AppHeader />
      <PlansScreen />
    </View>
  )
}
