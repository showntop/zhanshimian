// localizeDevImages —— 开发环境 http:// 图片本地化中间件（语义移植自原型 api.js）。
// 微信 3.17+ 拒绝渲染 http:// 图片：开发服务器下发的 http 图片 URL 需先下载到
// 本地文件再渲染。生产 https URL 完全绕过此路径。
// 平台注入 download 实现（小程序端：wx.request arraybuffer + FileSystemManager；
// RN 端一般不需要）。无注入则直通（identity），不产生任何副作用。
import type { ResponseMiddleware } from './client.ts'

/** 下载 http 图片到本地可渲染路径；失败返回 ''（由调用点显示空态/示例图） */
export type ImageDownloader = (url: string) => Promise<string>

const IMAGE_URL_PATTERN = /^http:\/\/\S+\.(png|jpe?g|webp)(\?\S*)?$/i

function collectImageUrls(value: unknown, found: Set<string>): Set<string> {
  if (Array.isArray(value)) {
    for (const item of value) collectImageUrls(item, found)
  } else if (value && typeof value === 'object') {
    for (const key of Object.keys(value as Record<string, unknown>)) {
      collectImageUrls((value as Record<string, unknown>)[key], found)
    }
  } else if (typeof value === 'string' && IMAGE_URL_PATTERN.test(value)) {
    found.add(value)
  }
  return found
}

function swapImageUrls(value: unknown, resolved: Map<string, string>): unknown {
  if (Array.isArray(value)) return value.map((item) => swapImageUrls(item, resolved))
  if (value && typeof value === 'object') {
    const copy: Record<string, unknown> = {}
    for (const key of Object.keys(value as Record<string, unknown>)) {
      copy[key] = swapImageUrls((value as Record<string, unknown>)[key], resolved)
    }
    return copy
  }
  if (typeof value === 'string' && resolved.has(value)) return resolved.get(value)
  return value
}

const LOOPBACK_ORIGIN = /^https?:\/\/(127\.0\.0\.1|localhost|0\.0\.0\.0)(:\d+)?/i

function mapStrings(value: unknown, rewrite: (s: string) => string): unknown {
  if (Array.isArray(value)) return value.map((item) => mapStrings(item, rewrite))
  if (value && typeof value === 'object') {
    const copy: Record<string, unknown> = {}
    for (const key of Object.keys(value as Record<string, unknown>)) {
      copy[key] = mapStrings((value as Record<string, unknown>)[key], rewrite)
    }
    return copy
  }
  return typeof value === 'string' ? rewrite(value) : value
}

/**
 * 生产机 PUBLIC_BASE_URL 误写成 127.0.0.1 时，把 /uploads /assets
 * 改写到当前 API 域名（nginx 会反代到同机 58000）。
 * API 本身就是回环地址时不改，交给 localizeDevImages。
 */
export function rewriteLoopbackAssetURLs(apiBase: string): ResponseMiddleware {
  const base = apiBase.replace(/\/$/, '')
  if (LOOPBACK_ORIGIN.test(base)) return (data) => data
  return (data) =>
    mapStrings(data, (value) => {
      if (!LOOPBACK_ORIGIN.test(value)) return value
      if (!/\/(uploads|assets)\//.test(value)) return value
      return value.replace(LOOPBACK_ORIGIN, base)
    })
}

export function localizeDevImages(download?: ImageDownloader): ResponseMiddleware {
  if (!download) return (data) => data
  // 同一 URL 只下载一次（跨请求共享，等同原型的 imageDownloads Map）
  const cache = new Map<string, Promise<string>>()
  const cachedDownload = (url: string): Promise<string> => {
    let entry = cache.get(url)
    if (!entry) {
      entry = Promise.resolve()
        .then(() => download(url))
        .catch(() => '')
      cache.set(url, entry)
    }
    return entry
  }
  return async (data) => {
    const urls = Array.from(collectImageUrls(data, new Set<string>()))
    if (!urls.length) return data
    const resolved = new Map<string, string>()
    await Promise.all(
      urls.map(async (url) => {
        resolved.set(url, await cachedDownload(url))
      })
    )
    return swapImageUrls(data, resolved)
  }
}
