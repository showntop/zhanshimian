// 首页工作台：新用户/回访双状态。
// 请求纪律：单次 /v1/home/bootstrap 聚合；仅当有活跃任务且页面可见时批量轮询(1.5s)。
// 重设计 IA：问候 → 今日造型/档案 hero → 工具 → 场景 → 最近方案。
// 进行中任务不占首页内容流：分析走建档按钮、工具卡徽章、方案 Tab badge；完整列表在「我的」。
import { memo, useCallback, useEffect, useRef, useState } from 'react'
import Taro, { useDidShow } from '@tarojs/taro'
import { Image, Text, View } from '@tarojs/components'
import {
  APP_NAME,
  APP_SLOGAN,
  HOME_COPY,
  HOME_TITLE,
  OUTFIT_COPY,
  PURCHASE_COPY,
  POLL_INTERVALS,
  PRIVACY_NOTE,
  SCENES,
  greetingForNow,
  isBundledAsset,
  trackEvent,
  type HomeBootstrap,
  type SceneCopy,
} from '@zsm/core'
import { usePageShell } from '../../hooks/use-page-visibility'
import { api } from '../../services/api'
import { hasOutfitResult, isOutfitPending } from '../../services/outfit-session'
import { hasPurchaseResult, isPurchasePending } from '../../services/purchase-session'
import { taskDoneTitle } from '../../services/task-utils'
import { STORAGE_KEYS, readStorage, writeStorage } from '../../services/storage'
import AppHeader from '../../components/app-header'
import PrimaryButton from '../../components/primary-button'
import ExampleImage from '../../components/example-image'
import ErrorState from '../../components/error-state'
import Skeleton from '../../components/skeleton'
import './index.scss'

const TOOLS = [
  { key: 'hair', label: '发型预览', desc: '先看效果再决定', path: '/packages/tools/pages/hair/index', badge: '推荐' },
  { key: 'outfit', label: '穿搭诊断', desc: '只指出最值得改的一处', path: '/packages/tools/pages/outfit/index', badge: '' },
  { key: 'purchase', label: '购买判断', desc: '买之前先看适不适合', path: '/packages/tools/pages/purchase/index', badge: '' },
] as const

const SCENE_ICONS: Record<SceneCopy['id'], string> = {
  interview: '/assets/icons/scene-interview.png',
  wedding: '/assets/icons/scene-wedding.png',
  date: '/assets/icons/scene-date.png',
  daily: '/assets/icons/scene-daily.png',
  gathering: '/assets/icons/scene-gathering.png',
}

const SceneTile = memo(function SceneTile({ scene }: { scene: SceneCopy }) {
  return (
    <View
      className={`home__scene home__scene--${scene.id} pressable`}
      onClick={() => Taro.navigateTo({ url: `/pages/scene/index?scene=${scene.id}` })}
    >
      <View className="home__scene-visual">
        <Image className="home__scene-icon" src={SCENE_ICONS[scene.id]} mode="aspectFit" lazyLoad={false} fadeIn={false} />
      </View>
      <View className="home__scene-copy">
        <Text className="home__scene-label">{scene.label}</Text>
        <Text className="home__scene-desc">{scene.note}</Text>
      </View>
    </View>
  )
})

/** 切 tab 回来若视图没变，禁止 setState，否则 Taro 会重写 <image src> 导致闪一下。 */
function homeViewKey(data: HomeBootstrap): string {
  const tasks = (data.active_tasks ?? []).map((t) => `${t.id}:${t.status}:${t.progress}:${t.stage}`)
  return [
    data.report?.id,
    data.report?.current_image_url,
    data.report?.priority_title,
    data.report?.priority_copy,
    data.today_plan?.id,
    data.today_plan?.generated_image_url,
    data.today_plan?.image_url,
    data.today_plan?.title,
    data.today_plan?.summary,
    data.recent_plan?.id,
    data.recent_plan?.generated_image_url,
    data.recent_plan?.image_url,
    data.recent_plan?.name,
    data.recent_plan?.why,
    tasks.join(','),
  ].join('|')
}

let cachedBootstrap: HomeBootstrap | null = null

// 复访闭环入口（life 分包）：顾问对话是产品核心特色，此前全站无入口
const LIFE = [
  { key: 'advisor', label: HOME_COPY.advisorEntry, desc: HOME_COPY.advisorEntryDesc, path: '/packages/life/pages/advisor/index' },
  { key: 'wardrobe', label: HOME_COPY.wardrobeEntry, desc: HOME_COPY.wardrobeEntryDesc, path: '/packages/life/pages/wardrobe/index' },
] as const

/** 今日语境日期（「今日造型」的时间锚点，非装饰） */
function todayLabel(): string {
  const d = new Date()
  const week = ['日', '一', '二', '三', '四', '五', '六'][d.getDay()] ?? ''
  return `${d.getMonth() + 1}月${d.getDate()}日 · 周${week}`
}

function lookBadge(lookProvider: string | undefined, generatedUrl: string | undefined): string {
  if ((lookProvider ?? '').startsWith('demo')) return '效果示例'
  if (generatedUrl) return 'AI 风格预览'
  return '风格参考'
}

export default function Home() {
  const [bootstrap, setBootstrap] = useState<HomeBootstrap | null>(cachedBootstrap)
  const [loading, setLoading] = useState(!cachedBootstrap)
  const [failed, setFailed] = useState(false)
  const [outfitActive, setOutfitActive] = useState(false)
  const [outfitReady, setOutfitReady] = useState(false)
  const [purchaseActive, setPurchaseActive] = useState(false)
  const [purchaseReady, setPurchaseReady] = useState(false)
  const recoveredRef = useRef(false)
  const visibleRef = useRef(true)
  const bootstrapRef = useRef<HomeBootstrap | null>(cachedBootstrap)
  const pollTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  bootstrapRef.current = bootstrap
  const { pageClass, enter } = usePageShell(!loading, 'page--tab', 'home')

  const load = useCallback(async () => {
    try {
      const data = await api.getHomeBootstrap()
      cachedBootstrap = data
      setBootstrap((prev) => (prev && homeViewKey(prev) === homeViewKey(data) ? prev : data))
      setFailed(false)
      // 聚合返回的最新报告落本地，供方案/清单等页复用
      if (data.report?.id) writeStorage(STORAGE_KEYS.reportId, data.report.id)
    } catch {
      // 已有内容时后台刷新失败不拆页，避免切 tab 闪到错误态
      if (!bootstrapRef.current) setFailed(true)
    } finally {
      setLoading(false)
    }
  }, [])

  // 缓存被清（换机/重装）时恢复报告引用
  const recoverReport = useCallback(async () => {
    if (readStorage(STORAGE_KEYS.reportId) || recoveredRef.current) return
    recoveredRef.current = true
    try {
      const report = await api.getCurrentReport()
      if (report?.id) writeStorage(STORAGE_KEYS.reportId, report.id)
    } catch {
      /* 无报告：保持新用户态 */
    }
  }, [])

  const scheduleTaskPoll = useCallback(() => {
    if (pollTimer.current) clearTimeout(pollTimer.current)
    const active = bootstrap?.active_tasks ?? []
    if (active.length === 0 || !visibleRef.current) return
    pollTimer.current = setTimeout(async () => {
      try {
        const tasks = await api.getTasks(active.map((t) => t.id))
        setBootstrap((prev) => {
          if (!prev) return prev
          const next = { ...prev, active_tasks: tasks }
          return homeViewKey(prev) === homeViewKey(next) ? prev : next
        })
        // 任一任务到达终态：轻提醒 + 整页聚合刷新一次
        if (tasks.some((t) => t.status === 'completed' || t.status === 'failed')) {
          const completed = tasks.find((t) => t.status === 'completed')
          const failedTask = tasks.find((t) => t.status === 'failed')
          if (completed) {
            Taro.showToast({ title: taskDoneTitle(completed), icon: 'none' })
          } else if (failedTask) {
            // 失败同样轻提醒：此前失败任务静默消失，用户完全不知道生成没完成
            Taro.showToast({ title: failedTask.error?.message || '生成没有完成，请重试', icon: 'none' })
          }
          load()
        }
      } catch {
        /* 轮询失败静默：下一轮重试 */
      }
      scheduleTaskPoll()
    }, POLL_INTERVALS.homeTasks)
  }, [bootstrap?.active_tasks, load])

  useEffect(() => {
    scheduleTaskPoll()
    return () => {
      if (pollTimer.current) clearTimeout(pollTimer.current)
    }
  }, [scheduleTaskPoll])

  useEffect(() => {
    if (!outfitActive) return
    const timer = setInterval(() => {
      if (!visibleRef.current || isOutfitPending()) return
      setOutfitActive(false)
      setOutfitReady(hasOutfitResult())
      Taro.showToast({ title: '穿搭诊断已完成', icon: 'none' })
    }, POLL_INTERVALS.homeTasks)
    return () => clearInterval(timer)
  }, [outfitActive])

  useEffect(() => {
    if (!purchaseActive) return
    const timer = setInterval(() => {
      if (!visibleRef.current || isPurchasePending()) return
      setPurchaseActive(false)
      setPurchaseReady(hasPurchaseResult())
      Taro.showToast({ title: '购买判断已完成', icon: 'none' })
    }, POLL_INTERVALS.homeTasks)
    return () => clearInterval(timer)
  }, [purchaseActive])

  useDidShow(() => {
    visibleRef.current = true
    trackEvent('page_view', { page: 'home' })
    const nextOutfitActive = isOutfitPending()
    const nextOutfitReady = hasOutfitResult()
    const nextPurchaseActive = isPurchasePending()
    const nextPurchaseReady = hasPurchaseResult()
    setOutfitActive((prev) => (prev === nextOutfitActive ? prev : nextOutfitActive))
    setOutfitReady((prev) => (prev === nextOutfitReady ? prev : nextOutfitReady))
    setPurchaseActive((prev) => (prev === nextPurchaseActive ? prev : nextPurchaseActive))
    setPurchaseReady((prev) => (prev === nextPurchaseReady ? prev : nextPurchaseReady))
    // 已有首屏就不要在 onShow 再拉 bootstrap：任何 setState 都可能让原生 image 重解码。
    if (!bootstrapRef.current) recoverReport().then(load)
    scheduleTaskPoll()
  })

  Taro.useDidHide(() => {
    visibleRef.current = false
    if (pollTimer.current) clearTimeout(pollTimer.current)
  })

  const report = bootstrap?.report
  const hasReport = Boolean(report?.id)
  const reportImage = report?.current_image_url ?? ''
  const reportImageIsDemo =
    Boolean(report?.provider_version?.startsWith('demo')) || isBundledAsset(reportImage)
  const recentPlan = bootstrap?.recent_plan
  const todayPlan = bootstrap?.today_plan
  const activeTasks = (bootstrap?.active_tasks ?? []).filter(
    (t) => t.status === 'queued' || t.status === 'processing',
  )
  const analysisActive = activeTasks.some((t) => t.type === 'analysis')
  const hairActive = activeTasks.some((t) => t.type === 'hair_preview')
  // 方案相关任务（方案组生成 + 每套形象图）都归到方案 Tab badge
  const planTaskCount = activeTasks.filter(
    (t) => t.type === 'plan_group' || t.type === 'plan_look' || t.type === 'today_look',
  ).length

  // 方案 Tab badge（A 方案）：方案/今日形象图任务进行中时在底部 Tab 标数，
  // 完成即清除；完整任务列表在「我的」页任务中心（C 方案）
  useEffect(() => {
    if (planTaskCount > 0) {
      Taro.setTabBarBadge({ index: 1, text: String(planTaskCount) }).catch(() => {})
    } else {
      Taro.removeTabBarBadge({ index: 1 }).catch(() => {})
    }
  }, [planTaskCount])

  return (
    <View className={pageClass}>
      <AppHeader transparent />
      {failed ? (
        <ErrorState onRetry={load} />
      ) : loading ? (
        <Skeleton rows={4} />
      ) : (
        <View className="home">
          <View className={`home__greeting ${enter()}`}>
            <View className="home__greeting-top">
              <Text className="home__greeting-kicker">{APP_SLOGAN}</Text>
              <Text className="home__greeting-date">{todayLabel()}</Text>
            </View>
            <Text className="home__greeting-title display">
              {hasReport ? `${greetingForNow()}，${HOME_COPY.returningTitle}` : HOME_TITLE}
            </Text>
          </View>

          {!hasReport ? (
            <View className={enter(1)}>
              <View className="home__hero home__hero--onboard card--hero halo">
                <View className="home__hero-top">
                  <View className="home__hero-copy">
                    <Text className="home__hero-eyebrow">{HOME_COPY.startArchive}</Text>
                    <Text className="home__hero-title">{HOME_COPY.archiveTitle}</Text>
                    <Text className="home__hero-desc">{HOME_COPY.archiveBody}</Text>
                  </View>
                  <View className="home__hero-preview">
                    <View className="home__preview-slice home__preview-slice--1">
                      <ExampleImage
                        className="home__preview-image"
                        slug="natural"
                        variant="portrait"
                        badgeText="风格参考"
                      />
                      <Text className="home__preview-caption">照片</Text>
                    </View>
                    <View className="home__preview-slice home__preview-slice--2">
                      <ExampleImage
                        className="home__preview-image"
                        slug="natural"
                        variant="report"
                        badgeText="风格参考"
                      />
                      <View className="home__preview-focus">
                        <View className="home__preview-focus-dot" />
                      </View>
                      <Text className="home__preview-caption">理解</Text>
                    </View>
                    <View className="home__preview-slice home__preview-slice--3">
                      <ExampleImage
                        className="home__preview-image"
                        slug="sharp"
                        variant="plan"
                        badgeText="风格参考"
                      />
                      <Text className="home__preview-caption">方案</Text>
                    </View>
                  </View>
                </View>
                <View className="home__steps">
                  {HOME_COPY.process.map((item, index) => (
                    <View key={item.title} className="home__step">
                      <Text className="home__step-index">{index + 1}</Text>
                      <View className="home__step-copy">
                        <Text className="home__step-title">{item.title}</Text>
                        <Text className="home__step-desc">{item.desc}</Text>
                      </View>
                    </View>
                  ))}
                </View>
                <PrimaryButton
                  text={analysisActive ? '正在分析，查看进度' : HOME_COPY.startAnalysis}
                  tone="onDark"
                  onClick={() =>
                    Taro.navigateTo({
                      url: analysisActive ? '/pages/analysis/index' : '/pages/capture/index',
                    })
                  }
                />
                <Text className="home__hero-note">{HOME_COPY.photoPrivacy}</Text>
              </View>
            </View>
          ) : todayPlan ? (
            <View>
              <View
                className="home__hero home__hero--today home__hero--photo card--hero pressable halo"
                onClick={() => Taro.navigateTo({ url: '/packages/life/pages/today/index' })}
              >
                <View className={`home__hero-copy ${enter(1)}`}>
                  <Text className="home__hero-eyebrow">今日造型</Text>
                  <Text className="home__hero-title">{todayPlan.title}</Text>
                  <Text className="home__hero-desc">{todayPlan.summary}</Text>
                  <Text className="home__hero-link">查看今天怎么穿 ›</Text>
                </View>
                <ExampleImage
                  className="home__hero-img"
                  src={todayPlan.generated_image_url || todayPlan.image_url}
                  badgeText={lookBadge(todayPlan.look_provider, todayPlan.generated_image_url)}
                />
              </View>
            </View>
          ) : (
            <View>
              <View
                className="home__hero home__hero--report card--hero pressable"
                onClick={() => Taro.navigateTo({ url: '/pages/report/index' })}
              >
                <View className="home__report-main">
                  <View className={`home__hero-copy ${enter(1)}`}>
                    <Text className="home__hero-eyebrow">{HOME_COPY.reportReady}</Text>
                    <Text className="home__hero-title">{report?.priority_title}</Text>
                    <Text className="home__hero-desc">{report?.priority_copy}</Text>
                  </View>
                  <View className="home__report-visual">
                    <ExampleImage
                      className="home__report-image"
                      src={reportImage}
                      user={!reportImageIsDemo}
                      mode="aspectFill"
                      badgeText={reportImageIsDemo ? '效果示例' : ''}
                    />
                    <View className="home__report-shade" />
                  </View>
                </View>
                <View className="home__report-action">
                  <Text className="home__report-action-label">{HOME_COPY.viewReport}</Text>
                  <Text className="home__report-action-meta">
                    <Text className="home__report-action-count">{(report?.findings ?? []).length}</Text>
                    个可提升点
                  </Text>
                </View>
              </View>
            </View>
          )}

          <View className={`home__section ${enter(2)}`}>
            <View className="home__section-head">
              <Text className="section-title">{HOME_COPY.toolsTitle}</Text>
              <View className="section-rule" />
            </View>
            <View className="home__tools">
              {TOOLS.map((tool) => {
                const badge =
                  tool.key === 'hair' && hairActive
                    ? '生成中'
                    : tool.key === 'outfit' && outfitActive
                      ? '诊断中'
                      : tool.key === 'outfit' && outfitReady
                        ? OUTFIT_COPY.lastResult
                        : tool.key === 'purchase' && purchaseActive
                          ? '判断中'
                          : tool.key === 'purchase' && purchaseReady
                            ? PURCHASE_COPY.lastResult
                            : (tool.badge || '')
                return (
                  <View
                    key={tool.key}
                    className={`home__tool pressable ${tool.key === 'hair' ? 'home__tool--lead' : ''}`}
                    onClick={() => Taro.navigateTo({ url: tool.path })}
                  >
                    <View className="home__tool-copy">
                      <Text className="home__tool-name">{tool.label}</Text>
                      <Text className="home__tool-desc">{tool.desc}</Text>
                    </View>
                    {tool.key === 'hair' ? (
                      <ExampleImage
                        className="home__tool-visual"
                        slug="natural"
                        variant="hair"
                        badgeText="风格参考"
                        mode="aspectFit"
                      />
                    ) : null}
                    {badge ? (
                      <Text className={`home__tool-badge ${badge === '生成中' || badge === '诊断中' || badge === '判断中' ? 'home__tool-badge--live' : ''}`}>
                        {badge}
                      </Text>
                    ) : null}
                  </View>
                )
              })}
            </View>
          </View>

          <View className="home__section">
            <View className={`home__section-head ${enter(3)}`}>
              <Text className="section-title">{HOME_COPY.scenesTitle}</Text>
              <View className="section-rule" />
            </View>
            <View className="home__scenes">
              {SCENES.map((scene) => (
                <SceneTile key={scene.id} scene={scene} />
              ))}
            </View>
            {hasReport ? <Text className="home__scene-note">{HOME_COPY.sceneReadyNote}</Text> : null}
          </View>

          {recentPlan ? (
            <View className="home__section">
              <View className={`home__section-head ${enter(3)}`}>
                <Text className="section-title">{HOME_COPY.recentTitle}</Text>
                <View className="section-rule" />
              </View>
              <View
                className="home__recent card pressable"
                onClick={() => Taro.navigateTo({ url: `/pages/plan/index?id=${recentPlan.id}` })}
              >
                <ExampleImage
                  className="home__recent-img"
                  src={recentPlan.generated_image_url || recentPlan.image_url}
                  badgeText={lookBadge(recentPlan.look_provider, recentPlan.generated_image_url)}
                />
                <View className={`home__recent-copy ${enter(3)}`}>
                  <Text className="home__recent-name">{recentPlan.name}</Text>
                  <Text className="home__recent-why">{recentPlan.why}</Text>
                </View>
                <Text className="home__recent-arrow">›</Text>
              </View>
            </View>
          ) : null}

          <View className={`home__section ${enter(3)}`}>
            <View className="home__section-head">
              <Text className="section-title">{HOME_COPY.lifeTitle}</Text>
              <View className="section-rule" />
            </View>
            <View className="home__life">
              {LIFE.map((item) => (
                <View
                  key={item.key}
                  className={`home__life-item pressable ${item.key === 'advisor' ? 'home__life-item--advisor' : ''}`}
                  onClick={() => Taro.navigateTo({ url: item.path })}
                >
                  <View className="home__life-item-copy">
                    <Text className="home__life-item-label">{item.label}</Text>
                    <Text className="home__life-item-desc">{item.desc}</Text>
                  </View>
                  <Text className="home__life-item-arrow">›</Text>
                </View>
              ))}
            </View>
          </View>

          <View className={`home__foot ${enter(3)}`}>
            <View className="home__foot-rule" />
            <Text className="home__foot-brand">{APP_NAME}</Text>
            <Text className="home__foot-note">{PRIVACY_NOTE}</Text>
          </View>
        </View>
      )}
    </View>
  )
}
