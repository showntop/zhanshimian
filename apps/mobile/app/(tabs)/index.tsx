import { useCallback, useEffect, useState } from 'react'
import { Pressable, StyleSheet, Text, View } from 'react-native'
import { useRouter } from 'expo-router'
import { greetingForNow, HOME_TITLE, SCENES, type HomeBootstrap } from '@zsm/core'
import { api } from '../../src/api'
import { STORAGE_KEYS, writeStorage } from '../../src/storage'
import { ErrorBlock, PrimaryButton, Screen } from '../../src/ui/Screen'
import { LookImage } from '../../src/ui/LookImage'
import { colors, radius, space } from '../../src/ui/theme'

export default function Home() {
  const router = useRouter()
  const [data, setData] = useState<HomeBootstrap | null>(null)
  const [failed, setFailed] = useState(false)
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    try {
      const bootstrap = await api.getHomeBootstrap()
      setData(bootstrap)
      setFailed(false)
      if (bootstrap.report?.id) writeStorage(STORAGE_KEYS.reportId, bootstrap.report.id)
    } catch {
      setFailed(true)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  const hasReport = Boolean(data?.report?.id)
  const today = data?.today_plan

  return (
    <Screen>
      <Text style={styles.hi}>{greetingForNow()}，</Text>
      <Text style={styles.title}>{HOME_TITLE.replace('你好，', '')}</Text>
      {failed ? <ErrorBlock onRetry={load} /> : null}
      {!loading && !failed && !hasReport ? (
        <View style={styles.card}>
          <Text style={styles.cardTitle}>三张照片，开始你的形象档案</Text>
          <Text style={styles.cardDesc}>正脸、45° 侧脸、正面全身。不用化妆，也不需要刻意摆姿势。</Text>
          <PrimaryButton text="开始形象分析" onPress={() => router.push('/capture')} />
        </View>
      ) : null}
      {hasReport && today ? (
        <Pressable style={styles.today} onPress={() => router.push('/today')}>
          <View style={styles.todayCopy}>
            <Text style={styles.todayLabel}>今日造型</Text>
            <Text style={styles.todayTitle}>{today.title}</Text>
            <Text style={styles.todaySummary}>{today.summary}</Text>
          </View>
          <LookImage
            style={styles.todayImg}
            src={today.generated_image_url || today.image_url}
            badgeText={(today.look_provider ?? '').startsWith('demo') ? '效果示例' : 'AI 预览'}
          />
        </Pressable>
      ) : null}
      <Text style={styles.section}>按场合开始</Text>
      <View style={styles.scenes}>
        {SCENES.map((scene) => (
          <Pressable key={scene.id} style={styles.scene} onPress={() => router.push(`/scene?scene=${scene.id}`)}>
            <Text style={styles.sceneLabel}>{scene.label}</Text>
            <Text style={styles.sceneNote}>{scene.note}</Text>
          </Pressable>
        ))}
      </View>
    </Screen>
  )
}

const styles = StyleSheet.create({
  hi: { fontSize: 15, color: colors.ink2 },
  title: { fontSize: 26, fontWeight: '700', color: colors.ink },
  card: { gap: space.md, padding: space.xl, borderRadius: radius.lg, backgroundColor: colors.surfaceStrong, borderWidth: 1, borderColor: colors.line },
  cardTitle: { fontSize: 20, fontWeight: '700', color: colors.ink },
  cardDesc: { fontSize: 14, lineHeight: 22, color: colors.ink2 },
  today: { flexDirection: 'row', gap: space.md, padding: space.lg, borderRadius: radius.lg, backgroundColor: colors.surfaceStrong, borderWidth: 1, borderColor: colors.line },
  todayCopy: { flex: 1, gap: 6 },
  todayLabel: { fontSize: 11, fontWeight: '600', color: colors.moss, letterSpacing: 2 },
  todayTitle: { fontSize: 16, fontWeight: '600', color: colors.ink },
  todaySummary: { fontSize: 13, color: colors.ink2 },
  todayImg: { width: 88, height: 110 },
  section: { fontSize: 16, fontWeight: '600', color: colors.ink },
  scenes: { flexDirection: 'row', flexWrap: 'wrap', gap: space.sm },
  scene: { width: '47%', padding: space.md, borderRadius: radius.md, backgroundColor: colors.surfaceStrong, borderWidth: 1, borderColor: colors.line, gap: 4 },
  sceneLabel: { fontSize: 16, fontWeight: '600', color: colors.ink },
  sceneNote: { fontSize: 12, color: colors.ink3 },
})
