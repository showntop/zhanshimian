import { useCallback, useEffect, useState } from 'react'
import { StyleSheet, Text, View } from 'react-native'
import { useLocalSearchParams, useRouter } from 'expo-router'
import type { Plan } from '@zsm/core'
import { api } from '../../src/api'
import { STORAGE_KEYS, writeStorage } from '../../src/storage'
import { ErrorBlock, PrimaryButton, Screen } from '../../src/ui/Screen'
import { LookImage } from '../../src/ui/LookImage'
import { colors, space } from '../../src/ui/theme'

const CATEGORY_LABEL: Record<string, string> = { hair: '发型', makeup: '妆容', outfit: '穿搭' }

export default function PlanDetail() {
  const router = useRouter()
  const { id } = useLocalSearchParams<{ id: string }>()
  const [plan, setPlan] = useState<Plan | null>(null)
  const [failed, setFailed] = useState(false)

  const load = useCallback(async () => {
    try {
      setPlan(await api.getPlan(id))
    } catch {
      setFailed(true)
    }
  }, [id])

  useEffect(() => {
    load()
  }, [load])

  const select = async () => {
    if (!plan) return
    const saved = await api.selectPlan(plan.id)
    writeStorage(STORAGE_KEYS.planId, saved.id)
    writeStorage(STORAGE_KEYS.savedPlanId, saved.id)
    router.push(`/checklist?id=${saved.id}`)
  }

  if (failed || !plan) {
    return (
      <Screen title="方案详情" onBack={() => router.back()}>
        <ErrorBlock onRetry={load} />
      </Screen>
    )
  }

  return (
    <Screen title={plan.name} onBack={() => router.back()}>
      <LookImage
        style={styles.hero}
        src={plan.generated_image_url || plan.image_url}
        badgeText={(plan.look_provider ?? '').startsWith('demo') ? '效果示例' : '风格参考'}
      />
      <Text style={styles.why}>{plan.why}</Text>
      {(plan.steps ?? []).map((step) => (
        <View key={step.id} style={styles.step}>
          <Text style={styles.stepCat}>{CATEGORY_LABEL[step.category] || step.category}</Text>
          <Text style={styles.stepTitle}>{step.title}</Text>
          <Text style={styles.stepSummary}>{step.summary}</Text>
        </View>
      ))}
      <PrimaryButton text="就用这套" onPress={() => void select()} />
      <Text style={styles.alt} onPress={() => router.push(`/feedback?plan_id=${plan.id}`)}>
        实际反馈
      </Text>
    </Screen>
  )
}

const styles = StyleSheet.create({
  hero: { width: '100%', height: 320 },
  why: { fontSize: 15, lineHeight: 24, color: colors.ink2 },
  step: { gap: 4, paddingVertical: space.sm, borderTopWidth: 1, borderTopColor: colors.line },
  stepCat: { fontSize: 12, color: colors.moss, fontWeight: '600' },
  stepTitle: { fontSize: 16, fontWeight: '600', color: colors.ink },
  stepSummary: { fontSize: 13, lineHeight: 20, color: colors.ink2 },
  alt: { textAlign: 'center', color: colors.moss, textDecorationLine: 'underline' },
})
