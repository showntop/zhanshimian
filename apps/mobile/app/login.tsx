import { useState } from 'react'
import { StyleSheet, Text, TextInput } from 'react-native'
import { useRouter } from 'expo-router'
import { DEFAULT_NICKNAME, HOME_TITLE, PRIVACY_NOTE } from '@zsm/core'
import { api } from '../src/api'
import { STORAGE_KEYS, writeStorage } from '../src/storage'
import { PrimaryButton, Screen } from '../src/ui/Screen'
import { colors, space } from '../src/ui/theme'

export default function Login() {
  const router = useRouter()
  const [phone, setPhone] = useState('')
  const [code, setCode] = useState('')
  const [sent, setSent] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const request = async () => {
    if (!/^1\d{10}$/.test(phone)) {
      setError('请填写 11 位手机号')
      return
    }
    setBusy(true)
    setError('')
    try {
      await api.requestSmsCode({ phone })
      setSent(true)
    } catch (e) {
      setError((e as Error).message || '验证码没有发出，请重试')
    } finally {
      setBusy(false)
    }
  }

  const verify = async () => {
    setBusy(true)
    setError('')
    try {
      const session = await api.verifySmsCode({ phone, code })
      writeStorage(STORAGE_KEYS.token, session.token)
      router.replace('/(tabs)')
    } catch (e) {
      setError((e as Error).message || '登录没有成功，请重试')
    } finally {
      setBusy(false)
    }
  }

  const devLogin = async () => {
    setBusy(true)
    setError('')
    try {
      const session = await api.loginDev({ nickname: DEFAULT_NICKNAME })
      writeStorage(STORAGE_KEYS.token, session.token)
      router.replace('/(tabs)')
    } catch (e) {
      setError((e as Error).message || '开发登录不可用')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Screen>
      <Text style={styles.hi}>{HOME_TITLE}</Text>
      <Text style={styles.lede}>用手机号登录，多端档案互通。</Text>
      <TextInput
        style={styles.input}
        placeholder="手机号"
        keyboardType="phone-pad"
        value={phone}
        onChangeText={setPhone}
        maxLength={11}
      />
      {sent ? (
        <TextInput
          style={styles.input}
          placeholder="验证码"
          keyboardType="number-pad"
          value={code}
          onChangeText={setCode}
          maxLength={6}
        />
      ) : null}
      {error ? <Text style={styles.error}>{error}</Text> : null}
      <PrimaryButton
        text={sent ? '登录' : '获取验证码'}
        loading={busy}
        onPress={sent ? verify : request}
      />
      <Text style={styles.dev} onPress={devLogin}>
        开发环境快速进入
      </Text>
      <Text style={styles.privacy}>{PRIVACY_NOTE}</Text>
    </Screen>
  )
}

const styles = StyleSheet.create({
  hi: { fontSize: 26, fontWeight: '700', color: colors.ink, marginTop: space.xxl },
  lede: { fontSize: 15, color: colors.ink2, lineHeight: 22 },
  input: {
    minHeight: 48,
    borderRadius: 10,
    borderWidth: 1,
    borderColor: colors.line,
    backgroundColor: colors.surfaceStrong,
    paddingHorizontal: space.lg,
    fontSize: 16,
    color: colors.ink,
  },
  error: { color: colors.danger, fontSize: 13 },
  dev: { textAlign: 'center', color: colors.moss, fontSize: 14, textDecorationLine: 'underline' },
  privacy: { textAlign: 'center', color: colors.ink3, fontSize: 12 },
})
