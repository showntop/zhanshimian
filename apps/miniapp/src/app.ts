import { PropsWithChildren } from 'react'
import { useLaunch } from '@tarojs/taro'
import { setLocalLooksResolver } from '@zsm/core'
import { localLooksResolver } from './services/local-looks'
import { STORAGE_KEYS, readStorage } from './services/storage'
import './app.scss'

// core 的示例图解析器在启动第一时间注入（业务 import api 时也会兜底注入）。
setLocalLooksResolver(localLooksResolver)

// 全局共享的少量状态：缓存被清后为空，页面按空值走恢复逻辑。
export const globalData = {
  reportId: '',
  planId: '',
}

function App({ children }: PropsWithChildren) {
  useLaunch(() => {
    globalData.reportId = readStorage(STORAGE_KEYS.reportId)
    globalData.planId = readStorage(STORAGE_KEYS.planId)
  })
  return children
}

export default App
