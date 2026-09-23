import { useState } from 'react'
import { StyleSheet, Text, View } from 'react-native'
import { useLocalSearchParams, useRouter } from 'expo-router'
import type { UserProfile } from '@zsm/core'
import { api } from '../src/api'
import { STORAGE_KEYS, writeStorage } from '../src/storage'
import { Pill, PrimaryButton, Screen } from '../src/ui/Screen'
import { colors, space } from '../src/ui/theme'

const ROLES = ['产品经理', '设计师', '咨询顾问', '学生', '其他']
const BUDGETS = ['500 以内', '500–1500', '1500 以上']

export default function ProfileSetup() {
  const router = useRouter()
  const { scene, media_ids } = useLocalSearchParams<{ scene?: string; media_ids?: string }>()
  const [height, setHeight] = useState(165)
  const [role, setRole] = useState('产品经理')
  const [budget, setBudget] = useState('500–1500')
  const [busy, setBusy] = useState(false)

  const submit = async () => {
    setBusy(true)
    const profile: UserProfile = { height_cm: height, role, budget }
    try {
      await api.updateMyProfile(profile).catch(() => undefined)
      const ids = (media_ids || '').split(',').filter(Boolean)
      const { data } = await api.createAnalysis({ scene: scene || 'interview', media_ids: ids, profile })
      writeStorage(STORAGE_KEYS.activeTaskAnalysis, data.id)
      router.replace(`/analysis?id=${data.id}`)
    } catch (e) {
      router.setParams({})
      throw e
    } finally {
      setBusy(false)
    }
  }

  return (
    <Screen title="补充资料" onBack={() => router.back()}>
      <Text style={styles.title}>告诉我你的现实条件</Text>
      <Text style={styles.lede}>体重与三围不是必填项，跳过也不影响分析。</Text>
      <View style={styles.stepper}>
        <Text style={styles.step} onPress={() => setHeight((h) => Math.max(145, h - 1))}>−</Text>
        <Text style={styles.height}>{height} cm</Text>
        <Text style={styles.step} onPress={() => setHeight((h) => Math.min(185, h + 1))}>＋</Text>
      </View>
      <View style={styles.chips}>
        {ROLES.map((item) => (
          <Pill key={item} label={item} active={role === item} onPress={() => setRole(item)} />
        ))}
      </View>
      <View style={styles.chips}>
        {BUDGETS.map((item) => (
          <Pill key={item} label={item} active={budget === item} onPress={() => setBudget(item)} />
        ))}
      </View>
      <PrimaryButton text={busy ? '正在提交…' : '开始分析'} loading={busy} onPress={() => void submit().catch((e: Error) => undefined)} />
    </Screen>
  )
}

const styles = StyleSheet.create({
  title: { fontSize: 24, fontWeight: '700', color: colors.ink },
  lede: { fontSize: 14, color: colors.ink2, lineHeight: 22 },
  stepper: { flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: space.xl },
  step: { fontSize: 28, color: colors.moss, width: 40, textAlign: 'center' },
  height: { fontSize: 28, fontWeight: '700', color: colors.ink },
  chips: { flexDirection: 'row', flexWrap: 'wrap', gap: 8 },
})
