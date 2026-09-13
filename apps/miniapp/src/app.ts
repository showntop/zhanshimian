import { PropsWithChildren } from 'react'
import { useLaunch } from '@tarojs/taro'
import { setDefaultEventSender, setLocalLooksResolver } from '@zsm/core'
import { localLooksResolver } from './services/local-looks'
import { syncUiSchemaVersion } from './services/storage'
import { client } from './app/api/client'
import './app.scss'

// core 的示例图解析器在启动第一时间注入（业务 import api 时也会兜底注入）。
setLocalLooksResolver(localLooksResolver)
// 埋点发送走唯一客户端：trackEvent 只组 payload，不发第二条网络通道。
setDefaultEventSender((body) =>
  client.POST('/v1/events', { body: body as { name: string } }).then((result) => {
    if (result.error) throw new Error('track_event_failed')
  }),
)

// 页面各自从服务端拿数据（app/cache + qualityApi），app 级不再持有业务 id。
function App({ children }: PropsWithChildren) {
  useLaunch(() => {
    syncUiSchemaVersion()
  })
  return children
}

export default App
