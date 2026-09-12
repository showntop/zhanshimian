// 方案 Tab：选择页重设计——上半屏回答「有几个选项、选它能得到什么、怎么选」。
// 拖动对比 hero（收益词叠加）+ 三选一条常驻 + 吸底 CTA；细节文案在折叠线下。
import { useCallback, useEffect, useRef, useState } from 'react'
import Taro from '@tarojs/taro'
import { ScrollView, Text, View } from '@tarojs/components'
import { EMPTY_COPY, POLL_INTERVALS, useTaskPolling, type Plan } from '@zsm/core'
import { usePageShell, useShowOnce } from '../../hooks/use-page-visibility'
import { api } from '../../services/api'
import { analysisPageUrl, isAnalysisRunning, resolveRunningAnalysisId } from '../../services/task-utils'
import { STORAGE_KEYS, readStorage, writeStorage } from '../../services/storage'
import AppHeader, { getNavMetrics } from '../../components/app-header'
import PrimaryButton from '../../components/primary-button'
import ExampleImage from '../../components/example-image'
import CompareSlider from '../../components/compare-slider'
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
  { key: 'gathering', label: '聚会' },
] as const

// hero 高度跟随照片比例：三套方案图同一管线产出、宽高比一致，
// 切换不跳动，因此可以让照片自己定高度——满宽 + 完整，无侧边区。
// 上限 = 视口剩余（防极端竖图），下限 320px。
// 收益词 + 三选一 + CTA 收进毛玻璃悬浮坞，不占文档流。
const NAV = getNavMetrics()
const HEADER_GAP_PX = 24 // spacer margin-bottom 48rpx
const TABS_PX = 40 // 场景 tab 行 + 容器间距
const MAX_HERO_PX = Math.max(
  320,
  Math.round(NAV.windowHeight - NAV.navHeight - HEADER_GAP_PX - TABS_PX),
)

export default function Plans() {
  const [scene, setScene] = useState<string>('general')
  const [plans, setPlans] = useState<Plan[]>([])
  const [currentImage, setCurrentImage] = useState('')
  const [index, setIndex] = useState(0)
  const [whyOpen, setWhyOpen] = useState(false)
  const [compareHint, setCompareHint] = useState(() => !readStorage('zsm.compareHint'))
  // 方案图真实宽高（onLoad 采集，决定 hero 高度）
  const [photoDims, setPhotoDims] = useState<Record<string, { w: number; h: number }>>({})
  const [loading, setLoading] = useState(true)
  const [failed, setFailed] = useState(false)
  // 区分「未建档」与「该场景无方案」：两者空态与 CTA 完全不同，
  // 此前一律按未建档处理，导致已建档用户切场景时被错误引导去重新建档
  const [hasReport, setHasReport] = useState(true)
  const [analyzing, setAnalyzing] = useState(false)
  // general 方案组生成任务（报告与方案解耦后由 plan_group 任务产出）
  const [groupTaskId, setGroupTaskId] = useState('')
  const plansRef = useRef<Plan[]>([])
  plansRef.current = plans
  const { pageClass, enter } = usePageShell(!loading || plans.length > 0, 'page--tab', 'plans')
  const activeLook = plans.find(
    (p, i) =>
      i === index &&
      p.look_task &&
      (p.look_task.status === 'queued' || p.look_task.status === 'processing'),
  )

  const load = useCallback(async (targetScene: string) => {
    // 已有方案时后台校验，不清空、不进骨架，避免切 tab 卸掉已渲染图
    if (plansRef.current.length === 0) {
      setLoading(true)
      setFailed(false)
    }
    try {
      const reportId = readStorage(STORAGE_KEYS.reportId)
      if (!reportId) {
        const [current, running] = await Promise.all([api.getCurrentReport(), isAnalysisRunning()])
        if (!current) {
          setHasReport(false)
          setAnalyzing(running)
          setPlans([])
          return
        }
        writeStorage(STORAGE_KEYS.reportId, current.id)
      }
      setHasReport(true)
      setAnalyzing(false)
      const items = await api.listPlans(readStorage(STORAGE_KEYS.reportId)!, targetScene)
      setPlans(items)
      // 报告与方案解耦：general 空组时接管已在进行的 plan_group 生成任务
      // （可能由报告页 CTA 或本页触发），直接进入生成中视图。
      if (targetScene === 'general' && items.length === 0) {
        const boot = await api.getHomeBootstrap().catch(() => null)
        const groupTask = (boot?.active_tasks ?? []).find(
          (t) => t.type === 'plan_group' && (t.status === 'queued' || t.status === 'processing'),
        )
        setGroupTaskId(groupTask?.id ?? '')
      }
    } catch {
      if (plansRef.current.length === 0) setFailed(true)
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

  useShowOnce(() => {
    load(scene)
  })

  // 存在进行中的 look 任务 → 轮询方案列表自身（间隔单源 @zsm/core）
  const hasActiveLook = plans.some(
    (p) => p.look_task && (p.look_task.status === 'queued' || p.look_task.status === 'processing'),
  )
  const activeLookCount = plans.filter(
    (p) => p.look_task && (p.look_task.status === 'queued' || p.look_task.status === 'processing'),
  ).length

  // Tab badge 推模型：任务创建点立即标数（此前仅首页拉取驱动，
  // 首页不可见时红点要等回到首页才出现）；离开本页后由首页拉取模型接管
  useEffect(() => {
    const count = groupTaskId ? 1 : activeLookCount
    if (count > 0) {
      Taro.setTabBarBadge({ index: 1, text: String(count) }).catch(() => {})
    } else {
      Taro.removeTabBarBadge({ index: 1 }).catch(() => {})
    }
  }, [groupTaskId, activeLookCount])

  const { stop } = useTaskPolling({
    fetcher: () => api.listPlans(readStorage(STORAGE_KEYS.reportId)!, scene),
    intervalMs: POLL_INTERVALS.planLook,
    enabled: hasActiveLook && plans.length > 0,
    onDone: () => {
      load(scene)
    },
  })
  Taro.useDidHide(() => stop())

  // plan_group 生成任务轮询：完成刷新列表，失败回退到空态引导重试
  const groupPoll = useTaskPolling({
    fetcher: () => api.getTask(groupTaskId),
    intervalMs: POLL_INTERVALS.planLook,
    enabled: Boolean(groupTaskId),
    onDone: (result) => {
      setGroupTaskId('')
      if (result.status === 'completed') {
        load(scene)
      } else {
        Taro.showToast({ title: '方案暂时没有生成，请重试', icon: 'none' })
      }
    },
  })
  Taro.useDidHide(() => groupPoll.stop())

  const plan = plans[index]
  const planImage = plan?.generated_image_url || plan?.image_url
  const isDemoLook = (plan?.look_provider ?? '').startsWith('demo')

  // hero 高度 = 屏宽 × 照片高宽比（满宽完整展示），封顶视口剩余；
  // 尺寸未就绪时按 82% 上限预估，onLoad 后校正
  const planDims = plan ? photoDims[plan.id] : undefined
  const heroPx = planDims
    ? Math.min(MAX_HERO_PX, Math.round((NAV.windowWidth * planDims.h) / planDims.w))
    : Math.round(MAX_HERO_PX * 0.82)

  const switchScene = (key: string) => {
    setScene(key)
    setIndex(0)
    setWhyOpen(false)
  }

  // 切换方案：轻震动反馈（沿用原 swiper 手势的触感），收起 why 展开
  const pickPlan = (i: number) => {
    if (i === index) return
    setIndex(i)
    setWhyOpen(false)
    Taro.vibrateShort({ type: 'light' })
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
      <View className={pageClass}>
        <AppHeader />
        <Skeleton rows={5} />
      </View>
    )
  }

  if (failed) {
    return (
      <View className={pageClass}>
        <AppHeader />
        <ErrorState onRetry={() => load(scene)} />
      </View>
    )
  }

  // 空态分三层：分析进行中 → 回进度页；未建档 → 去建档；
  // 已建档但该场景无方案 → 保留场景 tab，引导生成该场合方案
  if (plans.length === 0 && !hasReport) {
    const empty = analyzing ? EMPTY_COPY.plansAnalyzing : EMPTY_COPY.plansNeedArchive
    return (
      <View className={pageClass}>
        <AppHeader />
        <EmptyState
          title={empty.title}
          description={empty.body}
          actionText={empty.action}
          onAction={() => {
            if (!analyzing) {
              Taro.navigateTo({ url: '/pages/capture/index' })
              return
            }
            void resolveRunningAnalysisId().then((id) => {
              Taro.navigateTo({ url: analysisPageUrl(id) })
            })
          }}
        />
      </View>
    )
  }

  const sceneLabel = SCENE_TABS.find((t) => t.key === scene)?.label ?? ''
  const generateScenePlan = async () => {
    if (scene === 'general') {
      const reportId = readStorage(STORAGE_KEYS.reportId)
      if (!reportId) return
      try {
        // 202 + task = 方案组生成已启动（AI 从报告内容派生三套方案）
        const res = await api.upsertPlans(reportId, { scene: 'general', answers: {} })
        if (res.task) {
          setGroupTaskId(res.task.id)
          return
        }
        load(scene)
      } catch {
        Taro.showToast({ title: '方案暂时没有生成，请稍后重试', icon: 'none' })
      }
      return
    }
    Taro.navigateTo({ url: `/pages/scene/index?scene=${scene}` })
  }

  return (
    <View className={pageClass}>
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

        {plans.length === 0 ? (
          groupTaskId ? (
            <View className={`plans__scene-empty ${enter()}`}>
              <View className="plans__generating">
                <View className="plans__generating-spin spinner" />
                <Text className="plans__generating-text">正在从你的报告生成三套方案，通常需要 1-2 分钟</Text>
              </View>
            </View>
          ) : (
            <View className={`plans__scene-empty ${enter()}`}>
              <Text className="plans__scene-empty-title">{scene === 'general' ? '还没有形象方案' : `${sceneLabel}场合还没有方案`}</Text>
              <Text className="plans__scene-empty-desc">
                {scene === 'general' ? '基于你的形象档案生成三套方案' : '回答 4 个选择（约 30 秒），复用档案不重复要照片'}
              </Text>
              <PrimaryButton
                text={scene === 'general' ? '生成形象方案' : `生成${sceneLabel}方案`}
                onClick={generateScenePlan}
              />
            </View>
          )
        ) : (
          <>
        {/* 拖动对比 hero：照片满宽完整展示（高度跟随照片比例），
            无侧边区无裁切。key 随方案切换重挂载 → 交叉淡入 */}
        <View className="plans__hero">
          <View
            className="plans__hero-frame"
            style={{ height: `${heroPx}px` }}
            key={plan?.id}
            onClick={() => {
              if (!compareHint) return
              setCompareHint(false)
              writeStorage('zsm.compareHint', '1')
            }}
          >
            <CompareSlider
              single={!currentImage || !planImage || scene !== 'general'}
              current={<ExampleImage className="plans__hero-img" src={currentImage} user mode="widthFix" />}
              plan={
                <ExampleImage
                  className="plans__hero-img"
                  src={planImage}
                  badgeText={isDemoLook ? '效果示例' : 'AI 风格预览'}
                  mode="widthFix"
                  onLoad={(e) => {
                    const w = Number(e.detail.width)
                    const h = Number(e.detail.height)
                    if (!w || !h || !plan) return
                    setPhotoDims((prev) =>
                      prev[plan.id]?.w === w && prev[plan.id]?.h === h
                        ? prev
                        : { ...prev, [plan.id]: { w, h } },
                    )
                  }}
                />
              }
            />
            {compareHint && scene === 'general' && currentImage && planImage ? (
              <Text className="plans__hint">左右拖动，看原本和方案</Text>
            ) : null}
            <View className="plans__hero-fade" />
          </View>
        </View>

        {/* 细节区（第 2 屏起）：descriptor + 折叠的 why + 生成/重试状态 */}
        {plan ? (
          <View className={`plans__info ${enter(1)}`}>
            <Text className="plans__summary">{plan.descriptor}</Text>
            {plan.why ? (
              <View className="plans__why-wrap" onClick={() => setWhyOpen(!whyOpen)}>
                <Text className={`plans__why ${whyOpen ? 'plans__why--open' : ''}`}>{plan.why}</Text>
                <Text className="plans__why-toggle">{whyOpen ? '收起' : '为什么适合你'}</Text>
              </View>
            ) : null}
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

        {/* 悬浮选择坞：收益词 + 三选一 + CTA 收进毛玻璃坞，浮在照片底部
            上方不占文档流——照片有多高就展示多高，选择要素常驻第一屏 */}
        {plan ? (
          <View className={`plans__dock dock-glass ${enter(2)}`}>
            {(plan.outcome_tags ?? []).length > 0 ? (
              <View className="plans__outcome">
                {plan.outcome_tags.slice(0, 3).map((tag) => (
                  <Text key={tag} className="plans__outcome-tag">{tag}</Text>
                ))}
              </View>
            ) : null}
            <View className="plans__chooser">
              <View className="plans__choices">
                {plans.map((item, i) => (
                  <View
                    key={item.id}
                    className={`plans__choice ${i === index ? 'plans__choice--active' : ''} pressable`}
                    onClick={() => pickPlan(i)}
                  >
                    <View className="plans__choice-thumb">
                      <ExampleImage
                        className="plans__choice-img"
                        src={item.generated_image_url || item.image_url}
                        mode="aspectFit"
                      />
                      {item.recommended ? <Text className="plans__choice-badge">推荐</Text> : null}
                    </View>
                    <Text className="plans__choice-name">{item.name}</Text>
                  </View>
                ))}
              </View>
              <View className="plans__chooser-info">
                <Text className="plans__name">{plan.name}</Text>
                {(plan.difference_tags ?? []).length > 0 ? (
                  <View className="plans__diffs">
                    {plan.difference_tags.slice(0, 3).map((tag) => (
                      <Text key={tag} className="plans__diff">{tag}</Text>
                    ))}
                  </View>
                ) : null}
              </View>
            </View>
            <PrimaryButton
              text="选这套 · 查看执行清单"
              onClick={() => Taro.navigateTo({ url: `/pages/plan/index?id=${plan.id}` })}
            />
            <Text className="plans__cta-note">
              {isDemoLook ? '当前为效果示例，接入真实图像模型后展示本人效果' : '形象图由 AI 基于你的照片生成'}
            </Text>
          </View>
        ) : null}
          </>
        )}
      </View>
    </View>
  )
}
