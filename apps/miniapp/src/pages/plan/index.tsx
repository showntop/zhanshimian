// 方案详情：照片铺满整屏，文案从底部奶油渐变融进画面。
import { useCallback, useEffect, useState } from 'react'
import Taro, { useLoad } from '@tarojs/taro'
import { Text, View } from '@tarojs/components'
import { IMAGE_BADGE_COPY, PLAN_DETAIL_COPY, type Plan } from '@zsm/core'
import { usePageClass } from '../../hooks/use-page-visibility'
import { api } from '../../services/api'
import { STORAGE_KEYS, readStorage, writeStorage } from '../../services/storage'
import AppHeader, { getNavMetrics } from '../../components/app-header'
import PrimaryButton from '../../components/primary-button'
import ExampleImage from '../../components/example-image'
import CompareSlider from '../../components/compare-slider'
import BottomSheet from '../../components/bottom-sheet'
import Skeleton from '../../components/skeleton'
import ErrorState from '../../components/error-state'
import './index.scss'

const CATEGORIES = [
  { key: 'hair', label: '发型' },
  { key: 'makeup', label: '妆容' },
  { key: 'outfit', label: '穿搭' },
] as const

const NAV = getNavMetrics()
const { windowWidth = 375, windowHeight = 667 } = Taro.getSystemInfoSync()
const PLAN_FRAME_ASPECT = windowWidth / windowHeight

/** 发型师参考卡参数（G2）：来自方案步骤 details 的 hair_spec；缺失则隐藏入口。 */
interface HairSpec {
  length?: string
  fringe?: string
  texture?: string
}

function pickHairSpec(plan: Plan | null): HairSpec | null {
  const step = plan?.steps?.find((s) => s.category === 'hair')
  if (!step) return null
  const details = step.details as { hair_spec?: HairSpec } | undefined
  const spec = details?.hair_spec
  if (!spec || (!spec.length && !spec.fringe && !spec.texture)) return null
  return spec
}

export default function PlanDetail() {
  const [planId, setPlanId] = useState('')
  const [plan, setPlan] = useState<Plan | null>(null)
  const [currentImage, setCurrentImage] = useState('')
  const [active, setActive] = useState<string>('hair')
  const [loading, setLoading] = useState(true)
  const [failed, setFailed] = useState(false)
  const [sheetOpen, setSheetOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const pageClass = usePageClass(!loading || Boolean(plan), !loading && plan && !failed ? 'page--plan' : '')

  useLoad((options) => {
    if (options?.id) setPlanId(options.id)
  })

  const load = useCallback(async () => {
    if (!planId) return
    setLoading(true)
    setFailed(false)
    try {
      const item = await api.getPlan(planId)
      setPlan(item)
      if (item.current_image_url) setCurrentImage(item.current_image_url)
      const reportId = readStorage(STORAGE_KEYS.reportId)
      if (reportId) {
        const analysis = await api
          .getReport(reportId)
          .then((r) => api.getAnalysis(r.analysis_id))
          .catch(() => null)
        const body = analysis?.media?.find((m) => m.kind === 'body') ?? analysis?.media?.find((m) => m.kind === 'face')
        if (body) setCurrentImage(body.url)
      }
    } catch {
      setFailed(true)
    } finally {
      setLoading(false)
    }
  }, [planId])

  useEffect(() => {
    load()
  }, [load])

  useEffect(() => {
    if (!plan?.steps?.length) return
    if (plan.steps.some((item) => item.category === active)) return
    const first = plan.steps[0]
    if (first) setActive(first.category)
  }, [plan, active])

  const hairSpec = pickHairSpec(plan)
  const tabs = CATEGORIES.filter((cat) => (plan?.steps ?? []).some((item) => item.category === cat.key))
  const step = plan?.steps?.find((item) => item.category === active) ?? plan?.steps?.[0]
  const planImage = plan?.generated_image_url || plan?.image_url
  const isDemoLook = (plan?.look_provider ?? '').startsWith('demo')
  const specItems: { key: string; label: string; value: string }[] = []
  if (hairSpec?.length) specItems.push({ key: 'length', label: PLAN_DETAIL_COPY.specLength, value: hairSpec.length })
  if (hairSpec?.fringe) specItems.push({ key: 'fringe', label: PLAN_DETAIL_COPY.specFringe, value: hairSpec.fringe })
  if (hairSpec?.texture) specItems.push({ key: 'texture', label: PLAN_DETAIL_COPY.specTexture, value: hairSpec.texture })

  const selectAndContinue = async () => {
    if (!plan) return
    setBusy(true)
    try {
      await api.selectPlan(plan.id)
      writeStorage(STORAGE_KEYS.planId, plan.id)
      writeStorage(STORAGE_KEYS.savedPlanId, plan.id)
      Taro.navigateTo({ url: `/pages/checklist/index?id=${plan.id}` })
    } catch (e) {
      Taro.showToast({ title: (e as Error).message || '选择没有成功', icon: 'none' })
    } finally {
      setBusy(false)
    }
  }

  const saveReferenceCard = () => {
    const url = planImage
    if (!url) {
      Taro.showToast({ title: '参考图暂不可用', icon: 'none' })
      return
    }
    Taro.downloadFile({
      url,
      success: (res) => {
        Taro.saveImageToPhotosAlbum({
          filePath: res.tempFilePath,
          success: () => Taro.showToast({ title: '已保存到相册', icon: 'success' }),
          fail: () => Taro.showToast({ title: '未获得相册权限', icon: 'none' }),
        })
      },
      fail: () => Taro.showToast({ title: '下载没有成功', icon: 'none' }),
    })
  }

  if (loading) {
    return (
      <View className={pageClass}>
        <AppHeader title={PLAN_DETAIL_COPY.title} back />
        <Skeleton rows={5} />
      </View>
    )
  }

  if (failed || !plan) {
    return (
      <View className={pageClass}>
        <AppHeader title={PLAN_DETAIL_COPY.title} back />
        <ErrorState onRetry={load} />
      </View>
    )
  }

  return (
    <View className={pageClass}>
      <AppHeader title={PLAN_DETAIL_COPY.title} back transparent />
      <View className="pd" style={{ ['--pd-nav' as string]: `${NAV.navHeight}px` }}>
        <View className="pd__hero">
          <View className="pd__hero-frame">
            <CompareSlider
              single={!currentImage}
              current={<ExampleImage className="pd__hero-img" src={currentImage} user anchor="top" frameAspect={PLAN_FRAME_ASPECT} />}
              plan={
                <ExampleImage
                  className="pd__hero-img"
                  src={planImage}
                  badgeText={isDemoLook ? IMAGE_BADGE_COPY.demo : IMAGE_BADGE_COPY.aiPreview}
                  anchor="top"
                  frameAspect={PLAN_FRAME_ASPECT}
                />
              }
            />
          </View>
        </View>

        <View className="pd__board fade-up">
          <Text className="pd__hint">上滑看发型、妆容与穿搭细节</Text>
          <View className="pd__head">
            <Text className="pd__series">{plan.name}</Text>
            {tabs.length > 1 ? (
              <View className="pd__tabs">
                {tabs.map((cat) => (
                  <Text
                    key={cat.key}
                    className={`pd__tab ${active === cat.key ? 'pd__tab--active' : ''}`}
                    onClick={() => setActive(cat.key)}
                  >
                    {cat.label}
                  </Text>
                ))}
              </View>
            ) : null}
          </View>

          {specItems.length > 0 ? (
            <View className="pd__spec-line">
              {specItems.map((item, i) => (
                <View key={item.key} className="pd__spec-item">
                  {i > 0 ? <Text className="pd__spec-dot">·</Text> : null}
                  <Text className="pd__spec-k">{item.label}</Text>
                  <Text className="pd__spec-v">{item.value}</Text>
                </View>
              ))}
            </View>
          ) : null}

          {step ? (
            <View className="pd__step">
              <Text className="pd__display">{step.title}</Text>
              {step.summary ? <Text className="pd__body">{step.summary}</Text> : null}
            </View>
          ) : (
            <Text className="pd__body">{plan.descriptor || PLAN_DETAIL_COPY.emptyStep}</Text>
          )}

          <View className="pd__foot">
            <PrimaryButton text={PLAN_DETAIL_COPY.cta} loading={busy} onClick={selectAndContinue} />
            {hairSpec ? (
              <Text className="pd__salon-link pressable" onClick={() => setSheetOpen(true)}>
                {PLAN_DETAIL_COPY.salon}
              </Text>
            ) : null}
          </View>
        </View>
      </View>

      <BottomSheet
        open={sheetOpen}
        title="发型师参考卡"
        description="把这张卡给发型师看，沟通更省事"
        onClose={() => setSheetOpen(false)}
      >
        <View className="pd__sheet">
          <ExampleImage
            className="pd__sheet-img"
            src={planImage}
            badgeText={isDemoLook ? IMAGE_BADGE_COPY.demo : IMAGE_BADGE_COPY.aiPreview}
            anchor="top"
          />
          <View className="pd__spec">
            {hairSpec?.length ? (
              <View className="pd__spec-row">
                <Text className="pd__spec-key">{PLAN_DETAIL_COPY.specLength}</Text>
                <Text className="pd__spec-val">{hairSpec.length}</Text>
              </View>
            ) : null}
            {hairSpec?.fringe ? (
              <View className="pd__spec-row">
                <Text className="pd__spec-key">{PLAN_DETAIL_COPY.specFringe}</Text>
                <Text className="pd__spec-val">{hairSpec.fringe}</Text>
              </View>
            ) : null}
            {hairSpec?.texture ? (
              <View className="pd__spec-row">
                <Text className="pd__spec-key">{PLAN_DETAIL_COPY.specTexture}</Text>
                <Text className="pd__spec-val">{hairSpec.texture}</Text>
              </View>
            ) : null}
          </View>
          <PrimaryButton text="保存参考卡" onClick={saveReferenceCard} />
        </View>
      </BottomSheet>
    </View>
  )
}
