// 我的（Tab）：账户与身份、档案摘要（me/profile 持久化展示）、
// 报告/方案入口、体验实验室、删除我的数据。
import { useCallback, useState } from 'react'
import Taro, { useDidShow } from '@tarojs/taro'
import { Text, View } from '@tarojs/components'
import type { Account, UserProfile } from '@zsm/core'
import { api } from '../../services/api'
import { clearAllLocalState, STORAGE_KEYS, readStorage } from '../../services/storage'
import { globalData } from '../../app'
import AppHeader from '../../components/app-header'
import Skeleton from '../../components/skeleton'
import './index.scss'

export default function Profile() {
  const [account, setAccount] = useState<Account | null>(null)
  const [profile, setProfile] = useState<UserProfile | null>(null)
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [me, myProfile] = await Promise.all([
        api.getMe().catch(() => null),
        api.getMyProfile().catch(() => null),
      ])
      setAccount(me)
      setProfile(myProfile)
    } finally {
      setLoading(false)
    }
  }, [])

  useDidShow(() => {
    load()
  })

  const hasReport = Boolean(readStorage(STORAGE_KEYS.reportId))

  const deleteData = () => {
    Taro.showModal({
      title: '删除我的数据',
      content: '将删除你的全部照片、分析、方案、衣橱与分享记录，且无法恢复。确定继续吗？',
      confirmText: '删除',
      confirmColor: '#9B4B45',
      success: (res) => {
        if (!res.confirm) return
        api
          .deleteMyData()
          .then(() => {
            clearAllLocalState()
            globalData.reportId = ''
            globalData.planId = ''
            Taro.showToast({ title: '已删除', icon: 'success' })
            load()
          })
          .catch(() => Taro.showToast({ title: '删除没有成功，请重试', icon: 'none' }))
      },
    })
  }

  return (
    <View className="page page--tab">
      <AppHeader />
      <View className="me">
        {loading ? (
          <Skeleton rows={4} />
        ) : (
          <>
            <View className="me__card fade-up">
              <View className="me__avatar">{(account?.nickname ?? 'U').slice(0, 1)}</View>
              <View className="me__meta">
                <Text className="me__nickname">{account?.nickname ?? 'UP一下用户'}</Text>
                <Text className="me__identities">
                  已绑定：{(account?.identities ?? []).map((i) => (i.provider === 'wechat_miniapp' ? '微信' : i.provider)).join('、') || '微信'}
                </Text>
              </View>
            </View>

            <View className="me__card fade-up delay-1">
              <Text className="me__section">形象档案</Text>
              <View className="me__row pressable" onClick={() => Taro.navigateTo({ url: '/pages/report/index' })}>
                <Text className="me__row-label">最近的分析报告</Text>
                <Text className="me__row-value">{hasReport ? '查看' : '未建档'}</Text>
              </View>
              <View className="me__row pressable" onClick={() => Taro.switchTab({ url: '/pages/plans/index' })}>
                <Text className="me__row-label">我的方案</Text>
                <Text className="me__row-value">查看</Text>
              </View>
              <View className="me__row" onClick={() => Taro.navigateTo({ url: '/pages/capture/index?replace=1' })}>
                <Text className="me__row-label">更新形象档案</Text>
                <Text className="me__row-value">重拍三张</Text>
              </View>
            </View>

            <View className="me__card fade-up delay-2">
              <Text className="me__section">身体数据</Text>
              <View className="me__row">
                <Text className="me__row-label">身高</Text>
                <Text className="me__row-value">{profile?.height_cm ? `${profile.height_cm} cm` : '未填写'}</Text>
              </View>
              <View className="me__row">
                <Text className="me__row-label">职业 / 预算</Text>
                <Text className="me__row-value">
                  {profile?.role && profile?.budget ? `${profile.role} · ${profile.budget}` : '未填写'}
                </Text>
              </View>
              <Text className="me__note">体重与三围为选填，随时可在建档时补充</Text>
            </View>

            <View className="me__card fade-up delay-2">
              <Text className="me__section">更多</Text>
              <View className="me__row pressable" onClick={() => Taro.navigateTo({ url: '/packages/tools/pages/lab/index' })}>
                <Text className="me__row-label">体验实验室</Text>
                <Text className="me__row-value">AR / 3D / 试衣</Text>
              </View>
              <View className="me__row pressable" onClick={() => Taro.navigateTo({ url: '/packages/life/pages/wardrobe/index' })}>
                <Text className="me__row-label">我的衣橱</Text>
                <Text className="me__row-value">轻量版</Text>
              </View>
              <View className="me__row pressable" onClick={deleteData}>
                <Text className="me__row-label me__row-label--danger">删除我的数据</Text>
                <Text className="me__row-value">全部删除</Text>
              </View>
            </View>

            <Text className="me__privacy fade-up delay-3">照片与建议只对你可见</Text>
          </>
        )}
      </View>
    </View>
  )
}
