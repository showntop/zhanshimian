// G1 补充资料：身高 stepper + 职业/预算 chips，全部可跳过。
// 提交 = PUT /v1/me/profile 持久化 + 带快照进分析（createAnalysis.profile）。
import { useState } from 'react'
import Taro, { useLoad } from '@tarojs/taro'
import { Text, View } from '@tarojs/components'
import type { UserProfile } from '@zsm/core'
import { api } from '../../services/api'
import AppHeader from '../../components/app-header'
import PrimaryButton from '../../components/primary-button'
import Pill from '../../components/pill'
import './index.scss'

const ROLES = ['产品经理', '设计师', '咨询顾问', '学生', '其他']
const BUDGETS = ['500 以内', '500–1500', '1500 以上']

export default function ProfileSetup() {
  const [scene, setScene] = useState('interview')
  const [mediaIds, setMediaIds] = useState<string[]>([])
  const [height, setHeight] = useState(165)
  const [role, setRole] = useState('产品经理')
  const [budget, setBudget] = useState('500–1500')
  const [busy, setBusy] = useState(false)

  useLoad((options) => {
    if (options?.scene) setScene(options.scene)
    if (options?.media_ids) setMediaIds(options.media_ids.split(',').filter(Boolean))
  })

  const submit = async () => {
    setBusy(true)
    const profile: UserProfile = { height_cm: height, role, budget }
    try {
      // 持久化失败不阻塞：分析仍带当次快照
      await api.updateMyProfile(profile).catch(() => undefined)
      const { data } = await api.createAnalysis({ scene, media_ids: mediaIds, profile })
      Taro.redirectTo({ url: `/pages/analysis/index?id=${data.id}` })
    } catch (e) {
      Taro.showToast({ title: (e as Error).message || '提交没有成功，请重试', icon: 'none' })
    } finally {
      setBusy(false)
    }
  }

  return (
    <View className="page">
      <AppHeader title="补充资料" back />
      <View className="psetup">
        <View className="psetup__intro fade-up">
          <Text className="psetup__eyebrow">少一点填写，多一点准确</Text>
          <Text className="psetup__title">告诉我你的现实条件</Text>
          <Text className="psetup__lede">体重与三围不是必填项，跳过也不影响分析。</Text>
        </View>

        <View className="psetup__card fade-up delay-1">
          <Text className="psetup__field">身高</Text>
          <View className="psetup__stepper">
            <Text className="psetup__step pressable" onClick={() => setHeight((h) => Math.max(145, h - 1))}>
              −
            </Text>
            <Text className="psetup__height">{height} cm</Text>
            <Text className="psetup__step pressable" onClick={() => setHeight((h) => Math.min(185, h + 1))}>
              ＋
            </Text>
          </View>
        </View>

        <View className="psetup__card fade-up delay-2">
          <Text className="psetup__field">职业</Text>
          <View className="psetup__chips">
            {ROLES.map((item) => (
              <Pill key={item} label={item} active={role === item} onClick={() => setRole(item)} />
            ))}
          </View>
        </View>

        <View className="psetup__card fade-up delay-2">
          <Text className="psetup__field">形象预算</Text>
          <View className="psetup__chips">
            {BUDGETS.map((item) => (
              <Pill key={item} label={item} active={budget === item} onClick={() => setBudget(item)} />
            ))}
          </View>
        </View>

        <View className="psetup__foot fade-up delay-3">
          <PrimaryButton text="生成我的方案" loading={busy} onClick={submit} />
          <Text className="psetup__privacy">数据可随时删除</Text>
        </View>
      </View>
    </View>
  )
}
