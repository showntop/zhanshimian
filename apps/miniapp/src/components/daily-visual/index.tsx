// 每日内容的程序化视觉。
//
// 参照：Aesop / COS 的编辑式极简——大留白、细线、排印为主。
// 全部由 CSS 绘制，不加载图片（规避小程序 WebP 渲染空白的硬规则）。
//
// diagram 分两种，因为它们要回答的问题不同：
//   kind: 'body'  位置类（包背在哪 / 腰线在哪 / 下摆停在哪 / 驼色放哪）
//                 → 必须画在人体轮廓上。悬空的三个点没有参照，用户仍不知道「腰线」在哪。
//   kind: 'scale' 程度类（正式度 / 袖长）
//                 → 用刻度轴，表达的是等级而非身体位置。
//
// 标签一律中文且用户能懂（曾出现 flat-black / above-knee 这类开发者文字，属硬伤）。

import { Image, Text, View } from '@tarojs/components'
import type { ContentVisual } from '@zsm/core'
import './index.scss'

interface DailyVisualProps {
  visual: ContentVisual
  /** 首页卡片等小尺寸场景：省略文字标签，只保留色块与符号 */
  compact?: boolean
}

function asString(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

function asRecord(value: unknown): Record<string, unknown> {
  return typeof value === 'object' && value !== null ? (value as Record<string, unknown>) : {}
}

function asRecords(value: unknown): Record<string, unknown>[] {
  return Array.isArray(value)
    ? value.filter((v): v is Record<string, unknown> => typeof v === 'object' && v !== null)
    : []
}

function asNumber(value: unknown, fallback: number): number {
  return typeof value === 'number' ? value : fallback
}

export default function DailyVisual({ visual, compact = false }: DailyVisualProps) {
  const spec = visual.spec ?? {}

  // ---------- 色卡 ----------
  if (visual.modality === 'swatch') {
    const items = asRecords(spec.items)
    return (
      <View className="dv dv--swatch">
        {items.map((item, index) => {
          const tone = asString(item.tone)
          const label = asString(item.label)
          const state = asString(item.state)
          return (
            <View key={`sw-${tone}-${index}`} className="dv-sw-col">
              <View
                className={`dv-sw ${state === 'pick' ? 'is-pick' : ''} ${state === 'drop' ? 'is-drop' : ''}`}
                style={{ background: tone }}
              >
                {state === 'pick' ? <Text className="dv-sw-mark">✓</Text> : null}
                {state === 'drop' ? <Text className="dv-sw-mark is-drop">×</Text> : null}
                {!compact && label !== '' ? <Text className="dv-sw-label">{label}</Text> : null}
              </View>
            </View>
          )
        })}
      </View>
    )
  }

  // ---------- 对比：有真实素材就渲图，没有就退回色块（素材缺失不空屏） ----------
  if (visual.modality === 'compare') {
    const block = (o: Record<string, unknown>, layered: boolean) => {
      const tone = asString(o.tone)
      const image = asString(o.image)
      const label = asString(o.label)
      const useColor = image === '' && !layered
      return (
        <View className="dv-cp-col">
          <View
            className={`dv-cp ${layered ? 'is-layered' : ''}`}
            style={useColor ? { background: tone } : undefined}
          >
            {image !== '' ? (
              <Image className="dv-cp-img" src={image} mode="aspectFill" />
            ) : null}
            {image === '' && layered ? (
              <View className="dv-cp-fill" style={{ background: tone }} />
            ) : null}
            {/* 有真实材质图时不再叠高光：图本身已经表达了层次 */}
            {image === '' && layered ? <View className="dv-cp-sheen" /> : null}
          </View>
          {!compact && label !== '' ? <Text className="dv-cp-label">{label}</Text> : null}
        </View>
      )
    }
    return (
      <View className="dv dv--compare">
        {block(asRecord(spec.left), false)}
        {block(asRecord(spec.right), asRecord(spec.right).layered === true)}
      </View>
    )
  }

  // ---------- 海报式：多件素材错落 + 出血大字铺底 + 一处不和谐点缀 ----------
  // 位置 / 宽度 / 旋转全部来自 spec：将来逐件素材到位后只改数据，不改这里。
  if (visual.modality === 'poster') {
    const word = asRecord(spec.word)
    const chip = asRecord(spec.chip)
    const layers = asRecords(spec.layers)
    const wordRotate = asNumber(word.rotate, -7)
    return (
      <View className="dv dv--poster">
        <Text className="dv-pt-word" style={{ transform: `rotate(${wordRotate}deg)` }}>
          {asString(word.main)}
        </Text>
        {asString(word.sub) !== '' ? (
          <Text className="dv-pt-word dv-pt-word--sub" style={{ transform: `rotate(${wordRotate}deg)` }}>
            {asString(word.sub)}
          </Text>
        ) : null}
        {layers.map((layer, index) => (
          <Image
            key={`pt-${index}`}
            className="dv-pt-item"
            src={asString(layer.image)}
            mode="aspectFit"
            style={{
              left: asString(layer.x) || '0',
              top: asString(layer.y) || '0',
              width: `${asNumber(layer.w, 200)}rpx`,
              transform: `rotate(${asNumber(layer.rotate, 0)}deg)`,
            }}
          />
        ))}
        <View className="dv-pt-rule" />
        {asString(chip.text) !== '' ? (
          <View className="dv-pt-chip">
            <Text className="dv-pt-chip-text">{asString(chip.text)}</Text>
          </View>
        ) : null}
      </View>
    )
  }

  // ---------- 位置类：画在人体轮廓上 ----------
  if (visual.modality === 'diagram' && asString(spec.kind) === 'body') {
    const items = asRecords(spec.items)
    const figure = asString(spec.image)
    const total = items.length
    const marks = items.map((item, index) => {
      const label = asString(item.label)
      const state = asString(item.state)
      const ratio = total > 1 ? index / (total - 1) : 0.5
      // 有底图时按素材实际构图定位（这张线稿的胸/腰/胯大致在 41%~61%）。
      // TODO 素材规范化后应由服务端下发标记坐标，不在这里写死比例。
      const top =
        figure !== ''
          ? `${Math.round(41 + ratio * 20)}%`
          : `${Math.round(((index + 1) / (total + 1)) * 100)}%`
      return (
        <View
          key={`bd-${label}-${index}`}
          className={`dv-fig-mark ${state === 'pick' ? 'is-pick' : ''} ${
            state === 'avoid' ? 'is-avoid' : ''
          }`}
          style={{ top }}
        >
          <View className="dv-fig-dot" />
          {!compact && label !== '' ? <Text className="dv-fig-label">{label}</Text> : null}
        </View>
      )
    })
    return (
      <View className="dv dv--body">
        <View className="dv-fig">
          {figure !== '' ? (
            <Image className="dv-fig-img" src={figure} mode="aspectFit" />
          ) : (
            <View className="dv-fig-bare">
              <View className="dv-fig-head" />
              <View className="dv-fig-torso">{marks}</View>
            </View>
          )}
          {figure !== '' ? marks : null}
        </View>
      </View>
    )
  }

  // ---------- 程度类：刻度轴 ----------
  if (visual.modality === 'diagram') {
    const items = asRecords(spec.items)
    return (
      <View className="dv dv--axis">
        <View className="dv-axis-line" />
        {items.map((item, index) => {
          const label = asString(item.label)
          const state = asString(item.state)
          return (
            <View
              key={`ax-${label}-${index}`}
              className={`dv-ax ${state === 'pick' ? 'is-pick' : ''} ${state === 'avoid' ? 'is-avoid' : ''}`}
            >
              <View className="dv-ax-dot" />
              <Text className="dv-ax-label">{label}</Text>
            </View>
          )
        })}
      </View>
    )
  }

  // ---------- 动作演示 ----------
  if (visual.modality === 'video') {
    return (
      <View className="dv dv--video">
        <View className="dv-vd-btn">
          <Text className="dv-vd-icon">▶</Text>
        </View>
        <Text className="dv-vd-meta">
          {asNumber(spec.durationSec, 12)} 秒 · {asNumber(spec.steps, 2)} 步
        </Text>
      </View>
    )
  }

  return (
    <View className="dv dv--photo">
      <Text className="dv-ph">{visual.alt}</Text>
    </View>
  )
}
