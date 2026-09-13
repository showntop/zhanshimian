// 我的：全部内容来自服务端 HomeBootstrap——档案、权益、报告入口。
// 与旧页的差别：不再读任务接口与业务存储；「删除我的数据」仍走服务端，
// 清掉的本地内容只有 UI 偏好。
import { useCallback, useEffect, useRef, useState } from 'react'
import Taro, { useDidShow } from '@tarojs/taro'
import { Text, View } from '@tarojs/components'
import {
  BILLING_COPY,
  DEFAULT_NICKNAME,
  ERROR_COPY,
  HOME_COPY,
  PRIVACY_SECTION_TITLE,
  PROFILE_SETUP_COPY,
  REPORT_COPY,
} from '@zsm/core'
import type { HomeBootstrap } from '@zsm/core'
import { qualityApi } from '../../app/api/quality'
import { resourceCache, resourceKey } from '../../app/cache/resource-cache'
import { clearAllLocalState } from '../../services/storage'
import { usePageShell } from '../../hooks/use-page-visibility'
import AppHeader from '../../components/app-header'
import Skeleton from '../../components/skeleton'
import ErrorState from '../../components/error-state'
import './index.scss'

const HOME_CACHE_KEY = resourceKey('home', 'current')

export default function Profile() {
  const { pageClass, enter } = usePageShell(false, 'page--tab', 'profile')
  const [boot, setBoot] = useState<HomeBootstrap | null>(
    () => resourceCache.read<HomeBootstrap>(HOME_CACHE_KEY) ?? null,
  )
  const [loading, setLoading] = useState(!boot)
  const [failed, setFailed] = useState(false)
  const firstShow = useRef(true)

  const load = useCallback(async (background: boolean) => {
    if (!background) setLoading(true)
    setFailed(false)
    try {
      const next = await resourceCache.revalidate(HOME_CACHE_KEY, () => qualityApi.getHomeBootstrap())
      setBoot(next)
    } catch {
      setFailed(true)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load(false)
  }, [load])

  useDidShow(() => {
    if (firstShow.current) {
      firstShow.current = false
      return
    }
    void load(true)
  })

  const summary = boot?.profile_summary
  const billing = boot?.billing

  const deleteData = () => {
    Taro.showModal({
      title: ERROR_COPY.deleteConfirmTitle,
      content: ERROR_COPY.deleteConfirmBody,
      confirmColor: '#9B4B45',
      success: (res) => {
        if (!res.confirm) return
        void (async () => {
          try {
            await qualityApi.deleteMyData()
            clearAllLocalState()
            Taro.showToast({ title: PROFILE_SETUP_COPY.deleteDone, icon: 'success' })
            resourceCache.remove(HOME_CACHE_KEY)
            await load(false)
          } catch {
            Taro.showToast({ title: PROFILE_SETUP_COPY.deleteFailed, icon: 'none' })
          }
        })()
      },
    })
  }

  return (
    <View className={pageClass}>
      <AppHeader />
      <View className="me">
        {loading && !boot ? (
          <Skeleton rows={4} />
        ) : failed && !boot ? (
          <ErrorState onRetry={() => void load(false)} />
        ) : (
          <>
            <View className={`me__card ${enter()}`}>
              <Text className="me__nickname">{DEFAULT_NICKNAME}</Text>
              {summary ? (
                <View className="me__facts">
                  <Text className="me__fact">
                    {`${PROFILE_SETUP_COPY.height} ${summary.height_cm}`}
                  </Text>
                  <Text className="me__fact">{summary.role}</Text>
                  <Text className="me__fact">
                    {`${PROFILE_SETUP_COPY.budget} ${summary.budget}`}
                  </Text>
                  {summary.weight_kg ? (
                    <Text className="me__fact">
                      {`${PROFILE_SETUP_COPY.weight} ${summary.weight_kg}`}
                    </Text>
                  ) : null}
                </View>
              ) : (
                <Text
                  className="me__archive pressable"
                  onClick={() => void Taro.navigateTo({ url: '/pages/capture/index' })}
                >
                  {HOME_COPY.startArchive} ›
                </Text>
              )}
            </View>

            {billing ? (
              <View className={`me__card me__card--quiet ${enter(1)}`}>
                <Text className="me__section">{BILLING_COPY.section}</Text>
                <Text className="me__credits">
                  {`${BILLING_COPY.remaining} ${billing.credits} ${BILLING_COPY.packUnit}`}
                </Text>
                <Text className="me__hint">{BILLING_COPY.hint}</Text>
              </View>
            ) : null}

            <View className={`me__rows ${enter(2)}`}>
              {boot?.report ? (
                <View
                  className="me__row pressable"
                  onClick={() =>
                    void Taro.navigateTo({
                      url: `/pages/report/index?id=${encodeURIComponent(boot.report!.id)}`,
                    })
                  }
                >
                  <Text className="me__row-label">{REPORT_COPY.title}</Text>
                  <Text className="me__row-arrow">›</Text>
                </View>
              ) : null}
              <View
                className="me__row pressable"
                onClick={() => void Taro.switchTab({ url: '/pages/plans/index' })}
              >
                <Text className="me__row-label">{HOME_COPY.recentTitle}</Text>
                <Text className="me__row-arrow">›</Text>
              </View>
            </View>

            <View className={`me__privacy ${enter(3)}`}>
              <Text className="me__section">{PRIVACY_SECTION_TITLE}</Text>
              <View className="me__row pressable" onClick={deleteData}>
                <Text className="me__row-label me__row-label--danger">
                  {PROFILE_SETUP_COPY.deleteAction}
                </Text>
                <Text className="me__row-arrow">›</Text>
              </View>
            </View>
          </>
        )}
      </View>
    </View>
  )
}
