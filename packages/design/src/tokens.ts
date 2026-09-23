// tokens.data.mjs 的类型化 re-export —— TS 侧唯一入口。
// 改 token 只改 tokens.data.mjs，然后 `pnpm --filter @zsm/design build` 重生成 dist。
import { colors, radius, space, sizes, type, shadows } from './tokens.data.mjs'

export const tokens = {
  colors,
  radius,
  space,
  sizes,
  type,
  shadows
} as const

export type TokenSet = typeof tokens
export type ColorToken = keyof typeof colors
export type RadiusToken = keyof typeof radius
export type SpaceToken = keyof typeof space
export type SizeToken = keyof typeof sizes
export type TypeToken = keyof typeof type
export type ShadowTokenKey = keyof typeof shadows

// px×2 → rpx（designWidth: 750；design 源只写 px 的另一半约定）
export function pxToRpx(px: number): number {
  return px * 2
}
