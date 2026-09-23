import { useState } from 'react'
import { Pressable, StyleSheet, Text, View } from 'react-native'
import { useLocalSearchParams, useRouter } from 'expo-router'
import * as ImagePicker from 'expo-image-picker'
import type { MediaAsset, MediaKind } from '@zsm/core'
import { api } from '../src/api'
import { EmptyBlock, PrimaryButton, Screen } from '../src/ui/Screen'
import { colors, radius, space } from '../src/ui/theme'

const SHOTS: { kind: MediaKind; label: string; desc: string }[] = [
  { kind: 'face', label: '正脸', desc: '看清五官与肤色' },
  { kind: 'side', label: '45° 侧脸', desc: '判断轮廓与发型' },
  { kind: 'body', label: '正面全身', desc: '分析头肩与身材比例' },
]

export default function Capture() {
  const router = useRouter()
  const { scene } = useLocalSearchParams<{ scene?: string }>()
  const [assets, setAssets] = useState<Partial<Record<MediaKind, MediaAsset>>>({})
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const pick = async (kind: MediaKind) => {
    const result = await ImagePicker.launchImageLibraryAsync({ mediaTypes: ['images'], quality: 0.85 })
    if (result.canceled || !result.assets[0]) return
    setBusy(true)
    setError('')
    try {
      const asset = await api.uploadMedia({ kind, filePath: result.assets[0].uri })
      setAssets((prev) => ({ ...prev, [kind]: asset }))
    } catch (e) {
      setError((e as Error).message || '上传没有成功，请重试')
    } finally {
      setBusy(false)
    }
  }

  const next = () => {
    const ids = SHOTS.map((s) => assets[s.kind]?.id).filter(Boolean)
    if (ids.length < 3) {
      setError('三张照片都需要上传')
      return
    }
    router.push(`/profile-setup?scene=${scene || 'interview'}&media_ids=${ids.join(',')}`)
  }

  return (
    <Screen title="拍摄建档" onBack={() => router.back()}>
      <Text style={styles.lede}>正脸、侧脸、全身各一张。光线均匀即可。</Text>
      {SHOTS.map((shot) => (
        <Pressable key={shot.kind} style={styles.slot} onPress={() => pick(shot.kind)}>
          <Text style={styles.slotLabel}>{shot.label}</Text>
          <Text style={styles.slotDesc}>{assets[shot.kind] ? '已上传，轻触重拍' : shot.desc}</Text>
        </Pressable>
      ))}
      {error ? <EmptyBlock title={error} actionText="知道了" onAction={() => setError('')} /> : null}
      <PrimaryButton text="下一步" loading={busy} disabled={SHOTS.some((s) => !assets[s.kind])} onPress={next} />
    </Screen>
  )
}

const styles = StyleSheet.create({
  lede: { fontSize: 15, color: colors.ink2, lineHeight: 22 },
  slot: { padding: space.lg, borderRadius: radius.md, backgroundColor: colors.surfaceStrong, borderWidth: 1, borderColor: colors.line, gap: 4 },
  slotLabel: { fontSize: 17, fontWeight: '600', color: colors.ink },
  slotDesc: { fontSize: 13, color: colors.ink3 },
})
