import { useState } from 'react'
import { StyleSheet, Text, View } from 'react-native'
import { useLocalSearchParams, useRouter } from 'expo-router'
import * as ImagePicker from 'expo-image-picker'
import * as Haptics from 'expo-haptics'
import { FEEDBACK_WORDS } from '@zsm/core'
import { api } from '../src/api'
import { Pill, PrimaryButton, Screen } from '../src/ui/Screen'
import { colors, space } from '../src/ui/theme'

export default function Feedback() {
  const router = useRouter()
  const { plan_id } = useLocalSearchParams<{ plan_id?: string }>()
  const [photoPath, setPhotoPath] = useState('')
  const [selected, setSelected] = useState<string[]>([])
  const [busy, setBusy] = useState(false)
  const [doneMessage, setDoneMessage] = useState('')

  const pick = async () => {
    const result = await ImagePicker.launchImageLibraryAsync({ mediaTypes: ['images'], quality: 0.85 })
    if (!result.canceled && result.assets[0]) setPhotoPath(result.assets[0].uri)
  }

  const submit = async () => {
    if (!plan_id) return
    if (!photoPath) return
    setBusy(true)
    try {
      const asset = await api.uploadMedia({ kind: 'feedback', filePath: photoPath })
      const ack = await api.sendPlanFeedback(plan_id, { tags: selected, comment: '', media_id: asset.id })
      setDoneMessage(ack.message || '我们记住了什么对你有效')
      void Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success)
    } finally {
      setBusy(false)
    }
  }

  if (doneMessage) {
    return (
      <Screen title="实际反馈" onBack={() => router.back()}>
        <Text style={styles.done}>{doneMessage}</Text>
        <PrimaryButton text="回到首页" onPress={() => router.replace('/(tabs)')} />
      </Screen>
    )
  }

  return (
    <Screen title="实际反馈" onBack={() => router.back()}>
      <Text style={styles.lede}>上传今天的实拍，帮助下次更贴你。</Text>
      <Text style={styles.photo} onPress={() => void pick()}>
        {photoPath ? '已选择实拍，轻触重选' : '上传今天的实拍'}
      </Text>
      <View style={styles.chips}>
        {FEEDBACK_WORDS.map((word) => (
          <Pill
            key={word}
            label={word}
            active={selected.includes(word)}
            onPress={() => setSelected((prev) => (prev.includes(word) ? prev.filter((w) => w !== word) : [...prev, word]))}
          />
        ))}
      </View>
      <PrimaryButton text="提交反馈" loading={busy} disabled={!photoPath} onPress={() => void submit()} />
    </Screen>
  )
}

const styles = StyleSheet.create({
  lede: { fontSize: 15, color: colors.ink2, lineHeight: 22 },
  photo: { color: colors.moss, textDecorationLine: 'underline', fontSize: 15 },
  chips: { flexDirection: 'row', flexWrap: 'wrap', gap: 8 },
  done: { fontSize: 22, fontWeight: '700', color: colors.ink, lineHeight: 32 },
})
