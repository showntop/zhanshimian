// LocalLooksResolver 小程序实现：内置模特图 = 包内绝对路径。
// 这些路径由 config copy.patterns 保留（assetsInlineLimit: 0），微信 <image>
// 可直接渲染包内 /assets/*.jpg。
import type { LocalLooksResolver, LookSlug, LookVariant } from '@zsm/core'

const VARIANT_DIRS: Record<LookVariant, string> = {
  full: 'looks',
  portrait: 'portraits',
  report: 'reports',
  hair: 'hair',
  plan: 'plans',
}

export const localLooksResolver: LocalLooksResolver = {
  resolve(slug: LookSlug, variant: LookVariant): string {
    return `/assets/${VARIANT_DIRS[variant]}/${slug}.jpg`
  },
}
