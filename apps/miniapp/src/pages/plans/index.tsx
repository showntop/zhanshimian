// 方案 Tab：场景分组 + swiper 三方案 + 原本/方案对比 + 本人图生成轮询 +
// 单方案重试（regenerate，旧版只能整组重排的修复）。
import { useCallback, useEffect, useRef, useState } from 'react'
import Taro, { useDidShow } from '@tarojs/taro'
import { ScrollView, Swiper, SwiperItem, Text, View } from '@tarojs/components'
import { POLL_INTERVALS, useTaskPolling, type Plan } from '@zsm/core'
import { api } from '../../services/api'
import { STORAGE_KEYS, readStorage, writeStorage } from '../../services/storage'
import AppHeader from '../../components/app-header'
import PrimaryButton from '../../components/primary-button'
import ExampleImage from '../../components/example-image'
import CompareToggle from '../../components/compare-toggle'
import Skeleton from '../../components/skeleton'
import ErrorState from '../../components/error-state'
import EmptyState from '../../components/empty-state'
import './index.scss'

const SCENE_TABS = [
  { key: 'general', label: '形象方案' },
  { key: 'interview', label: '面试' },
  { key: 'wedding', label: '婚礼' },
  { key: 'date', label: '约会' },
  { key: 'daily', label: '日常' },
] as const

export default function Plans() {
  const [scene, setScene] = useState<string>('general')
  const [plans, setPlans] = useState<Plan[]>([])
  const [currentImage, setCurrentImage] = useState('')
  const [index, setIndex] = useState(0)
  const [mode, setMode] = useState<'current' | 'plan'>('plan')
  const [loading, setLoading] = useState(true)
  const [failed, setFailed] = useState(false)
  const skipFirstShow = useRef(true)
  const activeLook = plans.find(
    (p, i) =>
      i === index &&
      p.look_task &&
      (p.look_task.status === 'queued' || p.look_task.status === 'processing'),
  )

  const load = useCallback(async (targetScene: string) => {
    setLoading(true)
    setFailed(false)
    try {
      const reportId = readStorage(STORAGE_KEYS.reportId)
      if (!reportId) {
        const current = await api.getCurrentReport()
        if (!current) {
          setPlans([])
          return
        }
        writeStorage(STORAGE_KEYS.reportId, current.id)
      }
      const items = await api.listPlans(readStorage(STORAGE_KEYS.reportId)!, targetScene)
      setPlans(items)
    } catch {
      setFailed(true)
    } finally {
      setLoading(false)
    }
  }, [])

  // 对比底图：报告当前形象（body 优先）
  const loadCurrent = useCallback(async () => {
    try {
      const reportId = readStorage(STORAGE_KEYS.reportId)
      if (!reportId) return
      const analysis = await api
        .getReport(reportId)
        .then((r) => api.getAnalysis(r.analysis_id))
        .catch(() => null)
      const body = analysis?.media?.find((m) => m.kind === 'body') ?? analysis?.media?.find((m) => m.kind === 'face')
      if (body) setCurrentImage(body.url)
    } catch {
      /* 对比图缺失时切换会 toast */
    }
  }, [])

  useEffect(() => {
    load(scene)
    loadCurrent()
  }, [scene, load, loadCurrent])

  useDidShow(() => {
    if (skipFirstShow.current) {
      skipFirstShow.current = false
      return
    }
    load(scene)
  })

  // 存在进行中的 look 任务 → 轮询方案列表自身（间隔单源 @zsm/core）
  const hasActiveLook = plans.some(
    (p) => p.look_task && (p.look_task.status === 'queued' || p.look_task.status === 'processing'),
  )
  const { stop } = useTaskPolling({
    fetcher: () => api.listPlans(readStorage(STORAGE_KEYS.reportId)!, scene),
    intervalMs: POLL_INTERVALS.planLook,
    enabled: hasActiveLook && plans.length > 0,
    onDone: () => {
      load(scene)
    },
  })
  Taro.useDidHide(() => stop())

  const plan = plans[index]
  const planImage = plan?.generated_image_url || plan?.image_url
  const isDemoLook = (plan?.look_provider ?? '').startsWith('demo')

  const switchScene = (key: string) => {
    setScene(key)
    setIndex(0)
    setMode('plan')
  }

  const toggleCompare = (next: 'current' | 'plan') => {
    if (next === 'current' && !currentImage) {
      Taro.showToast({ title: '当前形象照暂不可用', icon: 'none' })
      return
    }
    setMode(next)
  }

  const retryOne = async () => {
    if (!plan) return
    try {
      await api.regeneratePlanLook(plan.id)
      Taro.vibrateShort({ type: 'light' })
      load(scene)
    } catch (e) {
      Taro.showToast({ title: (e as Error).message || '重试没有成功', icon: 'none' })
    }
  }

  if (loading && plans.length === 0) {
    return (
      <View className="page page--tab">
        <AppHeader />
        <Skeleton rows={5} />
      </View>
    )
  }

  if (failed) {
    return (
      <View className="page page--tab">
        <AppHeader />
        <ErrorState onRetry={() => load(scene)} />
      </View>
    )
  }

  if (plans.length === 0) {
    return (
      <View className="page page--tab">
        <AppHeader />
        <EmptyState
          title="还没有方案"
          description="先完成三图建档，或在场景页生成场合方案。"
          actionText="去建档"
          onAction={() => Taro.navigateTo({ url: '/pages/capture/index' })}
        />
      </View>
    )
  }

  return (
    <View className="page page--tab">
      <AppHeader />
      <View className="plans">
        <ScrollView className="plans__tabs" scrollX enhanced showScrollbar={false}>
          {SCENE_TABS.map((tab) => (
            <Text
              key={tab.key}
              className={`plans__tab ${scene === tab.key ? 'plans__tab--active' : ''}`}
              onClick={() => switchScene(tab.key)}
            >
              {tab.label}
            </Text>
          ))}
        </ScrollView>

        <View className="plans__hero fade-up">
          <View className="plans__hero-frame">
            {mode === 'plan' && planImage ? (
              <Swiper
                className="plans__swiper"
                current={index}
                onChange={(e) => {
                  setIndex(e.detail.current)
                  Taro.vibrateShort({ type: 'light' })
                }}
              >
                {plans.map((item) => (
                  <SwiperItem key={item.id}>
                    <ExampleImage
                      className="plans__hero-img"
                      src={item.generated_image_url || item.image_url}
                      badgeText={(item.look_provider ?? '').startsWith('demo') ? '效果示例' : 'AI 风格预览'}
                      mode="aspectFill"
                    />
                  </SwiperItem>
                ))}
              </Swiper>
            ) : (
              <ExampleImage className="plans__hero-img" src={currentImage} user mode="aspectFill" />
            )}
            <View className="plans__hero-toggle">
              <CompareToggle value={mode} onChange={toggleCompare} />
            </View>
          </View>
        </View>

        {plan ? (
          <View className="plans__info fade-up delay-1">
            <Text className="plans__name">{plan.name}</Text>
            <Text className="plans__summary">{plan.descriptor}</Text>
            <Text className="plans__why">{plan.why}</Text>
            {activeLook ? (
              <View className="plans__generating" onClick={retryOne}>
                <View className="plans__generating-spin spinner" />
                <Text className="plans__generating-text">{activeLook.look_task?.stage || '正在生成形象图'} · 点击查看</Text>
              </View>
            ) : null}
            {plan.look_task?.status === 'failed' ? (
              <View className="plans__retry" onClick={retryOne}>
                <Text className="plans__retry-text">形象图未生成，点此重试这一套</Text>
              </View>
            ) : null}
          </View>
        ) : null}

        <ScrollView className="plans__rail fade-up delay-2" scrollX enhanced showScrollbar={false}>
          {plans.map((item, i) => (
            <View
              key={item.id}
              className={`plans__thumb ${i === index ? 'plans__thumb--active' : ''} pressable`}
              onClick={() => setIndex(i)}
            >
              <ExampleImage
                className="plans__thumb-img"
                src={item.generated_image_url || item.image_url}
                badgeText={(item.look_provider ?? '').startsWith('demo') ? '效果示例' : 'AI 风格预览'}
              />
              <Text className="plans__thumb-name">{item.name}</Text>
            </View>
          ))}
        </ScrollView>

        <View className="plans__foot fade-up delay-3">
          {plan ? (
            <>
              <PrimaryButton
                text="选这套"
                onClick={() => Taro.navigateTo({ url: `/pages/plan/index?id=${plan.id}` })}
              />
              <Text className="plans__foot-note">
                {isDemoLook ? '当前为效果示例，接入真实图像模型后展示本人效果' : '形象图由 AI 基于你的照片生成'}
              </Text>
            </>
          ) : null}
        </View>
      </View>
    </View>
  )
}
