// 分享卡：创建（plan/today）→ 公开 token 分享 → 接收方查看 → 撤销即失效。
import { useCallback, useEffect, useState } from 'react'
import Taro, { useLoad, useShareAppMessage } from '@tarojs/taro'
import { Button, Text, View } from '@tarojs/components'
import { APP_NAME, lookImage, trackEvent, type Share } from '@zsm/core'
import { api } from '../../../../services/api'
import AppHeader from '../../../../components/app-header'
import PrimaryButton from '../../../../components/primary-button'
import ExampleImage from '../../../../components/example-image'
import Skeleton from '../../../../components/skeleton'
import ErrorState from '../../../../components/error-state'
import './index.scss'

interface Snapshot {
  title?: string
  summary?: string
  image_url?: string
  label?: string
}

export default function SharePage() {
  const [share, setShare] = useState<Share | null>(null)
  const [loading, setLoading] = useState(true)
  const [failed, setFailed] = useState(false)
  const [isOwner, setIsOwner] = useState(false)

  const loadByToken = useCallback(async (token: string) => {
    setLoading(true)
    setFailed(false)
    try {
      setShare(await api.getShareByToken(token))
    } catch {
      setFailed(true)
    } finally {
      setLoading(false)
    }
  }, [])

  const createForSource = useCallback(async (sourceType: 'plan' | 'today', sourceId: string) => {
    setLoading(true)
    setFailed(false)
    try {
      const card = await api.createShare({ source_type: sourceType, source_id: sourceId, include_photo: false })
      setShare(card)
      setIsOwner(true)
      trackEvent('share_card_create', { source_type: sourceType })
    } catch (e) {
      Taro.showToast({ title: (e as Error).message || '创建没有成功', icon: 'none' })
      setFailed(true)
    } finally {
      setLoading(false)
    }
  }, [])

  useLoad((options) => {
    if (options?.token) {
      loadByToken(options.token)
      return
    }
    if (options?.type === 'today') {
      api
        .getCurrentTodayPlan()
        .then((plan) => (plan ? createForSource('today', plan.id) : setFailed(true)))
        .catch(() => setFailed(true))
      return
    }
    // 默认：已选方案
    try {
      const planId = Taro.getStorageSync('zsm_saved_plan_id') || Taro.getStorageSync('zsm_plan_id')
      if (planId) {
        createForSource('plan', planId)
        return
      }
    } catch {
      /* fallthrough */
    }
    setFailed(true)
  })

  useShareAppMessage(() => ({
    title: snapshotTitle(share) || '我的形象方案',
    path: `/packages/life/pages/share/index?token=${share?.token ?? ''}`,
  }))

  const revoke = async () => {
    if (!share) return
    try {
      await api.revokeShare(share.id!)
      Taro.showToast({ title: '已撤销，链接即刻失效', icon: 'success' })
      setTimeout(() => Taro.navigateBack(), 800)
    } catch (e) {
      Taro.showToast({ title: (e as Error).message || '撤销没有成功', icon: 'none' })
    }
  }

  const snapshotTitle = (card: Share | null): string => {
    const snapshot = (card?.snapshot ?? {}) as Snapshot
    return snapshot.title ?? ''
  }

  if (loading) {
    return (
      <View className="page">
        <AppHeader title="分享卡" back />
        <Skeleton rows={4} />
      </View>
    )
  }

  if (failed || !share) {
    return (
      <View className="page">
        <AppHeader title="分享卡" back />
        <ErrorState
          message={failed ? undefined : '分享内容不存在或已被撤销'}
          retryText="返回"
          onRetry={() => Taro.navigateBack()}
        />
      </View>
    )
  }

  const snapshot = (share.snapshot ?? {}) as Snapshot
  const imageUrl = lookImage(snapshot.image_url)

  return (
    <View className="page">
      <AppHeader title="分享卡" back />
      <View className="sh">
        <View className="sh__card fade-up">
          <Text className="sh__brand">{APP_NAME}</Text>
          {snapshot.label ? <Text className="sh__label">{snapshot.label}</Text> : null}
          <Text className="sh__title">{snapshot.title || '我的形象方案'}</Text>
          {snapshot.summary ? <Text className="sh__summary">{snapshot.summary}</Text> : null}
          {lookImage(snapshot.image_url) ? (
            <ExampleImage className="sh__image" src={snapshot.image_url} badgeText="风格参考" />
          ) : null}
          <View className="sh__foot">
            <Text className="sh__foot-note">由 AI 形象顾问生成 · 效果仅供参考</Text>
          </View>
        </View>

        <View className="sh__actions fade-up delay-1">
          {isOwner ? (
            <>
              <Button className="sh__share-btn" openType="share">
                <Text className="sh__share-text">发给朋友</Text>
              </Button>
              <Text className="sh__revoke pressable" onClick={revoke}>撤销分享（链接即刻失效）</Text>
            </>
          ) : (
            <PrimaryButton
              text="我也要一份方案"
              onClick={() => Taro.switchTab({ url: '/pages/home/index' })}
            />
          )}
        </View>
      </View>
    </View>
  )
}
