import { useCallback, useEffect, useState } from 'react'
import { StyleSheet, Text, View } from 'react-native'
import { useRouter } from 'expo-router'
import type { Report } from '@zsm/core'
import { api } from '../src/api'
import { STORAGE_KEYS, readStorage, writeStorage } from '../src/storage'
import { ErrorBlock, PrimaryButton, Screen } from '../src/ui/Screen'
import { colors, radius, space } from '../src/ui/theme'

const CATEGORY_LABEL: Record<string, string> = { hair: '发型', makeup: '妆容', outfit: '穿搭', color: '色彩' }

export default function ReportPage() {
  const router = useRouter()
  const [report, setReport] = useState<Report | null>(null)
  const [failed, setFailed] = useState(false)
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    setFailed(false)
    try {
      let id = readStorage(STORAGE_KEYS.reportId)
      if (!id) {
        const current = await api.getCurrentReport()
        if (!current) {
          setReport(null)
          return
        }
        id = current.id
      }
      const item = await api.getReport(id)
      setReport(item)
      writeStorage(STORAGE_KEYS.reportId, item.id)
    } catch {
      setFailed(true)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  const viewPlans = async () => {
    if (!report) return
    router.push('/(tabs)/plans')
    await api.upsertPlans(report.id, { scene: 'general', answers: {} }).catch(() => undefined)
  }

  if (failed || (!loading && !report)) {
    return (
      <Screen title="形象报告" onBack={() => router.back()}>
        <ErrorBlock
          message={failed ? undefined : '还没有形象档案，先拍三张照片开始分析。'}
          retryText={failed ? '重试' : '去建档'}
          onRetry={() => (failed ? load() : router.replace('/capture'))}
        />
      </Screen>
    )
  }

  return (
    <Screen title="形象报告" onBack={() => router.back()}>
      <Text style={styles.priority}>{report?.priority_title}</Text>
      <Text style={styles.copy}>{report?.priority_copy}</Text>
      <View style={styles.tags}>
        {(report?.impression_tags ?? []).map((tag) => (
          <Text key={tag} style={styles.tag}>{tag}</Text>
        ))}
      </View>
      {(report?.findings ?? []).map((finding) => (
        <View key={finding.id} style={styles.finding}>
          <Text style={styles.findingCat}>{CATEGORY_LABEL[finding.category] || finding.category}</Text>
          <Text style={styles.findingLabel}>{finding.label}</Text>
          <Text style={styles.findingDetail}>{finding.detail}</Text>
        </View>
      ))}
      <PrimaryButton text="查看三套方案" onPress={() => void viewPlans()} />
    </Screen>
  )
}

const styles = StyleSheet.create({
  priority: { fontSize: 24, fontWeight: '700', color: colors.ink },
  copy: { fontSize: 15, lineHeight: 24, color: colors.ink2 },
  tags: { flexDirection: 'row', flexWrap: 'wrap', gap: 8 },
  tag: { paddingHorizontal: 10, paddingVertical: 4, borderRadius: radius.pill, backgroundColor: colors.mossSoft, color: colors.moss, fontSize: 12 },
  finding: { gap: 4, paddingVertical: space.sm, borderTopWidth: 1, borderTopColor: colors.line },
  findingCat: { fontSize: 12, color: colors.moss, fontWeight: '600' },
  findingLabel: { fontSize: 16, fontWeight: '600', color: colors.ink },
  findingDetail: { fontSize: 13, lineHeight: 20, color: colors.ink2 },
})
