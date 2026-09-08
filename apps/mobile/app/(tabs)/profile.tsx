import { useCallback, useEffect, useState } from 'react'
import { Alert, Pressable, StyleSheet, Text, View } from 'react-native'
import { useRouter } from 'expo-router'
import type { Account, UserProfile } from '@zsm/core'
import { api } from '../../src/api'
import { STORAGE_KEYS, clearAllLocalState, readStorage, writeStorage } from '../../src/storage'
import { PrimaryButton, Screen } from '../../src/ui/Screen'
import { colors, radius, space } from '../../src/ui/theme'

export default function Profile() {
  const router = useRouter()
  const [account, setAccount] = useState<Account | null>(null)
  const [profile, setProfile] = useState<UserProfile | null>(null)

  const load = useCallback(async () => {
    const [me, myProfile] = await Promise.all([api.getMe().catch(() => null), api.getMyProfile().catch(() => null)])
    setAccount(me)
    setProfile(myProfile)
  }, [])

  useEffect(() => {
    load()
  }, [load])

  const hasReport = Boolean(readStorage(STORAGE_KEYS.reportId))

  const deleteData = () => {
    Alert.alert('删除我的数据', '将删除你的全部照片、分析、方案、衣橱与分享记录，且无法恢复。', [
      { text: '取消', style: 'cancel' },
      {
        text: '删除',
        style: 'destructive',
        onPress: () => {
          api
            .deleteMyData()
            .then(() => {
              clearAllLocalState()
              load()
            })
            .catch(() => Alert.alert('删除没有成功，请重试'))
        },
      },
    ])
  }

  const logout = async () => {
    await api.logout().catch(() => undefined)
    writeStorage(STORAGE_KEYS.token, '')
    router.replace('/login')
  }

  return (
    <Screen title="我的">
      <View style={styles.card}>
        <View style={styles.avatar}>
          <Text style={styles.avatarText}>{(account?.nickname ?? '扮').slice(0, 1)}</Text>
        </View>
        <View>
          <Text style={styles.name}>{account?.nickname ?? '怎么打扮用户'}</Text>
          <Text style={styles.meta}>
            已绑定：{(account?.identities ?? []).map((i) => i.provider).join('、') || '手机号'}
          </Text>
        </View>
      </View>
      {profile ? (
        <View style={styles.card}>
          <Text style={styles.section}>形象条件</Text>
          <Text style={styles.meta}>{profile.height_cm} cm · {profile.role} · {profile.budget}</Text>
        </View>
      ) : null}
      <Pressable style={styles.row} onPress={() => router.push(hasReport ? '/report' : '/capture')}>
        <Text style={styles.rowText}>{hasReport ? '查看形象报告' : '建立形象档案'}</Text>
      </Pressable>
      <Pressable style={styles.row} onPress={() => router.push('/today')}>
        <Text style={styles.rowText}>今日造型（二期）</Text>
      </Pressable>
      <PrimaryButton text="删除我的数据" onPress={deleteData} />
      <Text style={styles.logout} onPress={logout}>退出登录</Text>
    </Screen>
  )
}

const styles = StyleSheet.create({
  card: { flexDirection: 'row', gap: space.md, padding: space.lg, borderRadius: radius.lg, backgroundColor: colors.surfaceStrong, borderWidth: 1, borderColor: colors.line, alignItems: 'center' },
  avatar: { width: 52, height: 52, borderRadius: 26, backgroundColor: colors.mossSoft, alignItems: 'center', justifyContent: 'center' },
  avatarText: { fontSize: 20, color: colors.moss, fontWeight: '700' },
  name: { fontSize: 18, fontWeight: '700', color: colors.ink },
  meta: { fontSize: 13, color: colors.ink2, marginTop: 4 },
  section: { fontSize: 14, fontWeight: '600', color: colors.ink },
  row: { paddingVertical: 16, borderBottomWidth: 1, borderBottomColor: colors.line },
  rowText: { fontSize: 16, color: colors.ink },
  logout: { textAlign: 'center', color: colors.danger, fontSize: 14 },
})
