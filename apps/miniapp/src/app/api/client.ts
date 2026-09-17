// 生成的 API 客户端的小程序接线：baseUrl、token、401 单飞重登、图片本地化。
// 必须在创建客户端之前安装 Fetch 运行时——openapi-fetch 在构造时读取全局量。
import Taro from '@tarojs/taro'
import {
  DEFAULT_NICKNAME,
  ERROR_COPY,
  createGeneratedApiClient,
  localizeDevImages,
  rewriteLoopbackAssetURLs,
} from '@zsm/core'
import type { ImageDownloader, ResponseMiddleware, UploadIntent, UploadedMediaAsset } from '@zsm/core'
import { resolveBaseURL } from '../../config/runtime'
import { STORAGE_KEYS, readStorage, writeStorage } from '../../services/storage'
import type { MediaUploadPort } from './media-upload'
import { TaroResponse, installTaroFetchRuntime } from './taro-fetch'
import { PublicApiError, dataOrThrow, isEmptySuccessStatus } from './result'

export const baseURL = resolveBaseURL()

function readToken(): string {
  return readStorage(STORAGE_KEYS.token)
}

/** 401 重登单飞：并发请求只换一次 token，其余等同一个 Promise。 */
let inflightRelogin: Promise<void> | null = null

export function ensureRelogin(): Promise<void> {
  if (!inflightRelogin) {
    inflightRelogin = relogin().finally(() => {
      inflightRelogin = null
    })
  }
  return inflightRelogin
}

function isDevelopEnv(): boolean {
  try {
    return Taro.getAccountInfoSync().miniProgram.envVersion === 'develop'
  } catch {
    return true
  }
}

function tokenFrom(res: { statusCode: number; data: unknown }): string {
  if (res.statusCode !== 201 && res.statusCode !== 200) return ''
  return (res.data as { data?: { token?: string } })?.data?.token ?? ''
}

/** wx.login 换 code → POST /v1/auth/wechat → 存 token；开发工具可退回 /v1/auth/dev。 */
async function relogin(): Promise<void> {
  const { code } = await Taro.login()
  const wechat = await Taro.request({
    url: `${baseURL}/v1/auth/wechat`,
    method: 'POST',
    data: { code, nickname: DEFAULT_NICKNAME },
    header: { 'content-type': 'application/json' },
    timeout: 15000,
  })
  let token = tokenFrom(wechat)
  if (!token && isDevelopEnv()) {
    const dev = await Taro.request({
      url: `${baseURL}/v1/auth/dev`,
      method: 'POST',
      data: { nickname: DEFAULT_NICKNAME },
      header: { 'content-type': 'application/json' },
      timeout: 15000,
    })
    token = tokenFrom(dev)
  }
  if (!token) throw new Error('微信登录失败，请重试')
  writeStorage(STORAGE_KEYS.token, token)
}

installTaroFetchRuntime({ resolveToken: readToken, onUnauthorized: ensureRelogin })

// 开发环境 http:// 图片本地化：微信 3.17+ 拒绝渲染 http:// 图片，
// 下载到 USER_DATA_PATH 再替换；URL 级缓存跨请求共享（生产 https 直接旁路）。
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
    /* 未缓存：继续下载 */
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

// 先把生产误下发的 127.0.0.1/uploads 改到当前 API 域名，再本地化剩余 http 图。
const transformBody: ResponseMiddleware = async (data: unknown) => {
  const rewritten = await rewriteLoopbackAssetURLs(baseURL)(data)
  return localizeDevImages(downloader)(rewritten)
}

export const client = createGeneratedApiClient({ baseUrl: baseURL })

// 解包与图片改写挂在响应中间件上：调用方拿到的是契约里的 body，不是 Fetch Response。
client.use({
  async onResponse({ response }) {
    if (!response.ok) return response
    // 204/205 没有响应体（「删除我的数据」成功就回 204）：没有 JSON 可解，
    // 原样交回 openapi-fetch 的空体分支；硬解只会让 JSON.parse('') 把成功炸成失败。
    if (isEmptySuccessStatus(response.status)) return response
    const payload: unknown = await response.json()
    const transformed = await transformBody(payload)
    return new TaroResponse(transformed, response.status, response.headers) as unknown as Response
  },
})

/**
 * 幂等键由业务语义决定：同一用途的同一个文件（purpose + sha256）重试时复用同一把键，
 * 服务端因此重放而不是重复建资产。不要在这里引入时间戳或随机数。
 * 注意种子必须含 purpose：同一张照片可以分别进建档（face/side/body）和
 * 穿搭/购买（body/wardrobe），不带 purpose 时同一 sha256 换用途会撞
 * 同一把键、payload 不同 → 409 idempotency_conflict。
 */
function idempotencyKey(kind: string, seed: string): string {
  return `${kind}:${seed}`
}

function readFileBuffer(filePath: string): Promise<ArrayBuffer> {
  const fs = Taro.getFileSystemManager()
  return new Promise((resolve, reject) => {
    fs.readFile({
      filePath,
      success: (res) => resolve(res.data as ArrayBuffer),
      fail: (e) => reject(new Error(e.errMsg || '读取照片失败')),
    })
  })
}

export const mediaUpload: MediaUploadPort = {
  createIntent: (input) =>
    client
      .POST('/v1/media/upload-intents', {
        body: input,
        params: { header: { 'Idempotency-Key': idempotencyKey('upload-intent', `${input.purpose}:${input.sha256}`) } },
      })
      .then(dataOrThrow),

  // 直传是原始字节 PUT，不是 multipart：Taro.request 的 data 传 ArrayBuffer 即原样发送。
  putObject: async (intent, filePath) => {
    const body = await readFileBuffer(filePath)
    const res = await Taro.request({
      url: intent.upload.url,
      method: intent.upload.method as never,
      data: body as never,
      header: intent.upload.headers,
      timeout: 30000,
    })
    if (res.statusCode < 200 || res.statusCode >= 300) {
      // 真机排障：COS 的错误体（XML）里才有 SignatureDoesNotMatch 这类具体原因
      console.warn('[upload] putObject rejected', res.statusCode, typeof res.data === 'string' ? res.data.slice(0, 500) : res.data)
      throw new PublicApiError('upload_failed', ERROR_COPY.uploadFailed, res.statusCode, '', true)
    }
  },

  completeIntent: (intentId) =>
    client
      .POST('/v1/media/upload-intents/{id}/complete', {
        params: {
          path: { id: intentId },
          header: { 'Idempotency-Key': idempotencyKey('upload-complete', intentId) },
        },
        body: {},
      })
      .then(dataOrThrow),
}
