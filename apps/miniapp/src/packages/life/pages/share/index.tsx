// 分享卡：创建（plan/today）→ 公开 token 分享 → 接收方查看 → 撤销即失效。
// 图片来自服务端带类型的 DisplayMedia；接收视图是 ShareView（无 id、不可撤销）。
import { useCallback, useEffect, useState } from 'react'
import Taro, { useLoad, useShareAppMessage } from '@tarojs/taro'
import { Button, Text, View } from '@tarojs/components'
import { APP_NAME, trackEvent, type Share, type ShareView } from '@zsm/core'
import { usePageShell } from '../../../../hooks/use-page-visibility'
import { peripherals } from '../../../../app/api/peripherals'
import AppHeader from '../../../../components/app-header'
import PrimaryButton from '../../../../components/primary-button'
import SourceImage from '../../../../components/source-image'
import Skeleton from '../../../../components/skeleton'
import ErrorState from '../../../../components/error-state'
import './index.scss'

interface Snapshot {
  title?: string
  summary?: string
  label?: string
}

export default function SharePage() {
  const [share, setShare] = useState<Share | ShareView | null>(null)
  const [isOwner, setIsOwner] = useState(false)
  const [loading, setLoading] = useState(true)
  const [failed, setFailed] = useState(false)
  const { pageClass, enter } = usePageShell(!loading || Boolean(share), '', 'share')

  const loadByToken = useCallback(async (token: string) => {
    setLoading(true)
    setFailed(false)
    try {
      setShare(await peripherals.getShareByToken(token))
    } catch {
      setFailed(true)
    } finally {
      setLoading(false)
    }
  }, [])

  const createForSource = useCallback(
    async (sourceType: 'plan_variant' | 'today_plan', sourceId: string) => {
      setLoading(true)
      setFailed(false)
      try {
        const card = await peripherals.createShare({ source_type: sourceType, source_id: sourceId, include_photo: false })
        setShare(card)
        setIsOwner(true)
        trackEvent('share_card_create', { source_type: sourceType })
      } catch (e) {
        Taro.showToast({ title: (e as Error).message || '创建没有成功', icon: 'none' })
        setFailed(true)
      } finally {
        setLoading(false)
      }
    },
    [],
  )

  useLoad((options) => {
    if (options?.token) {
      loadByToken(options.token)
      return
    }
    if (options?.type === 'today') {
      peripherals
        .getCurrentTodayPlan()
        .then((plan) => (plan ? createForSource('today_plan', plan.id) : setFailed(true)))
        .catch(() => setFailed(true))
      return
    }
    if (options?.plan_variant_id) {
      createForSource('plan_variant', options.plan_variant_id)
      return
    }
    // 没有可分享的来源：分享只能从方案详情或今日方案发起
    setFailed(true)
  })

  useShareAppMessage(() => ({
    title: snapshotTitle(share) || '我的形象方案',
    path: `/packages/life/pages/share/index?token=${share && 'token' in share ? share.token : ''}`,
  }))

  const revoke = async () => {
    if (!share || !('id' in share) || !share.id) return
    try {
      await peripherals.revokeShare(share.id)
      Taro.showToast({ title: '已撤销，链接即刻失效', icon: 'success' })
      setTimeout(() => Taro.navigateBack(), 800)
    } catch (e) {
      Taro.showToast({ title: (e as Error).message || '撤销没有成功', icon: 'none' })
    }
  }

  const snapshotTitle = (card: Share | ShareView | null): string => {
    const snapshot = (card?.snapshot ?? {}) as Snapshot
    return snapshot.title ?? ''
  }

  if (loading) {
    return (
      <View className={pageClass}>
        <AppHeader title="分享卡" back />
        <Skeleton rows={4} />
      </View>
    )
  }

  if (failed || !share) {
    return (
      <View className={pageClass}>
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

  return (
    <View className={pageClass}>
      <AppHeader title="分享卡" back />
      <View className="sh">
        <View className={`sh__card ${enter()}`}>
          <Text className="sh__brand">{APP_NAME}</Text>
          {snapshot.label ? <Text className="sh__label">{snapshot.label}</Text> : null}
          <Text className="sh__title">{snapshot.title || '我的形象方案'}</Text>
          {snapshot.summary ? <Text className="sh__summary">{snapshot.summary}</Text> : null}
          {share.media ? (
            <SourceImage className="sh__image" media={share.media} anchor="top" />
          ) : null}
          <View className="sh__foot">
            <Text className="sh__foot-note">由 AI 形象顾问生成 · 效果仅供参考</Text>
          </View>
        </View>

        <View className={`sh__actions ${enter(1)}`}>
          {isOwner ? (
            <>
              <Button className="sh__share-btn" openType="share">
                <Text className="sh__share-text">发给朋友</Text>
              </Button>
              <Text className="sh__revoke pressable" onClick={() => void revoke()}>撤销分享（链接即刻失效）</Text>
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
