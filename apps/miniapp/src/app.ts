import { PropsWithChildren } from 'react'
import { useLaunch } from '@tarojs/taro'
import { setLocalLooksResolver } from '@zsm/core'
import { localLooksResolver } from './services/local-looks'
import { syncUiSchemaVersion } from './services/storage'
import './app.scss'

// core 的示例图解析器在启动第一时间注入（业务 import api 时也会兜底注入）。
setLocalLooksResolver(localLooksResolver)

// 页面各自从服务端拿数据（app/cache + qualityApi），app 级不再持有业务 id。
function App({ children }: PropsWithChildren) {
  useLaunch(() => {
    syncUiSchemaVersion()
  })
  return children
}

export default App
