// 方案详情：对比左图严格来自方案集绑定的那一份报告，右图只用这一套自己的 render.media。
// 渲染没就绪时给文字步骤与渲染状态，绝不拿旧图顶上——"上次那张"不是这次的证据。
import { useCallback, useEffect, useState } from 'react'
import Taro from '@tarojs/taro'
import { Text, View } from '@tarojs/components'
import { ERROR_COPY, PLANNING_COPY, PLAN_DETAIL_COPY, SOURCE_IMAGE_COPY, planSlotLabel } from '@zsm/core'
import type { PlanSet, PlanStep, Report } from '@zsm/core'
import { qualityApi } from '../../app/api/quality'
import { resourceCache, resourceKey } from '../../app/cache/resource-cache'
import ErrorState from '../../components/error-state'
import RenderState from '../../components/render-state'
import SourceImage from '../../components/source-image'
import CompareSlider from '../../components/compare-slider'
import { boundBodyMedia, sortedVariants, stepActionText, stepDetailLines, variantRenderView } from './model'
import './index.scss'

const CATEGORIES = [
  { key: 'hair', label: '发型' },
  { key: 'makeup', label: '妆容' },
  { key: 'outfit', label: '穿搭' },
] as const

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

  if (failed && !planSet) {
    return (
      <ErrorState
        title={PLAN_DETAIL_COPY.loadFailed}
        retryText={ERROR_COPY.retryAction}
        onRetry={() => void load()}
      />
    )
  }

  if (!variant || !render) {
    return (
      <ErrorState
        title={PLAN_DETAIL_COPY.loadFailed}
        retryText={ERROR_COPY.retryAction}
        onRetry={() => void load()}
      />
    )
  }

  return (
    <View className="plan-detail">
      <View className="plan-detail__hero fade-up">
        <View className="plan-detail__frame">
          {bindingError || !leftMedia ? (
            <View className="plan-detail__frame-empty">
              <Text className="plan-detail__frame-empty-text">
                {SOURCE_IMAGE_COPY.userPhotoEmpty}
              </Text>
            </View>
          ) : render.kind === 'ready' ? (
            <CompareSlider
              current={<SourceImage className="plan-detail__img" media={leftMedia} mode="aspectFill" />}
              plan={<SourceImage className="plan-detail__img" media={render.media} mode="aspectFill" />}
              currentLabel={PLANNING_COPY.currentLabel}
              planLabel={PLANNING_COPY.planLabel}
            />
          ) : (
            <SourceImage className="plan-detail__img" media={leftMedia} mode="aspectFill" />
          )}
        </View>
        {/* 渲染没就绪：状态条独立于左图呈现，文字步骤照常可读 */}
        {render.kind !== 'ready' ? (
          <View className="plan-detail__render-state">
            <RenderState view={render} />
          </View>
        ) : null}
        <Text className="plan-detail__bound-note">{PLANNING_COPY.boundNote}</Text>
      </View>

      <View className="plan-detail__board fade-up delay-1">
        <Text className="plan-detail__name">{planSlotLabel(variant.name, variant.slot)}</Text>
        <Text className="plan-detail__desc">{variant.descriptor}</Text>
        {variant.rationale ? <Text className="plan-detail__why">{variant.rationale}</Text> : null}

        {categories.length > 0 ? (
          <>
            <View className="plan-detail__tabs">
              {CATEGORIES.filter((cat) => categories.includes(cat.key)).map((cat) => (
                <Text
                  key={cat.key}
                  className={`plan-detail__tab ${activeCategory === cat.key ? 'plan-detail__tab--active' : ''}`}
                  onClick={() => setActiveCategory(cat.key)}
                >
                  {cat.label}
                </Text>
              ))}
            </View>

            {steps.map((step) => (
              <View key={step.id} className="plan-detail__step">
                <View className="plan-detail__step-head">
                  <Text
                    className={`plan-detail__step-action plan-detail__step-action--${step.action}`}
                  >
                    {stepActionText(step)}
                  </Text>
                  <Text className="plan-detail__step-title">{step.title}</Text>
                </View>
                {step.summary ? (
                  <Text className="plan-detail__step-summary">{step.summary}</Text>
                ) : null}
                {stepDetailLines(step).length > 0 ? (
                  <View className="plan-detail__step-details">
                    {stepDetailLines(step).map((line) => (
                      <View key={line.label} className="plan-detail__step-detail">
                        <Text className="plan-detail__step-detail-label">{line.label}</Text>
                        <Text className="plan-detail__step-detail-value">{line.value}</Text>
                      </View>
                    ))}
                  </View>
                ) : null}
              </View>
            ))}
            {steps.length === 0 ? (
              <Text className="plan-detail__empty-step">{PLAN_DETAIL_COPY.emptyStep}</Text>
            ) : null}
          </>
        ) : (
          <Text className="plan-detail__empty-step">{PLAN_DETAIL_COPY.emptyStep}</Text>
        )}
      </View>
    </View>
  )
}
