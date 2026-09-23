import { useEffect, useState } from 'react'
import { StyleSheet, Text, View } from 'react-native'
import { useLocalSearchParams, useRouter } from 'expo-router'
import { POLL_INTERVALS, analysisStageText, type Analysis } from '@zsm/core'
import { api } from '../src/api'
import { useAppPolling } from '../src/hooks/useAppPolling'
import { STORAGE_KEYS, readStorage, writeStorage } from '../src/storage'
import { PrimaryButton, Screen } from '../src/ui/Screen'
import { colors, radius, space } from '../src/ui/theme'

export default function AnalysisPage() {
  const router = useRouter()
  const { id } = useLocalSearchParams<{ id?: string }>()
  const analysisId = id || readStorage(STORAGE_KEYS.activeTaskAnalysis)
  const [failed, setFailed] = useState<Analysis | null>(null)
  const [progress, setProgress] = useState(8)
  const [stage, setStage] = useState('正在安全上传照片')

  useEffect(() => {
    if (analysisId) writeStorage(STORAGE_KEYS.activeTaskAnalysis, analysisId)
  }, [analysisId])

  useAppPolling({
    fetcher: async () => {
      if (!analysisId) throw new Error('missing id')
      const item = await api.getAnalysis(analysisId)
      setProgress(item.progress)
      setStage(analysisStageText(item.stage))
      return item
    },
    intervalMs: POLL_INTERVALS.analysis,
    enabled: Boolean(analysisId) && !failed,
    onDone: (item) => {
      if (item.status === 'completed') {
        writeStorage(STORAGE_KEYS.activeTaskAnalysis, '')
        if (item.report_id) writeStorage(STORAGE_KEYS.reportId, item.report_id)
        router.replace('/report')
        return
      }
      setFailed(item)
    },
  })

  if (failed) {
    const reasons = (failed.error_message || '请按拍摄指引重新提交')
      .split(/[；;]/)
      .map((p) => p.trim())
      .filter(Boolean)
    return (
      <Screen title="正在分析" onBack={() => router.back()}>
        <Text style={styles.title}>照片没有通过检查</Text>
        {reasons.map((reason) => (
          <Text key={reason} style={styles.reason}>{reason}</Text>
        ))}
        <PrimaryButton text="重新拍摄" onPress={() => router.replace('/capture')} />
      </Screen>
    )
  }

  return (
    <Screen title="正在分析" onBack={() => router.back()}>
      <View style={styles.frame}>
        <Text style={styles.mark}>AI 分析中</Text>
      </View>
      <Text style={styles.stage}>{stage}</Text>
      <View style={styles.track}>
        <View style={[styles.fill, { width: `${Math.max(4, Math.min(100, progress))}%` }]} />
      </View>
      <Text style={styles.num}>{Math.round(progress)}%</Text>
    </Screen>
  )
}

const styles = StyleSheet.create({
  frame: { height: 280, borderRadius: radius.lg, backgroundColor: colors.mossSoft, alignItems: 'center', justifyContent: 'center' },
  mark: { color: colors.moss, fontWeight: '600' },
  stage: { fontSize: 16, color: colors.ink, textAlign: 'center' },
  track: { height: 8, borderRadius: 4, backgroundColor: colors.line, overflow: 'hidden' },
  fill: { height: 8, backgroundColor: colors.moss },
  num: { textAlign: 'center', color: colors.ink2 },
  title: { fontSize: 20, fontWeight: '700', color: colors.ink },
  reason: { fontSize: 14, color: colors.ink2, lineHeight: 22 },
})
