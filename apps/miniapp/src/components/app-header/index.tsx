// 自定义导航：真机测量状态栏高度与胶囊位置（禁止写死 rpx），
// 输出 --nav-height 供页面布局；styleIsolation apply-shared 共享全局类。
import { useEffect, useState } from 'react'
import Taro from '@tarojs/taro'
import { Text, View } from '@tarojs/components'
import './index.scss'

interface AppHeaderProps {
  title?: string
  back?: boolean
  transparent?: boolean
  onBack?: () => void
  right?: React.ReactNode
}

export default function AppHeader({ title, back, transparent, onBack, right }: AppHeaderProps) {
  const [padTop, setPadTop] = useState(88)

  useEffect(() => {
    try {
      const windowInfo = Taro.getWindowInfo()
      const capsule = Taro.getMenuButtonBoundingClientRect?.()
      const statusBar = windowInfo.statusBarHeight ?? 44
      const capsuleHeight = capsule && capsule.height > 0 ? capsule.height : 32
      // 导航内容高度对齐胶囊：胶囊高度 + 上下各 (capsuleTop - statusBar) 的空隙
      const gap = capsule ? Math.max(capsule.top - statusBar, 4) * 2 : 12
      setPadTop(statusBar + gap + capsuleHeight)
    } catch {
      setPadTop(88)
    }
  }, [])

  const handleBack = () => {
    if (onBack) {
      onBack()
      return
    }
    const pages = Taro.getCurrentPages()
    if (pages.length > 1) Taro.navigateBack()
    else Taro.switchTab({ url: '/pages/home/index' })
  }

  return (
    <View className={`app-header ${transparent ? 'app-header--transparent' : ''}`} style={{ paddingTop: `${padTop}px` }}>
      <View className="app-header__bar">
        <View className="app-header__left">
          {back ? (
            <View className="app-header__back pressable" onClick={handleBack}>
              <Text className="app-header__back-icon">‹</Text>
            </View>
          ) : (
            <Text className="app-header__wordmark">
              怎么打<Text className="app-header__wordmark-accent">扮</Text>
            </Text>
          )}
        </View>
        {title ? <Text className="app-header__title">{title}</Text> : <View />}
        <View className="app-header__right">{right}</View>
      </View>
    </View>
  )
}
