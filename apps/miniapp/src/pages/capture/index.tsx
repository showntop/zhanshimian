// 建档页外壳：页面只负责外壳（.page 内边距 / 导航），拍摄与提交流程全在 features/capture。
// 页面不再持有任何状态，也不再读 query——assessment 契约里没有场景字段。
import { View } from '@tarojs/components'
import { CAPTURE_COPY } from '@zsm/core'
import { usePageClass } from '../../hooks/use-page-visibility'
import AppHeader from '../../components/app-header'
import CaptureScreen from '../../features/capture/CaptureScreen'

export default function Capture() {
  const pageClass = usePageClass(true)

  return (
    <View className={pageClass}>
      <AppHeader title={CAPTURE_COPY.headerTitle} back />
      <CaptureScreen />
    </View>
  )
}
