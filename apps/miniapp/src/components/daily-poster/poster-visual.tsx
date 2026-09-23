import { View, Text } from '@tarojs/components'
import {
  asCompareSpec,
  asDiagramSpec,
  asPosterArtSpec,
  asSwatchSpec,
  type ContentVisual,
  type PosterTone,
} from '@zsm/core'
import './poster-visual.scss'

interface PosterVisualProps {
  visual: ContentVisual
  tone: PosterTone
}

/**
 * 内容视觉：画什么，由 content.visual 决定。
 *
 * 这里不按内容类型出图形 —— 同一个「色彩」类下的五条内容，
 * 要画的分别是色卡、身体位置图、材质对比，各不相同。
 * 画不出来（spec 解析失败 / 素材未到位）时降级为 alt 文字，
 * 而不是退回装饰方块：说不清的东西宁可不画。
 */
export default function PosterVisual({ visual, tone }: PosterVisualProps) {
  const cls = `pv pv--${tone}`

  // ---------- 色卡：每格有底色 + 色名 + 适合/不适合 ----------
  if (visual.modality === 'swatch') {
    const spec = asSwatchSpec(visual)
    if (spec) {
      return (
        <View className={cls}>
          <View className="pv__swatch">
            {spec.items.map((item) => (
              <View className="pv__sw-item" key={item.label}>
                <View
                  className={`pv__sw-chip${
                    item.state === 'pick' ? ' is-pick' : item.state === 'drop' ? ' is-drop' : ''
                  }`}
                  style={{ background: item.tone }}
                />
                <Text
                  className={`pv__sw-label${item.state === 'pick' ? ' is-pick' : ''}`}
                >
                  {item.label}
                </Text>
              </View>
            ))}
          </View>
        </View>
      )
    }
  }

  // ---------- 对比：左右两块 + 各自的标签 + 差异点 ----------
  if (visual.modality === 'compare') {
    const spec = asCompareSpec(visual)
    if (spec) {
      return (
        <View className={cls}>
          <View className="pv__compare">
            <View className="pv__cmp-side">
              <View className="pv__cmp-block" style={{ background: spec.left.tone }} />
              <Text className="pv__cmp-label">{spec.left.label}</Text>
            </View>
            <View className="pv__cmp-side pv__cmp-side--right">
              <View
                className={`pv__cmp-block${
                  spec.right.layered ? ' pv__cmp-block--layered' : ''
                }`}
                style={{ background: spec.right.tone }}
              />
              <Text className="pv__cmp-label is-on">{spec.right.label}</Text>
            </View>
            {spec.marker ? <Text className="pv__cmp-marker">{spec.marker}</Text> : null}
          </View>
        </View>
      )
    }
  }

  // ---------- 位置图 / 刻度：都是"若干带状态的条目"，只在排布方向不同 ----------
  if (visual.modality === 'diagram') {
    const spec = asDiagramSpec(visual)
    if (spec) {
      const isBody = spec.kind === 'body'
      return (
        <View className={cls}>
          <View className={isBody ? 'pv__body' : 'pv__scale'}>
            {spec.items.map((item) => (
              <View
                className={`pv__zone${isBody ? '' : ' pv__zone--row'}${
                  item.state === 'pick'
                    ? ' is-pick'
                    : item.state === 'avoid'
                      ? ' is-avoid'
                      : ''
                }`}
                key={item.label}
              >
                <View className="pv__zone-bar" />
                <Text className="pv__zone-label">{item.label}</Text>
              </View>
            ))}
          </View>
        </View>
      )
    }
  }

  // ---------- 海报体：出血大字 + 素材层（等真实素材） ----------
  if (visual.modality === 'poster') {
    const spec = asPosterArtSpec(visual)
    if (spec) {
      return (
        <View className={cls}>
          <View className="pv__art">
            {spec.layers.map((layer) => (
              <View
                className="pv__art-slot"
                key={layer.image}
                style={{
                  left: layer.x,
                  top: layer.y,
                  width: `${layer.w}rpx`,
                  transform: `rotate(${layer.rotate}deg)`,
                }}
              >
                <Text className="pv__art-hint">素材位</Text>
              </View>
            ))}
            <View
              className="pv__art-word"
              style={{ transform: `rotate(${spec.word.rotate ?? 0}deg)` }}
            >
              <Text className="pv__art-main">{spec.word.main}</Text>
              {spec.word.sub ? <Text className="pv__art-sub">{spec.word.sub}</Text> : null}
            </View>
          </View>
        </View>
      )
    }
  }

  // ---------- 单品图 / 视频：等真实素材，明确画成素材位 ----------
  if (visual.modality === 'photo' || visual.modality === 'video') {
    return (
      <View className={cls}>
        <View className="pv__slot">
          <Text className="pv__slot-text">
            {visual.modality === 'video' ? '视频位' : '单品图位'}
          </Text>
          <Text className="pv__slot-sub">{visual.alt}</Text>
        </View>
      </View>
    )
  }

  // ---------- 兜底：说不清宁可不画，只留 alt ----------
  return (
    <View className={cls}>
      <Text className="pv__alt">{visual.alt}</Text>
    </View>
  )
}
