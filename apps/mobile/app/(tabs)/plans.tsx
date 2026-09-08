import { useCallback, useEffect, useState } from 'react'
import { Pressable, StyleSheet, Text, View } from 'react-native'
import { useRouter } from 'expo-router'
import { POLL_INTERVALS, shouldStopPolling, type Plan } from '@zsm/core'
import { api } from '../../src/api'
import { useAppPolling } from '../../src/hooks/useAppPolling'
import { STORAGE_KEYS, readStorage, writeStorage } from '../../src/storage'
import { EmptyBlock, ErrorBlock, Pill, PrimaryButton, Screen } from '../../src/ui/Screen'
import { LookImage } from '../../src/ui/LookImage'
import { colors, radius, space } from '../../src/ui/theme'

const SCENE_TABS = [
  { key: 'general', label: '形象方案' },
  { key: 'interview', label: '面试' },
  { key: 'wedding', label: '婚礼' },
  { key: 'date', label: '约会' },
  { key: 'daily', label: '日常' },
] as const

export default function Plans() {
  const router = useRouter()
  const [scene, setScene] = useState('general')
  const [plans, setPlans] = useState<Plan[]>([])
  const [index, setIndex] = useState(0)
  const [failed, setFailed] = useState(false)
  const [loading, setLoading] = useState(true)

  const load = useCallback(async (target = scene) => {
    setLoading(true)
    setFailed(false)
    try {
      let reportId = readStorage(STORAGE_KEYS.reportId)
      if (!reportId) {
        const current = await api.getCurrentReport()
        if (!current) {
          setPlans([])
          return
        }
        writeStorage(STORAGE_KEYS.reportId, current.id)
        reportId = current.id
      }
      setPlans(await api.listPlans(reportId, target))
    } catch {
      setFailed(true)
    } finally {
      setLoading(false)
    }
  }, [scene])

  useEffect(() => {
    load(scene)
  }, [load, scene])

  const hasActive = plans.some((p) => p.look_task && (p.look_task.status === 'queued' || p.look_task.status === 'processing'))
  useAppPolling({
    fetcher: async () => {
      const reportId = readStorage(STORAGE_KEYS.reportId)
      if (!reportId) return []
      const items = await api.listPlans(reportId, scene)
      setPlans(items)
      return items
    },
    intervalMs: POLL_INTERVALS.planLook,
    enabled: hasActive,
    isSettled: (items) => items.every((p) => !p.look_task || shouldStopPolling(p.look_task.status)),
  })

  const plan = plans[index]
  if (failed) {
    return (
      <Screen title="方案">
        <ErrorBlock onRetry={() => load()} />
      </Screen>
    )
  }
  if (!loading && plans.length === 0) {
    return (
      <Screen title="方案">
        <EmptyBlock title="还没有方案" description="先完成形象分析，再看三套方案。" actionText="去建档" onAction={() => router.push('/capture')} />
      </Screen>
    )
  }

  return (
    <Screen title="方案">
      <View style={styles.tabs}>
        {SCENE_TABS.map((tab) => (
          <Pill key={tab.key} label={tab.label} active={scene === tab.key} onPress={() => { setScene(tab.key); setIndex(0) }} />
        ))}
      </View>
      {plan ? (
        <View style={styles.card}>
          <LookImage
            style={styles.hero}
            src={plan.generated_image_url || plan.image_url}
            badgeText={(plan.look_provider ?? '').startsWith('demo') ? '效果示例' : '风格参考'}
          />
          <Text style={styles.name}>{plan.name}</Text>
          <Text style={styles.why}>{plan.why}</Text>
          <View style={styles.switch}>
            {plans.map((item, i) => (
              <Pressable key={item.id} onPress={() => setIndex(i)} style={[styles.dot, i === index && styles.dotOn]} />
            ))}
          </View>
          <PrimaryButton text="查看这套" onPress={() => router.push(`/plan/${plan.id}`)} />
        </View>
      ) : null}
    </Screen>
  )
}

const styles = StyleSheet.create({
  tabs: { flexDirection: 'row', flexWrap: 'wrap', gap: 8 },
  card: { gap: space.md },
  hero: { width: '100%', height: 280 },
  name: { fontSize: 22, fontWeight: '700', color: colors.ink },
  why: { fontSize: 14, lineHeight: 22, color: colors.ink2 },
  switch: { flexDirection: 'row', gap: 8 },
  dot: { width: 8, height: 8, borderRadius: 4, backgroundColor: colors.line },
  dotOn: { backgroundColor: colors.moss, width: 18 },
})
