// 底部弹层：进出场对齐总计划 §4.3（进 .32s 品牌缓动 / 出 .24s 更快更重）。
// 下拉关闭：grabber 拖拽超过 sheetDismissThreshold（y 96px 或快速下滑）即收起。
import { useEffect, useRef, useState } from 'react'
import { Text, View } from '@tarojs/components'
import type { CommonEvent, ITouchEvent } from '@tarojs/components'
import { sheetDismissThreshold } from '@zsm/design'
import './index.scss'

interface BottomSheetProps {
  open: boolean
  title?: string
  description?: string
  /** 拉满可视高度，给内部滚动 + 底部操作条用 */
  tall?: boolean
  onClose: () => void
  children?: React.ReactNode
}

const EXIT_MS = 240

export default function BottomSheet({ open, title, description, tall, onClose, children }: BottomSheetProps) {
  // closed：不渲染；in：进场动画后停靠；exit：退场动画后卸载
  const [phase, setPhase] = useState<'closed' | 'in' | 'exit'>('closed')
  const [dragY, setDragY] = useState(0)
  const startY = useRef(0)
  const startTime = useRef(0)

  useEffect(() => {
    if (open) {
      setDragY(0)
      setPhase('in')
    } else {
      setPhase((prev) => (prev === 'closed' ? prev : 'exit'))
    }
  }, [open])

  useEffect(() => {
    if (phase !== 'exit') return
    const t = setTimeout(() => setPhase('closed'), EXIT_MS)
    return () => clearTimeout(t)
  }, [phase])

  const onTouchStart = (e: CommonEvent) => {
    const touch = (e as unknown as ITouchEvent).touches[0]
    if (!touch) return
    startY.current = touch.clientY
    startTime.current = Date.now()
  }

  const onTouchMove = (e: CommonEvent) => {
    const touch = (e as unknown as ITouchEvent).touches[0]
    if (!touch) return
    const dy = touch.clientY - startY.current
    // 只允许向下拖，向上越过起点即归零
    setDragY(Math.max(0, dy))
  }

  const onTouchEnd = (e: CommonEvent) => {
    const touch = (e as unknown as ITouchEvent).changedTouches[0]
    if (!touch) return
    const dy = touch.clientY - startY.current
    const dt = Math.max(1, Date.now() - startTime.current)
    const velocity = (dy / dt) * 1000 // px/s，向下为正
    if (dy >= sheetDismissThreshold.y || velocity >= sheetDismissThreshold.velocity * 1000) {
      onClose()
    }
    setDragY(0)
  }

  if (phase === 'closed') return null

  return (
    <View className={`bottom-sheet ${phase === 'exit' ? 'bottom-sheet--exit' : ''}`}>
      <View className="bottom-sheet__mask" onClick={onClose} catchMove />
      <View
        className={`bottom-sheet__panel ${tall ? 'bottom-sheet__panel--tall' : ''}`}
        style={dragY > 0 ? `transform: translateY(${dragY}px)` : ''}
      >
        <View
          className="bottom-sheet__grab-zone"
          onTouchStart={onTouchStart}
          onTouchMove={onTouchMove}
          onTouchEnd={onTouchEnd}
        >
          <View className="bottom-sheet__grabber" />
          {title ? <Text className="bottom-sheet__title">{title}</Text> : null}
          {description ? <Text className="bottom-sheet__desc">{description}</Text> : null}
        </View>
        <View className="bottom-sheet__body">{children}</View>
      </View>
    </View>
  )
}
