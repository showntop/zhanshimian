// 每日内容选品。
//
// 输入只有两类：形象基因（稳定）+ 今日语境（每日变）。
// 这里不做任何与「用户当天穿什么」有关的推断——那是臆想的来源。

import type {
  CollectionCategory,
  ContentType,
  DailyContent,
  DailyPick,
  DailyPickInput,
  StyleGene,
} from './types'

/** 基因条件是否命中；未声明该维度表示不挑人 */
function geneHit(gene: StyleGene, c: DailyContent): boolean {
  const g = c.gene
  if (!g) return true
  if (g.skinTone && !g.skinTone.includes(gene.skinTone)) return false
  if (g.contrast && !g.contrast.includes(gene.contrast)) return false
  if (g.shoulder && !g.shoulder.includes(gene.shoulder)) return false
  if (g.frame && !g.frame.includes(gene.frame)) return false
  if (g.vertical && !g.vertical.includes(gene.vertical)) return false
  if (g.neck && !g.neck.includes(gene.neck)) return false
  if (g.face && !g.face.includes(gene.face)) return false
  if (g.heightBand && !g.heightBand.includes(gene.heightBand)) return false
  return true
}

export function isEligible(c: DailyContent, input: DailyPickInput): boolean {
  const { gene, temperature, condition, dayType, season, seen } = input
  if (seen.includes(c.dedupeKey)) return false
  if (c.season && c.season.length > 0 && !c.season.includes(season)) return false
  if (c.dayType && c.dayType.length > 0 && !c.dayType.includes(dayType)) return false
  if (c.weather?.minTemp !== undefined && temperature < c.weather.minTemp) return false
  if (c.weather?.maxTemp !== undefined && temperature > c.weather.maxTemp) return false
  if (c.weather?.condition && !c.weather.condition.includes(condition)) return false
  return geneHit(gene, c)
}

/**
 * 选一条。排序规则：先补最薄的资产库，再按 id 稳定排序。
 * 同一个人同一天结果固定（不随机）——随机会让用户觉得系统在瞎猜。
 */
export function pickDaily(pool: DailyContent[], input: DailyPickInput): DailyPick | null {
  const candidates = pool.filter((c) => isEligible(c, input))
  if (candidates.length === 0) return null

  const sorted = [...candidates].sort((a, b) => {
    const ca = input.bucketCount[a.asset] ?? 0
    const cb = input.bucketCount[b.asset] ?? 0
    if (ca !== cb) return ca - cb
    return a.id < b.id ? -1 : 1
  })

  const content = sorted[0]
  if (!content) return null

  return {
    content,
    fitText: content.fit(input.gene),
    reason: [
      `命中 ${content.type}`,
      `温度 ${input.temperature}° 在区间内`,
      `补最薄的库：${content.asset}`,
    ],
  }
}

/**
 * 内容类型 → 手册分类的默认映射。
 *
 * 分类不该逐条手工分配 —— 手工分配必出错（曾把讲材质的「全黑并不自动显瘦」
 * 归到了配色库）。默认由类型推导，内容确需特例时用 asset 字段覆盖。
 */
const CATEGORY_BY_TYPE: Record<ContentType, CollectionCategory> = {
  color: 'color',
  silhouette: 'fit',
  proportion: 'proportion',
  fabric: 'fabric',
  item: 'outfit',
  occasion: 'occasion',
  howto: 'howto',
}

export function categoryOfType(type: ContentType): CollectionCategory {
  return CATEGORY_BY_TYPE[type] ?? 'color'
}

/** 手册各格当前条数，缺省补 0 */
export function emptyBuckets(): Record<CollectionCategory, number> {
  return { color: 0, fit: 0, proportion: 0, fabric: 0, occasion: 0, howto: 0, outfit: 0 }
}

export function countBuckets(categories: CollectionCategory[]): Record<CollectionCategory, number> {
  const out = emptyBuckets()
  for (const c of categories) out[c] = (out[c] ?? 0) + 1
  return out
}
