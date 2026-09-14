// 方案 tab：方案集的分阶段呈现。planning 全屏等待；rendering 起文字永远可读；
// 每套形象图独立就绪、独立失败、独立重试。
//
// 与旧 plans 页的差别：
// 1. 不再读 Storage 的 reportId / sceneBrief——入口只有「交接条」和「问服务端」；
// 2. 不再沿用旧的任务轮询——唯一轮询路径是 useOperationPolling，
//    盯着受理 operation 与各套渲染的 operation，settled 后整体刷新方案集；
// 3. 刷新走 resourceCache.revalidate：刷新期间继续显示当前 PlanSet，到达后整体替换，
//    不做客户端字段级拼接（那会造出服务端从没发布过的组合）。
import { useCallback, useEffect, useRef, useState } from 'react'
import Taro from '@tarojs/taro'
import { ScrollView, Text, View } from '@tarojs/components'
import {
  EMPTY_COPY,
  ERROR_COPY,
  PLANNING_COPY,
  SCENES,
  planSlotLabel,
} from '@zsm/core'
import type { PlanSet, PlanVariant, Report } from '@zsm/core'
import { qualityApi } from '../../app/api/quality'
import { PublicApiError } from '../../app/api/result'
import { resourceCache, resourceKey } from '../../app/cache/resource-cache'
import { takePlanSetHandoff } from '../../app/plan-set-handoff'
import { useOperationPolling } from '../../app/operations/use-operation-polling'
import EmptyState from '../../components/empty-state'
import ErrorState from '../../components/error-state'
import PrimaryButton from '../../components/primary-button'
import RenderState from '../../components/render-state'
import SourceImage from '../../components/source-image'
import TextLink from '../../components/text-link'
import {
  createIdempotencyKey,
  planSetView,
  sortedVariants,
  variantRenderView,
} from './model'
import './index.scss'

const HOME_ROUTE = '/pages/home/index'
const SCENE_ROUTE = '/pages/scene/index'
const PLAN_ROUTE = '/pages/plan/index'

/** 渲染还在动的状态集合：这些 operation 值得盯。 */
const RENDER_IN_FLIGHT = new Set(['queued', 'generating', 'checking'])

interface PlansScreenProps {
  planSetId?: string
  operationId?: string
}

export default function PlansScreen({ planSetId: routePlanSetId, operationId: routeOperationId }: PlansScreenProps) {
  // 交接条取走即清：只有第一次挂载能看到它
  const [handoff] = useState(() => takePlanSetHandoff())
  const [planSetId, setPlanSetId] = useState(() => handoff?.planSetId ?? routePlanSetId ?? '')
  // 受理 operation：planning 阶段唯一可盯的东西
  const [acceptOperationId, setAcceptOperationId] = useState(
    () => handoff?.operationId ?? routeOperationId ?? '',
  )
  const [report, setReport] = useState<Report | null>(null)
  const [planSet, setPlanSet] = useState<PlanSet | null>(() =>
    planSetId ? resourceCache.read<PlanSet>(resourceKey('plan-set', planSetId)) ?? null : null,
  )
  const [scene, setScene] = useState<string>('general')
  const [bootstrapped, setBootstrapped] = useState(Boolean(planSetId))
  const [analyzingOperationId, setAnalyzingOperationId] = useState('')
  const [failed, setFailed] = useState(false)
  const [activeId, setActiveId] = useState('')
  const [retryingId, setRetryingId] = useState('')
  const planSetRef = useRef<PlanSet | null>(null)
  planSetRef.current = planSet

  /**
   * 整体刷新：revalidate 合并同 key 并发，到达前界面继续显示当前值。
   * 响应无条件整体替换——不做"挑几个字段合并"的客户端拼装。
   */
  const refreshPlanSet = useCallback(async (id: string): Promise<PlanSet | null> => {
    try {
      const next = await resourceCache.revalidate(resourceKey('plan-set', id), () =>
        qualityApi.getPlanSet(id),
      )
      setPlanSet(next)
      setFailed(false)
      return next
    } catch {
      setFailed(true)
      return null
    }
  }, [])

  /** Tab 入口：先问当前报告，再列该场景的方案集；报告都没有时区分"在分析"与"未建档"。 */
  useEffect(() => {
    if (planSetId) return
    let cancelled = false
    void (async () => {
      try {
        const current = await qualityApi.getCurrentReport()
        if (cancelled) return
        if (!current) {
          const boot = await qualityApi.getHomeBootstrap().catch(() => null)
          if (cancelled) return
          const assessment = (boot?.active_operations ?? []).find(
            (op) => op.kind === 'assessment' && (op.status === 'accepted' || op.status === 'running' || op.status === 'retrying'),
          )
          setAnalyzingOperationId(assessment?.id ?? '')
          setReport(null)
          setBootstrapped(true)
          return
        }
        setReport(current)
        const list = await qualityApi.listPlanSets(current.id, 'general')
        if (cancelled) return
        const latest = [...list].sort((a, b) => b.created_at.localeCompare(a.created_at))[0]
        if (latest) {
          setPlanSetId(latest.id)
          setPlanSet(latest)
          await refreshPlanSet(latest.id)
        }
        setBootstrapped(true)
      } catch {
        if (!cancelled) {
          setFailed(true)
          setBootstrapped(true)
        }
      }
    })()
    return () => {
      cancelled = true
    }
  }, [planSetId, refreshPlanSet])

  // 有路由 planSetId（报告页交接之外进入）时拉一次
  useEffect(() => {
    if (routePlanSetId && routePlanSetId === planSetId) void refreshPlanSet(routePlanSetId)
  }, [routePlanSetId, planSetId, refreshPlanSet])

  const view = planSet ? planSetView(planSet) : null

  /** 值得盯的 operation：受理中的方案集 + 各套在途渲染。 */
  const watchedIds: string[] = []
  if (view?.kind === 'planning' && acceptOperationId) watchedIds.push(acceptOperationId)
  for (const variant of planSet?.variants ?? []) {
    if (RENDER_IN_FLIGHT.has(variant.render.state) && variant.render.operation_id) {
      watchedIds.push(variant.render.operation_id)
    }
  }

  const { refresh: refreshOperations } = useOperationPolling({
    operationIds: watchedIds,
    enabled: watchedIds.length > 0,
    onSettled: () => {
      // 所有被盯的 operation 都到终态了：整体刷新方案集看新状态
      if (planSetRef.current) void refreshPlanSet(planSetRef.current.id)
    },
  })

  const variants = planSet ? sortedVariants(planSet) : []
  useEffect(() => {
    if (variants.length === 0) return
    if (variants.some((v) => v.id === activeId)) return
    const recommended = variants.find((v) => v.recommended) ?? variants[0]
    if (recommended) setActiveId(recommended.id)
  }, [variants, activeId])
  const activeVariant = variants.find((v) => v.id === activeId) ?? null

  /** 切场景：general 之外各场景各自列最新方案集；没有就给「去回答 Brief」的空态。 */
  const switchScene = async (next: string) => {
    setScene(next)
    if (!report) return
    if (next === 'general') {
      const list = await qualityApi.listPlanSets(report.id, undefined)
      const latest = [...list].sort((a, b) => b.created_at.localeCompare(a.created_at))[0]
      if (latest) {
        setPlanSetId(latest.id)
        setPlanSet(latest)
        await refreshPlanSet(latest.id)
      } else {
        setPlanSetId('')
        setPlanSet(null)
      }
      return
    }
    const list = await qualityApi.listPlanSets(report.id, next as PlanSet['scene'])
    const latest = [...list].sort((a, b) => b.created_at.localeCompare(a.created_at))[0]
    if (latest) {
      setPlanSetId(latest.id)
      setPlanSet(latest)
      await refreshPlanSet(latest.id)
    } else {
      setPlanSetId('')
      setPlanSet(null)
    }
  }

  /** general 空态 → 生成形象方案（受理后原地进入 planning 视图）。 */
  const generateGeneral = async () => {
    if (!report) return
    try {
      const start = await qualityApi.createPlanSet(
        {
          report_id: report.id,
          scene: 'general',
          brief: { focus: 'balanced', preparation: 'closet', impression: 'natural' },
        },
        `plan-set:${report.id}`,
      )
      if (start.accepted) {
        resourceCache.write(resourceKey('operation', start.operation.id), start.operation)
        setAcceptOperationId(start.operation.id)
        setPlanSetId(start.data.id)
      } else {
        setPlanSetId(start.planSet.id)
      }
      setBootstrapped(true)
    } catch (error) {
      const message = error instanceof PublicApiError && error.message ? error.message : PLANNING_COPY.generateFailed
      Taro.showToast({ title: message, icon: 'none' })
    }
  }

  /** 单套重试：新幂等键发新请求；旧键重放只会拿回同一份失败。 */
  const retryVariant = async (variant: PlanVariant) => {
    if (retryingId) return
    setRetryingId(variant.id)
    try {
      const accepted = await qualityApi.createRenderRun(
        variant.id,
        createIdempotencyKey(`render:${variant.id}`),
      )
      resourceCache.write(resourceKey('operation', accepted.operation.id), accepted.operation)
      if (planSetRef.current) await refreshPlanSet(planSetRef.current.id)
      void refreshOperations()
    } catch (error) {
      const message = error instanceof PublicApiError && error.message ? error.message : PLANNING_COPY.generateFailed
      Taro.showToast({ title: message, icon: 'none' })
    } finally {
      setRetryingId('')
    }
  }

  const openDetail = (variant: PlanVariant) => {
    void Taro.navigateTo({
      url:
        `${PLAN_ROUTE}?plan_set_id=${encodeURIComponent(planSetId)}` +
        `&variant_id=${encodeURIComponent(variant.id)}`,
    })
  }

  // ---------- 全屏分支：加载失败 / 未建档 / 分析中 ----------
  if (failed && !planSet) {
    return <ErrorState title={PLANNING_COPY.loadFailed} retryText={ERROR_COPY.retryAction} onRetry={() => void switchScene(scene)} />
  }

  if (!planSet && bootstrapped && !report) {
    const empty = analyzingOperationId ? EMPTY_COPY.plansAnalyzing : EMPTY_COPY.plansNeedArchive
    return (
      <EmptyState
        title={empty.title}
        description={empty.body}
        actionText={empty.action}
        onAction={() => {
          if (analyzingOperationId) {
            void Taro.navigateTo({
              url:
                `/pages/analysis/index?operation_id=${encodeURIComponent(analyzingOperationId)}` +
                `&assessment_id=`,
            })
          } else {
            void Taro.redirectTo({ url: '/pages/capture/index' })
          }
        }}
      />
    )
  }

  if (!planSet) return null

  // ---------- planning：整屏等待，不虚构方案卡 ----------
  if (view?.kind === 'planning') {
    return (
      <View className="planning-screen">
        <View className="planning-screen__wait fade-up">
          <View className="spinner planning-screen__spin" />
          <Text className="planning-screen__wait-title">{PLANNING_COPY.planningTitle}</Text>
          <Text className="planning-screen__wait-body">{PLANNING_COPY.planningBody}</Text>
          <TextLink text={PLANNING_COPY.wander} onClick={() => void Taro.switchTab({ url: HOME_ROUTE })} />
        </View>
      </View>
    )
  }

  // ---------- failed 且没有任何文字：整屏失败 + 重新生成 ----------
  if (view?.kind === 'failed') {
    return (
      <EmptyState
        title={PLANNING_COPY.retryFailedTitle}
        description={PLANNING_COPY.retryFailedBody}
        actionText={PLANNING_COPY.regenerateAction}
        onAction={() => void generateGeneral()}
      />
    )
  }

  return (
    <View className="planning-screen">
      <ScrollView className="planning-screen__tabs" scrollX enhanced showScrollbar={false}>
        {[
          { key: 'general', label: PLANNING_COPY.generalTab },
          ...SCENES.map((s) => ({ key: s.id as string, label: s.label })),
        ].map((tab) => (
          <Text
            key={tab.key}
            className={`planning-screen__tab ${scene === tab.key ? 'planning-screen__tab--active' : ''}`}
            onClick={() => void switchScene(tab.key)}
          >
            {tab.label}
          </Text>
        ))}
      </ScrollView>

      {variants.length === 0 ? (
        <View className="planning-screen__empty fade-up">
          <Text className="planning-screen__empty-title">
            {scene === 'general' ? PLANNING_COPY.generalEmptyTitle : `${SCENES.find((s) => s.id === scene)?.label ?? ''}场合还没有方案`}
          </Text>
          <Text className="planning-screen__empty-body">
            {scene === 'general' ? PLANNING_COPY.generalEmptyBody : PLANNING_COPY.sceneEmptyBody}
          </Text>
          {scene === 'general' ? (
            <PrimaryButton text={PLANNING_COPY.generateGeneral} onClick={() => void generateGeneral()} />
          ) : (
            <PrimaryButton
              text={`${PLANNING_COPY.generateScenePrefix}${SCENES.find((s) => s.id === scene)?.label ?? ''}${PLANNING_COPY.generateSceneSuffix}`}
              onClick={() => void Taro.navigateTo({ url: `${SCENE_ROUTE}?scene=${scene}` })}
            />
          )}
        </View>
      ) : (
        <View className="planning-screen__list">
          {variants.map((variant) => {
            const render = variantRenderView(variant)
            const active = variant.id === activeId
            return (
              <View
                key={variant.id}
                className={[
                  'planning-screen__card',
                  active ? 'planning-screen__card--active' : '',
                ]
                  .filter(Boolean)
                  .join(' ')}
                onClick={() => setActiveId(variant.id)}
              >
                <View className="planning-screen__card-head">
                  <Text className="planning-screen__card-name">
                    {planSlotLabel(variant.name, variant.slot)}
                  </Text>
                  {variant.recommended ? (
                    <Text className="planning-screen__card-badge">{PLANNING_COPY.recommended}</Text>
                  ) : null}
                </View>
                <Text className="planning-screen__card-desc">{variant.descriptor}</Text>

                {/* 文字永远在；形象图按它自己的状态呈现 */}
                {render.kind === 'ready' ? (
                  <View className="planning-screen__card-media">
                    <SourceImage className="planning-screen__card-img" media={render.media} mode="aspectFill" anchor="top" frameAspect={1.6} />
                  </View>
                ) : (
                  <RenderState view={render} onRetry={() => void retryVariant(variant)} />
                )}

                {variant.outcome_tags.length > 0 ? (
                  <View className="planning-screen__card-outcomes">
                    {variant.outcome_tags.slice(0, 3).map((tag) => (
                      <Text key={tag} className="planning-screen__card-outcome">
                        {tag}
                      </Text>
                    ))}
                  </View>
                ) : null}

                {active && variant.rationale ? (
                  <View className="planning-screen__card-why">
                    <Text className="planning-screen__card-why-label">{PLANNING_COPY.whyLabel}</Text>
                    <Text className="planning-screen__card-why-text">{variant.rationale}</Text>
                  </View>
                ) : null}

                <View className="planning-screen__card-foot">
                  <PrimaryButton text={PLANNING_COPY.viewDetail} onClick={() => openDetail(variant)} />
                </View>
              </View>
            )
          })}
        </View>
      )}
    </View>
  )
}
