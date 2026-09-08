import { StyleSheet, Text } from 'react-native'
import { useLocalSearchParams, useRouter } from 'expo-router'
import { SCENES } from '@zsm/core'
import { PrimaryButton, Screen } from '../src/ui/Screen'
import { colors } from '../src/ui/theme'

export default function ScenePlaceholder() {
  const router = useRouter()
  const { scene } = useLocalSearchParams<{ scene?: string }>()
  const copy = SCENES.find((s) => s.id === scene)

  return (
    <Screen title={copy?.label || '场合'} onBack={() => router.back()}>
      <Text style={styles.title}>{copy?.label || '场合'}方案</Text>
      <Text style={styles.desc}>{copy?.note || '按场合给你一套可执行的建议。'}本期先走主闭环建档；复访快捷入口将在二期接上。</Text>
      <PrimaryButton text="先建立形象档案" onPress={() => router.push(`/capture?scene=${scene || 'daily'}`)} />
    </Screen>
  )
}

const styles = StyleSheet.create({
  title: { fontSize: 24, fontWeight: '700', color: colors.ink },
  desc: { fontSize: 15, lineHeight: 24, color: colors.ink2 },
})
