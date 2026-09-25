// 今日：每天一条内容（轻模式）+ 引导去生成今日造型（重模式）。
//
// 美学参照：Aesop / COS 的编辑式极简。
//   一个画面一个主角（视觉），其余全部降权：小字 meta、细线分隔、文字链次级动作。
//
// 状态机（方案 §3.1）：loading → waiting（按 scenario 播等待动画）→ settling
// （generate 返回，动画落位 1.2s）→ content；cacheHit 直接进 content；
// 网络错误 → offline 读本地缓存。generate 永远 200，没有「生成失败」分支，
// fallback 内容只是角标写「今日精选」。
//
// 重模式不在这里并列：它是「看看我穿这样」这个 action 的结果，跳 life 分包的今日造型页。

import { useCallback, useState } from 'react'
import Taro, { useDidShow } from '@tarojs/taro'
import { Image, Text, View } from '@tarojs/components'
import { DAILY_COPY, dailyTypeName } from '@zsm/core'
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
              {ctx.city !== '' ? <Text className="today__ctx-city">{ctx.city}</Text> : null}
            </View>
          ) : null}

          {(phase === 'waiting' || phase === 'settling') && !content ? (
            <DailyWaiting scenario={scenario} settling={phase === 'settling'} />
          ) : null}

          {content ? (
            <View
              className={`today__visual ${
                content.visual.modality === 'poster'
                  ? 'today__visual--tall'
                  : content.visual.modality === 'swatch' || content.visual.modality === 'compare'
                    ? 'today__visual--flat'
                    : ''
              }`}
            >
              <DailyVisual visual={content.visual} />
            </View>
          ) : null}

          {content ? (
            <View className="today__body">
              <Text className="today__meta">
                {isFallback ? DAILY_COPY.fallbackBadge : dailyTypeName(content.type)} · 第 {seq} 条
              </Text>
              <Text className="today__title">{content.topic}</Text>
              <Text className="today__lead">{content.lead}</Text>

              <View className="today__rule" />

              <View className="today__fit">
                <Text className="today__fit-text">{content.fitText}</Text>
              </View>

              <Text className="today__why">{content.why}</Text>

              <View className="today__cta" onClick={saveCurrent}>
                <Text className="today__cta-text">
                  {saved ? `${DAILY_COPY.savedPrefix}${bucketName}` : `${DAILY_COPY.saveAction}${bucketName}`}
                </Text>
              </View>
              {/* 行动行：试试从「看」升级为「做」——把今日建议拿去生成造型 */}
              <View className="today__try-row pressable" onClick={goPlan}>
                <Text className="today__try-row-text">{DAILY_COPY.trySuggestions}</Text>
                <Text className="today__try-row-arrow">›</Text>
              </View>

              <View className="today__rule" />

              {/* 今日造型状态行（闭环枢纽）：内容 → 试试 → 反馈 → 明天再来 */}
              <View className="today__tryon pressable" onClick={goPlan}>
                {tryonState === 'ready' && (tryonPlan?.mediaUrl ?? '') !== '' ? (
                  <Image
                    className="today__tryon-thumb"
                    src={tryonPlan?.mediaUrl ?? ''}
                    mode="aspectFill"
                  />
                ) : (
                  <View className="today__tryon-thumb today__tryon-thumb--empty">
                    <Text className="today__tryon-thumb-mark">
                      {tryonState === 'working' ? '⟩' : '＋'}
                    </Text>
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
                {tryonState === 'working' ? (
                  <View className="today__tryon-spin spinner" />
                ) : (
                  <Text className="today__tryon-arrow">›</Text>
                )}
              </View>

              <View className="today__rule" />
              <View className="today__handbook" onClick={goHandbook}>
                <Text className="today__handbook-title">{DAILY_COPY.handbookTitle}</Text>
                <Text className="today__handbook-go">{saves.length} ›</Text>
              </View>
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
        </View>
      )}
    </View>
  )
}

/** offline 空态的下一步动作：重新进入页面即重跑状态机 */
function runRetry(): void {
  void Taro.reLaunch({ url: '/pages/today/index' })
}
