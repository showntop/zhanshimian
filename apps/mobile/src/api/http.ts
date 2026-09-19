import {
  createApiClient,
  createApiEndpoints,
  setDefaultEventSender,
  setLocalLooksResolver,
  type HttpAdapter,
  type LookSlug,
  type LookVariant,
  type UploadAdapter,
} from '@zsm/core'
import { Image } from 'react-native'
import { resolveBaseURL } from '../config'
import { STORAGE_KEYS, tokenStore, writeStorage } from '../storage'

export const baseURL = resolveBaseURL()

// 男士发型方向（men-*）只有发型位有素材，与小程序同图同命名。
const EXAMPLES: Partial<Record<LookSlug, number>> = {
  natural: require('../../assets/examples/natural.jpg'),
  sharp: require('../../assets/examples/sharp.jpg'),
  warm: require('../../assets/examples/warm.jpg'),
  'men-crop': require('../../assets/examples/men-crop.jpg'),
  'men-side': require('../../assets/examples/men-side.jpg'),
  'men-texture': require('../../assets/examples/men-texture.jpg'),
}

setLocalLooksResolver({
  resolve(slug: LookSlug, _variant: LookVariant): string {
    // 没有对应素材就返回空（由调用方决定占位）——绝不回落到 natural，
    // 否则男士方向会拿到女模特图（红线 3）
    const asset = EXAMPLES[slug]
    if (!asset) return ''
    return Image.resolveAssetSource(asset)?.uri ?? ''
  },
})

const adapter: HttpAdapter = {
  async request(req) {
    const controller = new AbortController()
    const timer = setTimeout(() => controller.abort(), req.timeout ?? 15000)
    try {
      const res = await fetch(baseURL + req.path, {
        method: req.method || 'GET',
        headers: { 'content-type': 'application/json', ...req.header },
        body: req.data === undefined ? undefined : JSON.stringify(req.data),
        signal: controller.signal,
      })
      const text = await res.text()
      let data: unknown = text
      if (text) {
        try {
          data = JSON.parse(text)
        } catch {
          data = text
        }
      }
      return { statusCode: res.status, data, header: Object.fromEntries(res.headers.entries()) }
    } finally {
      clearTimeout(timer)
    }
  },
}

const upload: UploadAdapter = {
  async upload(req) {
    const form = new FormData()
    form.append(req.name || 'file', {
      uri: req.filePath,
      name: 'photo.jpg',
      type: 'image/jpeg',
    } as unknown as Blob)
    for (const [key, value] of Object.entries(req.formData ?? {})) {
      form.append(key, String(value))
    }
    const res = await fetch(baseURL + req.path, {
      method: 'POST',
      headers: { ...req.header },
      body: form,
    })
    const text = await res.text()
    let data: unknown = text
    if (text) {
      try {
        data = JSON.parse(text)
      } catch {
        data = text
      }
    }
    return { statusCode: res.status, data, header: Object.fromEntries(res.headers.entries()) }
  },
}

export const client = createApiClient({
  adapter,
  upload,
  baseUrl: baseURL,
  store: tokenStore,
  relogin: async () => {
    tokenStore.clear()
    throw new Error('登录已过期，请重新登录')
  },
})

setDefaultEventSender((body) => client.request('/v1/events', { method: 'POST', data: body }))

export const api = createApiEndpoints(client, {
  clearReportRef: () => writeStorage(STORAGE_KEYS.reportId, ''),
})
