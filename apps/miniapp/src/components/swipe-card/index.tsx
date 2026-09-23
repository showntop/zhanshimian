// 卡堆手势引擎：横向拖拽跟手（translateX + 轻微 rotate），越过阈值或快速甩动
// 即提交决策——盖章、飞出，动画结束后才回调 onDecide（父层随后卸载此卡）；
// 未越阈则弹回。纯手势、零方案语义；阈值与速度判定沿用 bottom-sheet 同款
// 相位机，常量取品牌动效参数。
import { useEffect, useRef, useState } from 'react'
import Taro from '@tarojs/taro'
import { Text, View } from '@tarojs/components'
import type { CommonEvent, ITouchEvent } from '@tarojs/components'
import { backGestureThreshold } from '@zsm/design'
import './index.scss'

export type SwipeDecision = 'like' | 'skip'

interface SwipeCardProps {
  children?: React.ReactNode
  /** 结果态/往期浏览禁拖。 */
  disabled?: boolean
  /** 决策印章文案（如「喜欢」「跳过」）：盖章 → 飞出 → 回调。 */
  stampLike?: string
  stampSkip?: string
  /** 飞出动画播完才调用；父层据此更新卡堆。 */
  onDecide: (decision: SwipeDecision) => void
}

/** 飞出动画时长（ms）：与 bottom-sheet 退场同一档，更快更重。 */
const EXIT_MS = 240
/** 判定「横向意图」的位移下限（px），防止纵向滚动被误吞。 */
const AXIS_LOCK_PX = 6
/** 飞出位移相对屏幕宽度的比例。 */
const FLYOUT_RATIO = 0.38

let cachedWindowWidth = 0
function windowWidth(): number {
  if (!cachedWindowWidth) {
    try {
      cachedWindowWidth = Taro.getSystemInfoSync().windowWidth || 375
    } catch {
      cachedWindowWidth = 375
    }
  }
  return cachedWindowWidth
}

export default function SwipeCard({ children, disabled, stampLike, stampSkip, onDecide }: SwipeCardProps) {
  const [dragX, setDragX] = useState(0)
  const [exit, setExit] = useState<SwipeDecision | null>(null)
  const startX = useRef(0)
  const startY = useRef(0)
  const startTime = useRef(0)
  const horizontal = useRef<boolean | null>(null)
  // onDecide 走 ref：飞出动画期间父层重渲染不该重置定时器
  const decideRef = useRef(onDecide)
  decideRef.current = onDecide

  useEffect(() => {
    if (exit === null) return
    const timer = setTimeout(() => decideRef.current(exit), EXIT_MS)
    return () => clearTimeout(timer)
  }, [exit])

  const commit = (decision: SwipeDecision) => {
    void Taro.vibrateShort({ type: 'light' }).catch(() => {})
    setExit(decision)
  }

  const onTouchStart = (e: CommonEvent) => {
    if (disabled || exit !== null) return
    const touch = (e as unknown as ITouchEvent).touches[0]
    if (!touch) return
    startX.current = touch.clientX
    startY.current = touch.clientY
    startTime.current = Date.now()
    horizontal.current = null
  }

  const onTouchMove = (e: CommonEvent) => {
    if (disabled || exit !== null) return
    const touch = (e as unknown as ITouchEvent).touches[0]
    if (!touch) return
    const dx = touch.clientX - startX.current
    const dy = touch.clientY - startY.current
    if (horizontal.current === null) {
      if (Math.abs(dx) < AXIS_LOCK_PX && Math.abs(dy) < AXIS_LOCK_PX) return
      // 先到 6px 的一方定轴：纵向让给页面滚动，本手势整段放弃
      horizontal.current = Math.abs(dx) > Math.abs(dy)
      if (!horizontal.current) return
    }
    if (!horizontal.current) return
    setDragX(dx)
  }

  const onTouchEnd = (e: CommonEvent) => {
    if (disabled || exit !== null) return
    const touch = (e as unknown as ITouchEvent).changedTouches[0]
    const dx = touch ? touch.clientX - startX.current : 0
    const dt = Math.max(1, Date.now() - startTime.current)
    const velocity = Math.abs((dx / dt) * 1000) // px/s
    horizontal.current = null
    const threshold = windowWidth() * FLYOUT_RATIO
    if (dx >= threshold || (dx > 0 && velocity >= backGestureThreshold.velocity * 1000)) {
      commit('like')
      return
    }
    if (dx <= -threshold || (dx < 0 && velocity >= backGestureThreshold.velocity * 1000)) {
      commit('skip')
      return
    }
    setDragX(0)
  }

  // 拖拽中给一点「态度预告」：偏向右 loveseat、偏向左冷处理
  const leaning = dragX > windowWidth() * 0.12 ? ' swipe-card--lean-like' : dragX < -windowWidth() * 0.12 ? ' swipe-card--lean-skip' : ''

  if (disabled) {
    return <View className="swipe-card">{children}</View>
  }

  // 飞出：交给 CSS 过渡（位移按方向 ×2 屏宽 + 旋转 + 淡出），播完再卸载
  const exitClass = exit === null ? '' : ` swipe-card--exit-${exit}`
  const dragging = exit === null && dragX !== 0
  const rotate = dragging ? (dragX / windowWidth()) * 10 : 0
  const style =
    exit === 'like'
      ? 'transform: translateX(200vw) rotate(24deg); opacity: 0;'
      : exit === 'skip'
        ? 'transform: translateX(-200vw) rotate(-24deg); opacity: 0;'
        : dragging
          ? `transform: translateX(${dragX}px) rotate(${rotate}deg);`
          : ''

  return (
    <View
      className={`swipe-card${exitClass}${dragging ? ' swipe-card--dragging' : ''}${leaning}`}
      style={style}
      catchMove
      onTouchStart={onTouchStart}
      onTouchMove={onTouchMove}
      onTouchEnd={onTouchEnd}
      onTouchCancel={onTouchEnd}
    >
      {children}
      {exit !== null ? (
        <Text className={`swipe-card__stamp swipe-card__stamp--${exit}`}>
          {exit === 'like' ? stampLike : stampSkip}
        </Text>
      ) : null}
    </View>
  )
}
