// 衣橱的纯逻辑：服务端响应的运行期归一。不碰 Taro、不碰网络。
//
// 契约里 WardrobeOutfit.items 是 required 数组，但服务端的 Go nil slice
// 会序列化成 JSON null（创建组合与记录穿着两条路径都不回填 items）。
// 类型声明护不住运行期事实，渲染前必须过这一道，否则 items.map 直接白屏。
import type { WardrobeItem, WardrobeOutfit } from '@zsm/core'

/** items 归一：null/undefined → []；已有数组原样保留，其余字段一概不碰。 */
export function normalizeOutfit(outfit: WardrobeOutfit): WardrobeOutfit {
  const items = (outfit.items as WardrobeItem[] | null | undefined) ?? []
  return { ...outfit, items }
}
