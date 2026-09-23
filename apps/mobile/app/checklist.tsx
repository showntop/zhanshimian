import { useCallback, useEffect, useState } from 'react'
import { Pressable, StyleSheet, Text, View } from 'react-native'
import * as Haptics from 'expo-haptics'
import { useLocalSearchParams, useRouter } from 'expo-router'
import type { ChecklistItem } from '@zsm/core'
import { api } from '../src/api'
import { STORAGE_KEYS, readStorage } from '../src/storage'
import { EmptyBlock, ErrorBlock, PrimaryButton, Screen } from '../src/ui/Screen'
import { colors, space } from '../src/ui/theme'

const CATEGORY_LABEL: Record<string, string> = { hair: '发型', makeup: '妆容', outfit: '穿搭' }

export default function Checklist() {
  const router = useRouter()
  const { id } = useLocalSearchParams<{ id?: string }>()
  const planId = id || readStorage(STORAGE_KEYS.planId)
  const [items, setItems] = useState<ChecklistItem[]>([])
  const [failed, setFailed] = useState(false)

  const load = useCallback(async () => {
    if (!planId) return
    try {
      setItems(await api.getChecklist(planId))
      setFailed(false)
    } catch {
      setFailed(true)
    }
  }, [planId])

  useEffect(() => {
    load()
  }, [load])

  const toggle = async (item: ChecklistItem) => {
    const next = !item.completed
    setItems((prev) => prev.map((it) => (it.id === item.id ? { ...it, completed: next } : it)))
    if (next) void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light)
    try {
      const saved = await api.updateChecklistItem(planId, item.id, { completed: next })
      setItems((prev) => prev.map((it) => (it.id === item.id ? saved : it)))
    } catch {
      setItems((prev) => prev.map((it) => (it.id === item.id ? { ...it, completed: item.completed } : it)))
    }
  }

  const done = items.filter((i) => i.completed).length
  const allDone = items.length > 0 && done === items.length

  if (failed) {
    return (
      <Screen title="执行清单" onBack={() => router.back()}>
        <ErrorBlock onRetry={load} />
      </Screen>
    )
  }
  if (items.length === 0) {
    return (
      <Screen title="执行清单" onBack={() => router.back()}>
        <EmptyBlock title="清单还没准备好" actionText="返回方案" onAction={() => router.back()} />
      </Screen>
    )
  }

  return (
    <Screen title="执行清单" onBack={() => router.back()}>
      <Text style={styles.progress}>{done} / {items.length} 已完成</Text>
      {items.map((item) => (
        <Pressable key={item.id} style={styles.row} onPress={() => void toggle(item)}>
          <View style={[styles.check, item.completed && styles.checkOn]}>
            <Text style={styles.checkMark}>{item.completed ? '✓' : ''}</Text>
          </View>
          <View style={styles.copy}>
            <Text style={styles.cat}>{CATEGORY_LABEL[item.category] || item.category}</Text>
            <Text style={styles.title}>{item.title}</Text>
          </View>
        </Pressable>
      ))}
      {allDone ? <PrimaryButton text="记录今天的效果" onPress={() => router.push(`/feedback?plan_id=${planId}`)} /> : null}
    </Screen>
  )
}

const styles = StyleSheet.create({
  progress: { fontSize: 14, color: colors.ink2 },
  row: { flexDirection: 'row', gap: space.md, alignItems: 'center', paddingVertical: space.sm },
  check: { width: 28, height: 28, borderRadius: 14, borderWidth: 2, borderColor: colors.line, alignItems: 'center', justifyContent: 'center' },
  checkOn: { backgroundColor: colors.moss, borderColor: colors.moss },
  checkMark: { color: '#fff', fontWeight: '700' },
  copy: { flex: 1, gap: 2 },
  cat: { fontSize: 12, color: colors.moss, fontWeight: '600' },
  title: { fontSize: 16, color: colors.ink },
})
