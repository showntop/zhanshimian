// 形态渲染器注册表：变体空间 + 怎么画。
//
// 这里的分工（契约的另一半）：
//   服务端 → 语义（讲什么、target 是哪套、counter 是哪套、节奏档位）
//   这里   → 视觉（这个轴有几个变体、每个变体长什么样、配色怎么用）
// 两边靠轴的 keys 对接。新增一种形态 = 加一个 FormSpec，播放器不用改。
//
// 两条硬约束（AGENTS）：
//   · 禁裸 px —— 尺寸一律用百分比 / CSS 变量，由 scss 消费
//   · 不用人体 —— 全是抽象几何块，避开「身材」红线，也更杂志感

import { View } from '@tarojs/components'
import type { CSSProperties } from 'react'

/** 一个轴的变体空间：keys 是语义名（服务端下发），variants 是对应的视觉值 */
export interface FormAxis {
  keys: string[]
  variants: number[]
}

export interface FormProps {
  /** 每个轴的当前变体索引，顺序与服务端 params.axes 一致 */
  values: number[]
  /** 注入的配色（gene 色板在前）；不足时由渲染器补品牌色 */
  palette: string[]
}

export interface FormSpec {
  axes: Record<string, FormAxis>
  render: (props: FormProps) => JSX.Element
}

const SAGE = '#8E9A83'
const INK = '#3F463C'
const CREAM = '#E8E4DC'
const BRAND = [SAGE, INK, CREAM, '#B4674D']

/** 取变体值：索引越界或缺省一律退回 fallback，绝不让形态画不出来 */
function valueOf(axis: FormAxis, index: number | undefined, fallback = 0): number {
  return axis.variants[index ?? fallback] ?? axis.variants[fallback] ?? 0
}

/** 语义值 → 变体索引：数字取最近，字符串按 keys 精确匹配 */
export function resolveAxisValue(axis: FormAxis | undefined, value: unknown): number {
  if (!axis || axis.variants.length === 0) return 0
  if (typeof value === 'number') {
    let best = 0
    let bestGap = Infinity
    axis.variants.forEach((v, i) => {
      const gap = Math.abs(v - value)
      if (gap < bestGap) {
        bestGap = gap
        best = i
      }
    })
    return best
  }
  const index = axis.keys.indexOf(String(value))
  return index < 0 ? 0 : index
}

export function variantCountsOf(spec: FormSpec, axes: Array<{ id: string }>): number[] {
  return axes.map((axis) => spec.axes[axis.id]?.variants.length ?? 1)
}

/** 取色：harmony 相邻、clash 拉开距离，色板不足时补品牌色 */
function colorsOf(palette: string[], paletteValue: number): [string, string] {
  const pool = palette.length >= 2 ? palette : BRAND
  const spread = paletteValue === 0 ? Math.max(1, Math.floor(pool.length / 2)) : 1
  return [pool[0] ?? SAGE, pool[Math.min(pool.length - 1, spread)] ?? INK]
}

function vars(map: Record<string, string>): CSSProperties {
  return map as CSSProperties
}

/** 上下两块：上块高度 + 下块高度（腰线自然落在接缝处） */
function blocks(topRatio: number, bottomRatio: number, colors: [string, string]) {
  const total = Math.max(0.2, Math.min(1, topRatio + bottomRatio))
  const top = (topRatio / total) * 100
  const bottom = (bottomRatio / total) * 100
  return (
    <View
      className="dm__blocks"
      style={vars({
        '--dm-top': `${top.toFixed(1)}%`,
        '--dm-bottom': `${bottom.toFixed(1)}%`,
        '--dm-c1': colors[0],
        '--dm-c2': colors[1],
      })}
    >
      <View className="dm__block dm__block--top" />
      <View className="dm__waist" />
      <View className="dm__block dm__block--bottom" />
    </View>
  )
}

// ---------- 轴定义（具名常量：避免 Record 索引带来的可选性） ----------

const TOP_AXIS: FormAxis = {
  keys: ['cropped', 'regular', 'long', 'oversize'],
  variants: [0.3, 0.44, 0.6, 0.72],
}
const BOTTOM_AXIS: FormAxis = {
  keys: ['wide', 'straight', 'slim', 'long'],
  variants: [0.62, 0.5, 0.38, 0.46],
}
const PALETTE_AXIS: FormAxis = { keys: ['clash', 'mixed', 'harmony'], variants: [0, 1, 2] }
const WAIST_AXIS: FormAxis = { keys: ['high', 'mid', 'low'], variants: [0.34, 0.44, 0.56] }
const VOLUME_AXIS: FormAxis = { keys: ['balanced', 'heavy'], variants: [0.44, 0.62] }
const HUE_AXIS: FormAxis = { keys: ['clash', 'mixed', 'harmony'], variants: [0, 1, 2] }
const ORDER_AXIS: FormAxis = { keys: ['shuffled', 'ascending'], variants: [0, 1] }
const LIFT_AXIS: FormAxis = { keys: ['muted', 'normal'], variants: [0.72, 1] }
const SHOULDER_AXIS: FormAxis = { keys: ['natural', 'wide'], variants: [0.62, 0.86] }
const SIL_WAIST_AXIS: FormAxis = { keys: ['straight', 'cinched'], variants: [0.62, 0.4] }
const HEM_AXIS: FormAxis = { keys: ['straight', 'flared'], variants: [0.6, 0.94] }
const DENSITY_AXIS: FormAxis = { keys: ['medium', 'dense'], variants: [8, 14] }
const DRAPE_AXIS: FormAxis = { keys: ['stiff', 'fluid'], variants: [4, 16] }
const TONE_AXIS: FormAxis = { keys: ['mixed', 'muted'], variants: [0, 1] }
const TEMP_AXIS: FormAxis = { keys: ['cold', 'warm'], variants: [0, 1] }
const LIGHT_AXIS: FormAxis = { keys: ['harsh', 'soft'], variants: [0.55, 0.85] }
const SHAPE_AXIS: FormAxis = { keys: ['busy', 'balanced'], variants: [0.8, 0.45] }
const FOLD_AXIS: FormAxis = { keys: ['none', 'one', 'two'], variants: [0, 1, 2] }
const FOLD_POS_AXIS: FormAxis = { keys: ['wrist', 'above_elbow'], variants: [0.72, 0.4] }
const FOLD_TILT_AXIS: FormAxis = { keys: ['tilted', 'neutral'], variants: [-8, 0] }

// ---------- 形态 ----------

const OUTFIT: FormSpec = {
  axes: { top: TOP_AXIS, bottom: BOTTOM_AXIS, palette: PALETTE_AXIS },
  render: ({ values, palette }) =>
    blocks(valueOf(TOP_AXIS, values[0]), valueOf(BOTTOM_AXIS, values[1], 1), colorsOf(palette, values[2] ?? 2)),
}

// 比例与搭配共用同一套构图：上/下长度比本身就讲比例
const RATIO: FormSpec = {
  axes: { waist: WAIST_AXIS, volume: VOLUME_AXIS, palette: PALETTE_AXIS },
  render: ({ values, palette }) => {
    const split = valueOf(WAIST_AXIS, values[0])
    return blocks(split, 1 - split, colorsOf(palette, values[2] ?? 2))
  },
}

const SWATCH: FormSpec = {
  axes: { hue: HUE_AXIS, order: ORDER_AXIS, lift: LIFT_AXIS },
  render: ({ values, palette }) => {
    const pool = palette.length >= 3 ? palette : BRAND
    const step = values[0] === 2 ? 1 : 2 // harmony 取相邻色，clash 拉开
    const picked = [
      pool[0] ?? SAGE,
      pool[Math.min(pool.length - 1, step)] ?? INK,
      pool[Math.min(pool.length - 1, step + 1)] ?? SAGE,
    ]
    const ordered = values[1] === 1 ? picked : [...picked].reverse()
    return (
      <View className="dm__swatch" style={vars({ '--dm-lift': String(valueOf(LIFT_AXIS, values[2], 1)) })}>
        {ordered.map((color, i) => (
          <View key={`bar-${color}-${i}`} className={`dm__bar dm__bar--${i}`} style={vars({ '--dm-c': color })} />
        ))}
      </View>
    )
  },
}

const SILHOUETTE: FormSpec = {
  axes: { shoulder: SHOULDER_AXIS, waist: SIL_WAIST_AXIS, hem: HEM_AXIS },
  render: ({ values, palette }) => {
    const [c1, c2] = colorsOf(palette, 2)
    return (
      <View
        className="dm__silhouette"
        style={vars({
          '--dm-shoulder': `${(valueOf(SHOULDER_AXIS, values[0]) * 100).toFixed(0)}%`,
          '--dm-waist': `${(valueOf(SIL_WAIST_AXIS, values[1]) * 100).toFixed(0)}%`,
          '--dm-hem': `${(valueOf(HEM_AXIS, values[2]) * 100).toFixed(0)}%`,
          '--dm-c1': c1,
          '--dm-c2': c2,
        })}
      >
        <View className="dm__band dm__band--shoulder" />
        <View className="dm__band dm__band--waist" />
        <View className="dm__band dm__band--hem" />
      </View>
    )
  },
}

const TEXTURE: FormSpec = {
  axes: { density: DENSITY_AXIS, drape: DRAPE_AXIS, tone: TONE_AXIS },
  render: ({ values, palette }) => {
    const count = Math.round(valueOf(DENSITY_AXIS, values[0]))
    const amp = valueOf(DRAPE_AXIS, values[1], 1)
    const color = palette[0] ?? SAGE
    const lines: Array<{ top: number; offset: number }> = []
    for (let i = 0; i < count; i++) {
      lines.push({
        top: (i / count) * 100,
        offset: Math.sin((i / count) * Math.PI * 2) * amp,
      })
    }
    return (
      <View className="dm__texture" style={vars({ '--dm-c1': color })}>
        {lines.map((line, i) => (
          <View
            key={`thread-${i}`}
            className="dm__thread"
            style={vars({ '--dm-top': `${line.top.toFixed(1)}%`, '--dm-x': `${line.offset.toFixed(1)}%` })}
          />
        ))}
      </View>
    )
  },
}

const SCENE: FormSpec = {
  axes: { temp: TEMP_AXIS, light: LIGHT_AXIS, shape: SHAPE_AXIS },
  render: ({ values, palette }) => (
    <View
      className="dm__scene"
      style={vars({
        '--dm-bg': values[0] === 0 ? '#8FA6B8' : '#D8BFA0',
        '--dm-light': String(valueOf(LIGHT_AXIS, values[1], 1)),
        '--dm-left': `${(valueOf(SHAPE_AXIS, values[2], 1) * 40 + 12).toFixed(0)}%`,
        '--dm-c1': palette[0] ?? SAGE,
      })}
    >
      <View className="dm__scene-mark" />
    </View>
  ),
}

const FOLD: FormSpec = {
  axes: { fold: FOLD_AXIS, position: FOLD_POS_AXIS, tilt: FOLD_TILT_AXIS },
  render: ({ values, palette }) => {
    const folds = Math.round(valueOf(FOLD_AXIS, values[0], 2))
    const pos = valueOf(FOLD_POS_AXIS, values[1], 1)
    const tilt = valueOf(FOLD_TILT_AXIS, values[2], 1)
    const creases: Array<{ top: number }> = []
    for (let i = 0; i < folds; i++) creases.push({ top: (pos + i * 0.1) * 100 })
    return (
      <View className="dm__fold" style={vars({ '--dm-c1': palette[1] ?? INK, '--dm-c2': palette[0] ?? SAGE })}>
        <View className="dm__sleeve" />
        {creases.map((crease, i) => (
          <View
            key={`crease-${i}`}
            className="dm__crease"
            style={vars({ '--dm-top': `${crease.top.toFixed(0)}%`, '--dm-tilt': `${tilt}deg` })}
          />
        ))}
      </View>
    )
  },
}

/**
 * 注册表：form 名 → 规格。查不到就回落 outfit_blocks（上/下块最通用），
 * 客户端因此永远不会因为不认识的 form 而空白。
 */
export const FORMS: Record<string, FormSpec> = {
  outfit_blocks: OUTFIT,
  ratio_blocks: RATIO,
  swatch_bars: SWATCH,
  silhouette_shape: SILHOUETTE,
  texture_lines: TEXTURE,
  scene_panel: SCENE,
  fold_lines: FOLD,
}

export function formOf(name: string | undefined): FormSpec {
  return (name && FORMS[name]) || OUTFIT
}
