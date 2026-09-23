import { PropsWithChildren } from 'react'
import Taro, { useLaunch } from '@tarojs/taro'
import { setDefaultEventSender, setLocalLooksResolver } from '@zsm/core'
import { localLooksResolver } from './services/local-looks'
import { syncUiSchemaVersion } from './services/storage'
import { client } from './app/api/client'
import { resolveBaseURL } from './config/runtime'
import { warmDressAssets } from './components/dress-shuffle/use-dress-assets'
import './app.scss'

// 换装洗牌素材预热：上一次服务端给的 variant 是 dress 时，在首页挂载前就开始
// 拉素材，把首屏那 1~3s 的静态前奏压掉。键与首页一致（同天才认）。
function warmDressIfNeeded(): void {
  try {
    const now = new Date()
    const dateKey = `${now.getFullYear()}-${now.getMonth() + 1}-${now.getDate()}`
    const remembered = Taro.getStorageSync('zsm_dress_variant')
    if (typeof remembered !== 'string' || !remembered.startsWith(`${dateKey}|dress`)) return
    warmDressAssets(`${resolveBaseURL()}/assets/daily/dress`)
  } catch {
    // 预热是纯优化：读不到/失败都不影响首页（首页自己会预载）
  }
}

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
    warmDressIfNeeded()
  })
  return children
}

export default App
