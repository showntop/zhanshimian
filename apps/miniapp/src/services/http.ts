// Taro 适配器：把 Taro.request / Taro.uploadFile 桥接到 @zsm/core 的传输接口。
// 业务代码只允许 import 本文件的 client，不得直接调 Taro.request。
import Taro from '@tarojs/taro'
import {
  createApiClient,
  localizeDevImages,
  type HttpAdapter,
  type ImageDownloader,
  type ResponseMiddleware,
  type UploadAdapter,
} from '@zsm/core'
import { resolveBaseURL } from '../config/runtime'
import { STORAGE_KEYS, readStorage, writeStorage } from './storage'

export const baseURL = resolveBaseURL()

const adapter: HttpAdapter = {
  async request(req) {
    const res = await Taro.request({
      url: baseURL + req.path,
      method: (req.method || 'GET') as never,
      data: req.data as never,
      header: { ...req.header },
      timeout: req.timeout ?? 15000,
    })
    return {
      statusCode: res.statusCode,
      data: res.data as unknown,
      header: (res.header ?? {}) as Record<string, string>,
    }
  },
}

const upload: UploadAdapter = {
  async upload(req) {
    const res = await Taro.uploadFile({
      url: baseURL + req.path,
      filePath: req.filePath,
      name: req.name || 'file',
      formData: req.formData as Record<string, string>,
      header: { ...req.header },
      timeout: req.timeout ?? 30000,
    })
    return {
      statusCode: res.statusCode,
      data: res.data as unknown,
      header: (res.header ?? {}) as Record<string, string>,
    }
  },
}

// 开发环境 http:// 图片本地化：微信 3.17+ 拒绝 http:// 图片 URL，
// 下载到 USER_DATA_PATH 并替换（生产 https 完全旁路；URL 级缓存跨请求共享）。
const downloader: ImageDownloader = async (url: string): Promise<string> => {
  let hash = 5381
  for (let i = 0; i < url.length; i += 1) hash = ((hash << 5) + hash + url.charCodeAt(i)) >>> 0
  const match = /\.(png|jpe?g|webp)(\?|$)/i.exec(url)
  const ext: string = match?.[1] ?? 'jpg'
  const target = `${Taro.env.USER_DATA_PATH}/zsm-${hash.toString(36)}.${ext}`
  const fs = Taro.getFileSystemManager()
  try {
    fs.accessSync(target)
    return target
  } catch {
    /* 未缓存：下载 */
  }
  const res = await Taro.request({ url, responseType: 'arraybuffer', timeout: 15000 })
  if (res.statusCode !== 200) throw new Error(`download failed: ${res.statusCode}`)
  await new Promise<void>((resolve, reject) => {
    fs.writeFile({
      filePath: target,
      data: res.data as ArrayBuffer,
      success: () => resolve(),
      fail: (e) => reject(new Error(e.errMsg || 'write failed')),
    })
  })
  return target
}

const middleware: ResponseMiddleware[] = baseURL.startsWith('https://')
  ? []
  : [localizeDevImages(downloader)]

/** 401 单飞重登：wx.login 换微信 code → POST /v1/auth/wechat → 存 token。 */
async function relogin(): Promise<void> {
  const { code } = await Taro.login()
  const res = await Taro.request({
    url: `${baseURL}/v1/auth/wechat`,
    method: 'POST',
    data: { code, nickname: '怎么打扮用户' },
    header: { 'content-type': 'application/json' },
    timeout: 15000,
  })
  if (res.statusCode !== 201 && res.statusCode !== 200) {
    throw new Error('微信登录失败，请重试')
  }
  const token = (res.data as { data?: { token?: string } })?.data?.token ?? ''
  if (!token) throw new Error('微信登录失败，请重试')
  writeStorage(STORAGE_KEYS.token, token)
}

export const client = createApiClient({
  adapter,
  upload,
  baseUrl: baseURL,
  store: {
    get: () => readStorage(STORAGE_KEYS.token),
    set: (t) => writeStorage(STORAGE_KEYS.token, t),
    clear: () => writeStorage(STORAGE_KEYS.token, ''),
  },
  relogin,
  middleware,
})
