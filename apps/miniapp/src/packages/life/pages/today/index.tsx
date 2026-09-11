// 今日造型：天气上下文 + 当日方案（生成/换一个/加入清单/反馈）+ 生成图轮询。
import { useCallback, useEffect, useRef, useState } from 'react'
import Taro from '@tarojs/taro'
import { Image, Text, View } from '@tarojs/components'
import { POLL_INTERVALS, lookImage, shouldStopPolling, useTaskPolling, trackEvent, type TodayContext, type TodayPlan } from '@zsm/core'
import { usePageClass, useShowOnce } from '../../../../hooks/use-page-visibility'
import { api } from '../../../../services/api'
import { STORAGE_KEYS, readStorage, writeStorage } from '../../../../services/storage'
import AppHeader from '../../../../components/app-header'
import PrimaryButton from '../../../../components/primary-button'
import ExampleImage from '../../../../components/example-image'
import Skeleton from '../../../../components/skeleton'
import ErrorState from '../../../../components/error-state'
import './index.scss'

const FEEDBACKS = ['适合我', '太正式', '想更轻松', '今天穿了']

export default function Today() {
  const [plan, setPlan] = useState<TodayPlan | null>(null)
  const [context, setContext] = useState<TodayContext | null>(null)
  const [loading, setLoading] = useState(true)
  const [failed, setFailed] = useState(false)
  const [busy, setBusy] = useState(false)
  const [previewOpen, setPreviewOpen] = useState(false)
  const planRef = useRef<TodayPlan | null>(null)
  planRef.current = plan
  const pageClass = usePageClass(!loading || Boolean(plan))

  const load = useCallback(async () => {
    const cached = Boolean(planRef.current)
    if (!cached) {
      setLoading(true)
      setFailed(false)
    }
    try {
      const city = readStorage(STORAGE_KEYS.city) || undefined
      const [ctx, current] = await Promise.all([
        api.getTodayContext(city).catch(() => null),
        api.getCurrentTodayPlan().catch(() => null),
      ])
      setContext(ctx)
      setPlan(current)
      if (!current) await generate(false)
    } catch {
      if (!planRef.current) setFailed(true)
    } finally {
      setLoading(false)
    }
  }, [])

  const generate = async (refresh: boolean) => {
    setBusy(true)
    try {
      const reportId = readStorage(STORAGE_KEYS.reportId) || undefined
      const city = readStorage(STORAGE_KEYS.city) || undefined
      const { data } = await api.createTodayPlan({ report_id: reportId, city, refresh })
      setPlan(data)
      trackEvent('today_plan_generate', { refresh: String(refresh) })
    } catch (e) {
      Taro.showToast({ title: (e as Error).message || '生成没有成功', icon: 'none' })
    } finally {
      setBusy(false)
    }
  }

  useEffect(() => {
    load()
  }, [load])

  useShowOnce(() => {
    if (planRef.current) load()
  })

  const generating = plan && plan.look_task && (plan.look_task.status === 'queued' || plan.look_task.status === 'processing')
  const { stop } = useTaskPolling({
    fetcher: async () => {
      const current = await api.getCurrentTodayPlan()
      if (current) setPlan(current)
      return current
    },
    intervalMs: POLL_INTERVALS.today,
    enabled: Boolean(generating),
    isSettled: (result) => {
      const status = result?.look_task?.status
      return !status || shouldStopPolling(status)
    },
    onDone: (result) => {
      if (result) setPlan(result)
    },
  })
  Taro.useDidHide(() => stop())

  const editCity = () => {
    const modal = Taro.showModal as unknown as (opts: Record<string, unknown>) => Promise<{ confirm: boolean; content?: string }>
    modal({
      title: '所在城市',
      editable: true,
      placeholderText: '如：杭州',
      success: (res: { confirm: boolean; content?: string }) => {
        if (!res.confirm) return
        writeStorage(STORAGE_KEYS.city, res.content || '')
        generate(true)
      },
    })
  }

  const activate = async () => {
    if (!plan) return
    try {
      const item = await api.activateTodayPlan(plan.id)
      setPlan(item)
      trackEvent('today_plan_activate', {})
      Taro.showToast({ title: '已加入今日清单', icon: 'success' })
    } catch (e) {
      Taro.showToast({ title: (e as Error).message || '操作没有成功', icon: 'none' })
    }
  }

  const feedback = async (word: string) => {
    if (!plan) return
    try {
      const item = await api.sendTodayPlanFeedback(plan.id, word)
      setPlan(item)
      trackEvent('today_feedback', { word })
    } catch (e) {
      Taro.showToast({ title: (e as Error).message || '反馈没有成功', icon: 'none' })
    }
  }

  const generatedUrl = lookImage(plan?.generated_image_url)
  const imageUrl = generatedUrl || lookImage(plan?.image_url)
  const aiBadge = generatedUrl
    ? (plan?.look_provider ?? '').startsWith('demo')
      ? '效果示例'
      : 'AI 风格预览'
    : ''

  if (loading && !plan) {
    return (
      <View className={pageClass}>
        <AppHeader title="今日造型" back />
        <Skeleton rows={4} />
      </View>
    )
  }

  if (failed) {
    return (
      <View className={pageClass}>
        <AppHeader title="今日造型" back />
        <ErrorState onRetry={load} />
      </View>
    )
  }

  return (
    <View className={pageClass}>
      <AppHeader title="今日造型" back />
      <View className="today">
        <View className="today__ctx fade-up" onClick={editCity}>
          <Text className="today__ctx-city">{context?.city || '设置城市'}</Text>
          {context ? (
            <Text className="today__ctx-weather">
              {context.condition} · {context.temperature} · {context.day_type}
              {context.schedule ? ` · ${context.schedule}` : ''}
            </Text>
          ) : null}
          <Text className="today__ctx-edit">轻触修改</Text>
        </View>

        <View className="today__card fade-up delay-1">
          <View className="today__img-wrap pressable" onClick={() => imageUrl && setPreviewOpen(true)}>
            <ExampleImage
              className="today__img"
              src={plan?.generated_image_url || plan?.image_url}
              badgeText={(plan?.look_provider ?? '').startsWith('demo') ? '效果示例' : '风格参考'}
            />
            {aiBadge ? (
              <View className="today__img-badge">
                <Text>{aiBadge}</Text>
              </View>
            ) : null}
            {generating ? (
              <View className="today__mask">
                <View className="today__mask-spin spinner" />
                <Text className="today__mask-text">{plan?.look_task?.stage || '正在生成搭配图'}</Text>
              </View>
            ) : null}
          </View>
          <View className="today__copy">
            <Text className="today__title">{plan?.title ?? ''}</Text>
            <Text className="today__summary">{plan?.summary ?? ''}</Text>
            {(plan?.steps ?? []).map((step, i) => (
              <View key={i} className="today__step">
                <Text className="today__step-label">{step.label || step.category}</Text>
                <Text className="today__step-text">
                  {step.title}
                  {step.copy ? ` · ${step.copy}` : ''}
                </Text>
              </View>
            ))}
          </View>
        </View>

        <View className="today__actions fade-up delay-2">
          <PrimaryButton text={plan?.active ? '已加入今日清单' : '加入今日清单'} disabled={plan?.active} onClick={activate} />
          <View className="today__actions-row">
            <Text className="today__actions-alt pressable" onClick={() => generate(true)}>换一个方案</Text>
            <Text className="today__actions-alt pressable" onClick={() => Taro.navigateTo({ url: '/packages/life/pages/advisor/index' })}>问问顾问</Text>
          </View>
        </View>

        <View className="today__feedback fade-up delay-3">
          <Text className="today__feedback-title">今天穿了效果如何</Text>
          <View className="today__feedback-pills">
            {FEEDBACKS.map((word) => (
              <View
                key={word}
                className={`today__feedback-pill ${plan?.feedback === word ? 'today__feedback-pill--active' : ''} pressable`}
                onClick={() => feedback(word)}
              >
                <Text>{word}</Text>
              </View>
            ))}
          </View>
        </View>
      </View>

      {previewOpen && imageUrl ? (
        <View className="today__viewer" onClick={() => setPreviewOpen(false)}>
          <Image className="today__viewer-img" src={imageUrl} mode="aspectFit" />
          <Text className="today__viewer-close">轻触关闭</Text>
        </View>
      ) : null}
    </View>
  )
}
