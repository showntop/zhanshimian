// 每日内容的等待动画（方案 §3.1：scenario 枚举决定播哪套，settle 由
// generate 返回触发——动画实现载体不锁死，这里用 CSS 序列形态，
// 零图片、零外部依赖，规避 WebP 渲染空白与包体限制）。
//
// 场景只覆盖部分主题：未覆盖场景回落 fallback（方案 §6 的风险对策）。
// prefers-reduced-motion 时动画归零（AGENTS 动效规约）。

import { Text, View } from '@tarojs/components'
import { DAILY_COPY } from '@zsm/core'
import './index.scss'

interface DailyWaitingProps {
  /** prepare 返回的 scenario（color/fit/proportion/…/fallback） */
  scenario: string
  /** true = generate 已返回，播放落位收尾 */
  settling?: boolean
}

const SCENE_LABELS: Record<string, string> = {
  color: '颜色',
  fit: '版型',
  proportion: '比例',
  fabric: '面料',
  occasion: '场合',
  howto: '技巧',
  outfit: '搭配',
  fallback: DAILY_COPY.eyebrow,
}

export default function DailyWaiting({ scenario, settling = false }: DailyWaitingProps) {
  const theme = SCENE_LABELS[scenario] !== undefined ? scenario : 'fallback'
  return (
    <View className={`dw-wait dw-wait--${theme} ${settling ? 'is-settling' : ''}`}>
      {theme === 'color' ? (
        <View className="dw-wait__swatch">
          <View className="dw-wait__chip" />
          <View className="dw-wait__chip dw-wait__chip--2" />
          <View className="dw-wait__chip dw-wait__chip--3" />
        </View>
      ) : null}

      {theme === 'proportion' ? (
        <View className="dw-wait__axis">
          <View className="dw-wait__axis-dot" />
          <View className="dw-wait__axis-line" />
        </View>
      ) : null}

      {theme === 'fabric' || theme === 'fit' ? (
        <View className="dw-wait__fold">
          <View className="dw-wait__fold-line" />
          <View className="dw-wait__fold-line dw-wait__fold-line--2" />
          <View className="dw-wait__fold-line dw-wait__fold-line--3" />
        </View>
      ) : null}

      {theme === 'fallback' || theme === 'occasion' || theme === 'howto' || theme === 'outfit' ? (
        <View className="dw-wait__pulse">
          <View className="dw-wait__pulse-ring" />
          <View className="dw-wait__pulse-dot" />
        </View>
      ) : null}

      <Text className="dw-wait__label">{DAILY_COPY.eyebrow}</Text>
    </View>
  )
}
