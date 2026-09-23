// 单套方案渲染状态的唯一呈现。六种状态各自怎么说都在这里，
// 页面不再见到裸的 state 字符串，也就不会各页各写一套措辞。
import { Text, View } from '@tarojs/components'
import { PLANNING_COPY } from '@zsm/core'
import type { VariantRenderView } from '../../features/planning/model'
import './index.scss'

interface RenderStateProps {
  view: VariantRenderView
  /** failed + retryable 时必须给；点了就发起单套重试 */
  onRetry?: () => void
}

export default function RenderState({ view, onRetry }: RenderStateProps) {
  if (view.kind === 'ready') return null

  if (view.kind === 'queued' || view.kind === 'generating' || view.kind === 'checking') {
    const text =
      view.kind === 'queued'
        ? PLANNING_COPY.renderQueued
        : view.kind === 'generating'
          ? PLANNING_COPY.renderGenerating
          : PLANNING_COPY.renderChecking
    return (
      <View className="render-state render-state--working">
        <View className="spinner render-state__spin" />
        <Text className="render-state__text">{text}</Text>
      </View>
    )
  }

  if (view.kind === 'unavailable') {
    return (
      <View className="render-state render-state--unavailable">
        <Text className="render-state__text">{PLANNING_COPY.renderUnavailable}</Text>
        <Text className="render-state__note">{PLANNING_COPY.renderUnavailableNote}</Text>
      </View>
    )
  }

  return (
    <View className="render-state render-state--failed">
      <Text className="render-state__text">{PLANNING_COPY.renderFailed}</Text>
      <Text className="render-state__note">{PLANNING_COPY.renderFailedNote}</Text>
      {view.retryable && onRetry ? (
        <Text className="render-state__retry pressable" onClick={onRetry}>
          {PLANNING_COPY.renderRetry}
        </Text>
      ) : null}
    </View>
  )
}
