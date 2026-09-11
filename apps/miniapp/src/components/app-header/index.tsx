// 自定义导航：状态栏与胶囊按真机测量，禁止写死 rpx。
// 固定栏 + 同高占位，页面内容从导航下方开始，不再依赖 .page 的猜测顶距。
import { useEffect, useState } from 'react'
import Taro from '@tarojs/taro'
import { Text, View } from '@tarojs/components'
import { APP_NAME } from '@zsm/core'
import './index.scss'

interface AppHeaderProps {
  title?: string
  back?: boolean
  transparent?: boolean
  onBack?: () => void
  right?: React.ReactNode
}

function measureNav() {
  try {
    const win = Taro.getWindowInfo()
    const capsule = Taro.getMenuButtonBoundingClientRect?.()
    const statusBar = win.statusBarHeight ?? 47
    if (capsule && capsule.bottom > 0) {
      const gap = Math.max(capsule.top - statusBar, 4)
      return {
        statusBar,
        navHeight: capsule.bottom + gap,
        rightPad: Math.max(96, win.windowWidth - capsule.left + 8),
        windowHeight: win.windowHeight,
        windowWidth: win.windowWidth,
      }
    }
    return {
      statusBar,
      navHeight: statusBar + 44,
      rightPad: 96,
      windowHeight: win.windowHeight,
      windowWidth: win.windowWidth,
    }
  } catch {
    return { statusBar: 47, navHeight: 100, rightPad: 96, windowHeight: 667, windowWidth: 375 }
  }
}

/** 导航真机测量（含视口高度）：满屏布局的页面（如方案页 hero）据此计算可用高度 */
export function getNavMetrics() {
  return measureNav()
}

export default function AppHeader({ title, back, transparent, onBack, right }: AppHeaderProps) {
  const [nav, setNav] = useState(measureNav)

  useEffect(() => {
    setNav(measureNav())
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
    <View className="app-header-wrap">
      <View
        className={`app-header ${transparent ? 'app-header--transparent' : ''}`}
        style={{
          paddingTop: `${nav.statusBar}px`,
          height: `${nav.navHeight}px`,
          paddingRight: `${nav.rightPad}px`,
        }}
      >
        <View className="app-header__bar">
          <View className="app-header__left">
            {back ? (
              <View className="app-header__back pressable" onClick={handleBack}>
                <Text className="app-header__back-icon">‹</Text>
              </View>
            ) : (
              <Text className="app-header__wordmark">{APP_NAME}</Text>
            )}
          </View>
          {title ? <Text className="app-header__title">{title}</Text> : <View />}
          <View className="app-header__right">{right}</View>
        </View>
      </View>
      <View className="app-header-spacer" style={{ height: `${nav.navHeight}px` }} />
    </View>
  )
}
