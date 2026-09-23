// 媒体真实性契约（逐函数移植原型 miniapp/utils/media.js，见 AGENTS.md 红线 3）。
// - 用户本人照片用 userImage()：URL 无效一律返回 ''，保持可见的空；
// - API 下发的方案图/效果图用 lookImage()：无效（空、非法、旧 webp）同样返回 ''，
//   绝不隐式回退到内置模特图。调用点必须显式二选一：显示占位/空态，或调用
//   exampleImage() 并在 UI 上叠加「风格参考」/「示例」角标 + .example-soft 弱化；
// - exampleImage() 是唯一返回内置模特图的入口（经注入的 LocalLooksResolver 解析）；
// - isBundledAsset()：服务端下发的 /assets/(looks|plans|portraits|reports|hair)/*
//   是与包内同源的内置模特素材，命中时按示例图对待（叠角标），不论 URL 是否可渲染。

// 后三个是发型页的男士方向参考图：只有 hair 位有真实资产（assets/hair/men-*.jpg），
// 其它 variant 位不存在对应素材——映射里填的是同一张，但**只有 hair 位允许使用**。
export const LOCAL_LOOK_SLUGS = ['natural', 'sharp', 'warm', 'men-crop', 'men-side', 'men-texture'] as const
export const LOOK_VARIANTS = ['full', 'portrait', 'report', 'hair', 'plan'] as const

export type LookSlug = (typeof LOCAL_LOOK_SLUGS)[number]
export type LookVariant = (typeof LOOK_VARIANTS)[number]

/** 平台注入的本地示例图解析器：小程序返回包内绝对路径，RN 返回 require 资源 */
export interface LocalLooksResolver {
  resolve(slug: LookSlug, variant: LookVariant): string
}

// 默认解析器：小程序包内绝对路径（copy.patterns 保 /assets/*.jpg 契约）
const BUNDLED_LOOK_PATHS: Record<LookSlug, Record<LookVariant, string>> = {
  natural: {
    full: '/assets/looks/natural.jpg',
    portrait: '/assets/portraits/natural.jpg',
    report: '/assets/reports/natural.jpg',
    hair: '/assets/hair/natural.jpg',
    plan: '/assets/plans/natural.jpg'
  },
  sharp: {
    full: '/assets/looks/sharp.jpg',
    portrait: '/assets/portraits/sharp.jpg',
    report: '/assets/reports/sharp.jpg',
    hair: '/assets/hair/sharp.jpg',
    plan: '/assets/plans/sharp.jpg'
  },
  warm: {
    full: '/assets/looks/warm.jpg',
    portrait: '/assets/portraits/warm.jpg',
    report: '/assets/reports/warm.jpg',
    hair: '/assets/hair/warm.jpg',
    plan: '/assets/plans/warm.jpg'
  },
  // 男士发型方向：只有 hair 位有真实资产，其余位是为了满足映射完整性填的同一个
  // 路径——**调用方只能用 variant: 'hair'**，拿它当 full/portrait 用就是错配素材。
  'men-crop': {
    full: '/assets/hair/men-crop.jpg',
    portrait: '/assets/hair/men-crop.jpg',
    report: '/assets/hair/men-crop.jpg',
    hair: '/assets/hair/men-crop.jpg',
    plan: '/assets/hair/men-crop.jpg'
  },
  'men-side': {
    full: '/assets/hair/men-side.jpg',
    portrait: '/assets/hair/men-side.jpg',
    report: '/assets/hair/men-side.jpg',
    hair: '/assets/hair/men-side.jpg',
    plan: '/assets/hair/men-side.jpg'
  },
  'men-texture': {
    full: '/assets/hair/men-texture.jpg',
    portrait: '/assets/hair/men-texture.jpg',
    report: '/assets/hair/men-texture.jpg',
    hair: '/assets/hair/men-texture.jpg',
    plan: '/assets/hair/men-texture.jpg'
  }
}

const defaultResolver: LocalLooksResolver = {
  resolve(slug, variant) {
    return BUNDLED_LOOK_PATHS[slug][variant]
  }
}

let resolver: LocalLooksResolver = defaultResolver

/** 平台启动时注入自己的示例图解析器（RN 端 require 静态资源等） */
export function setLocalLooksResolver(next: LocalLooksResolver): void {
  resolver = next
}

export function getLocalLooksResolver(): LocalLooksResolver {
  return resolver
}

function isDisplayableImage(value: unknown): value is string {
  if (typeof value !== 'string' || !value) return false
  return (
    value.startsWith('https://') ||
    value.startsWith('http://') ||
    value.startsWith('/assets/') ||
    value.startsWith('wxfile://') ||
    value.startsWith('file://')
  )
}

// 严格模式：只返回真实可用的 API/本地图片 URL。空、非法、旧 webp 引用一律返回 ''，
// 由调用点决定占位或显式示例图。这里不再把 .png/.webp 母版改写成同名 .jpg——
// 那是给旧 API 响应兜底的兼容行为，已由带类型的 DisplayMedia 取代（见 media/display.ts）。
export function lookImage(value: unknown): string {
  if (typeof value === 'string' && /\.webp(\?\S*)?$/i.test(value)) return ''
  return isDisplayableImage(value) ? value : ''
}

export function lookVideo(value: unknown): string {
  if (typeof value !== 'string' || !value) return ''
  if (/\.(webp|webm)(\?\S*)?$/i.test(value)) return ''
  if (!isDisplayableImage(value)) return ''
  if (/\.mp4(\?\S*)?$/i.test(value)) return value
  return ''
}

// 显式示例图：仅用于「风格参考」场景，调用点必须叠加
// 「风格参考」/「示例」角标（.example-badge + .example-soft）。
// 未知 slug 回落 natural、未知 variant 回落 full（与原型一致）。
export function exampleImage(slug: string = 'natural', variant: string = 'full'): string {
  const safeSlug = (LOCAL_LOOK_SLUGS as readonly string[]).includes(slug) ? (slug as LookSlug) : 'natural'
  const safeVariant = (LOOK_VARIANTS as readonly string[]).includes(variant) ? (variant as LookVariant) : 'full'
  return resolver.resolve(safeSlug, safeVariant)
}

// 服务端下发的 /assets/(looks|plans|portraits|reports|hair)/* 是与包内同源的
// 内置模特素材（含服务端 demo 数据），不是用户本人照片也不是 AI 生成效果图。
// 命中时必须按示例图对待（叠「风格参考」角标），不论 URL 是否可渲染。
// 注意：新代码不要再用它判角标——角标只认 DisplayMedia.source_kind（media/display.ts）。
export function isBundledAsset(value: unknown): boolean {
  if (typeof value !== 'string') return false
  const assetPath = value.replace(/^https?:\/\/[^/]+/i, '')
  return /^\/assets\/(looks|plans|portraits|reports|hair)\//i.test(assetPath)
}

// 用户本人照片：无效 URL 保持可见的空，绝不静默变成模特图。
export function userImage(value: unknown): string {
  return isDisplayableImage(value) ? value : ''
}

export { isDisplayableImage }
