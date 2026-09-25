// 规划等待的进度视图：照片锚 + 三步指示 + 服务端真实进度。
// 所有数字与阶段文案都来自轮询快照；快照未到达时退到安静态——
// 照片静置、步骤全灰、进度条留空，绝不编一个 0% 或假阶段装忙。
import { Text, View } from '@tarojs/components'
import { PLANNING_COPY } from '@zsm/core'
import type { DisplayMedia } from '@zsm/core'
import SourceImage from '../../components/source-image'
import TextLink from '../../components/text-link'
import type { PlanProgressSnapshot } from './model'

interface PlanProgressViewProps {
  /** 方案集绑定报告的来源照片；绑定对不上就不展示，不拿别的照片凑 */
  media: DisplayMedia | null
  /** 轮询快照（null = 还没拿到第一次快照的安静态） */
  snapshot: PlanProgressSnapshot | null
  onWander: () => void
}

export default function PlanProgressView({ media, snapshot, onWander }: PlanProgressViewProps) {
  const stepClass = (index: number) => {
    if (!snapshot || index > snapshot.stepIndex) return ''
    return index === snapshot.stepIndex ? ' plan-progress__step--active' : ' plan-progress__step--done'
  }

  return (
    <View className="plan-progress">
      <View className="plan-progress__arch halo-pulse">
        {media ? (
          <SourceImage className="plan-progress__photo" media={media} anchor="top" frameAspect={3 / 4} />
        ) : (
          <View className="plan-progress__placeholder">
            <Text className="plan-progress__placeholder-text">{PLANNING_COPY.boundNote}</Text>
          </View>
        )}
      </View>

      <Text className="plan-progress__stage fade-up">
        {snapshot?.stageLine ?? PLANNING_COPY.progressFallback}
      </Text>
      {snapshot?.retrying ? (
        <Text className="plan-progress__retrying">{PLANNING_COPY.progressRetrying}</Text>
      ) : null}

      <View className="plan-progress__steps">
        {PLANNING_COPY.progressSteps.map((label, index) => (
          <View key={label} className={`plan-progress__step${stepClass(index)}`}>
            <View className="plan-progress__dot" />
            <Text className="plan-progress__step-label">{label}</Text>
          </View>
        ))}
      </View>

      <View className="plan-progress__bar">
        <View
          className="plan-progress__bar-fill"
          style={{ transform: `scaleX(${(snapshot?.percent ?? 0) / 100})` }}
        />
      </View>
      {snapshot ? <Text className="plan-progress__percent">{snapshot.percent}%</Text> : null}

      <Text className="plan-progress__eta">{PLANNING_COPY.progressEta}</Text>
      <TextLink text={PLANNING_COPY.wander} onClick={onWander} />
    </View>
  )
}
