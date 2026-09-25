// 今日：每天一条内容（轻模式）+ 引导去生成今日造型（重模式）。
//
// 美学参照（2026-09 重构）：编辑式海报页。
//   页眉是「手写体刊头 + 宋体大标题」的杂志跨页；建议本体收进白卡；
//   主行动是一颗苔绿大药丸；视觉区浮在有机形状的苔绿底上。
//   层次纪律：视觉是主角 → 大标题次之 → 白卡正文 → 主按钮 → 行动卡 → 手册行。
//
// 状态机（方案 §3.1）：loading → waiting（按 scenario 播等待动画）→ settling
// （generate 返回，动画落位 1.2s）→ content；cacheHit 直接进 content；
// 网络错误 → offline 读本地缓存。generate 永远 200，没有「生成失败」分支，
// fallback 内容只是角标写「今日精选」。
//
// 重模式不在这里并列：它是「看看我穿这样」这个 action 的结果，跳 life 分包的今日造型页。

import { useCallback, useEffect, useState } from 'react'
import Taro, { useDidShow } from '@tarojs/taro'
import { Image, Text, View } from '@tarojs/components'
import { DAILY_COPY, DAILY_HISTORY_COPY, asHeroArtSpec, dailyTypeName } from '@zsm/core'
import { peripherals } from '../../app/api/peripherals'
import { usePageShell } from '../../hooks/use-page-visibility'
import { useDailyPick, useTodayContext } from '../../features/daily/use-daily-pick'
import AppHeader from '../../components/app-header'
import DailyVisual from '../../components/daily-visual'
import DailyWaiting from '../../components/daily-waiting'
import Skeleton from '../../components/skeleton'
import './index.scss'

const TODAY_PLAN_PATH = '/packages/life/pages/today/index'
const HANDBOOK_PATH = '/packages/life/pages/handbook/index'
const HISTORY_PATH = '/packages/life/pages/history/index'

export default function Today() {
  const {
    saves, phase, scenario, content, bucketName, loading, reloadSaves, saveCurrent,
  } = useDailyPick()
  const ctx = useTodayContext()
  const { pageClass } = usePageShell(true, '', 'today')

  // 从手册返回时同步一次：那边可以移出，回来要看到最新状态
  useDidShow(() => {
    reloadSaves()
  })

  const goPlan = useCallback(() => {
    void Taro.navigateTo({ url: TODAY_PLAN_PATH })
  }, [])

  const goHandbook = useCallback(() => {
    void Taro.navigateTo({ url: HANDBOOK_PATH })
  }, [])

  // 「试试这些建议」= 历史建议页：推送过的全部，按日期回看（手册是收下的，这里是全部）
  const goHistory = useCallback(() => {
    void Taro.navigateTo({ url: HISTORY_PATH })
  }, [])

  // 今日造型状态行（闭环枢纽）：current 接口缓存优先，onShow 刷新。
  // planning/rendering = 生成中；ready = 已生成；null = 还没有
  const [tryonPlan, setTryonPlan] = useState<
    { state: string; title: string; mediaUrl: string } | null
  >(null)
  useDidShow(() => {
    void peripherals
      .getCurrentTodayPlan()
      .then((plan) => {
        if (!plan) return
        setTryonPlan({
          state: plan.state,
          title: plan.title ?? '',
          mediaUrl: plan.media?.url ?? '',
        })
      })
      .catch(() => {})
  })
  const tryonState =
    tryonPlan === null
      ? 'empty'
      : tryonPlan.state === 'ready' || tryonPlan.state === 'ready_partial'
        ? 'ready'
        : 'working'

  const seq = String(saves.length + 1).padStart(2, '0')
  const saved = Boolean(content && saves.some((save) => save.key === content.dedupeKey))
  const isFallback = content?.source === 'fallback'
  // 插画主视觉：服务端按分类下发的内置时装插画（spec.image）。
  // 加载失败退回程序化视觉——素材缺席不能把舞台弄空。
  const hero = content ? asHeroArtSpec(content.visual) : null
  const [heroBroken, setHeroBroken] = useState(false)
  useEffect(() => {
    setHeroBroken(false)
  }, [content?.dedupeKey])
  const showHero = hero !== null && !heroBroken
  // 舞台形态：海报要更高的舞台；色卡/对比是信息图，高度随内容走
  const stageClass = content
    ? content.visual.modality === 'poster'
      ? 'today__stage--tall'
      : content.visual.modality === 'swatch' || content.visual.modality === 'compare'
        ? 'today__stage--flat'
        : ''
    : ''

  return (
    <View className={pageClass}>
      <AppHeader back />
      {loading ? (
        <Skeleton rows={4} />
      ) : (
        <View className="today">
          {ctx && ctx.temperature !== 0 ? (
            <View className="today__ctx">
              <Text className="today__ctx-main">
                {ctx.date.slice(5).replace('-', '月')}日 · {ctx.temperature}° {ctx.condition}
              </Text>
              {ctx.city !== '' ? (
                <View className="today__ctx-city">
                  <View className="today__ctx-pin" />
                  <Text className="today__ctx-city-text">{ctx.city}</Text>
                </View>
              ) : null}
            </View>
          ) : null}

          {(phase === 'waiting' || phase === 'settling') && !content ? (
            <DailyWaiting scenario={scenario} settling={phase === 'settling'} />
          ) : null}

          {content ? (
            <View className="today__masthead">
              <Text className="today__script">{DAILY_COPY.pageTitle}</Text>
              <Text className="today__seq">· 第 {seq} 条</Text>
              <Text className="today__title serif">{content.topic}</Text>
              <Text className="today__lead">{content.lead}</Text>
            </View>
          ) : null}

          {content ? (
            <View className={`today__stage ${showHero ? 'today__stage--art' : stageClass}`}>
              <View className="today__stage-blob" />
              {showHero ? (
                <Image
                  className="today__hero"
                  src={hero.image}
                  mode="aspectFit"
                  onError={() => setHeroBroken(true)}
                />
              ) : (
                <DailyVisual visual={content.visual} />
              )}
              {showHero ? (
                <Text className="today__stage-note">{DAILY_COPY.stageGhost}</Text>
              ) : null}
            </View>
          ) : null}

          {/* 白卡：为什么值得试试 —— 参考稿里对比卡与主按钮都收在卡内 */}
          {content ? (
            <View className="today__why">
              <View className="today__why-head">
                <View className="today__why-bulb">
                  <View className="today__why-bulb-filament" />
                </View>
                <Text className="today__why-title">{DAILY_COPY.whyTitle}</Text>
                <Text className="today__why-badge">
                  {isFallback ? DAILY_COPY.fallbackBadge : dailyTypeName(content.type)}
                </Text>
              </View>
              <Text className="today__fit-text">{content.fitText}</Text>
              <Text className="today__why-body">{content.why}</Text>

              {/* 有插画主视觉时，色票/对比/示意收进卡内（参考稿的 ✓/✗ 位） */}
              {showHero ? (
                <View className="today__why-visual">
                  <DailyVisual visual={content.visual} />
                </View>
              ) : null}

              <View
                className={`today__cta pressable ${saved ? 'today__cta--saved' : ''}`}
                onClick={saveCurrent}
              >
                <View className="today__cta-spark" />
                <Text className="today__cta-text">
                  {saved
                    ? `${DAILY_COPY.savedPrefix}${bucketName}`
                    : `${DAILY_COPY.saveAction}${bucketName}`}
                </Text>
                <View className="today__cta-arrow" />
                <View className="today__cta-tick" />
                <View className="today__cta-tick today__cta-tick--short" />
              </View>
            </View>
          ) : null}

          {content ? (
            <View className="today__sec pressable" onClick={goHistory}>
              <View className="today__sec-tick" />
              <Text className="today__sec-title">{DAILY_HISTORY_COPY.entryLabel}</Text>
              <Text className="today__sec-go">›</Text>
            </View>
          ) : null}

          {/* 今日造型状态行（闭环枢纽）：内容 → 试试 → 反馈 → 明天再来 */}
          {content ? (
            <View className="today__tryon pressable" onClick={goPlan}>
              {tryonState === 'ready' && (tryonPlan?.mediaUrl ?? '') !== '' ? (
                <Image
                  className="today__tryon-thumb"
                  src={tryonPlan?.mediaUrl ?? ''}
                  mode="aspectFill"
                />
              ) : (
                <View className="today__tryon-thumb today__tryon-thumb--empty">
                  {tryonState === 'working' ? (
                    <View className="today__tryon-spin spinner" />
                  ) : (
                    <View className="today__tryon-plus">
                      <View className="today__tryon-plus-bar" />
                      <View className="today__tryon-plus-bar today__tryon-plus-bar--v" />
                    </View>
                  )}
                </View>
              )}
              <View className="today__tryon-main">
                <Text className="today__tryon-title">
                  {tryonState === 'ready'
                    ? tryonPlan?.title || DAILY_COPY.tryonReady
                    : tryonState === 'working'
                      ? DAILY_COPY.tryonWorking
                      : DAILY_COPY.tryonEmpty}
                </Text>
                <Text className="today__tryon-sub">
                  {tryonState === 'ready'
                    ? DAILY_COPY.tryonReady
                    : tryonState === 'working'
                      ? DAILY_COPY.dressCaptionSub
                      : DAILY_COPY.trySuggestions}
                </Text>
              </View>
              {tryonState === 'working' ? null : (
                <Text className="today__tryon-arrow">›</Text>
              )}
            </View>
          ) : null}

          {content ? (
            <View className="today__handbook pressable" onClick={goHandbook}>
              <View className="today__handbook-icon" />
              <Text className="today__handbook-title">{DAILY_COPY.handbookTitle}</Text>
              <View className="today__handbook-count">
                <Text className="today__handbook-count-num">{saves.length}</Text>
              </View>
              <Text className="today__handbook-go">›</Text>
            </View>
          ) : null}

          {phase === 'offline' && !content ? (
            <View className="today__empty">
              <Text className="today__empty-title">{DAILY_COPY.offlineTitle}</Text>
              <Text className="today__empty-body">{DAILY_COPY.offlineBody}</Text>
              <View className="today__empty-cta" onClick={() => void runRetry()}>
                <Text className="today__empty-cta-text">{DAILY_COPY.retryAction}</Text>
              </View>
            </View>
          ) : null}

          {/* 页脚的纸样台余韵：一道弧线 + 一角苔绿，不承载信息 */}
          <View className="today__ground">
            <View className="today__ground-arc" />
            <View className="today__ground-blob" />
          </View>
        </View>
      )}
    </View>
  )
}

/** offline 空态的下一步动作：重新进入页面即重跑状态机 */
function runRetry(): void {
  void Taro.reLaunch({ url: '/pages/today/index' })
}
