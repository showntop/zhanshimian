// 往期方案区：同一报告 + 场景下历史生成的方案集，按「集」列出（日期倒序）。
// 点击把那一集装回卡堆（PlansScreen 负责）；当前集不重复列自己。
import { ScrollView, Text, View } from '@tarojs/components'
import { PLANNING_COPY } from '@zsm/core'
import type { PlanSet } from '@zsm/core'
import SourceImage from '../../components/source-image'
import './plans-history.scss'

interface PlansHistoryProps {
  sets: PlanSet[]
  activeSetId: string
  onSelect: (setId: string) => void
}

/** created_at → 「9.12」式短日期。服务端时间是 ISO 串，截取即得，不做时区换算。 */
function dateLabel(set: PlanSet): string {
  return set.created_at.slice(5, 10).replace('-', '.')
}

export default function PlansHistory({ sets, activeSetId, onSelect }: PlansHistoryProps) {
  // 只有一集就没有「往期」可言
  if (sets.length < 2) return null

  return (
    <View className="plans-history">
      <Text className="plans-history__title">{PLANNING_COPY.historyTitle}</Text>
      <ScrollView className="plans-history__row" scrollX enhanced showScrollbar={false}>
        {sets.map((set) => {
          const liked = set.variants.filter((variant) => variant.decision?.decision === 'like').length
          const skipped = set.variants.filter((variant) => variant.decision?.decision === 'skip').length
          const active = set.id === activeSetId
          // 集级状态标注：制作中/未生成的集不再装死缩略图无解释
          const inFlight = set.state === 'planning' || set.state === 'rendering'
          const failed = set.state === 'failed'
          return (
            <View
              key={set.id}
              className={`plans-history__item pressable ${active ? 'plans-history__item--active' : ''}`}
              onClick={() => {
                if (!active) onSelect(set.id)
              }}
            >
              <View className="plans-history__thumbs">
                {set.variants.slice(0, 3).map((variant) =>
                  variant.render.state === 'ready' && variant.render.media ? (
                    <View key={variant.id} className="plans-history__thumb">
                      <SourceImage media={variant.render.media} mode="aspectFill" />
                    </View>
                  ) : (
                    <View key={variant.id} className="plans-history__thumb plans-history__thumb--empty">
                      <Text className="plans-history__thumb-text">
                        {variant.render.state === 'failed' ? PLANNING_COPY.renderThumbFailed : PLANNING_COPY.renderThumbUnavailable}
                      </Text>
                    </View>
                  ),
                )}
              </View>
              <View className="plans-history__meta">
                <Text className="plans-history__date">{dateLabel(set)}</Text>
                {inFlight ? (
                  <Text className="plans-history__status">{PLANNING_COPY.tabInFlightSuffix}</Text>
                ) : failed ? (
                  <Text className="plans-history__status plans-history__status--failed">{PLANNING_COPY.renderThumbFailed}</Text>
                ) : liked > 0 || skipped > 0 ? (
                  // 每轮的情况：喜欢几个、跳过几个，一眼对比轮次
                  <Text className="plans-history__likes">
                    {liked > 0 ? `♥ ${liked}` : ''}
                    {liked > 0 && skipped > 0 ? ' · ' : ''}
                    {skipped > 0 ? `✕ ${skipped}` : ''}
                  </Text>
                ) : null}
              </View>
            </View>
          )
        })}
      </ScrollView>
    </View>
  )
}
