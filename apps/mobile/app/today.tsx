import { StyleSheet, Text } from 'react-native'
import { useRouter } from 'expo-router'
import { PrimaryButton, Screen } from '../src/ui/Screen'
import { colors } from '../src/ui/theme'

export default function TodayPlaceholder() {
  const router = useRouter()
  return (
    <Screen title="今日造型" onBack={() => router.back()}>
      <Text style={styles.title}>今日造型即将上线</Text>
      <Text style={styles.desc}>手机端本期先打通登录与主闭环。天气上下文、换一套、加入清单会在二期对齐小程序。</Text>
      <PrimaryButton text="回到首页" onPress={() => router.replace('/(tabs)')} />
    </Screen>
  )
}

const styles = StyleSheet.create({
  title: { fontSize: 24, fontWeight: '700', color: colors.ink },
  desc: { fontSize: 15, lineHeight: 24, color: colors.ink2 },
})
