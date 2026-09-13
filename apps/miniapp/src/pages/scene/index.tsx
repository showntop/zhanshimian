// 场合 Brief 外壳：页面只负责外壳与路由参数（scene），问答与提交全在 features/planning。
// 答案只在组件 state 与 POST body 里存在，不写 Storage。
import { View } from '@tarojs/components'
import Taro, { useLoad } from '@tarojs/taro'
import { useState } from 'react'
import { SCENE_BRIEF_COPY } from '@zsm/core'
import { usePageClass } from '../../hooks/use-page-visibility'
import AppHeader from '../../components/app-header'
import SceneBriefScreen from '../../features/planning/SceneBriefScreen'

export default function Scene() {
  const pageClass = usePageClass(true)
  const [scene, setScene] = useState('')

  useLoad((options) => {
    setScene(options?.scene ?? '')
  })

  // 没带场景参数就没法出题：回方案 tab，不替用户挑一个场景
  if (!scene) {
    void Taro.switchTab({ url: '/pages/plans/index' })
    return <View className={pageClass} />
  }

  return (
    <View className={pageClass}>
      <AppHeader title={SCENE_BRIEF_COPY.title} back />
      <SceneBriefScreen scene={scene} />
    </View>
  )
}
