// 方案 tab 外壳：方案集状态全在 features/planning；
// tab 页不接受 query，入口由内存交接条或服务端「最近方案集」决定。
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
