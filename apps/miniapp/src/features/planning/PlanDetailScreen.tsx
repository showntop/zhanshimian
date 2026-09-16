// 方案详情：旧线（recovery/ui-0911）全屏沉浸视觉在新数据模型上的恢复——
// 照片 fixed 钉在上半屏、导航透明、奶油渐变内容板在文档流，上滑盖过照片看全量步骤。
// 架构不让步的部分：
// 1. 对比左图严格来自方案集绑定的那一份报告，右图只用这一套自己的 render.media；
//    绑定不了就空态，绝不拿"手头最近一份报告"的照片凑对比；
// 2. 渲染没就绪时给渲染状态条，绝不拿旧图顶上——"上次那张"不是这次的证据；
// 3. 步骤是全量列表（旧线只摆当前分类第一条），在板的文档流里随页面上滑展开。
import { useCallback, useEffect, useState } from 'react'
import Taro from '@tarojs/taro'
import { Text, View } from '@tarojs/components'
import { ERROR_COPY, CHECKLIST_COPY, PLANNING_COPY, PLANS_COPY, PLAN_DETAIL_COPY, SOURCE_IMAGE_COPY, planSlotLabel } from '@zsm/core'
import type { PlanSet, PlanStep, Report } from '@zsm/core'
import { qualityApi } from '../../app/api/quality'
import { PublicApiError } from '../../app/api/result'
import { resourceCache, resourceKey } from '../../app/cache/resource-cache'
import { selectAndCreateExecution } from '../execution/start'
import GenerationFeedback from '../feedback/GenerationFeedback'
import ErrorState from '../../components/error-state'
import RenderState from '../../components/render-state'
import Skeleton from '../../components/skeleton'
import SourceImage from '../../components/source-image'
import CompareSlider from '../../components/compare-slider'
import PrimaryButton from '../../components/primary-button'
import { getNavMetrics } from '../../components/app-header'
import { boundBodyMedia, sortedVariants, stepActionText, stepDetailLines, variantRenderView } from './model'
import './index.scss'

const CATEGORIES = [
  { key: 'hair', label: '发型' },
  { key: 'makeup', label: '妆容' },
  { key: 'outfit', label: '穿搭' },
] as const

// 满屏相框的宽高比：SourceImage 顶对齐自动铺满据此在裁底/裁侧之间选择。
// 相框高 = 视口 62vh（.pd 只钉上半屏），不是整屏——裁切比例必须跟着相框走
const NAV = getNavMetrics()
const PLAN_FRAME_ASPECT = NAV.windowWidth / (NAV.windowHeight * 0.62)

interface PlanDetailScreenProps {
  planSetId: string
  variantId: string
}

export default function PlanDetailScreen({ planSetId, variantId }: PlanDetailScreenProps) {
  const [planSet, setPlanSet] = useState<PlanSet | null>(() =>
    resourceCache.read<PlanSet>(resourceKey('plan-set', planSetId)) ?? null,
  )
  const [boundReport, setBoundReport] = useState<Report | null>(null)
  const [failed, setFailed] = useState(false)
  const [selecting, setSelecting] = useState(false)
  const [activeCategory, setActiveCategory] = useState<'hair' | 'makeup' | 'outfit'>('hair')

  const load = useCallback(async () => {
    setFailed(false)
    try {
      // 绑定报告必须重新对账：本地缓存里的可能是上一份报告
      const nextSet = await resourceCache.revalidate(resourceKey('plan-set', planSetId), () =>
        qualityApi.getPlanSet(planSetId),
      )
      setPlanSet(nextSet)
      const nextReport = await qualityApi.getReport(nextSet.report_id)
      resourceCache.write(resourceKey('report', nextReport.id), nextReport)
      setBoundReport(nextReport)
    } catch {
      setFailed(true)
    }
  }, [planSetId])

  useEffect(() => {
    void load()
  }, [load])

  // 没有完整参数就无法定位一套方案：回方案 tab，不猜「最近在看的那套」
  useEffect(() => {
    if (!planSetId || !variantId) void Taro.switchTab({ url: '/pages/plans/index' })
  }, [planSetId, variantId])

  const variant = planSet ? sortedVariants(planSet).find((v) => v.id === variantId) ?? null : null
  const render = variant ? variantRenderView(variant) : null

  const categories = (variant?.steps ?? [])
    .map((step: PlanStep) => step.category)
    .filter((value, index, all) => all.indexOf(value) === index)
  useEffect(() => {
    if (categories.length > 0 && !categories.includes(activeCategory)) {
      setActiveCategory(categories[0] as 'hair' | 'makeup' | 'outfit')
    }
  }, [categories, activeCategory])

  const steps: PlanStep[] = (variant?.steps ?? [])
    .filter((step: PlanStep) => step.category === activeCategory)
    .sort((a, b) => a.position - b.position)

  /** 「选这套」：PUT Selection → POST Execution → 带着快照进清单页。 */
  const selectThis = async () => {
    if (!planSet || !variant || selecting) return
    setSelecting(true)
    try {
      const { execution } = await selectAndCreateExecution(planSet, variant)
      await Taro.navigateTo({
        url: `/pages/checklist/index?execution_id=${encodeURIComponent(execution.id)}`,
      })
    } catch (error) {
      const message = error instanceof PublicApiError && error.message ? error.message : CHECKLIST_COPY.selectFailed
      Taro.showToast({ title: message, icon: 'none' })
    } finally {
      setSelecting(false)
    }
  }

  // 对比左图：绑定校验不过就抛错——这里把它接住并呈现为空态，绝不退回"随便哪份报告"
  let leftMedia: ReturnType<typeof boundBodyMedia> = null
  let bindingError = false
  if (variant && boundReport) {
    try {
      leftMedia = boundBodyMedia(planSet ?? { id: planSetId, report_id: boundReport.id }, boundReport)
    } catch {
      bindingError = true
    }
  }

  // 加载中与错误态：盖一层浅色整屏（本页外壳是黑底沉浸页，状态组件不压照片）
  if (failed && !planSet) {
    return (
      <View className="pd-plain">
        <ErrorState
          title={PLAN_DETAIL_COPY.loadFailed}
          retryText={ERROR_COPY.retryAction}
          onRetry={() => void load()}
        />
      </View>
    )
  }

  if (!planSet) {
    return (
      <View className="pd-plain">
        <Skeleton rows={5} />
      </View>
    )
  }

  if (!variant || !render || !planSetId || !variantId) {
    return (
      <View className="pd-plain">
        <ErrorState
          title={PLAN_DETAIL_COPY.loadFailed}
          retryText={ERROR_COPY.retryAction}
          onRetry={() => void load()}
        />
      </View>
    )
  }

  return (
    <>
      {/* hero（fixed 钉在上半屏，不再满屏——满屏会在滚动惯性/橡皮筋时从板底漏出）：
          ready 时拖动对比，未 ready 单图（左）+ 板上状态条 */}
      <View className="pd" style={{ ['--pd-nav' as string]: `${NAV.navHeight}px` }}>
        <View className="pd__hero">
          <View className="pd__hero-frame">
            {bindingError || !leftMedia ? (
              <View className="pd__hero-empty">
                <Text className="pd__hero-empty-text">{SOURCE_IMAGE_COPY.userPhotoEmpty}</Text>
              </View>
            ) : render.kind === 'ready' ? (
              <CompareSlider
                current={<SourceImage className="pd__hero-img" media={leftMedia} anchor="top" frameAspect={PLAN_FRAME_ASPECT} />}
                plan={<SourceImage className="pd__hero-img" media={render.media} anchor="top" frameAspect={PLAN_FRAME_ASPECT} />}
                currentLabel={PLANNING_COPY.currentLabel}
                planLabel={PLANNING_COPY.planLabel}
              />
            ) : (
              <SourceImage className="pd__hero-img" media={leftMedia} anchor="top" frameAspect={PLAN_FRAME_ASPECT} />
            )}
          </View>
        </View>
      </View>

      {/* 奶油渐变内容板（文档流）：渲染状态 + 方案名 + 分类 tab + 全量步骤 + CTA。
          margin-top 让出首屏照片区，上滑整板盖过照片——hint 承诺的交互。
          不嵌 ScrollView：微信 ScrollView 在只有 max-height 的父级里塌成 0 高。 */}
      <View className="pd__board">
        {render.kind !== 'ready' ? (
          <View className="pd__render-state">
            <RenderState view={render} />
          </View>
        ) : null}

        <View className="pd__head">
          <Text className="pd__series">{planSlotLabel(variant.name, variant.slot)}</Text>
          {categories.length > 1 ? (
            <View className="pd__tabs">
              {CATEGORIES.filter((cat) => categories.includes(cat.key)).map((cat) => (
                <Text
                  key={cat.key}
                  className={`pd__tab ${activeCategory === cat.key ? 'pd__tab--active' : ''}`}
                  onClick={() => setActiveCategory(cat.key)}
                >
                  {cat.label}
                </Text>
              ))}
            </View>
          ) : null}
        </View>

        {/* hint 放实色区（板头之后）：压在板顶渐变的半透明区会浮在照片脸上 */}
        <Text className="pd__hint">{PLAN_DETAIL_COPY.boardHint}</Text>

        <View className="pd__steps">
          {variant.descriptor ? <Text className="pd__desc">{variant.descriptor}</Text> : null}
          {variant.rationale ? <Text className="pd__why">{variant.rationale}</Text> : null}
          {steps.map((step) => (
            <View key={step.id} className="pd__step">
              <View className="pd__step-head">
                <Text className={`pd__step-action pd__step-action--${step.action}`}>
                  {stepActionText(step)}
                </Text>
                <Text className="pd__step-title">{step.title}</Text>
              </View>
              {step.summary ? <Text className="pd__step-summary">{step.summary}</Text> : null}
              {stepDetailLines(step).length > 0 ? (
                <View className="pd__step-details">
                  {stepDetailLines(step).map((line) => (
                    <View key={line.label} className="pd__step-detail">
                      <Text className="pd__step-detail-label">{line.label}</Text>
                      <Text className="pd__step-detail-value">{line.value}</Text>
                    </View>
                  ))}
                </View>
              ) : null}
            </View>
          ))}
          {steps.length === 0 ? (
            <Text className="pd__empty-step">{PLAN_DETAIL_COPY.emptyStep}</Text>
          ) : null}
        </View>

        <View className="pd__foot">
          <PrimaryButton
            text={PLANS_COPY.cta}
            loading={selecting}
            onClick={() => void selectThis()}
          />
          {/* 反馈绑定用户真正看到的那张 publication；没发布就没有入口 */}
          <GenerationFeedback
            publicationId={variant.render.publication_id}
            className="pd__feedback-entry"
          />
          <Text className="pd__bound-note">{PLANNING_COPY.boundNote}</Text>
        </View>
      </View>
    </>
  )
}
