// 卡堆决策台 UI：满幅沉浸卡——照片驱动卡高（满宽完整展示全身，无裁切），
// 收益词/方案名浮在底部奶油渐变上；双层卡堆景深，左滑跳过/右滑喜欢盖章飞出。
// 纯展示组件——决策状态机在 model.ts，网络同步在 PlansScreen。
import { useEffect, useRef, useState } from 'react'
import Taro from '@tarojs/taro'
import { ScrollView, Text, View } from '@tarojs/components'
import { PLANNING_COPY } from '@zsm/core'
import type { DisplayMedia, PlanVariant } from '@zsm/core'
import type { DecisionKind, DecisionStack } from './model'
import {
  decidedCards,
  isStackEnded,
  stackProgress,
  topCard,
  variantRenderView,
} from './model'
import RenderState from '../../components/render-state'
import SourceImage from '../../components/source-image'
import SwipeCard from '../../components/swipe-card'
import TextLink from '../../components/text-link'
import { getNavMetrics } from '../../components/app-header'
import './plans-deck.scss'

const RENDER_IN_FLIGHT = new Set(['queued', 'generating', 'checking'])

// 一屏预算：nav 以下扣掉场景 tab、进度行、控制组、说明行、行距，以及
// 页面外壳的底部呼吸（.page--deck 的 safe-area + 20rpx）。
// 204 = tab 行 40 + 进度行 20 + 行距 50 + 控制组 62 + 说明行 14 + 底垫 10pt
// + 安全余量 8。纸面算术在真机上总有缝（导航测量、字体行高、平台差异），
// 所以预算只做初值，挂载/换图后由 fitStageToViewport 实测收缩兜底。
// 照片比例已知后「高定宽」——放不下就等比缩小卡宽，全身照永不裁切。
// px 是 rpx 规约的显式例外。
const NAV = getNavMetrics()
const SYSTEM = (() => {
  try {
    return Taro.getSystemInfoSync()
  } catch {
    return { windowWidth: 375, windowHeight: 667, safeArea: null }
  }
})()
/** 外壳底垫 .page--deck 的 bottom = safe-area + 20rpx（10pt）。 */
const SAFE_INSET = (() => {
  const area = SYSTEM.safeArea
  if (!area || !SYSTEM.screenHeight) return 0
  return Math.max(0, SYSTEM.screenHeight - area.bottom)
})()
const FULL_WIDTH = SYSTEM.windowWidth - 32
// 实测收缩的允许下限：说明行底边到屏底只留 4rpx（2px）+ safe-area。
// 原来留 20rpx（10px），一屏里底部空出一截、整屏看着偏上——这一项是
// 「整体往下挪」真正生效的杠杆（在页面上加 padding-top 会被实测 1:1 扣回卡高）。
const PAGE_BOTTOM_PX = 2 + SAFE_INSET
// 208 = tab 行 40 + 进度行 20 + 行距 50 + 控制组 62 + 说明行 14
// + 底垫 2 + 安全余量 6 + 舞台底缘出血 14（28rpx，纸堆下缘错落的余量，
// 见 plans-deck.scss &__stage）。出血不计进去初值就会偏高、首帧再被实测收缩。
const STAGE_BUDGET_BASE = Math.max(320, SYSTEM.windowHeight - NAV.navHeight - (208 + SAFE_INSET))
const HINT_ROW_PX = 40
/** 渲染未就绪时按 1.35 竖版预估，onLoad 后校正。 */
const ESTIMATE_ASPECT = 1.35

interface StageSize {
  width: number
  height: number
}

const estimateStageSize = (budget: number): StageSize => {
  return { width: Math.min(FULL_WIDTH, Math.round(budget / ESTIMATE_ASPECT)), height: budget }
}

interface PlansDeckProps {
  stack: DecisionStack
  /** 当前方案集的新鲜 variant 快照：渲染状态以它为准，决策以 stack 为准。 */
  variants: PlanVariant[]
  /** 渲染未就绪卡的占位照片（方案集绑定的用户原本照），没有就退苔绿浅底。 */
  fallbackMedia: DisplayMedia | null
  pastBadge: boolean
  /** 新一轮受理在途、屏上是上一轮：进度行给「制作中」chip，结算区改口「上一轮」。 */
  generatingNext: boolean
  /** 重新生成的受理失败公开文案（''= 没有失败）：「生成失败」链接 + 点击弹原因。 */
  regenError: string
  /** 新一轮受理的真实进度（0-100，来自受理 operation 轮询）；null = 暂无快照，走扫描动画。 */
  nextProgress: number | null
  /** 其他方案集数量（含往期）：>0 时进度行右侧出现「往期 N ›」入口。 */
  pastCount: number
  hintVisible: boolean
  retryingId: string
  /** 决策/撤销网络在途：按钮与手势一起禁用，防连点把决策打到下一张卡上。 */
  busy: boolean
  onDecide: (variantId: string, decision: DecisionKind) => void
  onUndo: () => void
  onHintDismiss: () => void
  onOpenHistory: () => void
  onBackToLatest: () => void
  onOpenDetail: (variant: PlanVariant) => void
  onRegenerate: () => void
  onRetryRender: (variant: PlanVariant) => void
  /** 结算区「重试生成」：把已决但没图的套按顺序补生成（PlansScreen 串行）。 */
  onRetryAll: (variants: PlanVariant[]) => void
}

export default function PlansDeck({
  stack,
  variants,
  fallbackMedia,
  pastBadge,
  generatingNext,
  regenError,
  nextProgress,
  pastCount,
  hintVisible,
  retryingId,
  busy,
  onDecide,
  onUndo,
  onHintDismiss,
  onOpenHistory,
  onBackToLatest,
  onOpenDetail,
  onRegenerate,
  onRetryRender,
  onRetryAll,
}: PlansDeckProps) {
  const budget = STAGE_BUDGET_BASE - (hintVisible && !busy ? HINT_ROW_PX : 0)
  const [stage, setStage] = useState<StageSize>(() => estimateStageSize(budget))
  const aspectRef = useRef<number | null>(null)
  const ended = isStackEnded(stack)
  // 成衣拼贴高度 = 一屏吃满再留一点呼吸：视口 - 导航 - 链接行/边距 - 底部呼吸 24px。
  // 照片顶对齐铺满此框（多余裁底，头永远在）
  const COLLAGE_CHROME_PX = 120
  const collageHeight = Math.max(
    340,
    Math.min(640, SYSTEM.windowHeight - NAV.navHeight - COLLAGE_CHROME_PX - SAFE_INSET),
  )

  // 预算变化（提示出现/收掉）时按已知的照片比例重排卡尺寸
  useEffect(() => {
    setStage((prev) => {
      const aspect = aspectRef.current ?? ESTIMATE_ASPECT
      const height = Math.min(budget, Math.round(FULL_WIDTH * aspect))
      const width = Math.min(FULL_WIDTH, Math.round(height / aspect))
      return width === prev.width && height === prev.height ? prev : { width, height }
    })
  }, [budget])

  // 纸面预算的实测兜底：量「说明行底边」到视口可用的距离，超了就收缩舞台。
  // 全部用文档坐标差值（滚动不变量），页面滚到哪儿量出来的都一样；
  // 只缩不涨 + 1px 阈值，收敛即停。
  useEffect(() => {
    if (ended) return
    const timer = setTimeout(() => {
      const query = Taro.createSelectorQuery()
      query.select('.plans').boundingClientRect()
      query.select('.plans-deck__note').boundingClientRect()
      query.selectViewport().scrollOffset()
      query.exec((res) => {
        const plans = res?.[0]
        const note = res?.[1]
        const scroll = res?.[2]
        if (!plans || !note) return
        const scrollTop = scroll?.scrollTop ?? 0
        const plansTopDoc = plans.top + scrollTop
        const available = SYSTEM.windowHeight - plansTopDoc - PAGE_BOTTOM_PX
        const used = note.bottom + scrollTop - plansTopDoc
        const overflow = used - available
        if (overflow <= 1) return
        setStage((prev) => {
          const height = Math.max(300, prev.height - Math.ceil(overflow))
          const width = Math.min(FULL_WIDTH, Math.round(height / (aspectRef.current ?? ESTIMATE_ASPECT)))
          return width === prev.width && height === prev.height ? prev : { width, height }
        })
      })
    }, 80)
    return () => clearTimeout(timer)
    // 换卡 / busy 翻转也会改变布局（卡高、提示行），一并触发重测；
    // 决策台上方的条件横幅（制作中/切换中）出现在 PlansScreen，锁死的
    // overflow:hidden 保证那种瞬态最多轻微遮挡，不会产生滚动
  }, [stage.height, budget, ended, stack.cursor, busy])
  const top = topCard(stack)
  const under1 = stack.cards[stack.cursor + 1] ?? null
  const under2 = stack.cards[stack.cursor + 2] ?? null
  const progress = stackProgress(stack)
  const liked = decidedCards(stack, 'like')
  const skipped = decidedCards(stack, 'skip')
  // 渲染内容永远取最新 variant（轮询刷新后生成图/状态跟上来），决策取 stack
  const freshById = new Map(variants.map((variant) => [variant.id, variant]))
  // 结算区的失败汇总：已决的套里有没图的，给一句说明 + 一次性的「重试生成」
  const failedDecided = [...liked, ...skipped].filter((variant) => {
    const render = variantRenderView(freshById.get(variant.id) ?? variant)
    return render.kind === 'failed' || render.kind === 'unavailable'
  })
  const renderFailedNote =
    failedDecided.length > 0 ? (
      <View className="plans-deck__result-failed">
        <Text className="plans-deck__result-failed-text">
          {failedDecided.length} {PLANNING_COPY.resultRenderFailedNote}
        </Text>
        <TextLink
          className="plans-deck__result-failed-retry"
          text={PLANNING_COPY.resultRetryAll}
          onClick={() => onRetryAll(failedDecided)}
        />
      </View>
    ) : null

  // 照片真实宽高 → 卡尺寸：一屏放不下就等比缩宽（全身完整的关键，宁小勿裁）
  const handleDims = (event: { detail: { width: number | string; height: number | string } }) => {
    const w = Number(event.detail.width)
    const h = Number(event.detail.height)
    if (!w || !h) return
    const aspect = h / w
    aspectRef.current = aspect
    setStage((prev) => {
      const height = Math.min(budget, Math.round(FULL_WIDTH * aspect))
      const width = Math.min(FULL_WIDTH, Math.round(height / aspect))
      return width === prev.width && height === prev.height ? prev : { width, height }
    })
  }

  const renderPhoto = (variant: PlanVariant, onDims?: typeof handleDims) => {
    const fresh = freshById.get(variant.id) ?? variant
    const render = variantRenderView(fresh)
    if (render.kind === 'ready') {
      return (
        <SourceImage
          className="plans-deck__img"
          media={render.media}
          mode="widthFix"
          onLoad={onDims}
        />
      )
    }
    // 未就绪：原本照占位（没有退苔绿浅底），状态浮层居中（失败可就地重试）
    return (
      <>
        {fallbackMedia ? (
          <SourceImage className="plans-deck__img" media={fallbackMedia} mode="widthFix" onLoad={onDims} />
        ) : null}
        <View className="plans-deck__state">
          <View className="plans-deck__state-card">
            <RenderState view={render} onRetry={() => onRetryRender(fresh)} />
          </View>
        </View>
      </>
    )
  }

  const renderCard = (variant: PlanVariant, onDims?: typeof handleDims, interactive = false) => {
    const fresh = freshById.get(variant.id) ?? variant
    return (
      <View className="plans-deck__body">
        <View className="plans-deck__photo">{renderPhoto(variant, onDims)}</View>
        {/* 底部奶油渐变：照片融进文字，全身照完整呈现 */}
        <View className="plans-deck__veil" />
        <View className="plans-deck__meta">
          {fresh.outcome_tags.length > 0 ? (
            <View className="plans-deck__tags">
              {fresh.outcome_tags.slice(0, 3).map((tag) => (
                <Text key={tag} className="plans-deck__tag">{tag}</Text>
              ))}
            </View>
          ) : null}
          <Text className="plans-deck__name">{fresh.name}</Text>
          {fresh.descriptor ? <Text className="plans-deck__desc">{fresh.descriptor}</Text> : null}
          {interactive ? (
            <View
              className="plans-deck__detail-chip pressable"
              onClick={() => onOpenDetail(fresh)}
            >
              <Text className="plans-deck__detail-chip-text">{PLANNING_COPY.viewDetail}</Text>
              <Text className="plans-deck__detail-chip-chevron">›</Text>
            </View>
          ) : null}
        </View>
      </View>
    )
  }

  return (
    <View className="plans-deck">
      {/* 进度行：本轮 N 套 + 分段进度条 + 「往期 N ›」入口（历史收进弹层，
          文档流里不再有第二个区块——页面才有条件锁死一屏） */}
      <View className="plans-deck__progress">
        {pastBadge ? (
          <>
            <Text className="plans-deck__past-badge">{PLANNING_COPY.historyBadge}</Text>
            <TextLink className="plans-deck__back-latest" text={PLANNING_COPY.historyBackLatest} onClick={onBackToLatest} />
          </>
        ) : generatingNext ? (
          // 新一轮在途：chip 替代「往期/回到最新」——此时"最新"还没发布
          <View className="plans-deck__next-chip">
            <View className="plans-deck__next-spin spinner" />
            <Text className="plans-deck__next-text">{PLANNING_COPY.nextRoundGenerating}</Text>
          </View>
        ) : (
          <>
            <Text className="plans-deck__round">
              {PLANNING_COPY.deckRoundPrefix} {progress.total} {PLANNING_COPY.deckRoundSuffix}
            </Text>
            <View className="plans-deck__bar">
              {stack.cards.map((card, index) => (
                <View
                  key={card.variant.id}
                  className={`plans-deck__seg ${card.decision !== null ? 'plans-deck__seg--done' : ''} ${
                    index === stack.cursor ? 'plans-deck__seg--top' : ''
                  }`}
                />
              ))}
            </View>
            {/* ended 后结果区有更大的「往期 N 轮 ›」入口，这里不再重复 */}
            {pastCount > 0 && !ended ? (
              <Text className="plans-deck__history-entry pressable" onClick={onOpenHistory}>
                {PLANNING_COPY.historyEntry} {pastCount} ›
              </Text>
            ) : null}
          </>
        )}
      </View>

      {/* 缝线进度条：新一轮在途时贴在进度行下（8rpx，不挤一屏预算）。
          有真实进度按百分比走，暂无快照时来回扫描 */}
      {generatingNext ? (
        <View className="plans-deck__threadbar">
          <View
            className={`plans-deck__threadbar-fill ${nextProgress === null ? 'plans-deck__threadbar-fill--sweep' : ''}`}
            style={nextProgress === null ? undefined : { transform: `scaleX(${nextProgress / 100})` }}
          />
        </View>
      ) : null}

      {ended ? (
        <View className="plans-deck__result">
          {/* 成衣拼贴：本轮的交付仪式。大图 = 首张喜欢，右上盖「喜欢」章；
              其余已决方案（最多 2 张，每轮 3 套封顶）作小拍立得斜嵌大图下角，
              各带自己的态度章（♥ 苔绿 / ✕ 墨灰）。点任何一张进详情。
              0 喜欢给裁缝店节拍的空态构图 */}
          {(() => {
            const first = liked[0]
            if (!first) {
              return (
                <View className="plans-deck__result-none">
                  <View className="plans-deck__result-none-paper">
                    <View className="plans-deck__result-none-stitch" />
                  </View>
                  <Text className="plans-deck__result-empty">{PLANNING_COPY.resultLikedEmpty}</Text>
                </View>
              )
            }
            const firstFresh = freshById.get(first.id) ?? first
            const firstRender = variantRenderView(firstFresh)
            // 小拍立得 = 其余已决（喜欢在前、跳过在后），每轮 3 套封顶 → 最多 2 张
            const smalls = [...liked.slice(1), ...skipped].slice(0, 2)
            return (
              <View className="plans-deck__collage">
                <View
                  className="plans-deck__hero pressable"
                  style={{ height: `${collageHeight}px` }}
                  onClick={() => onOpenDetail(firstFresh)}
                >
                  <View className="plans-deck__hero-photo">
                    {firstRender.kind === 'ready' ? (
                      // anchor="top"：人像短框顶对齐 cover，头不切、腿裁底
                      <SourceImage media={firstRender.media} anchor="top" />
                    ) : (
                      <>
                        {fallbackMedia ? (
                          <SourceImage media={fallbackMedia} anchor="top" />
                        ) : null}
                        <View className="plans-deck__state">
                          <View className="plans-deck__state-card">
                            <RenderState view={firstRender} onRetry={() => onRetryRender(firstFresh)} />
                          </View>
                        </View>
                      </>
                    )}
                  </View>
                  <View className="plans-deck__veil" />
                  {/* 喜欢章：与卡堆的盖章语言同源，斜盖在右上 */}
                  <View className="plans-deck__stamp plans-deck__stamp--like">
                    <Text>♥ {PLANNING_COPY.resultLikedGroup}</Text>
                  </View>
                  <View className="plans-deck__meta">
                    {firstFresh.outcome_tags.length > 0 ? (
                      <View className="plans-deck__tags">
                        {firstFresh.outcome_tags.slice(0, 3).map((tag) => (
                          <Text key={tag} className="plans-deck__tag">{tag}</Text>
                        ))}
                      </View>
                    ) : null}
                    <Text className="plans-deck__name">{firstFresh.name}</Text>
                    {firstFresh.descriptor ? <Text className="plans-deck__desc">{firstFresh.descriptor}</Text> : null}
                    <View className="plans-deck__detail-chip pressable" onClick={() => onOpenDetail(firstFresh)}>
                      <Text className="plans-deck__detail-chip-text">{PLANNING_COPY.viewDetail}</Text>
                      <Text className="plans-deck__detail-chip-chevron">›</Text>
                    </View>
                  </View>
                </View>
                {smalls.map((variant, index) => {
                  const fresh = freshById.get(variant.id) ?? variant
                  const render = variantRenderView(fresh)
                  const decision = liked.slice(1).includes(variant) ? 'like' : 'skip'
                  return (
                    <View
                      key={variant.id}
                      className={`plans-deck__mini plans-deck__mini--${index === 0 ? 'left' : 'right'} pressable`}
                      onClick={() => onOpenDetail(fresh)}
                    >
                      <View className="plans-deck__mini-photo">
                        {render.kind === 'ready' ? (
                          <SourceImage media={render.media} anchor="top" />
                        ) : (
                          <Text className="plans-deck__mini-failed">
                            {render.kind === 'unavailable' ? PLANNING_COPY.renderThumbUnavailable : PLANNING_COPY.renderThumbFailed}
                          </Text>
                        )}
                      </View>
                      <Text className={`plans-deck__mini-badge plans-deck__mini-badge--${decision}`}>
                        {decision === 'like' ? '♥' : '✕'} {decision === 'like' ? PLANNING_COPY.resultLikedGroup : PLANNING_COPY.resultSkippedGroup}
                      </Text>
                    </View>
                  )
                })}
              </View>
            )
          })()}
          {renderFailedNote}
          {/* 底部文字链接：往期（辅助）+ 生成新的一轮（主行动）。
              生成期间不给「生成新的」（新一轮已在路上，防重复提交） */}
          <View className="plans-deck__result-actions">
            {pastCount > 0 ? (
              <TextLink
                className="plans-deck__result-history"
                text={`${PLANNING_COPY.historyEntry} ${pastCount} ${PLANNING_COPY.historyRoundUnit} ›`}
                onClick={onOpenHistory}
              />
            ) : null}
            {generatingNext ? null : regenError ? (
              // 失败态：警示色「生成失败」，点开弹原因 + 重新生成
              <TextLink
                className="plans-deck__regen plans-deck__regen--error"
                text={PLANNING_COPY.resultRegenFailed}
                onClick={() => {
                  void Taro.showModal({
                    title: PLANNING_COPY.retryFailedTitle,
                    content: regenError,
                    confirmText: PLANNING_COPY.regenerateAction,
                    cancelText: PLANNING_COPY.cancelText,
                    success: (res) => {
                      if (res.confirm) onRegenerate()
                    },
                  })
                }}
              />
            ) : (
              <TextLink className="plans-deck__regen" text={PLANNING_COPY.resultRegenerate} onClick={onRegenerate} />
            )}
          </View>
        </View>
      ) : (
        <>
          {/* 卡堆：等比缩宽保证全身 + 一屏；双层景深，顶卡可滑、盖章飞出。
              卡居中由 wrapper 定宽定位（SwipeCard 自身的 transform 留给拖拽）。
              key 随 variant 重挂 → 手势状态复位 */}
          <View className="plans-deck__stage" style={{ height: `${stage.height}px` }}>
            {/* 桌上的碎纸片：纯装饰（无照片、不可点），只负责让纸堆显得随手撂下 */}
            <View className="plans-deck__scrap" />
            {under2 ? (
              <View
                className="plans-deck__card plans-deck__card--under2"
                style={{ width: `${stage.width}px`, height: `${stage.height}px`, marginLeft: `${-stage.width / 2}px` }}
              >
                {renderCard(under2.variant)}
              </View>
            ) : null}
            {under1 ? (
              <View
                className="plans-deck__card plans-deck__card--under1"
                style={{ width: `${stage.width}px`, height: `${stage.height}px`, marginLeft: `${-stage.width / 2}px` }}
              >
                {renderCard(under1.variant)}
              </View>
            ) : null}
            {top ? (
              <View
                className="plans-deck__card plans-deck__card--top"
                style={{ width: `${stage.width}px`, height: `${stage.height}px`, marginLeft: `${-stage.width / 2}px` }}
              >
                <SwipeCard
                  key={top.variant.id}
                  disabled={busy}
                  stampLike={PLANNING_COPY.deckLike}
                  stampSkip={PLANNING_COPY.deckSkip}
                  onDecide={(decision) => onDecide(top.variant.id, decision)}
                >
                  {renderCard(top.variant, handleDims, true)}
                </SwipeCard>
              </View>
            ) : null}
          </View>

          {/* 手势提示：文档流一行（悬浮版会盖到人脸上），点了即收 */}
          {hintVisible && !busy ? (
            <View className="plans-deck__hint-row">
              <Text className="plans-deck__hint-pill" onClick={onHintDismiss}>{PLANNING_COPY.deckHint}</Text>
            </View>
          ) : null}

          {/* 控制组（环形缩小一档）+ 说明行：全部留在一屏内 */}
          <View className="plans-deck__controls">
            <View className="plans-deck__action">
              <View
                className={`plans-deck__circle plans-deck__circle--undo pressable ${
                  stack.history.length === 0 || busy ? 'plans-deck__circle--disabled' : ''
                }`}
                onClick={() => {
                  if (stack.history.length > 0 && !busy) onUndo()
                }}
              >
                <Text className="plans-deck__glyph plans-deck__glyph--undo">↺</Text>
              </View>
              <Text className="plans-deck__action-label">{PLANNING_COPY.deckUndo}</Text>
            </View>
            <View className="plans-deck__action">
              <View
                className={`plans-deck__circle plans-deck__circle--skip pressable ${busy ? 'plans-deck__circle--disabled' : ''}`}
                onClick={() => {
                  if (top && !busy) onDecide(top.variant.id, 'skip')
                }}
              >
                <Text className="plans-deck__glyph">✕</Text>
              </View>
              <Text className="plans-deck__action-label">{PLANNING_COPY.deckSkip}</Text>
            </View>
            <View className="plans-deck__action">
              <View
                className={`plans-deck__circle plans-deck__circle--like pressable ${busy ? 'plans-deck__circle--disabled' : ''}`}
                onClick={() => {
                  if (top && !busy) onDecide(top.variant.id, 'like')
                }}
              >
                <Text className="plans-deck__glyph plans-deck__glyph--like">♥</Text>
              </View>
              <Text className="plans-deck__action-label">{PLANNING_COPY.deckLike}</Text>
            </View>
          </View>

          <View className="plans-deck__note">
            <Text className="plans-deck__note-text">{PLANNING_COPY.ctaNote}</Text>
            <TextLink className="plans-deck__note-link" text={PLANNING_COPY.regenerateAction} onClick={onRegenerate} />
          </View>
        </>
      )}
    </View>
  )
}
