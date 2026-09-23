// 前后对比滑杆（交互重设计核心件）：
// 底层「原本」（用户照片）+ 上层「方案」（AI 预览）宽度裁切，拖动手柄揭示。
// 红线：两层图均由调用方经 ExampleImage 传入，标识/弱化契约不在此重复。
import { useEffect, useRef, useState } from 'react'
import Taro from '@tarojs/taro'
import { Text, View } from '@tarojs/components'
import type { CommonEvent, ITouchEvent } from '@tarojs/components'
import './index.scss'

interface CompareSliderProps {
  /** 底层：原本（ExampleImage user 模式） */
  current: React.ReactNode
  /** 上层：方案（ExampleImage，含 badge） */
  plan: React.ReactNode
  currentLabel?: string
  planLabel?: string
  /** 底层缺失时退化为单图（无手柄） */
  single?: boolean
}

let seq = 0

export default function CompareSlider({
  current,
  plan,
  currentLabel = '原本',
  planLabel = '方案',
  single
}: CompareSliderProps) {
  const idRef = useRef(`cmp-${++seq}`)
  const rectRef = useRef<{ left: number; width: number } | null>(null)
  // pos：上层可见宽度百分比，初始展示方案为主
  const [pos, setPos] = useState(66)
  const startRef = useRef({ x: 0, pos: 66 })

  // 量容器尺寸：single 切换会换根节点（单图分支此前没有 id，查询扑空后
  // rectRef 永远为 null，拖动手柄失效），故依赖里带上 single；
  // onTouchStart 里再兜底一次，覆盖布局迟于挂载的场景
  const measure = () => {
    Taro.createSelectorQuery()
      .select(`#${idRef.current}`)
      .boundingClientRect((res) => {
        // 回调可能返回数组（exec 场景），统一收窄为单个 rect
        const rect = Array.isArray(res) ? res[0] : res
        if (rect && rect.width > 0) rectRef.current = { left: rect.left, width: rect.width }
      })
      .exec()
  }

  useEffect(() => {
    measure()
  }, [single])

  const onTouchStart = (e: CommonEvent) => {
    if (!rectRef.current) measure()
    const touch = (e as unknown as ITouchEvent).touches[0]
    if (!touch) return
    startRef.current = { x: touch.clientX, pos }
  }

  const onTouchMove = (e: CommonEvent) => {
    if (!rectRef.current) return
    const touch = (e as unknown as ITouchEvent).touches[0]
    if (!touch) return
    const dx = touch.clientX - startRef.current.x
    const next = startRef.current.pos + (dx / rectRef.current.width) * 100
    setPos(Math.min(98, Math.max(2, next)))
  }

  if (single) {
    // id 必须带上：selector 查询按 id 定位，缺了会让 rectRef 永远为空
    return (
      <View id={idRef.current} className="cmp cmp--single">
        {plan}
      </View>
    )
  }

  return (
    <View id={idRef.current} className="cmp" onTouchStart={onTouchStart} onTouchMove={onTouchMove} catchMove>
      <View className="cmp__layer">{current}</View>
      <View className="cmp__layer cmp__layer--top" style={{ width: `${pos}%` }}>
        {/* 反向补偿宽度：裁切外壳变窄时，内层图保持与底层同宽对齐 */}
        <View className="cmp__clip" style={{ width: `${(10000 / pos).toFixed(3)}%` }}>
          {plan}
        </View>
      </View>
      <View className="cmp__handle" style={{ left: `${pos}%` }}>
        <View className="cmp__handle-line" />
        <View className="cmp__handle-knob">
          <Text className="cmp__handle-arrow">‹</Text>
          <Text className="cmp__handle-arrow">›</Text>
        </View>
      </View>
      <Text className="cmp__label cmp__label--left">{currentLabel}</Text>
      <Text className="cmp__label cmp__label--right">{planLabel}</Text>
    </View>
  )
}
