// 方案 tab：卡堆决策台——三套方案满幅堆叠，左滑跳过/右滑喜欢（按钮与手势
// 等价），栈空落结果态；下方往期方案区按「集」回装卡堆。对比滑块移出展示页
// （深看一套是详情页的事，展示页只管三选一）。
//
// 架构不让步的部分：
// 1. 不读 Storage 的 reportId / sceneBrief——入口只有「交接条」和「问服务端」；
//    deckHint 这类 UI 偏好仍走 services/storage（偏好不是业务状态）；
// 2. 唯一轮询路径是 useOperationPolling，盯受理 operation 与各套在途渲染，
//    settled 后整体刷新方案集；刷新期间继续显示当前 PlanSet，到达后整体替换；
//    卡堆的决策/撤销在刷新后以本地栈为准（渲染内容经 freshById 跟上服务端）；
// 3. 决策乐观落栈、PUT 失败本地回退——服务端是决策的事实来源，
//    重进页面 / 重取方案集都会以 variant.decision 恢复。
import { useCallback, useEffect, useRef, useState } from 'react'
import Taro, { useDidShow } from '@tarojs/taro'
import { ScrollView, Text, View } from '@tarojs/components'
import {
  EMPTY_COPY,
  ERROR_COPY,
  PLANNING_COPY,
  SCENES,
} from '@zsm/core'
import type { DisplayMedia, PlanSet, PlanVariant, Report } from '@zsm/core'
import { qualityApi } from '../../app/api/quality'
import { PublicApiError } from '../../app/api/result'
import { resourceCache, resourceKey } from '../../app/cache/resource-cache'
import { takePlanSetHandoff, type PlanSetHandoff } from '../../app/plan-set-handoff'
import { clearPlanSetPending, readPlanSetPending, writePlanSetPending } from '../../app/plan-set-pending'
import { useOperationPolling } from '../../app/operations/use-operation-polling'
import { useShowOnce } from '../../hooks/use-page-visibility'
import { handleBillingError } from '../../services/billing'
import { STORAGE_KEYS, readStorage, writeStorage } from '../../services/storage'
import EmptyState from '../../components/empty-state'
import ErrorState from '../../components/error-state'
import BottomSheet from '../../components/bottom-sheet'
import PlanProgressView from './PlanProgressView'
import PrimaryButton from '../../components/primary-button'
import PlansDeck from './PlansDeck'
import PlansHistory from './PlansHistory'
import Skeleton from '../../components/skeleton'
import {
  analyzingAssessmentOperationId,
  boundBodyMedia,
  briefFingerprint,
  createDecisionStack,
  createIdempotencyKey,
  deckOrder,
  decideCard,
  inFlightPlanSetOperationIds,
  planProgressView,
  planSetRetryMarkerKey,
  planSetSceneKey,
  planSetView,
  topCard,
  undoCard,
  sortedVariants,
  type DecisionKind,
  type DecisionStack,
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

const EMPTY_STACK: DecisionStack = { cards: [], cursor: 0, history: [] }

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
  // 往期方案区：当前报告 + 场景下的全部方案集（含当前集），日期倒序。
  // 旧实现只留最新一份、旧集沉没——历史闭环就是把这份全量接住。
  const [sets, setSets] = useState<PlanSet[]>([])
  const [scene, setScene] = useState<string>('general')
  const [bootstrapped, setBootstrapped] = useState(Boolean(planSetId))
  const [analyzingOperationId, setAnalyzingOperationId] = useState('')
  // bootstrap 里在途的方案集受理 id（含别的场景提交的）：进轮询与「制作中」呈现
  const [activePlanSetOps, setActivePlanSetOps] = useState<string[]>([])
  const [failed, setFailed] = useState(false)
  // 切场景请求在途：tab 已高亮、数据未到期间给内联指示，不让旧内容冒充新场景
  const [switching, setSwitching] = useState(false)
  // 卡堆决策状态机 + 网络在途；deckHint 是「左滑跳过 · 右滑喜欢」的一次性提示
  const [stack, setStack] = useState<DecisionStack>(EMPTY_STACK)
  const [deciding, setDeciding] = useState(false)
  const [retryingId, setRetryingId] = useState('')
  const [deckHint, setDeckHint] = useState(() => readStorage(STORAGE_KEYS.deckHint) === '')
  // 往期弹层开关（历史收进 BottomSheet，不占文档流）
  const [historyOpen, setHistoryOpen] = useState(false)
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
  const decidingRef = useRef(false)
  decidingRef.current = deciding
  const stackRef = useRef<DecisionStack>(EMPTY_STACK)
  stackRef.current = stack
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

  /**
   * 认领在途受理（冷进入专用）：进程重启后内存交接条与受理归属 ref 全没了，
   * bootstrap 只说「有个方案集在制作中」，不说它属于哪个场景。回执补上场景与
   * 方案集 id，让在途受理能被摆回它的场景——纸样台进度动画与「不给生成按钮」
   * 都靠这一次认领。是否在途只认服务端在途清单，回执不参与事实判断。
   */
  const claimPendingAccept = useCallback((inFlightIds: readonly string[]): boolean => {
    // 已经盯上了一份受理（交接条/路由/本次已认领）：不抢
    if (acceptOperationIdRef.current && acceptPendingRef.current) return false
    const ticket = readPlanSetPending()
    if (!ticket) return false
    // 这次对账没拿到在途清单（bootstrap 里 getHomeBootstrap 被 catch）：
    // 既不能认领（无法确认还在途），也不能据此清回执
    if (inFlightIds.length === 0) return false
    if (!inFlightIds.includes(ticket.operationId)) {
      // 服务端不再把它算在途：这份回执已经过期，清掉免得下次还来问
      clearPlanSetPending()
      return false
    }
    setAcceptOperationId(ticket.operationId)
    setAcceptPending(true)
    setPlanSetId(ticket.planSetId)
    const claimedScene = ticket.scene || 'general'
    acceptSceneRef.current = claimedScene
    acceptPlanSetIdRef.current = ticket.planSetId
    if (ticket.scene) setScene(ticket.scene)
    setBootstrapped(true)
    return true
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
      const inFlight = inFlightPlanSetOperationIds(boot?.active_operations ?? [])
      setActivePlanSetOps(inFlight)
      const claimed = claimPendingAccept(inFlight)
      if (!current) {
        setAnalyzingOperationId(analyzingAssessmentOperationId(boot?.active_operations ?? []))
        setReport(null)
        setBootstrapped(true)
        return
      }
      setReport(current)
      const list = await qualityApi.listPlanSets(current.id, 'general')
      setSets([...list].sort((a, b) => b.created_at.localeCompare(a.created_at)))
      const latest = [...list].sort((a, b) => b.created_at.localeCompare(a.created_at))[0]
      // 认领成功 = 用户在等一份还没发布的集：不许把上一份已发布集顶到前台，
      // 那会把纸样台的进度动画换成旧方案（和受理刚提交时看到的不一样）
      if (latest && !claimed) {
        setPlanSetId(latest.id)
        setPlanSet(latest)
        await refreshPlanSet(latest.id)
      }
      setBootstrapped(true)
    } catch {
      setFailed(true)
      setBootstrapped(true)
    }
  }, [claimPendingAccept, refreshPlanSet])

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
      // general 不写场景侧信道（没有 Brief 页），没读到就是 general；
      // 留空会让受理在途的纸样台落在别的分支上（进度动画不展示）
      const acceptScene = handoffScene || 'general'
      // 受理在途才记场景与归属 id；200 复用（没有任务在跑）不算受理
      acceptSceneRef.current = next.operationId ? acceptScene : ''
      acceptPlanSetIdRef.current = next.operationId ? next.planSetId : ''
      if (next.operationId) {
        // 回执：跨进程重启也能把这份在途受理认领回它的场景
        writePlanSetPending({
          operationId: next.operationId,
          planSetId: next.planSetId,
          scene: acceptScene,
        })
        return
      }
      // 200 复用（没有任务在跑）：清回执 + 立刻对账展示已发布集
      clearPlanSetPending()
      void refreshPlanSet(next.planSetId)
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

  const {
    operations,
    refresh: refreshOperations,
  } = useOperationPolling({
    operationIds: watchedIds,
    enabled: watchedIds.length > 0,
    onSettled: (operations) => {
      // 受理 operation 终态失败：方案集永远不会发布，刷新只会再拿 404。
      // 把公开失败文案直接上屏，给「重新生成」而不是误导性的网络错误。
      const acceptId = acceptOperationIdRef.current
      const accept = acceptId ? operations.find((op) => op.id === acceptId) : undefined
      // 受理已到终态（成功或失败）：回执作废，别让下次冷启动认领一份已完成的受理
      if (accept) clearPlanSetPending()
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
            const sorted = [...list].sort((a, b) => b.created_at.localeCompare(a.created_at))
            setSets(sorted)
            const latest = sorted[0]
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

  // 卡堆装载：方案集换身份（受理新集/切场景/装回往期）时整体重建，
  // 服务端已决（variant.decision）随之恢复；同集刷新不重建——会话内的
  // 撤销栈和乐观决策不被后台对账掀掉，渲染内容经 freshById 跟上。
  const planSetKeyRef = useRef('')
  useEffect(() => {
    const key = planSet?.id ?? ''
    if (key === planSetKeyRef.current) return
    planSetKeyRef.current = key
    setStack(planSet ? createDecisionStack(deckOrder(planSet)) : EMPTY_STACK)
  }, [planSet])

  /** 切场景：高亮先切、旧内容保留到响应到达；在途给内联指示，乱序响应不得盖回。 */
  const switchScene = async (next: string) => {
    const req = ++sceneReqRef.current
    setScene(next)
    if (!report) return
    setSwitching(true)
    try {
      const list = await qualityApi.listPlanSets(report.id, next as PlanSet['scene'])
      if (req !== sceneReqRef.current) return
      const sorted = [...list].sort((a, b) => b.created_at.localeCompare(a.created_at))
      setSets(sorted)
      const latest = sorted[0]
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
    if (!report) {
      // 报告还没到手（bootstrap 未回 / 刚因 404 被重置）：静默 return 就是「点了没反应」，
      // 重新对账一次并说明（对账到了用户再点一次即可）
      void bootstrap()
      Taro.showToast({ title: PLANNING_COPY.loadFailed, icon: 'none' })
      return
    }
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
        // 回执：退出小程序再进来时，这份受理还能被认领回 general 场景
        writePlanSetPending({
          operationId: start.operation.id,
          planSetId: start.data.id,
          scene: 'general',
        })
      } else {
        // 复用已发布方案集：没有任务在跑，旧的受理 id 必须清掉，
        // 否则轮询会盯上那份已终态的 operation 把失败卡又顶回来
        setAcceptOperationId('')
        setAcceptPending(false)
        acceptSceneRef.current = ''
        acceptPlanSetIdRef.current = ''
        setPlanSetId(start.planSet.id)
        clearPlanSetPending()
      }
      setBootstrapped(true)
    } catch (error) {
      // 402/429 等计费错误先走购买引导（弹层→标记→profile 购买层），其余才落通用提示
      if (handleBillingError(error)) return
      // report id 失效（服务端重置/报告被替换）：陈旧 state 必须丢掉重新对账，
      // 不然空态/失败卡上的按钮会一直拿着死 id 撞 404
      if (error instanceof PublicApiError && error.statusCode === 404) {
        setReport(null)
        setPlanSet(null)
        setPlanSetId('')
        void bootstrap()
        return
      }
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

  // ---------- 卡堆决策：乐观落栈，失败回退；服务端是事实来源 ----------

  const handleDecide = (variantId: string, decision: DecisionKind) => {
    if (decidingRef.current) return
    const top = topCard(stackRef.current)
    // 手势回调在飞出动画后到达，此间用户可能已用按钮决策过：顶卡对不上就丢弃
    if (!top || top.variant.id !== variantId) return
    setStack(decideCard(stackRef.current, decision))
    setDeciding(true)
    void (async () => {
      try {
        await qualityApi.putVariantDecision(variantId, decision, createIdempotencyKey(`decision:${variantId}`))
        // 第一次决策顺手收掉手势提示（与 compareHint 同一条 UI 偏好规则）
        if (readStorage(STORAGE_KEYS.deckHint) === '') writeStorage(STORAGE_KEYS.deckHint, '1')
        setDeckHint(false)
      } catch (error) {
        if (handleBillingError(error)) {
          setStack(undoCard(stackRef.current))
          return
        }
        // 保存失败：本地回退这一张（服务端为准），下次进来还是未决
        setStack(undoCard(stackRef.current))
        Taro.showToast({ title: PLANNING_COPY.deckDecisionFailed, icon: 'none' })
      } finally {
        setDeciding(false)
      }
    })()
  }

  const handleUndo = () => {
    if (decidingRef.current) return
    const current = stackRef.current
    const index = current.history[current.history.length - 1]
    const target = index === undefined ? undefined : current.cards[index]
    if (!target) return
    setStack(undoCard(current))
    setDeciding(true)
    void (async () => {
      try {
        await qualityApi.deleteVariantDecision(target.variant.id)
      } catch {
        // 撤销没存上：本地已回退，但服务端还留着旧决策——重进页面会恢复
        Taro.showToast({ title: PLANNING_COPY.deckDecisionFailed, icon: 'none' })
      } finally {
        setDeciding(false)
      }
    })()
  }

  /** 装回往期集：列表里是全量图，直接换当前集；后台再对账一次签名 URL。 */
  const loadPast = (setId: string) => {
    if (setId === planSetIdRef.current) return
    const target = sets.find((set) => set.id === setId)
    if (!target) return
    setPlanSetId(setId)
    setPlanSet(target)
    void refreshPlanSet(setId)
  }

  const backToLatest = () => {
    const latest = sets[0]
    if (latest) loadPast(latest.id)
  }

  /** 结果态/说明行的重新生成：场景方案回 Brief 页改答案，general refresh 重出。 */
  const regenerateActive = () => {
    if (planSet && planSet.scene !== 'general') {
      void Taro.navigateTo({ url: `${SCENE_ROUTE}?scene=${planSet.scene}` })
      return
    }
    void generateGeneral(true)
  }

  const dismissDeckHint = () => {
    setDeckHint(false)
    writeStorage(STORAGE_KEYS.deckHint, '1')
  }

  // 对比左图：绑定校验不过就抛错——这里接住并退单图（与 PlanDetailScreen 同一条规则）。
  // 展示页不再摆对比交互，这张图只做两件事：规划等待的照片锚 + 未就绪卡的占位。
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

  // 生成中的视觉 = 纸样台：竖裁缝尺 + 三条参差版型条 + 出血宋体「3」
  // （构图/配色/数字规律见 index.scss .plans__atelier 注释）
  const atelierCard = (
    <View className="plans__scene-empty plans__scene-empty--atelier">
      <View className="plans__atelier">
        <View className="plans__atelier-gauge">
          <Text className="plans__atelier-no plans__atelier-no--1">01</Text>
          <Text className="plans__atelier-no plans__atelier-no--2">02</Text>
          <Text className="plans__atelier-no plans__atelier-no--3">03</Text>
        </View>
        <Text className="plans__atelier-numeral">3</Text>
        <View className="plans__atelier-needle" />
        <View className="plans__atelier-slots">
          <View className="plans__atelier-slot plans__atelier-slot--1">
            <View className="plans__atelier-fill" />
            <View className="plans__atelier-stitch" />
          </View>
          <View className="plans__atelier-slot plans__atelier-slot--2">
            <View className="plans__atelier-fill" />
            <View className="plans__atelier-stitch" />
          </View>
          <View className="plans__atelier-slot plans__atelier-slot--3">
            <View className="plans__atelier-fill" />
            <View className="plans__atelier-stitch" />
          </View>
        </View>
      </View>
      <View className="plans__atelier-foot">
        <View className="plans__atelier-rule" />
        <Text className="plans__atelier-text">{PLANNING_COPY.sceneGenerating}</Text>
      </View>
    </View>
  )

  // 已建档但当前场景还没有方案集：保留场景 tab，只替换内容区——
  // 受理失败的给失败卡（公开文案 + 重新生成），在途的给纸样台进度动画，
  // 切换中的给骨架，其余才给该场景的空态卡（含「生成形象方案」）。
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
          // 认不出归属的在途受理由下面的兜底分支接住。
          atelierCard
        ) : planSetId || switching ? (
          <Skeleton rows={3} />
        ) : foreignPlanSetOps.length > 0 ? (
          // 服务端确实有方案集在制作中，只是认不出它属于哪个场景（换设备 /
          // 清了缓存，回执不在了）：给纸样台而不是空态卡——空态卡上的「生成」
          // 会把用户引向重复提交。落定后 refreshInFlightOps 清空，空态卡自己回来。
          atelierCard
        ) : (
          sceneEmptyCard
        )}
      </View>
    )
  }

  // ---------- planning：整屏等待，不虚构方案卡 ----------
  if (view?.kind === 'planning') {
    // 只把方案集受理相关的快照交给进度视图；单套渲染的在途不进这里
    const candidateIds = new Set([acceptOperationId, ...activePlanSetOps])
    const snapshot = planProgressView(operations.filter((operation) => candidateIds.has(operation.id)))
    // 照片锚：优先绑定校验过的对比左图；等待期方案集未发布拿不到绑定，
    // 退当前报告的身体照（纯展示锚，不参与任何对比/证据语义）
    const progressMedia = leftMedia ?? report?.source_media?.body?.media ?? null
    return (
      <View className="planning-screen">
        <PlanProgressView
          media={progressMedia}
          snapshot={snapshot}
          onWander={() => void Taro.switchTab({ url: HOME_ROUTE })}
        />
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

  // ---------- 卡堆决策台 + 往期方案区 ----------
  // 当前集在往期列表里不再是第一份 → 往期浏览态（徽标 + 回到最新）。
  const viewingPast = sets.length > 0 && planSetId !== sets[0]?.id

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
          <PlansDeck
            stack={stack}
            variants={variants}
            fallbackMedia={leftMedia}
            pastBadge={viewingPast}
            pastCount={Math.max(0, sets.length - 1)}
            hintVisible={deckHint}
            retryingId={retryingId}
            busy={deciding}
            onDecide={handleDecide}
            onUndo={handleUndo}
            onHintDismiss={dismissDeckHint}
            onOpenHistory={() => setHistoryOpen(true)}
            onBackToLatest={backToLatest}
            onOpenDetail={openDetail}
            onRegenerate={() => void regenerateActive()}
            onRetryRender={(variant) => void retryVariant(variant)}
          />
          {/* 往期收进弹层：历史常驻文档流会让页面总高必然超过一屏（滚动的主因），
              页面锁死后历史只能从「往期 N ›」入口进 */}
          <BottomSheet
            open={historyOpen}
            title={PLANNING_COPY.historyTitle}
            onClose={() => setHistoryOpen(false)}
          >
            <PlansHistory
              sets={sets}
              activeSetId={planSetId}
              onSelect={(setId) => {
                setHistoryOpen(false)
                loadPast(setId)
              }}
            />
          </BottomSheet>
        </>
      )}
    </View>
  )
}
