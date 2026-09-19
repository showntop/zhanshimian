// 内容视觉规格（ContentVisual.spec）的类型与解析。
//
// 分工（这是本文件存在的理由）：
//   content.visual  = 画什么。由内容自己声明，每条不同。
//   poster.ts       = 装在哪。底板 / 承载块 / 文字位置，按内容类型给容器。
//
// 之前走错过一次：忽略 content.visual，改用 type 出一套固定布局。结果是
// 「驼色是个陷阱」声明了"画身体三个位置"，却得到两块灰方块——视觉与内容对不上，
// 观感就是"方框表达力弱"。问题不在方块，在没画该画的东西。
//
// spec 在契约里是 Record<string, unknown>（服务端可扩展），这里做窄化解析：
// 解析不出来就返回 null，由组件降级，绝不猜。

import type { ContentVisual } from './types'

// ---------- swatch：色卡 ----------

export interface SwatchItem {
  tone: string
  label: string
  /** pick=适合，drop=不适合。缺省为中性 */
  state?: 'pick' | 'drop'
}

export interface SwatchSpec {
  items: SwatchItem[]
}

// ---------- compare：左右对比 ----------

export interface CompareSide {
  label: string
  tone: string
  /** 有光泽/有层次的那一侧 */
  layered?: boolean
}

export interface CompareSpec {
  left: CompareSide
  right: CompareSide
  /** 差异点的名字，如「反光」「针距」 */
  marker?: string
}

// ---------- diagram：位置示意 / 刻度 ----------

export interface DiagramItem {
  label: string
  /** pick=建议，avoid=避开。缺省为中性 */
  state?: 'pick' | 'avoid'
}

export interface DiagramSpec {
  /** body=身体位置分区，scale=单向刻度 */
  kind: 'body' | 'scale'
  items: DiagramItem[]
}

// ---------- poster：大字 + 素材层（需真实素材） ----------

export interface PosterArtLayer {
  image: string
  x: string
  y: string
  w: number
  rotate: number
}

export interface PosterArtSpec {
  /** 出血大字，不承载信息，只提供氛围 */
  word: { main: string; sub?: string; rotate?: number }
  layers: PosterArtLayer[]
  /** 一处点缀文字 */
  chip?: { text: string }
}

// ---------- 解析 ----------

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}

export function asSwatchSpec(visual: ContentVisual): SwatchSpec | null {
  const items = isRecord(visual.spec) ? visual.spec.items : null
  if (!Array.isArray(items) || items.length === 0) return null
  const parsed = items.filter(
    (item): item is SwatchItem =>
      isRecord(item) && typeof item.tone === 'string' && typeof item.label === 'string',
  )
  return parsed.length > 0 ? { items: parsed } : null
}

export function asCompareSpec(visual: ContentVisual): CompareSpec | null {
  if (!isRecord(visual.spec)) return null
  const { left, right, marker } = visual.spec
  const ok = (side: unknown): side is CompareSide =>
    isRecord(side) && typeof side.label === 'string' && typeof side.tone === 'string'
  if (!ok(left) || !ok(right)) return null
  return { left, right, marker: typeof marker === 'string' ? marker : undefined }
}

export function asDiagramSpec(visual: ContentVisual): DiagramSpec | null {
  if (!isRecord(visual.spec)) return null
  const { kind, items } = visual.spec
  if (kind !== 'body' && kind !== 'scale') return null
  if (!Array.isArray(items) || items.length === 0) return null
  const parsed = items.filter(
    (item): item is DiagramItem => isRecord(item) && typeof item.label === 'string',
  )
  return parsed.length > 0 ? { kind, items: parsed } : null
}

export function asPosterArtSpec(visual: ContentVisual): PosterArtSpec | null {
  if (!isRecord(visual.spec)) return null
  const word = visual.spec.word
  if (!isRecord(word) || typeof word.main !== 'string') return null
  const rawLayers = visual.spec.layers
  const layers = Array.isArray(rawLayers)
    ? rawLayers.filter(
        (layer): layer is PosterArtLayer =>
          isRecord(layer) && typeof layer.image === 'string' && typeof layer.x === 'string',
      )
    : []
  const chip = isRecord(visual.spec.chip) && typeof visual.spec.chip.text === 'string'
    ? { text: visual.spec.chip.text }
    : undefined
  return {
    word: {
      main: word.main,
      sub: typeof word.sub === 'string' ? word.sub : undefined,
      rotate: typeof word.rotate === 'number' ? word.rotate : undefined,
    },
    layers,
    chip,
  }
}
