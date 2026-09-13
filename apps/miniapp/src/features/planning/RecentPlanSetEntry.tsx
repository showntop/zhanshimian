// 首页的「最近方案集」入口：方案页在 tabBar 里，switchTab 不带 query，
// 所以用内存交接条把 plan_set_id 递过去（无在途 operation）。
import { Text, View } from '@tarojs/components'
import Taro from '@tarojs/taro'
import { HOME_COPY, SCENES, planSlotLabel } from '@zsm/core'
import type { PlanSet } from '@zsm/core'
import { writePlanSetHandoff } from '../../app/plan-set-handoff'

interface RecentPlanSetEntryProps {
  planSet: PlanSet
}

export default function RecentPlanSetEntry({ planSet }: RecentPlanSetEntryProps) {
  const sceneLabel =
    planSet.scene === 'general'
      ? '形象方案'
      : SCENES.find((scene) => scene.id === planSet.scene)?.label ?? ''
  const featured = [...planSet.variants].sort((a, b) => a.slot - b.slot)[0]

  return (
    <View
      className="plan-entry pressable"
      onClick={() => {
        writePlanSetHandoff({ planSetId: planSet.id, operationId: null })
        void Taro.switchTab({ url: '/pages/plans/index' })
      }}
    >
      <Text className="plan-entry__mark">{HOME_COPY.recentTitle}</Text>
      <Text className="plan-entry__title">
        {featured ? planSlotLabel(featured.name, featured.slot) : sceneLabel}
      </Text>
      <Text className="plan-entry__cta">{HOME_COPY.continuePlan}</Text>
    </View>
  )
}
