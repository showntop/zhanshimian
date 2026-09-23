// LocalLooksResolver 小程序实现：内置模特图 = 包内绝对路径。
// 这些路径由 config copy.patterns 保留（assetsInlineLimit: 0），微信 <image>
// 可直接渲染包内 /assets/*.jpg。
import type { LocalLooksResolver, LookSlug, LookVariant } from '@zsm/core'

// 包体积：full/portrait/report/plan 四组内置模特图目前字节完全相同
// （同一套 2400×1792 源图，只是四种裁切意图），曾各存一份 = 516KB 重复。
// 合并指向同一份；将来某个变体真要单独裁切时，把对应目录加回来即可。
const VARIANT_DIRS: Record<LookVariant, string> = {
  full: 'looks',
  portrait: 'looks',
  report: 'looks',
  hair: 'hair',
  plan: 'looks',
}

export const localLooksResolver: LocalLooksResolver = {
  resolve(slug: LookSlug, variant: LookVariant): string {
    return `/assets/${VARIANT_DIRS[variant]}/${slug}.jpg`
  },
}
