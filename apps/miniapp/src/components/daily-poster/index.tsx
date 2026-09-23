import { View, Text } from '@tarojs/components'
import {
  plateToneFor,
  shellFor,
  type ContentType,
  type ContentVisual,
  type PosterShell,
} from '@zsm/core'
import PosterVisual from './poster-visual'
import './index.scss'

interface DailyPosterProps {
  type: ContentType
  /** 内容自己声明的视觉规格 —— 画什么由它决定，不由 type 决定 */
  visual: ContentVisual
  /** 选题（杂志式标题） */
  topic: string
  /** 适配说明：首页只给一到两句，完整信息在今日页 */
  fitText: string
  /** 海报角落编号，传日期（09.19）比序号更有时间感 */
  seq: string
  /** 收下动作的文案，如「收进我的色卡」 */
  saveLabel: string
  onSave: () => void
  onOpen: () => void
}

/**
 * 首页海报 = 容器 + 内容视觉。
 *
 *   容器（shellFor(type)）  → 底板色调、承载块位置、文字位置、视觉画框
 *   视觉（content.visual）  → 色卡 / 位置图 / 对比 / 素材，每条内容不同
 *
 * 两者的分工是这个组件的全部结构。早先版本把视觉也按 type 出，
 * 导致「驼色是个陷阱」这种要画位置图的内容拿到了通用方块。
 */
export default function DailyPoster({
  type,
  visual,
  topic,
  fitText,
  seq,
  saveLabel,
  onSave,
  onOpen,
}: DailyPosterProps) {
  const shell: PosterShell = shellFor(type)
  // 文字色跟承载块走：fabric 类是"深块压浅底"，按底板取色会读不出来
  const textTone = plateToneFor(shell)

  return (
    <View className={`daily-poster daily-poster--${shell.tone}`} onClick={onOpen}>
      <View className="daily-poster__base" style={{ background: shell.base }} />

      {/* 视觉画框：内容图形画在这个范围内，上界 8% / 下界 38% 是硬约束 */}
      <View
        className="daily-poster__stage"
        style={{
          left: shell.stage.left,
          top: shell.stage.top,
          width: shell.stage.width,
          height: shell.stage.height,
        }}
      >
        <PosterVisual visual={visual} tone={shell.tone} />
      </View>

      {/*
        承载块与文字同属一个容器：容器高度由文字撑开（并带 min-height 保证它始终是
        构图里最大的那块），承载块用负 inset 跟随容器。
      */}
      <View
        className="daily-poster__plate-wrap"
        style={{ left: shell.text.left, top: shell.plate.top, width: shell.text.width }}
      >
        <View
          className="daily-poster__plate"
          style={{ background: shell.plate.bg, transform: `rotate(${shell.plate.rotate}deg)` }}
        />
        <View className={`daily-poster__text daily-poster__text--${textTone}`}>
          <Text className="daily-poster__title">{topic}</Text>
          <Text className="daily-poster__hook">{fitText}</Text>
          <View className="daily-poster__acts">
            <View
              className="daily-poster__cta"
              onClick={(event) => {
                event.stopPropagation()
                onSave()
              }}
            >
              <Text className="daily-poster__cta-text">{saveLabel}</Text>
            </View>
            <Text className="daily-poster__link">今日页 ›</Text>
          </View>
        </View>
      </View>

      <Text className="daily-poster__seq">{seq}</Text>
      <Text className="daily-poster__vmark">{shell.vmark}</Text>
    </View>
  )
}
