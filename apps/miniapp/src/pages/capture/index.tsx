// 建档页外壳：拍摄与提交流程全在 features/capture。
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
