// 我的（Tab）：账户与身份、档案摘要（me/profile 持久化展示）、
// 任务中心（进行中任务的聚合列表，无任务不占位）、
// 报告/方案入口、体验实验室、删除我的数据。
import { useCallback, useRef, useState } from 'react'
import Taro, { useDidShow } from '@tarojs/taro'
import { Text, View } from '@tarojs/components'
import { DEFAULT_NICKNAME, type Account, type Task, type UserProfile } from '@zsm/core'
import { usePageClass } from '../../hooks/use-page-visibility'
import { api } from '../../services/api'
import { groupTasksByType, openTask, taskTitle } from '../../services/task-utils'
import { clearAllLocalState, STORAGE_KEYS, readStorage } from '../../services/storage'
import { globalData } from '../../app'
import AppHeader from '../../components/app-header'
import Skeleton from '../../components/skeleton'
import './index.scss'

export default function Profile() {
  const [account, setAccount] = useState<Account | null>(null)
  const [profile, setProfile] = useState<UserProfile | null>(null)
  const [tasks, setTasks] = useState<Task[]>([])
  const [loading, setLoading] = useState(true)
  const hasCacheRef = useRef(false)
  const pageClass = usePageClass(!loading, 'page--tab', 'profile')

  const load = useCallback(async () => {
    if (!hasCacheRef.current) setLoading(true)
    try {
      const [me, myProfile, bootstrap] = await Promise.all([
        api.getMe().catch(() => null),
        api.getMyProfile().catch(() => null),
        api.getHomeBootstrap().catch(() => null),
      ])
      setAccount(me)
      setProfile(myProfile)
      setTasks(bootstrap?.active_tasks ?? [])
      hasCacheRef.current = true
    } finally {
      setLoading(false)
    }
  }, [])

  useDidShow(() => {
    load()
  })

  const hasReport = Boolean(readStorage(STORAGE_KEYS.reportId))

  // 任务中心：按类型聚合同类任务（一次方案生成 = 3 个 plan_look），分组逻辑与 task-utils 单源
  const activeTasks = tasks.filter(
    (t) => t.status === 'queued' || t.status === 'processing' || t.status === 'failed',
  )
  const taskGroups = groupTasksByType(activeTasks)

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
    <View className={pageClass}>
      <AppHeader />
      <View className="me">
        {loading ? (
          <Skeleton rows={4} />
        ) : (
          <>
            <View className="me__card fade-up">
              <View className="me__avatar">{(account?.nickname ?? 'U').slice(0, 1)}</View>
              <View className="me__meta">
                <Text className="me__nickname">{account?.nickname ?? DEFAULT_NICKNAME}</Text>
                <Text className="me__identities">
                  已绑定：{(account?.identities ?? []).map((i) => (i.provider === 'wechat_miniapp' ? '微信' : i.provider)).join('、') || '微信'}
                </Text>
              </View>
            </View>

            {activeTasks.length > 0 ? (
              <View className="me__card fade-up delay-1">
                <Text className="me__section">进行中的任务</Text>
                {taskGroups.map((group) => {
                  const first = group[0]
                  if (!first) return null
                  const activeCount = group.filter(
                    (t) => t.status === 'queued' || t.status === 'processing',
                  ).length
                  const failedCount = group.length - activeCount
                  const failed = activeCount === 0
                  return (
                    <View
                      key={first.type}
                      className="me__row pressable"
                      onClick={() => openTask(first)}
                    >
                      <Text className="me__row-label">{taskTitle(first)}</Text>
                      <Text className={`me__row-value ${failed ? 'me__row-value--warn' : 'me__row-value--moss'}`}>
                        {failed
                          ? `${failedCount} 个未完成`
                          : group.length === 1
                            ? `${Math.min(100, Math.max(0, first.progress ?? 0))}%`
                            : `${activeCount} 个生成中`}
                      </Text>
                    </View>
                  )
                })}
              </View>
            ) : null}

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
