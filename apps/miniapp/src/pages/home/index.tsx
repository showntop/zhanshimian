// 首页工作台：新用户 / 今日造型 / 报告就绪三态互斥 hero + 工具 + 场景 + 最近方案 + 顾问与衣橱。
// 视觉恢复自 09-11 旧线（recovery/ui-0911）：问候 kicker + 日期锚点、深绿 hero 卡（halo 呼吸）、
// 分区规则线、非对称场景格、通栏顾问卡、页脚品牌签名、enter(n) 错峰入场。
// 数据纪律（新架构不变）：单次 /v1/home/bootstrap 聚合 + resourceCache 缓存优先；
// 进行中 Operation 只经 useOperationPolling 观察，全部到终态后整页静默对账一次。
// 图片一律走 SourceImage：角标按 source_kind 投影，不手动叠标、不回退内置图。
import { memo, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import Taro, { useDidShow } from '@tarojs/taro'
import { Image, Text, View } from '@tarojs/components'
import {
  ANALYSIS_FAIL_COPY,
  APP_NAME,
  DAILY_COPY,
  ERROR_COPY,
  HOME_COPY,
  HOME_TITLE,
  OUTFIT_COPY,
  PRIVACY_NOTE,
  PURCHASE_COPY,
  SCENES,
  greetingForNow,
  projectDisplayMedia,
  readDressLockParams,
  roamVariant,
  taskDoneText,
  trackEvent,
  type DisplayMedia,
  type HomeBootstrap,
  type SceneCopy,
} from '@zsm/core'
import { qualityApi } from '../../app/api/quality'
import { resolveBaseURL } from '../../config/runtime'
import { peripherals } from '../../app/api/peripherals'
import { resourceCache, resourceKey } from '../../app/cache/resource-cache'
import { useOperationPolling } from '../../app/operations/use-operation-polling'
import { useDailyPick } from '../../features/daily/use-daily-pick'
import { usePageShell } from '../../hooks/use-page-visibility'
import AppHeader from '../../components/app-header'
import DailyPoster from '../../components/daily-poster'
import DailyMotion from '../../components/daily-motion'
import DressShuffle from '../../components/dress-shuffle'
import { useDressAssets } from '../../components/dress-shuffle/use-dress-assets'
import { dressTargetFromLock } from '../../components/dress-shuffle/presets'
import PrimaryButton from '../../components/primary-button'
import SourceImage from '../../components/source-image'
import ErrorState from '../../components/error-state'
import Skeleton from '../../components/skeleton'
import './index.scss'

const HOME_CACHE_KEY = resourceKey('home', 'current')
const IN_FLIGHT = new Set(['accepted', 'running', 'retrying'])
// 方案各套渲染的在途状态（RenderStatusView.state，与 PlansScreen 同一集合）
const RENDER_IN_FLIGHT = new Set(['queued', 'generating', 'checking'])
// 发型预览在途状态（HairPreview.state；诊断类是同步接口，没有跨页在途态可读）
const HAIR_IN_FLIGHT = new Set(['queued', 'generating', 'checking'])

// ---------- 换装洗牌（dress variant，spec 2026-09-23） ----------
// M3 服务端 dress_lock 未接：先用「装机种子 + 日期」稳定二选一（同人同天
// 稳定、隔天切换），M3 后由 settle 脚本 kind 分发覆盖；收敛 target 同理走
// 稳定本地派生。素材走 M4 CDN（未接线时 base 为空 → 预载必然失败 →
// 回落 sketch 巡游顶位，行为安全）。开发者可用 storage 临时覆盖：
//   zsm_dress_base = 素材基地址；zsm_dress_filter_off = '1'（模拟端不支持滤镜）
const DRESS_VARIANT_KEY = 'zsm_dress_variant'
const DRESS_BASE_KEY = 'zsm_dress_base'
const DRESS_FILTER_OFF_KEY = 'zsm_dress_filter_off'
// 等待期（脚本未到）的素材基地址：服务端 /assets/ 静态路由，与序列帧揭晓同源。
// 收敛期以 dress_lock.assets.base 为准；storage 可覆盖（本地联调另一台源时用）。
const DRESS_ASSET_BASE = `${resolveBaseURL()}/assets/daily/dress`

// 工具卡文案在 HOME_COPY.tools（红线 5），这里只配 key → 路由
const TOOL_PATHS: Record<(typeof HOME_COPY.tools)[number]['key'], string> = {
  hair: '/packages/tools/pages/hair/index',
  outfit: '/packages/tools/pages/outfit/index',
  purchase: '/packages/tools/pages/purchase/index',
}

// 工具卡图标：与场合图标同一套定制几何线稿（深苔绿圆头粗线）
const TOOL_ICONS: Record<(typeof HOME_COPY.tools)[number]['key'], string> = {
  hair: '/assets/icons/tool-hair.png',
  outfit: '/assets/icons/tool-outfit.png',
  purchase: '/assets/icons/tool-purchase.png',
}

const SCENE_ICONS: Record<SceneCopy['id'], string> = {
  interview: '/assets/icons/scene-interview.png',
  wedding: '/assets/icons/scene-wedding.png',
  date: '/assets/icons/scene-date.png',
  daily: '/assets/icons/scene-daily.png',
  gathering: '/assets/icons/scene-gathering.png',
}

// 复访闭环入口（life 分包）：顾问对话是产品核心特色
const LIFE = [
  { key: 'advisor', label: HOME_COPY.advisorEntry, desc: HOME_COPY.advisorEntryDesc, path: '/packages/life/pages/advisor/index' },
  { key: 'wardrobe', label: HOME_COPY.wardrobeEntry, desc: HOME_COPY.wardrobeEntryDesc, path: '/packages/life/pages/wardrobe/index' },
] as const

function isActive(operation: HomeBootstrap['active_operations'][number]): boolean {
  return IN_FLIGHT.has(operation.status)
}

/** 媒体存在但签名已过期（URL 还挂着，投影却必然判不可用） */
function mediaExpired(media: DisplayMedia | null | undefined): boolean {
  if (!media?.asset_id) return false
  const at = Date.parse(media.url_expires_at)
  return Number.isFinite(at) && at <= Date.now()
}

// 报告入口行：衬线数字 + 小后缀。原「不规则比例条」已删——条形会被读成
// 评分/进度（产品红线 1：不打分），只留数字注脚。

const SceneTile = memo(function SceneTile({ scene }: { scene: SceneCopy }) {
  return (
    <View
      className={`home__scene home__scene--${scene.id} pressable`}
      onClick={() => void Taro.navigateTo({ url: `/pages/scene/index?scene=${scene.id}` })}
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

export default function Home() {
  const [boot, setBoot] = useState<HomeBootstrap | null>(
    () => resourceCache.read<HomeBootstrap>(HOME_CACHE_KEY) ?? null,
  )
  const [loading, setLoading] = useState(!boot)
  const [failed, setFailed] = useState(false)
  // 工具卡 live 徽章：hair 在途（active 端点）；outfit/purchase 有上次诊断（latest 端点）
  const [hairActive, setHairActive] = useState(false)
  const [outfitReady, setOutfitReady] = useState(false)
  const [purchaseReady, setPurchaseReady] = useState(false)
  // 发卡右侧视觉：用户已有出图的设计时展示最近一次效果，否则保持内置参考位
  const [hairLatestMedia, setHairLatestMedia] = useState<DisplayMedia | null>(null)
  const firstShow = useRef(true)
  // 轮询连续失败自停后：toast 只报一次（ref 去重），恢复靠回 tab 对账时 restartKey 重装
  const [pollRestart, setPollRestart] = useState(0)
  const pollHalted = useRef(false)
  // 内容上屏后才播入场、播完钉住：切 tab 回来不重播 fade-up（防已渲染图片被藏）
  const { pageClass, enter } = usePageShell(Boolean(boot), 'page--tab', 'home')

  /** 拉取并整体替换。revalidate 合并发；到达前界面继续显示旧值。 */
  const load = useCallback(async (background: boolean) => {
    if (!background) setLoading(true)
    setFailed(false)
    try {
      const next = await resourceCache.revalidate(HOME_CACHE_KEY, () => qualityApi.getHomeBootstrap())
      setBoot(next)
    } catch {
      // 已有内容时后台刷新失败不拆页，避免切 tab 闪到错误态
      if (!resourceCache.read<HomeBootstrap>(HOME_CACHE_KEY)) setFailed(true)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load(false)
  }, [load])

  // 工具卡徽章不走 bootstrap（聚合里没有它们）：hair 问 active 端点，
  // outfit/purchase 问 latest 诊断。单个端点失败只当自己没数据，不拖垮其余两个。
  const refreshToolBadges = useCallback(async () => {
    const [hair, outfit, purchase, hairHistory] = await Promise.all([
      peripherals.getActiveHairPreview().catch(() => null),
      peripherals.getLatestDiagnosis('outfit').catch(() => null),
      peripherals.getLatestDiagnosis('purchase').catch(() => null),
      peripherals.listHairPreviews().catch(() => []),
    ])
    setHairActive(Boolean(hair && HAIR_IN_FLIGHT.has(hair.state)))
    setOutfitReady(Boolean(outfit))
    setPurchaseReady(Boolean(purchase))
    // 最近一张已出图的设计（列表新到旧）：media 走投影自带「风格参考」角标，不手动叠标
    setHairLatestMedia(hairHistory.find((preview) => preview.state === 'ready' && preview.media)?.media ?? null)
  }, [])

  // 首次 onShow 跳过静默对账（挂载时已拉）；此后每次回 tab 对账一次
  useDidShow(() => {
    trackEvent('page_view', { page: 'home' })
    void refreshToolBadges()
    if (firstShow.current) {
      firstShow.current = false
      return
    }
    // 轮询曾连续失败 5 次自停（控制器 stop 后 refresh 是空操作）：
    // 借这次对账换 restartKey 把它重新装起来，用户回 tab 即恢复，不用重进小程序
    if (pollHalted.current) {
      pollHalted.current = false
      setPollRestart((key) => key + 1)
    }
    void load(true)
  })

  // 有进行中的操作就盯着；全部到终态后只刷新 home
  const activeIds = (boot?.active_operations ?? []).filter(isActive).map((operation) => operation.id)
  useOperationPolling({
    operationIds: activeIds,
    enabled: activeIds.length > 0,
    restartKey: pollRestart,
    onSettled: (operations) => {
      // 到终态要给具体说法（旧线行为）：完成按 kind/subject_type 给文案；
      // 服务端的 active_operations 只含在途：失败终态一刷新就从 bootstrap 消失，
      // 不吭声的话「正在分析」会悄悄翻回「开始形象分析」。完成优先于失败。
      const done = operations.find(
        (operation) => operation.status === 'succeeded' && taskDoneText(operation.kind, operation.subject_type),
      )
      const failedOperation = operations.find((operation) => operation.status === 'failed')
      if (done) {
        Taro.showToast({ title: taskDoneText(done.kind, done.subject_type), icon: 'none' })
      } else if (failedOperation) {
        Taro.showToast({
          title: failedOperation.public_message || ANALYSIS_FAIL_COPY.timeoutBody,
          icon: 'none',
        })
      }
      void load(true)
      // 终态可能就是工具卡盯着的那个任务（hair 渲染与方案渲染同 kind）：顺手对一次徽章
      void refreshToolBadges()
    },
    onFetchFailure: () => {
      // 连续失败 5 次控制器自停（AGENTS 规约的失败态）：页面有内容不拆页，
      // toast 一次告知；恢复入口是回 tab 时的 restartKey 重装（见 useDidShow）
      if (pollHalted.current) return
      pollHalted.current = true
      Taro.showToast({ title: ERROR_COPY.network, icon: 'none' })
    },
  })

  const report = boot?.report ?? null
  const hasReport = Boolean(report?.id)
  const todayPlan = boot?.today_plan ?? null
  const planSet = boot?.plan_set ?? null
  const analyzing = (boot?.active_operations ?? []).find(
    (operation) => operation.kind === 'assessment' && isActive(operation),
  )
  const planning = (boot?.active_operations ?? []).find(
    (operation) => operation.kind === 'plan_set' && isActive(operation),
  )
  // 最近方案续看：优先挑「形象图已就绪」的一套（渲染中/失败的不配上首页），
  // 都没图就退回 slot 最小的一套（无图时卡片不摆「图片暂不可用」的空框）
  const featured = useMemo(() => {
    if (!planSet) return undefined
    const bySlot = [...planSet.variants].sort((a, b) => a.slot - b.slot)
    return (
      bySlot.find((variant) => projectDisplayMedia(variant.render?.media ?? null) !== null) ??
      bySlot[0]
    )
  }, [planSet])
  const featuredHasRender = featured
    ? projectDisplayMedia(featured.render?.media ?? null) !== null
    : false
  // 签名 URL 过期（服务端 ASSET_URL_TTL 900s）：长时间停在首页时缓存里的 URL
  // 会悄悄失效，投影判定不可用 → 卡片变成空框。这里检测到过期就静默重拉一次
  // bootstrap 换新签名（详情页是现拉现签，所以那里一直有图）。
  const featuredExpired = featured
    ? mediaExpired(featured.render?.media ?? null)
    : false
  const resignedRef = useRef<string | null>(null)
  useEffect(() => {
    if (!featuredExpired || !featured) return
    const mark = `${featured.id}:${featured.render?.media?.asset_id ?? ''}`
    if (resignedRef.current === mark) return
    resignedRef.current = mark
    void load(true)
  }, [featuredExpired, featured, load])
  const findingsCount = (report?.findings ?? []).length

  // 每日内容：与今日页共用同一个状态机（服务端幂等保证同一天同一条），
  // 首页只放海报（精简），点进去看完整；生成中不占首页空间。
  const {
    phase,
    content: dailyContent,
    bucketName: dailyBucketName,
    saveCurrent,
    reload: reloadDaily,
    roamScript,
    settleScript,
    reveal,
  } = useDailyPick()
  // 揭晓由收敛动画播完触发（reveal），不再写死 1.2s。
  // offline 有缓存内容也照常呈现海报（today 页同策略）——否则离线时海报位直接消失。
  const dailyReady = phase === 'content' || (phase === 'offline' && Boolean(dailyContent))
  const dailySettling = phase === 'settling'
  // 生成中播巡游：这两段不给东西的话，等待期间首页这块是空的。
  const dailyWaiting = phase === 'loading' || phase === 'waiting'
  // 离线且无缓存：不空屏，给静默文案 + 重试动作（红线 4；文案与今日页同款）
  const dailyOfflineEmpty = phase === 'offline' && !dailyContent
  // gene 色板接入点：脚本是通用的、可缓存的，不携带用户隐私，
  // 所以配色在渲染时由客户端注入（数据到位后传进来即可）。
  const dailyPalette: string[] = []

  // 换装洗牌 variant：等待期与收敛期都只信服务端脚本（hash(uid+date) 同一套
  // 种子）。客户端不再本地另算——本地猜会整段猜错（明明是 dress 却走旧线）。
  // 素材预载失败 → dress 整体退位，sketch 巡游 + 帧揭晓照常（spec §4）。
  const dressEnv = useMemo(() => {
    const read = (key: string): string => {
      try {
        return (Taro.getStorageSync(key) as string) || ''
      } catch {
        return ''
      }
    }
    const now = new Date()
    return {
      dateKey: `${now.getFullYear()}-${now.getMonth() + 1}-${now.getDate()}`,
      base: read(DRESS_BASE_KEY) || DRESS_ASSET_BASE,
      colorLocked: read(DRESS_FILTER_OFF_KEY) === '1',
    }
  }, [])
  const dressRoamVariant = useMemo(
    () => roamVariant(roamScript?.stages.find((stage) => stage.phase === 'roam')),
    [roamScript],
  )
  // 记住服务端这次给的 variant：供 App 启动时（首页还没挂载）判断要不要预热
  useEffect(() => {
    if (!dressRoamVariant) return
    try {
      Taro.setStorageSync(DRESS_VARIANT_KEY, `${dressEnv.dateKey}|${dressRoamVariant}`)
    } catch {
      // 存不下就不存：本次会话内仍有 roam 脚本兜底
    }
  }, [dressRoamVariant, dressEnv.dateKey])
  // 未知时不再本地猜：默认走洗牌（静态前奏），roam 脚本一到就校正。
  // 此前本地 50% 猜成 sketch，会整段走旧线——「明明是 dress 却没有洗牌」的元凶。
  const dressPredict = dressRoamVariant !== 'sketch'
  const dressLock = useMemo(
    () => readDressLockParams(settleScript?.stages.find((stage) => stage.kind === 'dress_lock')),
    [settleScript],
  )
  // 词汇表对不上（客户端落后于服务端新值）→ null → 回落旧揭晓线
  const dressLockTarget = useMemo(
    () => (dressLock ? dressTargetFromLock(dressLock.target) : null),
    [dressLock],
  )
  // 素材基地址以脚本下发为准（服务端可独立换源），本地常量只是开发覆盖
  const dressBase = dressLock?.assets.base || dressEnv.base
  const dressAssets = useDressAssets(dressBase, dressPredict || Boolean(dressLock))
  // 预载中（pending）也走洗牌——停在它的静态前奏态。此前用 sketch 巡游顶位，
  // 素材就绪后硬切成洗牌，开头会先窜一小段线稿。只有真失败才回落旧线。
  const dressOnWaiting = dressPredict && !dressAssets.failed
  const dressOnSettling = Boolean(dressLockTarget) && dressAssets.ready
  const dressHold = !dressAssets.ready

  // 收敛线一锤定音：进入 settling 那一刻判定走洗牌还是旧揭晓线（DailyMotion），
  // 之后即使素材到位也不再换。素材晚到时旧线已经揭晓（phase=content），
  // 此时把 DressShuffle 换上来会带着 settling 重新挂载、把整个收敛+揭晓重播一遍
  // ——「左栏定格后又刷一次」的根因。缓存命中（跳过 settling 直接 content）
  // 不经过这里，仍按实时状态判定，「落地即保留」不受影响。
  const [dressSettleLatched, setDressSettleLatched] = useState(false)
  const settleEntrySeenRef = useRef(false)
  useEffect(() => {
    if (phase !== 'settling' || settleEntrySeenRef.current) return
    settleEntrySeenRef.current = true
    setDressSettleLatched(Boolean(dressLockTarget) && dressAssets.ready)
  }, [phase, dressLockTarget, dressAssets.ready])
  const dressSettle = settleEntrySeenRef.current ? dressSettleLatched : dressOnSettling

  // 海报角落编号用日期而非序号：序号是静态的，日期才有"每天换一张"的时间感
  const todaySeq = useMemo(() => {
    const now = new Date()
    const month = String(now.getMonth() + 1).padStart(2, '0')
    const day = String(now.getDate()).padStart(2, '0')
    return `${month}.${day}`
  }, [])

  const goToday = useCallback(() => {
    void Taro.navigateTo({ url: '/pages/today/index' })
  }, [])

  const goReport = useCallback(() => {
    void Taro.navigateTo({ url: `/pages/report/index?id=${encodeURIComponent(report?.id ?? '')}` })
  }, [report?.id])

  // 方案 Tab 角标（与 PlansScreen 同一规则：在途受理 + 各套在途渲染）。
  // 首页是默认 tab、启动即挂载，方案 tab 懒挂载——首页不设的话，
  // 在首页等待生成的用户不点方案 tab 永远看不到红点。
  // 只数 plan_set 受理与当前方案集的渲染：hair/today 渲染与方案渲染同 kind，
  // 而 OperationRef 没有 subject_type，按 kind 全数会把红点贴到错的 tab 上。
  const planTaskCount =
    (boot?.active_operations ?? []).filter(
      (operation) => isActive(operation) && operation.kind === 'plan_set',
    ).length +
    (planSet?.variants ?? []).filter(
      (variant) => RENDER_IN_FLIGHT.has(variant.render.state) && Boolean(variant.render.operation_id),
    ).length
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
      {loading && !boot ? (
        <Skeleton rows={4} />
      ) : failed && !boot ? (
        <ErrorState onRetry={() => void load(false)} />
      ) : (
        <View className="home">
          <View className={`home__greeting ${enter()}`}>
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
                      <SourceImage
                        className="home__preview-image"
                        reference={{ slug: 'natural', variant: 'portrait' }}
                        anchor="top"
                        frameAspect={118 / 164}
                      />
                      <Text className="home__preview-caption">{HOME_COPY.previewCaptions[0]}</Text>
                    </View>
                    <View className="home__preview-slice home__preview-slice--2">
                      <SourceImage
                        className="home__preview-image"
                        reference={{ slug: 'natural', variant: 'report' }}
                        anchor="top"
                        frameAspect={118 / 164}
                      />
                      <Text className="home__preview-caption">{HOME_COPY.previewCaptions[1]}</Text>
                    </View>
                    <View className="home__preview-slice home__preview-slice--3">
                      <SourceImage
                        className="home__preview-image"
                        reference={{ slug: 'sharp', variant: 'plan' }}
                        anchor="top"
                        frameAspect={118 / 164}
                      />
                      <Text className="home__preview-caption">{HOME_COPY.previewCaptions[2]}</Text>
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
                  text={analyzing ? HOME_COPY.analyzingAction : HOME_COPY.startAnalysis}
                  tone="onDark"
                  onClick={() =>
                    void Taro.navigateTo({
                      url: analyzing
                        ? `/pages/analysis/index?operation_id=${encodeURIComponent(analyzing.id)}` +
                          `&assessment_id=`
                        : '/pages/capture/index',
                    })
                  }
                />
                <Text className="home__hero-note">{HOME_COPY.photoPrivacy}</Text>
              </View>
            </View>
          ) : (
            <View>
              {/* 有今日造型时顶部换成这张卡。报告入口不能挂在卡的分支里——
                  否则最活跃的用户（天天生成造型的那批）反而看不到通往报告的路。 */}
              {todayPlan ? (
                <View
                  className="home__hero home__hero--today home__hero--photo card--hero pressable halo"
                  onClick={() => void Taro.navigateTo({ url: '/packages/life/pages/today/index' })}
                >
                  <View className={`home__hero-copy ${enter(1)}`}>
                    <Text className="home__hero-eyebrow">{HOME_COPY.todayEyebrow}</Text>
                    <Text className="home__hero-title">{todayPlan.title}</Text>
                    <Text className="home__hero-desc">{todayPlan.summary}</Text>
                    <Text className="home__hero-link">{HOME_COPY.emptyTodayLink}</Text>
                  </View>
                  <SourceImage
                    className="home__hero-img"
                    media={todayPlan.media}
                    anchor="top"
                    frameAspect={232 / 344}
                  />
                </View>
              ) : dailyReady && dailyContent && !dressSettle ? (
                <View className={enter(1)}>
                  <DailyPoster
                    type={dailyContent.type}
                    visual={dailyContent.visual}
                    topic={dailyContent.topic}
                    fitText={dailyContent.fitText}
                    seq={todaySeq}
                    saveLabel={DAILY_COPY.saveAction}
                    onSave={saveCurrent}
                    onOpen={goToday}
                  />
                </View>
              ) : dailySettling || (dailyReady && dressSettle) ? (
                // 落地即保留：洗牌揭晓面板（人物 + 这一身 + 建议正文）一直留在首页，
                // 整卡就是进今日页的主入口（内容就绪才可点；洗牌播放中点了没反应）。
                <View
                  className={`home__daily-waiting ${dressSettle ? 'home__daily-waiting--dress' : ''} ${enter(1)} ${
                    dailyReady && dailyContent && dressSettle ? 'pressable' : ''
                  }`}
                  onClick={dailyReady && dailyContent && dressSettle ? goToday : undefined}
                >
                  {dressSettle ? (
                    <DressShuffle
                      target={dressLockTarget ?? undefined}
                      settling
                      assetBase={dressBase}
                      resolveAsset={dressAssets.resolve}
                      hairAvailable={dressAssets.hairReady}
                      colorLocked={dressEnv.colorLocked}
                      onSettled={reveal}
                      topic={dailyContent?.topic}
                      lead={dailyContent?.lead}
                      category={dailyContent?.asset}
                      seq={todaySeq}
                      // CTA 不能挂在 dailyReady 上：内容与 settling 是同一次 set 出来的，
                      // 而按钮若等到 phase 变 content（揭晓定格后约 0.8s）才出现，
                      // 左栏就会在定格之后再单独弹一次按钮——看着像刷新了两下
                      cta={dailyContent ? DAILY_COPY.dressContentLink : undefined}
                    />
                  ) : (
                    <DailyMotion
                      presentation={settleScript}
                      phase="settling"
                      palette={dailyPalette}
                      seq={todaySeq}
                      onSettled={reveal}
                    />
                  )}
                </View>
              ) : dailyWaiting ? (
                <View
                  className={`home__daily-waiting ${dressOnWaiting ? 'home__daily-waiting--dress' : ''} ${enter(1)}`}
                >
                  {dressOnWaiting ? (
                    <DressShuffle
                      settling={false}
                      hold={dressHold}
                      assetBase={dressBase}
                      resolveAsset={dressAssets.resolve}
                      hairAvailable={dressAssets.hairReady}
                      colorLocked={dressEnv.colorLocked}
                    />
                  ) : (
                    <DailyMotion
                      presentation={roamScript}
                      phase="waiting"
                      palette={dailyPalette}
                      seq={todaySeq}
                    />
                  )}
                </View>
              ) : dailyOfflineEmpty ? (
                <View className={`home__daily-offline ${enter(1)}`}>
                  <View className="home__daily-offline-copy">
                    <Text className="home__daily-offline-title">{DAILY_COPY.offlineTitle}</Text>
                    <Text className="home__daily-offline-body">{DAILY_COPY.offlineBody}</Text>
                  </View>
                  <Text
                    className="home__daily-offline-retry pressable"
                    onClick={() => void reloadDaily()}
                  >
                    {DAILY_COPY.retryAction}
                  </Text>
                </View>
              ) : null}
              {/* 报告退位：不再是首页主角，但入口保留，降级为一行。
                  常驻在卡片下方——不管今天显示的是今日造型卡、今日海报还是等待态，
                  只要有报告就都能走到报告页。
                  只留衬线数字注脚——条形会被读成评分/进度，红线 1 不打分 */}
              <View className="home__archive pressable" onClick={goReport}>
                <Text className="home__archive-text">{HOME_COPY.viewReport}</Text>
                <View className="home__archive-meta">
                  <Text className="home__archive-num serif">{findingsCount}</Text>
                  <Text className="home__archive-suffix">{HOME_COPY.findingsSuffix}</Text>
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
              {HOME_COPY.tools.map((tool) => {
                // live 徽章覆盖静态 badge：hair 在途「生成中」（呼吸样式）、有历史效果
                // 「查看效果」（卡面已直出最近图）；outfit/purchase 有上次诊断给
                // 「查看结果」（同步诊断没有跨页在途态）
                const liveBadge =
                  tool.key === 'hair' && hairActive
                    ? HOME_COPY.toolLiveHair
                    : tool.key === 'hair' && hairLatestMedia
                      ? HOME_COPY.toolHairResult
                      : tool.key === 'outfit' && outfitReady
                        ? OUTFIT_COPY.lastResult
                        : tool.key === 'purchase' && purchaseReady
                          ? PURCHASE_COPY.lastResult
                          : ''
                const badge = liveBadge || tool.badge
                const toolIcon = TOOL_ICONS[tool.key]
                // 徽章是文档流内 chip：主卡挂标题行尾（绝对定位会被右上 128rpx 拱角
                // 裁掉），小卡 --float 排到列首右上、标题在下方占满整行（窄卡不与标题挤一行）
                const badgeNode = badge ? (
                  <Text
                    className={
                      `home__tool-badge` +
                      `${tool.key === 'hair' ? '' : ' home__tool-badge--float'}` +
                      `${liveBadge === HOME_COPY.toolLiveHair ? ' home__tool-badge--live' : ''}`
                    }
                  >
                    {badge}
                  </Text>
                ) : null
                return (
                  <View
                    key={tool.key}
                    className={`home__tool home__tool--${tool.key} pressable ${tool.key === 'hair' ? 'home__tool--lead' : ''}`}
                    onClick={() => void Taro.navigateTo({ url: TOOL_PATHS[tool.key] })}
                  >
                    {tool.key === 'hair' ? (
                      <Image
                        className="home__tool-watermark"
                        src={TOOL_ICONS.hair}
                        mode="aspectFit"
                        lazyLoad={false}
                        fadeIn={false}
                      />
                    ) : null}
                    <View className="home__tool-copy">
                      <View className="home__tool-name-row">
                        {toolIcon ? (
                          <Image
                            className="home__tool-icon"
                            src={toolIcon}
                            mode="aspectFit"
                            lazyLoad={false}
                            fadeIn={false}
                          />
                        ) : null}
                        <Text className="home__tool-name">{tool.label}</Text>
                        {tool.key === 'hair' ? badgeNode : null}
                      </View>
                      <Text className="home__tool-desc">{tool.desc}</Text>
                    </View>
                    {tool.key === 'hair' ? (
                      // 全出血直出：4:3 头肩图顶对齐铺满右区，无任何滤镜效果。
                      // 裁切方向枢纽 = 视觉区真实比例（384×238rpx，rpx 域常数）：
                      // 图比例 ≤ 1.614 走「按宽铺满、顶对齐裁底」，> 1.614 的真横图
                      // 走「按高铺满裁两侧」——任何比例都必然铺满，无露底缝隙。
                      <View className="home__tool-visual">
                        {hairLatestMedia ? (
                          <SourceImage
                            className="home__tool-visual-photo"
                            media={hairLatestMedia}
                            anchor="top"
                            frameAspect={384 / 238}
                          />
                        ) : (
                          <SourceImage
                            className="home__tool-visual-photo"
                            reference={{ slug: 'natural', variant: 'hair' }}
                            anchor="top"
                            frameAspect={384 / 238}
                          />
                        )}
                      </View>
                    ) : null}
                    {tool.key !== 'hair' ? badgeNode : null}
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

          {featured && planSet ? (
            <View className="home__section">
              <View className={`home__section-head ${enter(3)}`}>
                <Text className="section-title">{HOME_COPY.recentTitle}</Text>
                <View className="section-rule" />
              </View>
              <View
                className="home__recent card pressable"
                onClick={() =>
                  void Taro.navigateTo({
                    url:
                      `/pages/plan/index?plan_set_id=${encodeURIComponent(planSet.id)}` +
                      `&variant_id=${encodeURIComponent(featured.id)}`,
                  })
                }
              >
                <View className="home__recent-visual">
                  /* 只用真实媒体：没拿到就不画任何图（示例图会把内置参考位
                     读成用户自己的效果图，已撤回） */
                  {featuredHasRender ? (
                    <SourceImage
                      className="home__recent-img"
                      media={featured.render.media}
                      anchor="top"
                      frameAspect={120 / 150}
                    />
                  ) : null}
                </View>
                <View className={`home__recent-copy ${enter(3)}`}>
                  <Text className="home__recent-name">{featured.name}</Text>
                  <Text className="home__recent-why">{featured.rationale}</Text>
                  <Text className="home__recent-link">{HOME_COPY.continuePlan}</Text>
                </View>
                <Text className="home__recent-arrow">›</Text>
              </View>
            </View>
          ) : null}

          {planning ? (
            <View className={`home__section ${enter(3)}`}>
              <Text
                className="home__planning pressable"
                onClick={() => void Taro.switchTab({ url: '/pages/plans/index' })}
              >
                {HOME_COPY.planningLink}
              </Text>
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
                  onClick={() => void Taro.navigateTo({ url: item.path })}
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
