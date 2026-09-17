// 方案 tab：旧线（recovery/ui-0911）选择页视觉在新数据模型上的恢复——
// 场景胶囊 tab + 拖动对比 hero（满宽、高度随照片比例）+ 细节卡（why 折叠）
// + 吸底毛玻璃选择坞（收益词 / 三选一 / 差异 chips / CTA / 来源说明）。
//
// 架构不让步的部分：
// 1. 不读 Storage 的 reportId / sceneBrief——入口只有「交接条」和「问服务端」；
//    compareHint 这类 UI 偏好仍走 services/storage（偏好不是业务状态）；
// 2. 唯一轮询路径是 useOperationPolling，盯受理 operation 与各套在途渲染，
//    settled 后整体刷新方案集；刷新期间继续显示当前 PlanSet，到达后整体替换；
// 3. 对比左图只用方案集绑定的报告（boundBodyMedia），绑定不了就退单图，
//    绝不拿"手头最近一份报告"的照片凑对比。
import { useCallback, useEffect, useRef, useState } from 'react'
import Taro, { useDidShow } from '@tarojs/taro'
import { ScrollView, Text, View } from '@tarojs/components'
import {
  EMPTY_COPY,
  ERROR_COPY,
  PLANNING_COPY,
  SCENES,
  planSlotLabel,
} from '@zsm/core'
import type { DisplayMedia, PlanSet, PlanVariant, Report } from '@zsm/core'
import { qualityApi } from '../../app/api/quality'
import { PublicApiError } from '../../app/api/result'
import { resourceCache, resourceKey } from '../../app/cache/resource-cache'
import { takePlanSetHandoff, type PlanSetHandoff } from '../../app/plan-set-handoff'
import { useOperationPolling } from '../../app/operations/use-operation-polling'
import { useShowOnce } from '../../hooks/use-page-visibility'
import { handleBillingError } from '../../services/billing'
import { STORAGE_KEYS, readStorage, writeStorage } from '../../services/storage'
import { getNavMetrics } from '../../components/app-header'
import CompareSlider from '../../components/compare-slider'
import EmptyState from '../../components/empty-state'
import ErrorState from '../../components/error-state'
import PrimaryButton from '../../components/primary-button'
import RenderState from '../../components/render-state'
import Skeleton from '../../components/skeleton'
import SourceImage from '../../components/source-image'
import TextLink from '../../components/text-link'
import {
  analyzingAssessmentOperationId,
  boundBodyMedia,
  briefFingerprint,
  createIdempotencyKey,
  inFlightPlanSetOperationIds,
  planSetRetryMarkerKey,
  planSetSceneKey,
  planSetView,
  sortedVariants,
  variantRenderView,
} from './model'
import './index.scss'

const HOME_ROUTE = '/pages/home/index'
const SCENE_ROUTE = '/pages/scene/index'
const PLAN_ROUTE = '/pages/plan/index'
const CAPTURE_ROUTE = '/pages/capture/index'

/** 渲染还在动的状态集合：这些 operation 值得盯。 */
const RENDER_IN_FLIGHT = new Set(['queued', 'generating', 'checking'])

/** general 的 brief 固定（没有 Brief 页）；「不满意重出」走 refresh 绕开语义键去重。 */
const GENERAL_BRIEF = { focus: 'balanced', preparation: 'closet', impression: 'natural' } as const

// hero 高度跟随照片比例（旧线思路保留）：三套方案图同一管线产出、宽高比一致，
// 切换不跳动，因此让照片自己定高度——满宽 + 完整，无侧边区。
// 上限 = 内容视口高（防极端竖图把首屏撑死），下限 320px；tab 行不占 hero 预算——
// 页面可滚动，hero 只管自己不超过一屏，把空间让给照片。
// 这是基于 windowHeight 的 px 计算，是 rpx 规约的显式例外（与旧线相同）。
const NAV = getNavMetrics()
// spacer 的 margin-bottom（24rpx）换算成 px：hero 上限要扣除导航下的这段净距
const HEADER_GAP_PX = 12
const MAX_HERO_PX = Math.max(
  320,
  Math.round(NAV.windowHeight - NAV.navHeight - HEADER_GAP_PX),
)
// 渲染未就绪且原本照片也缺失时，状态块只给紧凑高度——不摆一整框空状态
const EMPTY_STATE_HERO_PX = 240

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
  // 受理是否在途：独立状态，绝不从当前 view 推导——切场景会改写 planSet/planSetId，
  // 推导式会把在途受理误判「不在途」，轮询与进度一起丢（切走再切回就显示旧方案）
  const [acceptPending, setAcceptPending] = useState(() => Boolean(handoff?.operationId ?? routeOperationId))
  const [report, setReport] = useState<Report | null>(null)
  // 受理 operation 到达终态失败时上屏的公开文案（'' = 没有失败）；
  // 规划在途的 404 不算失败——那是方案集还没发布的预期状态
  const [acceptFailed, setAcceptFailed] = useState('')
  // 对比左图的来源报告：与当前报告不是同一份时单独拉取，不污染 report
  // （report 是「当前」语义，切场景、生成方案都靠它）
  const [boundReport, setBoundReport] = useState<Report | null>(null)
  const [planSet, setPlanSet] = useState<PlanSet | null>(() =>
    planSetId ? resourceCache.read<PlanSet>(resourceKey('plan-set', planSetId)) ?? null : null,
  )
  const [scene, setScene] = useState<string>('general')
  const [bootstrapped, setBootstrapped] = useState(Boolean(planSetId))
  const [analyzingOperationId, setAnalyzingOperationId] = useState('')
  // bootstrap 里在途的方案集受理 id（含别的场景提交的）：进轮询与「制作中」呈现
  const [activePlanSetOps, setActivePlanSetOps] = useState<string[]>([])
  const [failed, setFailed] = useState(false)
  // 切场景请求在途：tab 已高亮、数据未到期间给内联指示，不让旧内容冒充新场景
  const [switching, setSwitching] = useState(false)
  const [activeId, setActiveId] = useState('')
  const [whyOpen, setWhyOpen] = useState(false)
  const [retryingId, setRetryingId] = useState('')
  const [compareHint, setCompareHint] = useState(() => !readStorage(STORAGE_KEYS.compareHint))
  // 方案图真实宽高（onLoad 采集，决定 hero 高度）
  const [photoDims, setPhotoDims] = useState<Record<string, { w: number; h: number }>>({})
  const planSetRef = useRef<PlanSet | null>(null)
  planSetRef.current = planSet
  const planSetIdRef = useRef(planSetId)
  planSetIdRef.current = planSetId
  const acceptOperationIdRef = useRef(acceptOperationId)
  acceptOperationIdRef.current = acceptOperationId
  const acceptPendingRef = useRef(acceptPending)
  acceptPendingRef.current = acceptPending
  const sceneRef = useRef(scene)
  sceneRef.current = scene
  const reportRef = useRef(report)
  reportRef.current = report
  // 切场景乱序保护：后发的请求赢，先到的慢响应不得盖回去
  const sceneReqRef = useRef(0)
  // 受理的场景归属：OperationRef 不带场景，受理时记下。用 ref 而不是
  // planSetSceneKey(planSetId) 回查——切场景会改写 planSetId，回查必然串场。
  const acceptSceneRef = useRef('')
  // 受理归属的方案集 id：同理不能拿 planSetId 回查
  const acceptPlanSetIdRef = useRef(
    (handoff?.operationId ?? routeOperationId) ? (handoff?.planSetId ?? routePlanSetId ?? '') : '',
  )

  /**
   * 整体刷新：revalidate 合并同 key 并发，到达前界面继续显示当前值。
   * 响应无条件整体替换——不做"挑几个字段合并"的客户端拼装。
   * 唯一例外是 stale 着陆：交接/切场景已经把目光换到别的方案集（ref 已走、
   * 响应才到），旧响应不得盖回——否则交接当帧的生成中行会被旧集顶掉。
   */
  const refreshPlanSet = useCallback(async (id: string): Promise<PlanSet | null> => {
    try {
      const next = await resourceCache.revalidate(resourceKey('plan-set', id), () =>
        qualityApi.getPlanSet(id),
      )
      if (planSetIdRef.current === id) setPlanSet(next)
      setFailed(false)
      return next
    } catch (error) {
      // 受理在途期间方案集还没发布，404 是预期应答不是网络错误；
      // 界面停在生成中视图，由受理 operation 的轮询推向终态。
      // 没有受理 operation 可盯的 404 才是真「没找到」。
      if (
        error instanceof PublicApiError &&
        error.statusCode === 404 &&
        acceptOperationIdRef.current &&
        acceptPendingRef.current
      ) {
        return null
      }
      setFailed(true)
      return null
    }
  }, [])

  /** 在途方案集受理对账：bootstrap.active_operations 是唯一事实来源。 */
  const refreshInFlightOps = useCallback(async () => {
    const boot = await qualityApi.getHomeBootstrap().catch(() => null)
    setActivePlanSetOps(inFlightPlanSetOperationIds(boot?.active_operations ?? []))
    return boot
  }, [])

  /** Tab 入口：先问当前报告，再列 general 的方案集；报告都没有时区分"在分析"与"未建档"。
   *  background=true 是回 tab 的后台对账：已渲染内容（含空态）保留到响应到达，不闪骨架。 */
  const bootstrap = useCallback(async (background = false) => {
    if (!background) setBootstrapped(false)
    try {
      const [current, boot] = await Promise.all([
        qualityApi.getCurrentReport(),
        qualityApi.getHomeBootstrap().catch(() => null),
      ])
      setActivePlanSetOps(inFlightPlanSetOperationIds(boot?.active_operations ?? []))
      if (!current) {
        setAnalyzingOperationId(analyzingAssessmentOperationId(boot?.active_operations ?? []))
        setReport(null)
        setBootstrapped(true)
        return
      }
      setReport(current)
      const list = await qualityApi.listPlanSets(current.id, 'general')
      const latest = [...list].sort((a, b) => b.created_at.localeCompare(a.created_at))[0]
      if (latest) {
        setPlanSetId(latest.id)
        setPlanSet(latest)
        await refreshPlanSet(latest.id)
      }
      setBootstrapped(true)
    } catch {
      setFailed(true)
      setBootstrapped(true)
    }
  }, [refreshPlanSet])

  /** 交接条落地：清失败、换受理 id、高亮跟随受理时写入的侧信道场景。 */
  const applyHandoff = useCallback(
    (next: PlanSetHandoff) => {
      setAcceptFailed('')
      setFailed(false)
      setAcceptOperationId(next.operationId ?? '')
      setAcceptPending(Boolean(next.operationId))
      setPlanSetId(next.planSetId)
      // 换了一份集才清内容（进生成中行/拉取分支）；
      // 同一份集（200 复用、答案没改）保留已渲染内容，后台对账不闪屏
      if (planSetRef.current?.id !== next.planSetId) setPlanSet(null)
      setBootstrapped(true)
      const handoffScene = resourceCache.read<string>(planSetSceneKey(next.planSetId))
      if (handoffScene) setScene(handoffScene)
      // 受理在途才记场景与归属 id；200 复用（没有任务在跑）不算受理
      acceptSceneRef.current = next.operationId ? handoffScene ?? '' : ''
      acceptPlanSetIdRef.current = next.operationId ? next.planSetId : ''
      // 200 复用（没有任务在跑）：立刻对账展示已发布集
      if (!next.operationId) void refreshPlanSet(next.planSetId)
    },
    [refreshPlanSet],
  )

  // tab 页常驻：useState 初始化只在首次挂载消费交接条，
  // 「方案页 → Brief 页 → 提交 → switchTab 回来」的受理会无声丢失。
  // 每次 onShow 都取一次；取走即清，首次挂载已取过时这里是空操作。
  const firstShowRef = useRef(true)
  useDidShow(() => {
    const next = takePlanSetHandoff()
    if (next) applyHandoff(next)
    // 首次 onShow 跳过对账：挂载效应刚跑过 bootstrap（与 home 同一规则）
    if (firstShowRef.current) {
      firstShowRef.current = false
      return
    }
    // 空态回访对账：tab 常驻、bootstrap 只跑一次——挂载后才发起的形象分析
    //（拍摄页提交 → 进度页 → 切回方案 tab）拿不到，空态会停在「去形象分析」
    // 把正在分析的用户引去重拍；离开期间分析完成的也一样查不到新报告。
    // home 每次 onShow 都对账，这里对空态（无报告无方案集）做同一件事。
    if (next || reportRef.current || planSetIdRef.current) return
    void bootstrap(true)
  })

  // 只自动跑一次：切场景把 planSetId 清回 '' 时不得再次 bootstrap 把场景顶回去；
  // 失败重试走 retryLoad 显式调用。
  const bootstrappedOnceRef = useRef(false)
  useEffect(() => {
    if (planSetId || bootstrappedOnceRef.current) return
    bootstrappedOnceRef.current = true
    void bootstrap()
  }, [planSetId, bootstrap])

  // 有 id 没数据（交接条冷缓存、受理后首拉）：立刻拉，不躺在白屏上
  useEffect(() => {
    if (planSetId && !planSet && !failed) void refreshPlanSet(planSetId)
  }, [planSetId, planSet, failed, refreshPlanSet])

  // 切 tab 回来：缓存优先渲染（state 留着），后台校验一遍，不清空已渲染图片
  useShowOnce(() => {
    if (planSetRef.current) void refreshPlanSet(planSetRef.current.id)
  })

  // 方案集自身决定场景 tab 高亮（从报告页交接/路由进来的可能是场合方案）
  useEffect(() => {
    if (planSet) setScene(planSet.scene)
  }, [planSet])

  // 交接/路由进入时 report 为空：后台补一份当前报告，
  // 场景切换与「生成形象方案」才有依据（拉不到就保持只读当前方案集）
  useEffect(() => {
    if (report || !planSetId) return
    let cancelled = false
    void qualityApi
      .getCurrentReport()
      .then((current) => {
        if (!cancelled && current) setReport(current)
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [report, planSetId])

  // 对比左图的来源报告：拉不到就退单图，绝不拿别的报告照片凑
  useEffect(() => {
    if (!planSet) {
      setBoundReport(null)
      return
    }
    if (report && report.id === planSet.report_id) return
    if (boundReport && boundReport.id === planSet.report_id) return
    let cancelled = false
    void qualityApi
      .getReport(planSet.report_id)
      .then((bound) => {
        if (!cancelled) setBoundReport(bound)
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [planSet, report, boundReport])

  const view = planSet ? planSetView(planSet) : null

  // 受理在途 = 有未到终态的受理任务（显式 pending，不从当前 view 推导）
  const acceptInFlight = Boolean(acceptOperationId && acceptPending)
  // 受理场景从 ref 取：planSetId 在切场景时被改写，侧信道回查会串场
  const acceptScene = acceptInFlight ? acceptSceneRef.current || undefined : undefined
  // 归属不到当前受理的在途操作：无法定位场景，走顶部全局提示
  const foreignPlanSetOps = activePlanSetOps.filter((id) => id !== acceptOperationId)

  /** 值得盯的 operation：受理中的方案集 + bootstrap 在途受理 + 各套在途渲染。 */
  const watchedIds: string[] = []
  if (acceptInFlight) {
    watchedIds.push(acceptOperationId)
  }
  for (const id of foreignPlanSetOps) watchedIds.push(id)
  for (const variant of planSet?.variants ?? []) {
    if (RENDER_IN_FLIGHT.has(variant.render.state) && variant.render.operation_id) {
      watchedIds.push(variant.render.operation_id)
    }
  }

  const { refresh: refreshOperations } = useOperationPolling({
    operationIds: watchedIds,
    enabled: watchedIds.length > 0,
    onSettled: (operations) => {
      // 受理 operation 终态失败：方案集永远不会发布，刷新只会再拿 404。
      // 把公开失败文案直接上屏，给「重新生成」而不是误导性的网络错误。
      const acceptId = acceptOperationIdRef.current
      const accept = acceptId ? operations.find((op) => op.id === acceptId) : undefined
      if (accept && (accept.status === 'failed' || accept.status === 'cancelled' || accept.status === 'superseded')) {
        // 固定幂等键 24h 内只会重放同一份失败：记下场景，
        // 下次发起换新键（Brief 页与 generateGeneral 读同一个标记）。
        // 场景优先取受理归属 ref（planSetId 可能已被切场景改写）
        const failedScene =
          acceptSceneRef.current ||
          resourceCache.read<string>(planSetSceneKey(planSetIdRef.current)) ||
          'general'
        resourceCache.write(planSetRetryMarkerKey(failedScene), '1')
        setAcceptFailed(accept.public_message || PLANNING_COPY.retryFailedBody)
        setAcceptPending(false)
        return
      }
      if (accept) {
        // 受理成功终态：不再在途。用户还停在这个场景就把新发布的集顶上来
        // （切走了不拽回——切过去时 switchScene 会取到最新已发布集）
        setAcceptPending(false)
        const acceptedPlanSetId = acceptPlanSetIdRef.current
        if (acceptedPlanSetId && sceneRef.current === acceptSceneRef.current) {
          setPlanSetId(acceptedPlanSetId)
          void refreshPlanSet(acceptedPlanSetId)
        }
      }
      // 所有被盯的 operation 都到终态了：整体刷新方案集看新状态
      const id = planSetRef.current?.id ?? planSetIdRef.current
      if (id) void refreshPlanSet(id)
      // 在途受理对账；新发布的方案集可能就在当前场景（跨会话回来、无受理 id 可盯时），
      // 静默重取当前场景列表——取到不同的新集才替换，空列表不动已渲染内容
      void refreshInFlightOps()
      const currentReport = reportRef.current
      if (currentReport) {
        const currentScene = sceneRef.current as PlanSet['scene']
        void qualityApi
          .listPlanSets(currentReport.id, currentScene)
          .then((list) => {
            const latest = [...list].sort((a, b) => b.created_at.localeCompare(a.created_at))[0]
            if (latest && latest.id !== planSetIdRef.current) {
              setPlanSetId(latest.id)
              setPlanSet(latest)
              void refreshPlanSet(latest.id)
            }
          })
          .catch(() => {})
      }
    },
  })

  // 方案 Tab 角标：在途 operation 数（受理中计 1 + 各套在途渲染）。
  // settled → 整体刷新 → watchedIds 归零 → 角标随之移除。
  const watchedCount = watchedIds.length
  useEffect(() => {
    if (watchedCount > 0) {
      Taro.setTabBarBadge({ index: 1, text: String(watchedCount) }).catch(() => {})
    } else {
      Taro.removeTabBarBadge({ index: 1 }).catch(() => {})
    }
  }, [watchedCount])

  const variants = planSet ? sortedVariants(planSet) : []
  useEffect(() => {
    if (variants.length === 0) return
    if (variants.some((v) => v.id === activeId)) return
    const recommended = variants.find((v) => v.recommended) ?? variants[0]
    if (recommended) setActiveId(recommended.id)
  }, [variants, activeId])
  const activeVariant = variants.find((v) => v.id === activeId) ?? null

  /** 切场景：高亮先切、旧内容保留到响应到达；在途给内联指示，乱序响应不得盖回。 */
  const switchScene = async (next: string) => {
    const req = ++sceneReqRef.current
    setScene(next)
    setWhyOpen(false)
    if (!report) return
    setSwitching(true)
    try {
      const list = await qualityApi.listPlanSets(report.id, next as PlanSet['scene'])
      if (req !== sceneReqRef.current) return
      const latest = [...list].sort((a, b) => b.created_at.localeCompare(a.created_at))[0]
      if (latest) {
        setPlanSetId(latest.id)
        setPlanSet(latest)
        await refreshPlanSet(latest.id)
      } else {
        setPlanSetId('')
        setPlanSet(null)
      }
    } catch {
      if (req !== sceneReqRef.current) return
      // 有内容在屏就只提示（内容继续可读），空屏才进错误态
      if (planSetRef.current) {
        Taro.showToast({ title: PLANNING_COPY.loadFailed, icon: 'none' })
      } else {
        setFailed(true)
      }
    } finally {
      if (req === sceneReqRef.current) setSwitching(false)
    }
  }

  /** general → 生成形象方案；refresh=true 是「不满意重出」：服务端绕开语义键复用，派生新身份。 */
  const generateGeneral = async (refresh = false) => {
    if (!report) return
    setAcceptFailed('')
    try {
      // 换新键的两种情况：上次受理终态 failed（同键 24h 内重放同一份失败）、
      // refresh 强制重出（每次都是新任务，重放旧 202 会把新任务吞掉）；其余同键保幂等。
      const retryKey = planSetRetryMarkerKey('general')
      const fresh = refresh || Boolean(resourceCache.read<string>(retryKey))
      const baseKey = `plan-set:${report.id}:${briefFingerprint(GENERAL_BRIEF)}`
      const start = await qualityApi.createPlanSet(
        {
          report_id: report.id,
          scene: 'general',
          brief: { ...GENERAL_BRIEF },
          ...(refresh ? { refresh: true } : {}),
        },
        fresh ? createIdempotencyKey(baseKey) : baseKey,
      )
      if (fresh) resourceCache.remove(retryKey)
      if (start.accepted) {
        resourceCache.write(resourceKey('operation', start.operation.id), start.operation)
        setAcceptOperationId(start.operation.id)
        setAcceptPending(true)
        acceptSceneRef.current = 'general'
        acceptPlanSetIdRef.current = start.data.id
        setPlanSetId(start.data.id)
      } else {
        // 复用已发布方案集：没有任务在跑，旧的受理 id 必须清掉，
        // 否则轮询会盯上那份已终态的 operation 把失败卡又顶回来
        setAcceptOperationId('')
        setAcceptPending(false)
        acceptSceneRef.current = ''
        acceptPlanSetIdRef.current = ''
        setPlanSetId(start.planSet.id)
      }
      setBootstrapped(true)
    } catch (error) {
      // 402/429 等计费错误先走购买引导（弹层→标记→profile 购买层），其余才落通用提示
      if (handleBillingError(error)) return
      const message = error instanceof PublicApiError && error.message ? error.message : PLANNING_COPY.generateFailed
      Taro.showToast({ title: message, icon: 'none' })
    }
  }

  /** 受理失败的「重新生成」：回它自己的场景——场景方案回 Brief 页（预填上次答案改完重发），general 原地重发。 */
  const regenerateAccepted = () => {
    // 场景取受理归属 ref 而不是 pending 门控的 acceptScene：失败后 pending 已落，门控值必然为空
    const failedScene = acceptSceneRef.current
    if (failedScene && failedScene !== 'general') {
      void Taro.navigateTo({ url: `${SCENE_ROUTE}?scene=${failedScene}` })
      return
    }
    void generateGeneral()
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
      void Taro.vibrateShort({ type: 'light' })
      if (planSetRef.current) await refreshPlanSet(planSetRef.current.id)
      void refreshOperations()
    } catch (error) {
      if (handleBillingError(error)) return
      const message = error instanceof PublicApiError && error.message ? error.message : PLANNING_COPY.generateFailed
      Taro.showToast({ title: message, icon: 'none' })
    } finally {
      setRetryingId('')
    }
  }

  /** 切换方案：轻震动反馈（沿用旧线 swiper 手势的触感），收起 why 展开。 */
  const pickVariant = (variant: PlanVariant) => {
    if (variant.id === activeId) return
    setActiveId(variant.id)
    setWhyOpen(false)
    void Taro.vibrateShort({ type: 'light' })
  }

  const openDetail = (variant: PlanVariant) => {
    void Taro.navigateTo({
      url:
        `${PLAN_ROUTE}?plan_set_id=${encodeURIComponent(planSetId)}` +
        `&variant_id=${encodeURIComponent(variant.id)}`,
    })
  }

  /** 错误态重试：按当前手上有什么决定重新走哪条路。 */
  const retryLoad = () => {
    setFailed(false)
    if (planSetId) return // failed 复位后，「有 id 没数据」的 effect 会重新拉
    if (report) void switchScene(scene)
    else void bootstrap()
  }

  // 对比左图：绑定校验不过就抛错——这里接住并退单图（与 PlanDetailScreen 同一条规则）
  let leftMedia: DisplayMedia | null = null
  if (planSet) {
    const candidate = report && report.id === planSet.report_id ? report : boundReport
    if (candidate) {
      try {
        leftMedia = boundBodyMedia(planSet, candidate)
      } catch {
        leftMedia = null
      }
    }
  }

  const sceneLabel = SCENES.find((s) => s.id === scene)?.label ?? ''
  const sceneTabs = (
    <ScrollView className="plans__tabs" scrollX enhanced showScrollbar={false}>
      {[
        { key: 'general', label: PLANNING_COPY.generalTab },
        ...SCENES.map((s) => ({ key: s.id as string, label: s.label })),
      ].map((tab) => (
        <Text
          key={tab.key}
          className={`plans__tab ${scene === tab.key ? 'plans__tab--active' : ''}`}
          onClick={() => void switchScene(tab.key)}
        >
          {tab.label}
          {acceptScene === tab.key ? (
            <Text className="plans__tab-pending">{PLANNING_COPY.tabInFlightSuffix}</Text>
          ) : null}
        </Text>
      ))}
    </ScrollView>
  )
  const sceneEmptyCard = (
    <View className="plans__scene-empty fade-up">
      <Text className="plans__scene-empty-title">
        {scene === 'general' ? PLANNING_COPY.generalEmptyTitle : `${sceneLabel}场合还没有方案`}
      </Text>
      <Text className="plans__scene-empty-desc">
        {scene === 'general' ? PLANNING_COPY.generalEmptyBody : PLANNING_COPY.sceneEmptyBody}
      </Text>
      {scene === 'general' ? (
        <PrimaryButton text={PLANNING_COPY.generateGeneral} onClick={() => void generateGeneral()} />
      ) : (
        <PrimaryButton
          text={`${PLANNING_COPY.generateScenePrefix}${sceneLabel}${PLANNING_COPY.generateSceneSuffix}`}
          onClick={() => void Taro.navigateTo({ url: `${SCENE_ROUTE}?scene=${scene}` })}
        />
      )}
    </View>
  )

  // ---------- 全屏分支：加载失败 / 启动骨架 / 未建档 / 分析中 ----------
  if (failed && !planSet) {
    return <ErrorState title={PLANNING_COPY.loadFailed} retryText={ERROR_COPY.retryAction} onRetry={retryLoad} />
  }

  // 首次 bootstrap / 重试期间不给白屏：骨架占位
  if (!planSet && !bootstrapped) {
    return (
      <View className="plans">
        <Skeleton rows={5} />
      </View>
    )
  }

  if (!planSet && !report && !planSetId) {
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

  // 已建档但当前场景还没有方案集：保留场景 tab，只替换内容区——
  // 受理失败的给失败卡（公开文案 + 重新生成），受理中的给生成中行，
  // 切换中的给骨架，其余给该场景的空态卡（含「生成形象方案」）。
  if (!planSet) {
    return (
      <View className="plans">
        {sceneTabs}
        {foreignPlanSetOps.length > 0 ? (
          <Text className="plans__inflight-banner">{PLANNING_COPY.inFlightBanner}</Text>
        ) : null}
        {acceptFailed && acceptSceneRef.current === scene ? (
          <View className="plans__scene-empty fade-up">
            <Text className="plans__scene-empty-title">{PLANNING_COPY.retryFailedTitle}</Text>
            <Text className="plans__scene-empty-desc">{acceptFailed}</Text>
            <PrimaryButton text={PLANNING_COPY.regenerateAction} onClick={regenerateAccepted} />
          </View>
        ) : acceptInFlight && acceptSceneRef.current === scene ? (
          // 生成中只归受理所属的场景：别的场景走空态/骨架，
          // 在途受理由 foreignPlanSetOps 的横幅提示
          <View className="plans__scene-empty">
            <View className="plans__generating">
              <View className="plans__generating-spin spinner" />
              <Text className="plans__generating-text">{PLANNING_COPY.sceneGenerating}</Text>
            </View>
          </View>
        ) : planSetId || switching ? (
          <Skeleton rows={3} />
        ) : (
          sceneEmptyCard
        )}
      </View>
    )
  }

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
        onAction={() => {
          // 回方案集自己的场景：场景方案回 Brief 页改答案重发，不能顶到 general
          if (planSet && planSet.scene !== 'general') {
            void Taro.navigateTo({ url: `${SCENE_ROUTE}?scene=${planSet.scene}` })
          } else {
            void generateGeneral()
          }
        }}
      />
    )
  }

  const activeRender = activeVariant ? variantRenderView(activeVariant) : null
  const heroReady = activeRender?.kind === 'ready'
  // 对比只在 general 场景开启（旧线行为保留）；左图缺失一律退单图
  const canCompare = Boolean(heroReady && leftMedia && scene === 'general')
  // 尺寸未就绪时按 82% 上限预估，onLoad 后校正
  const activeDims = activeVariant ? photoDims[activeVariant.id] : undefined
  const heroPx = activeDims
    ? Math.min(MAX_HERO_PX, Math.round((NAV.windowWidth * activeDims.h) / activeDims.w))
    : Math.round(MAX_HERO_PX * 0.82)

  return (
    <View className="plans">
      {sceneTabs}

      {foreignPlanSetOps.length > 0 ? (
        <Text className="plans__inflight-banner">{PLANNING_COPY.inFlightBanner}</Text>
      ) : null}

      {/* 当前场景有在途受理但屏上还是旧方案集（重新设计提交后切走又切回）：
          内容继续可读，进度行钉在内容上方，不静默 */}
      {acceptInFlight && acceptSceneRef.current === scene && planSet?.id !== acceptPlanSetIdRef.current ? (
        <View className="plans__generating">
          <View className="plans__generating-spin spinner" />
          <Text className="plans__generating-text">{PLANNING_COPY.sceneGenerating}</Text>
        </View>
      ) : null}

      {switching ? (
        <View className="plans__generating">
          <View className="plans__generating-spin spinner" />
          <Text className="plans__generating-text">{PLANNING_COPY.sceneLoading}</Text>
        </View>
      ) : null}

      {variants.length === 0 ? (
        sceneEmptyCard
      ) : (
        <>
        {/* 拖动对比 hero：照片满宽完整展示（高度跟随照片比例），无侧边区无裁切。
            相框常驻（旧线结构）：就绪出对比图；未就绪展示原本照片单图 + 状态 pill
            （都没有才退紧凑状态块）——页面骨架不随渲染结果塌缩，
            后面的信息卡永远不会上叠场景 tab。key 随方案切换重挂载 → 交叉淡入。 */}
        {activeVariant && activeRender ? (
          <View className="plans__hero">
            <View
              className="plans__hero-frame"
              style={{ height: `${activeRender.kind === 'ready' || leftMedia ? heroPx : EMPTY_STATE_HERO_PX}px` }}
              key={activeVariant.id}
              onClick={() => {
                if (!compareHint) return
                setCompareHint(false)
                writeStorage(STORAGE_KEYS.compareHint, '1')
              }}
            >
              {activeRender.kind === 'ready' ? (
                <>
                  <CompareSlider
                    single={!canCompare}
                    current={<SourceImage className="plans__hero-img" media={leftMedia} mode="widthFix" />}
                    plan={
                      <SourceImage
                        className="plans__hero-img"
                        media={activeRender.media}
                        mode="widthFix"
                        onLoad={(e) => {
                          const w = Number(e.detail.width)
                          const h = Number(e.detail.height)
                          if (!w || !h) return
                          setPhotoDims((prev) =>
                            prev[activeVariant.id]?.w === w && prev[activeVariant.id]?.h === h
                              ? prev
                              : { ...prev, [activeVariant.id]: { w, h } },
                          )
                        }}
                      />
                    }
                    currentLabel={PLANNING_COPY.currentLabel}
                    planLabel={PLANNING_COPY.planLabel}
                  />
                  {compareHint && canCompare ? (
                    <Text className="plans__hint">{PLANNING_COPY.compareHint}</Text>
                  ) : null}
                </>
              ) : leftMedia ? (
                <>
                  {/* 渲染未就绪但原本照片在架：单图展示当前形象（标「原本」）+
                      底部状态 pill（生成中转圈 / 失败点按重试 / 暂不可用），
                      与详情页同一处理——不摆一整框空状态把文字方案挤出首屏 */}
                  <SourceImage className="plans__hero-img" media={leftMedia} mode="widthFix" />
                  <Text className="plans__hero-current-label">{PLANNING_COPY.currentLabel}</Text>
                  <View
                    className="plans__hero-state-pill"
                    onClick={
                      activeRender.kind === 'failed' && activeRender.retryable
                        ? () => void retryVariant(activeVariant)
                        : undefined
                    }
                  >
                    {RENDER_IN_FLIGHT.has(activeRender.kind) || retryingId === activeVariant.id ? (
                      <View className="spinner spinner--on-deep plans__hero-state-spin" />
                    ) : null}
                    <Text className="plans__hero-state-text">
                      {activeRender.kind === 'queued'
                        ? PLANNING_COPY.renderQueued
                        : activeRender.kind === 'generating'
                          ? PLANNING_COPY.renderGenerating
                          : activeRender.kind === 'checking'
                            ? PLANNING_COPY.renderChecking
                            : activeRender.kind === 'unavailable'
                              ? PLANNING_COPY.renderUnavailable
                              : activeRender.retryable
                                ? `${PLANNING_COPY.renderFailed} · ${PLANNING_COPY.renderRetry}`
                                : PLANNING_COPY.renderFailed}
                    </Text>
                  </View>
                </>
              ) : (
                <View className="plans__hero-state">
                  <RenderState view={activeRender} onRetry={() => void retryVariant(activeVariant)} />
                </View>
              )}
              <View className="plans__hero-fade" />
            </View>
          </View>
        ) : null}

        {/* 细节卡（第 2 屏起）：descriptor + 折叠 why，正常文档流跟随 hero。
            负边距上叠 hero 的改法在没有 hero（渲染未就绪）时会把卡片拉上去
            盖住场景 tab——回旧线，不再负边距，状态也不再进这张卡。 */}
        {activeVariant ? (
          <View className="plans__info fade-up delay-1">
            <Text className="plans__summary">{activeVariant.descriptor}</Text>
            {activeVariant.rationale ? (
              <View className="plans__why-wrap" onClick={() => setWhyOpen(!whyOpen)}>
                <Text className={`plans__why ${whyOpen ? 'plans__why--open' : ''}`}>{activeVariant.rationale}</Text>
                <Text className="plans__why-toggle">{whyOpen ? PLANNING_COPY.whyClose : PLANNING_COPY.whyLabel}</Text>
              </View>
            ) : null}
          </View>
        ) : null}

        {/* 悬浮选择坞（旧线结构）：收益词顶行 + 三选一（方案名 + 差异 chips）
            + CTA + 来源说明。浮在照片底部上方不占文档流——照片有多高就展示多高，
            选择要素常驻第一屏。 */}
        {activeVariant && activeRender ? (
          <View className="plans__dock dock-glass fade-up delay-2">
            {activeVariant.outcome_tags.length > 0 ? (
              <View className="plans__outcome">
                {activeVariant.outcome_tags.slice(0, 3).map((tag) => (
                  <Text key={tag} className="plans__outcome-tag">{tag}</Text>
                ))}
              </View>
            ) : null}
            <View className="plans__chooser">
              <View className="plans__choices">
                {variants.map((item) => {
                  const itemRender = variantRenderView(item)
                  return (
                    <View
                      key={item.id}
                      className={`plans__choice ${item.id === activeId ? 'plans__choice--active' : ''} pressable`}
                      onClick={() => pickVariant(item)}
                    >
                      <View className="plans__choice-thumb">
                        {itemRender.kind === 'ready' ? (
                          <SourceImage className="plans__choice-img" media={itemRender.media} mode="aspectFit" />
                        ) : RENDER_IN_FLIGHT.has(itemRender.kind) ? (
                          <View className="spinner plans__choice-spin" />
                        ) : (
                          <Text className="plans__choice-state">
                            {itemRender.kind === 'unavailable'
                              ? PLANNING_COPY.renderThumbUnavailable
                              : PLANNING_COPY.renderThumbFailed}
                          </Text>
                        )}
                        {item.recommended ? (
                          <Text className="plans__choice-badge">{PLANNING_COPY.recommended}</Text>
                        ) : null}
                      </View>
                    </View>
                  )
                })}
              </View>
              <View className="plans__chooser-info">
                <Text className="plans__name">{activeVariant.name}</Text>
                {activeVariant.difference_tags.length > 0 ? (
                  <View className="plans__diffs">
                    {activeVariant.difference_tags.slice(0, 3).map((tag) => (
                      <Text key={tag} className="plans__diff">{tag}</Text>
                    ))}
                  </View>
                ) : null}
              </View>
            </View>
            <PrimaryButton text={PLANNING_COPY.viewDetail} onClick={() => openDetail(activeVariant)} />
            <Text className="plans__cta-note">
              {activeRender.kind === 'ready' && activeRender.media.source_kind === 'demo_example'
                ? PLANNING_COPY.ctaNoteDemo
                : PLANNING_COPY.ctaNote}
            </Text>
            {/* 重新设计入口：场景回 Brief 页预填改答案；general 直接 refresh 重出，
                重拍仅作为更新档案的路径保留 */}
            {scene === 'general' ? (
              <>
                <TextLink
                  className="plans__redesign"
                  text={PLANNING_COPY.regenerateAction}
                  onClick={() => void generateGeneral(true)}
                />
                <TextLink
                  className="plans__redesign"
                  text={PLANNING_COPY.updateGeneralLink}
                  onClick={() => void Taro.navigateTo({ url: CAPTURE_ROUTE })}
                />
              </>
            ) : (
              <TextLink
                className="plans__redesign"
                text={PLANNING_COPY.redesignAction}
                onClick={() => void Taro.navigateTo({ url: `${SCENE_ROUTE}?scene=${scene}` })}
              />
            )}
          </View>
        ) : null}
        </>
      )}
    </View>
  )
}
