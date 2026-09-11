// 空间标注层：左右胶囊 + 引导线 + 锚点 + 就地详情。
// 报告与穿搭共用同一套坐标映射，避免各页各写一套。
import { Text, View } from '@tarojs/components'
import './index.scss'

export type AnnotationItem = {
  id: string
  label: string
  detail?: string
  category?: string
  categoryLabel?: string
  anchorX: number
  anchorY: number
}

export function mapAnchorInFrame(
  dims: { w: number; h: number } | undefined,
  ax: number,
  ay: number,
  frameW: number,
  frameH: number
): { x: number; y: number } {
  if (!dims) return { x: ax * frameW, y: ay * frameH }
  const scale = Math.max(frameW / dims.w, frameH / dims.h)
  const scaledW = dims.w * scale
  const scaledH = dims.h * scale
  return {
    x: (frameW - scaledW) / 2 + ax * scaledW,
    y: (frameH - scaledH) / 2 + ay * scaledH,
  }
}

export function layoutAnnotationSides(items: AnnotationItem[]) {
  const layout = (sideItems: AnnotationItem[]) => {
    const sorted = [...sideItems].sort((a, b) => a.anchorY - b.anchorY)
    const out: { item: AnnotationItem; topPct: number }[] = []
    let last = 0
    for (const item of sorted) {
      const y = item.anchorY * 100
      let top = Math.min(86, Math.max(12, y))
      if (top - last < 16) top = Math.min(86, last + 16)
      out.push({ item, topPct: top })
      last = top
    }
    return out
  }
  return {
    left: layout(items.filter((item) => item.anchorX < 0.5)),
    right: layout(items.filter((item) => item.anchorX >= 0.5)),
  }
}

interface PhotoAnnotationLayerProps {
  items: AnnotationItem[]
  activeId: string
  frameW: number
  frameH: number
  photoDims?: { w: number; h: number }
  onTap: (item: AnnotationItem) => void
  showDrawer?: boolean
}

const CAP_W = 180
const CAP_H = 56
const EDGE = 16

export default function PhotoAnnotationLayer({
  items,
  activeId,
  frameW,
  frameH,
  photoDims,
  onTap,
  showDrawer = true,
}: PhotoAnnotationLayerProps) {
  const { left, right } = layoutAnnotationSides(items)
  const active = items.find((item) => item.id === activeId)

  const renderTag = (entry: { item: AnnotationItem; topPct: number }, side: 'left' | 'right', index: number) => {
    const { item, topPct } = entry
    const a = mapAnchorInFrame(photoDims, item.anchorX, item.anchorY, frameW, frameH)
    const startX = side === 'left' ? EDGE + CAP_W : frameW - EDGE - CAP_W
    const centerY = (topPct / 100) * frameH + CAP_H / 2
    const dx = a.x - startX
    const dy = a.y - centerY
    const dist = Math.sqrt(dx * dx + dy * dy)
    const angle = (Math.atan2(dy, dx) * 180) / Math.PI
    const delay = side === 'left' ? index * 80 : 80 + index * 80
    return (
      <View
        key={item.id}
        className="anno"
        style={{ animationDelay: `${delay}ms` }}
      >
        <View
          className="anno__leader"
          style={{
            left: `${(startX / frameW) * 100}%`,
            top: `${(centerY / frameH) * 100}%`,
            width: `${dist}rpx`,
            transform: `rotate(${angle}deg)`,
          }}
        />
        <View
          className="anno__dot"
          style={{
            left: `${(a.x / frameW) * 100}%`,
            top: `${(a.y / frameH) * 100}%`,
          }}
        />
        <View
          className={`anno__tag anno__tag--${side} ${activeId === item.id ? 'anno__tag--active' : ''}`}
          style={{ top: `${topPct}%` }}
          onClick={() => onTap(item)}
        >
          <Text className="anno__tag-label">{item.label}</Text>
        </View>
      </View>
    )
  }

  return (
    <View className="anno-layer">
      {left.map((entry, index) => renderTag(entry, 'left', index))}
      {right.map((entry, index) => renderTag(entry, 'right', index))}
      {showDrawer && active ? (
        <View className="anno__drawer" key={active.id}>
          {active.categoryLabel ? (
            <View className="anno__drawer-head">
              <Text className="anno__drawer-cat">{active.categoryLabel}</Text>
              <Text className="anno__drawer-label">{active.label}</Text>
            </View>
          ) : (
            <Text className="anno__drawer-label">{active.label}</Text>
          )}
          <Text className="anno__drawer-copy">{active.detail || active.label}</Text>
        </View>
      ) : null}
    </View>
  )
}
