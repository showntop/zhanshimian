// 体验实验室：AR/3D/试衣（M3 评估身份一致性前保持占位，不冒充真实能力）。
import { useState } from 'react'
import Taro from '@tarojs/taro'
import { Text, View } from '@tarojs/components'
import { usePageShell } from '../../../../hooks/use-page-visibility'
import AppHeader from '../../../../components/app-header'
import ExampleImage from '../../../../components/example-image'
import './index.scss'

const FEATURES = [
  { key: 'hair-ar', name: '发型与妆容 AR', status: '内测', desc: '实时切换发型轮廓、发色与眉眼重点。', slug: 'sharp' },
  { key: '3d', name: '3D 形象 Lite', status: '开发中', desc: '表达比例和穿搭轮廓，不承诺精确测量。', slug: 'natural' },
  { key: 'try-on', name: '上半身试衣', status: '排队中', desc: '先支持外套和上衣，不做完整商城。', slug: 'warm' },
] as const

export default function Lab() {
  const [waitlisted, setWaitlisted] = useState<string[]>([])
  const { pageClass, enter } = usePageShell(true, '', 'lab')

  const act = (feature: (typeof FEATURES)[number]) => {
    if (feature.status === '内测') {
      Taro.showModal({
        title: feature.name,
        content: '内测名额逐步开放。生成能力需通过身份一致性评测后上线，不会用静态图冒充真实效果。',
        showCancel: false,
        confirmText: '知道了',
      })
      return
    }
    if (waitlisted.includes(feature.key)) {
      Taro.showToast({ title: '已在候补名单', icon: 'none' })
      return
    }
    setWaitlisted((prev) => [...prev, feature.key])
    Taro.showToast({ title: '已加入候补', icon: 'success' })
  }

  return (
    <View className={pageClass}>
      <AppHeader title="体验实验室" back />
      <View className="lab">
        <View className={`lab__intro ${enter()}`}>
          <Text className="lab__title">这里放「哇塞」，不打断核心流程</Text>
        </View>
        {FEATURES.map((feature, i) => (
          <View key={feature.key} className={`lab__card card ${enter((i + 1) as 1 | 2 | 3)}`}>
            <ExampleImage className="lab__card-img" slug={feature.slug} variant="full" badgeText="风格参考" anchor="top" />
            <View className="lab__card-copy">
              <View className="lab__card-head">
                <Text className="lab__card-name">{feature.name}</Text>
                <Text className={`lab__card-status ${feature.status === '内测' ? 'lab__card-status--live' : ''}`}>
                  {waitlisted.includes(feature.key) ? '已预约' : feature.status}
                </Text>
              </View>
              <Text className="lab__card-desc">{feature.desc}</Text>
              <View
                className="lab__card-btn pressable"
                onClick={() => act(feature)}
              >
                <Text>{feature.status === '内测' ? '了解进展' : '预约体验'}</Text>
              </View>
            </View>
          </View>
        ))}
      </View>
    </View>
  )
}
