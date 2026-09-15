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
  // 路由参数在 useLoad 前是空的：未到位前不渲染 Screen，否则挂载效应会拿空 scene 误判跳走。
  // 空串是合法值（未知场景，由 Screen 负责回方案 tab），所以用 null 区分「还没到位」。
  const [scene, setScene] = useState<string | null>(null)

  useLoad((options) => { setScene(options?.scene ?? '') })

  return (
    <View className={pageClass}>
      <AppHeader title={SCENE_BRIEF_COPY.title} back />
      {scene === null ? null : <SceneBriefScreen scene={scene} />}
    </View>
  )
}
