// 场合 Brief 外壳：问答与提交全在 features/planning；答案只在 state 与 POST body。
import { useState } from 'react'
import { useLoad } from '@tarojs/taro'
import { View } from '@tarojs/components'
import { SCENE_BRIEF_COPY } from '@zsm/core'
import { usePageClass } from '../../hooks/use-page-visibility'
import AppHeader from '../../components/app-header'
import SceneBriefScreen from '../../features/planning/SceneBriefScreen'

export default function Scene() {
  const pageClass = usePageClass(true)
  // 路由参数在 useLoad 前为空：未到位前不渲染 Screen（空串是合法值，用 null 区分「还没到位」）。
  const [scene, setScene] = useState<string | null>(null)

  useLoad((options) => { setScene(options?.scene ?? '') })

  return (
    <View className={pageClass}>
      <AppHeader title={SCENE_BRIEF_COPY.title} back />
      {scene === null ? null : <SceneBriefScreen scene={scene} />}
    </View>
  )
}
