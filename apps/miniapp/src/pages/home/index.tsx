// 首页工作台：新用户 / 今日造型 / 报告就绪三态互斥 hero + 工具 + 场景 + 最近方案 + 顾问与衣橱。
// 视觉恢复自 09-11 旧线（recovery/ui-0911）：问候 kicker + 日期锚点、深绿 hero 卡（halo 呼吸）、
// 分区规则线、非对称场景格、通栏顾问卡、页脚品牌签名、enter(n) 错峰入场。
// 数据纪律（新架构不变）：单次 /v1/home/bootstrap 聚合 + resourceCache 缓存优先；
// 进行中 Operation 只经 useOperationPolling 观察，全部到终态后整页静默对账一次。
// 图片一律走 SourceImage：角标按 source_kind 投影，不手动叠标、不回退内置图。
import { memo, useCallback, useEffect, useRef, useState } from 'react'
import Taro, { useDidShow } from '@tarojs/taro'
import { Image, Text, View } from '@tarojs/components'
import {
  APP_NAME,
  APP_SLOGAN,
  HOME_COPY,
  HOME_TITLE,
  PRIVACY_NOTE,
  SCENES,
  greetingForNow,
  planSlotLabel,
  type HomeBootstrap,
  type SceneCopy,
} from '@zsm/core'
import { qualityApi } from '../../app/api/quality'
import { resourceCache, resourceKey } from '../../app/cache/resource-cache'
import { useOperationPolling } from '../../app/operations/use-operation-polling'
import { usePageShell } from '../../hooks/use-page-visibility'
import AppHeader from '../../components/app-header'
import PrimaryButton from '../../components/primary-button'
import SourceImage from '../../components/source-image'
import ErrorState from '../../components/error-state'
import Skeleton from '../../components/skeleton'
import './index.scss'

const HOME_CACHE_KEY = resourceKey('home', 'current')
const IN_FLIGHT = new Set(['accepted', 'running', 'retrying'])

// 工具卡文案在 HOME_COPY.tools（红线 5），这里只配 key → 路由
const TOOL_PATHS: Record<(typeof HOME_COPY.tools)[number]['key'], string> = {
  hair: '/packages/tools/pages/hair/index',
  outfit: '/packages/tools/pages/outfit/index',
  purchase: '/packages/tools/pages/purchase/index',
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

/** 今日语境日期（问候区的时间锚点，非装饰） */
function todayLabel(): string {
  const d = new Date()
  const week = ['日', '一', '二', '三', '四', '五', '六'][d.getDay()] ?? ''
  return `${d.getMonth() + 1}月${d.getDate()}日 · 周${week}`
}

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
  const firstShow = useRef(true)
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

  // 首次 onShow 跳过（挂载时已拉）；此后每次回 tab 静默对账
  useDidShow(() => {
    if (firstShow.current) {
      firstShow.current = false
      return
    }
    void load(true)
  })

  // 有进行中的操作就盯着；全部到终态后只刷新 home
  const activeIds = (boot?.active_operations ?? []).filter(isActive).map((operation) => operation.id)
  useOperationPolling({
    operationIds: activeIds,
    enabled: activeIds.length > 0,
    onSettled: () => void load(true),
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
  const featured = planSet ? [...planSet.variants].sort((a, b) => a.slot - b.slot)[0] : undefined
  const findingsCount = (report?.findings ?? []).length

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
                      <View className="home__preview-focus">
                        <View className="home__preview-focus-dot" />
                      </View>
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
          ) : todayPlan ? (
            <View>
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
                  frameAspect={200 / 260}
                />
              </View>
            </View>
          ) : (
            <View>
              <View
                className="home__hero home__hero--report card--hero pressable"
                onClick={() =>
                  void Taro.navigateTo({ url: `/pages/report/index?id=${encodeURIComponent(report?.id ?? '')}` })
                }
              >
                <View className="home__report-main">
                  <View className={`home__hero-copy ${enter(1)}`}>
                    <Text className="home__hero-eyebrow">{HOME_COPY.reportReady}</Text>
                    <Text className="home__hero-title">{report?.priority_title}</Text>
                    <Text className="home__hero-desc">{report?.priority_copy}</Text>
                  </View>
                  <View className="home__report-visual">
                    <SourceImage
                      className="home__report-image"
                      media={report?.source_media.face.media}
                      anchor="top"
                      frameAspect={248 / 314}
                    />
                    <View className="home__report-shade" />
                  </View>
                </View>
                <View className="home__report-action">
                  <Text className="home__report-action-label">{HOME_COPY.viewReport}</Text>
                  <Text className="home__report-action-meta">
                    <Text className="home__report-action-count">{findingsCount}</Text>
                    {HOME_COPY.findingsSuffix}
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
              {HOME_COPY.tools.map((tool) => (
                <View
                  key={tool.key}
                  className={`home__tool pressable ${tool.key === 'hair' ? 'home__tool--lead' : ''}`}
                  onClick={() => void Taro.navigateTo({ url: TOOL_PATHS[tool.key] })}
                >
                  <View className="home__tool-copy">
                    <Text className="home__tool-name">{tool.label}</Text>
                    <Text className="home__tool-desc">{tool.desc}</Text>
                  </View>
                  {tool.key === 'hair' ? (
                    <SourceImage
                      className="home__tool-visual"
                      reference={{ slug: 'natural', variant: 'hair' }}
                      mode="aspectFit"
                    />
                  ) : null}
                  {tool.badge ? <Text className="home__tool-badge">{tool.badge}</Text> : null}
                </View>
              ))}
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
                <SourceImage
                  className="home__recent-img"
                  media={featured.render.media}
                  anchor="top"
                  frameAspect={120 / 150}
                />
                <View className={`home__recent-copy ${enter(3)}`}>
                  <Text className="home__recent-name">{planSlotLabel(featured.name, featured.slot)}</Text>
                  <Text className="home__recent-why">{featured.rationale}</Text>
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
