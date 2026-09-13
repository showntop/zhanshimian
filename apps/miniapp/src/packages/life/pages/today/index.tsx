// 今日造型：天气上下文 + 当日方案（生成/换一个/加入清单/反馈）。
// 生成是异步受理（202 + 公开 Operation）：状态只通过唯一轮询路径观察，
// 就绪后整体替换本地方案；城市偏好留在本地（UI 偏好，不是业务 id）。
import { useCallback, useEffect, useRef, useState } from 'react'
import Taro from '@tarojs/taro'
import { Image, Text, View } from '@tarojs/components'
import { trackEvent, type TodayContext, type TodayPlan } from '@zsm/core'
import { usePageShell, useShowOnce } from '../../../../hooks/use-page-visibility'
import { peripherals } from '../../../../app/api/peripherals'
import { qualityApi } from '../../../../app/api/quality'
import { resourceCache, resourceKey } from '../../../../app/cache/resource-cache'
import { useOperationPolling } from '../../../../app/operations/use-operation-polling'
import { handleBillingError } from '../../../../services/billing'
import { readStorage, writeStorage, STORAGE_KEYS } from '../../../../services/storage'
import AppHeader from '../../../../components/app-header'
import PrimaryButton from '../../../../components/primary-button'
import SourceImage from '../../../../components/source-image'
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
  const [preview, setPreview] = useState(false)
  const planRef = useRef<TodayPlan | null>(null)
  planRef.current = plan
  const { pageClass, enter } = usePageShell(!loading || Boolean(plan), '', 'today')

  const load = useCallback(async () => {
    const cached = Boolean(planRef.current)
    if (!cached) {
      setLoading(true)
      setFailed(false)
    }
    try {
      const city = readStorage(STORAGE_KEYS.city) || undefined
      const [ctx, current] = await Promise.all([
        peripherals.getTodayContext(city).catch(() => null),
        peripherals.getCurrentTodayPlan().catch(() => null),
      ])
      setContext(ctx)
      setPlan(current)
      if (!current) await generate(false)
    } catch {
      if (!planRef.current) setFailed(true)
    } finally {
      setLoading(false)
    }
    // generate 在下方定义；依赖只取 useCallback 稳定引用
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const generate = async (refresh: boolean) => {
    setBusy(true)
    try {
      const report = await qualityApi.getCurrentReport()
      const city = readStorage(STORAGE_KEYS.city) || undefined
      const accepted = await peripherals.createTodayPlan({
        report_id: report?.id,
        city,
        refresh,
      })
      resourceCache.write(resourceKey('operation', accepted.operation.id), accepted.operation)
      setPlan(accepted.data)
      trackEvent('today_plan_generate', { refresh: String(refresh) })
    } catch (e) {
      if (handleBillingError(e)) return
      Taro.showToast({ title: (e as Error).message || '生成没有成功', icon: 'none' })
    } finally {
      setBusy(false)
    }
  }

  useEffect(() => {
    void load()
  }, [load])

  useShowOnce(() => {
    if (planRef.current) void load()
  })

  // 生成中（planning/rendering）就盯着受理 Operation；终态后重新拉当前方案
  const operationId = plan?.state === 'planning' || plan?.state === 'rendering' ? plan.operation?.id ?? '' : ''
  useOperationPolling({
    operationIds: operationId ? [operationId] : [],
    enabled: Boolean(operationId),
    onSettled: () => {
      void peripherals.getCurrentTodayPlan().then((current) => {
        if (current) setPlan(current)
      })
    },
  })

  const editCity = () => {
    const modal = Taro.showModal as unknown as (opts: Record<string, unknown>) => Promise<{ confirm: boolean; content?: string }>
    modal({
      title: '所在城市',
      editable: true,
      placeholderText: '如：杭州',
      success: (res: { confirm: boolean; content?: string }) => {
        if (!res.confirm) return
        writeStorage(STORAGE_KEYS.city, res.content || '')
        void generate(true)
      },
    })
  }

  const activate = async () => {
    if (!plan) return
    try {
      const item = await peripherals.activateTodayPlan(plan.id)
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
      const item = await peripherals.submitTodayPlanFeedback(plan.id, word)
      setPlan(item)
      trackEvent('today_feedback', { word })
    } catch (e) {
      Taro.showToast({ title: (e as Error).message || '反馈没有成功', icon: 'none' })
    }
  }

  const imageUrl = plan?.media?.url ?? ''

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
        <ErrorState onRetry={() => void load()} />
      </View>
    )
  }

  return (
    <View className={pageClass}>
      <AppHeader title="今日造型" back />
      <View className="today">
        <View className={`today__ctx card--quiet ${enter()}`} onClick={editCity}>
          <Text className="today__ctx-city">{context?.city || '设置城市'}</Text>
          {context ? (
            <Text className="today__ctx-weather">
              {context.condition} · {context.temperature}° · {context.day_type}
              {context.schedule ? ` · ${context.schedule}` : ''}
            </Text>
          ) : null}
          <Text className="today__ctx-edit">轻触修改</Text>
        </View>

        <View className={`today__card card ${enter(1)}`}>
          <View className="today__img-wrap photo-hero pressable" onClick={() => imageUrl && setPreview(true)}>
            <SourceImage className="today__img" media={plan?.media ?? null} anchor="top" />
            {plan && plan.state !== 'ready' && plan.state !== 'ready_partial' ? (
              <View className="today__mask">
                <View className="scan-sweep" />
                <View className="today__mask-spin spinner" />
                <Text className="today__mask-text">正在生成搭配图</Text>
              </View>
            ) : null}
          </View>
          <View className="today__copy">
            <Text className="today__title">{plan?.title ?? ''}</Text>
            <Text className="today__summary">{plan?.summary ?? ''}</Text>
            {(plan?.steps ?? []).map((step) => (
              <View key={step.title} className="today__step">
                <Text className="today__step-label">{step.label || step.category}</Text>
                <Text className="today__step-text">
                  {step.title}
                  {step.copy ? ` · ${step.copy}` : ''}
                </Text>
              </View>
            ))}
          </View>
        </View>

        <View className={`today__actions ${enter(2)}`}>
          <PrimaryButton text={plan?.active ? '已加入今日清单' : '加入今日清单'} disabled={plan?.active} onClick={() => void activate()} />
          <View className="today__actions-row">
            <Text className="today__actions-alt pressable" onClick={() => void generate(true)}>换一个方案</Text>
            <Text className="today__actions-alt pressable" onClick={() => Taro.navigateTo({ url: '/packages/life/pages/advisor/index' })}>问问顾问</Text>
          </View>
        </View>

        <View className={`today__feedback ${enter(3)}`}>
          <Text className="today__feedback-title">今天穿了效果如何</Text>
          <View className="today__feedback-pills">
            {FEEDBACKS.map((word) => (
              <View
                key={word}
                className={`today__feedback-pill ${plan?.feedback === word ? 'today__feedback-pill--active' : ''} pressable`}
                onClick={() => void feedback(word)}
              >
                <Text>{word}</Text>
              </View>
            ))}
          </View>
        </View>
      </View>

      {preview && imageUrl ? (
        <View className="today__viewer" onClick={() => setPreview(false)}>
          <Image className="today__viewer-img" src={imageUrl} mode="aspectFit" />
          <Text className="today__viewer-close">轻触关闭</Text>
        </View>
      ) : null}
    </View>
  )
}
