// 发型方向目录（按性别分组的包内兜底目录）。
//
// 权威目录是服务端 GET /v1/hairstyles，但当前 baseline 还没有 hairstyles 表
// （adapter 缺表时降级为空目录），页面因此需要一份包内目录兜底：性别、差异标签与
// 参考图 slug 都落在这里，页面只做「服务端有则用服务端，缺项按 id 补齐」。
//
// name 是产品事实：它既上屏（方向名 / 结果判词 / 历史卡），也随创建请求的
// direction 传给服务端当生成提示词——改文案等于改生成效果，别只当装饰。
import type { DisplayMedia, HairStyle } from '../api/types.ts'
import type { LookSlug } from '../media/truth.ts'

/** 性别分组：两侧各自成目录。 */
export const HAIR_GENDERS = ['women', 'men'] as const
export type HairGender = (typeof HAIR_GENDERS)[number]

/** 目录/服务端的性别标记：unisex 是服务端目录才有的「两侧都列出」。 */
export type HairGenderTag = HairGender | 'unisex'

export interface HairDirection {
  id: string
  name: string
  /** 差异标签：缩略图看不清发型差别，选中前靠它分辨 */
  tag: string
  /** 一句适合谁：为什么是这个方向 */
  desc: string
  gender: HairGender
  /** 包内风格参考图（exampleImage 的 slug）；缺省 = 没有参考图，出文本卡 */
  slug?: LookSlug
}

/**
 * 两侧都有包内参考图：选方向不是盲选，先看见发型长什么样再决定。
 * 图只经 exampleImage（variant 固定 'hair'），卡面叠弱化 + CTA 下来源说明
 * （红线 2/3：内置图不给未经确认的参考语义，男士不拿女模特图顶替）。
 */
export const HAIR_DIRECTIONS: readonly HairDirection[] = [
  { id: 'sharp', name: '锁骨层次发', tag: '中长 · 层次', desc: '修饰脸型线条，利落不挑人', gender: 'women', slug: 'sharp' },
  { id: 'warm', name: '空气微卷', tag: '微卷 · 蓬松', desc: '蓬松显发量，柔和日常感', gender: 'women', slug: 'warm' },
  { id: 'natural', name: '自然偏分', tag: '偏分 · 利落', desc: '干净利落，省心百搭', gender: 'women', slug: 'natural' },
  { id: 'men-crop', name: '清爽短寸', tag: '短寸 · 精神', desc: '剪短就好打理，日常最省心', gender: 'men', slug: 'men-crop' },
  { id: 'men-side', name: '侧分短碎', tag: '侧分 · 利落', desc: '露出额头更利落，正式场合也稳', gender: 'men', slug: 'men-side' },
  { id: 'men-texture', name: '纹理蓬松', tag: '纹理 · 蓬松', desc: '顶部留一点长度，看起来更蓬松', gender: 'men', slug: 'men-texture' }
]

/** 自定义方向的 style_id：目录里没有，方向名由用户写（≤40 字）。 */
export const CUSTOM_DIRECTION_ID = 'custom'

/** 页面用的方向视图：服务端目录条目与包内条目合并后的同一形状。 */
export interface HairDirectionView {
  id: string
  name: string
  tag?: string
  desc?: string
  /** 包内参考图 slug（服务端条目按 id 认领，认不到就没有） */
  slug?: LookSlug
  /** 服务端下发的图；有图就不再看 slug */
  media: DisplayMedia | null
}

/**
 * 合成某一侧的方向列表：服务端条目在前（保留其展示顺序），包内条目按 id 补齐缺项。
 * 目录表当前缺失时这里就是整份包内目录；表补上后服务端条目自动接管。
 */
export function hairDirectionViews(
  gender: HairGender,
  serverStyles: readonly HairStyle[]
): HairDirectionView[] {
  const views: HairDirectionView[] = []
  const seen = new Set<string>()
  for (const style of serverStyles) {
    const tag: HairGenderTag = style.gender ?? 'unisex'
    if (tag !== 'unisex' && tag !== gender) continue
    const local = HAIR_DIRECTIONS.find((item) => item.id === style.id)
    seen.add(style.id)
    views.push({
      id: style.id,
      name: style.name,
      tag: local?.tag,
      desc: style.reason || local?.desc,
      slug: local?.slug,
      media: style.media ?? null
    })
  }
  for (const local of HAIR_DIRECTIONS) {
    if (local.gender !== gender || seen.has(local.id)) continue
    views.push({ id: local.id, name: local.name, tag: local.tag, desc: local.desc, slug: local.slug, media: null })
  }
  return views
}
