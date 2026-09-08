// 方案详情：步骤 tabs（发型/妆容/穿搭）+ 前后对比 + G2 发型师参考卡 +
// 选定 → 生成清单。
import { useCallback, useEffect, useState } from 'react'
import Taro, { useLoad } from '@tarojs/taro'
import { Text, View } from '@tarojs/components'
import type { Plan } from '@zsm/core'
import { api } from '../../services/api'
import { STORAGE_KEYS, readStorage, writeStorage } from '../../services/storage'
import AppHeader from '../../components/app-header'
import PrimaryButton from '../../components/primary-button'
import ExampleImage from '../../components/example-image'
import CompareToggle from '../../components/compare-toggle'
import BottomSheet from '../../components/bottom-sheet'
import Skeleton from '../../components/skeleton'
import ErrorState from '../../components/error-state'
import './index.scss'

const CATEGORIES = [
  { key: 'hair', label: '发型' },
  { key: 'makeup', label: '妆容' },
  { key: 'outfit', label: '穿搭' },
] as const

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
  const [mode, setMode] = useState<'current' | 'plan'>('plan')
  const [active, setActive] = useState<string>('hair')
  const [loading, setLoading] = useState(true)
  const [failed, setFailed] = useState(false)
  const [sheetOpen, setSheetOpen] = useState(false)
  const [busy, setBusy] = useState(false)

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
      // 对比底图：报告当前形象
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

  const hairSpec = pickHairSpec(plan)
  const step = plan?.steps?.find((s) => s.category === active)
  const planImage = plan?.generated_image_url || plan?.image_url

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
    // 参考卡即方案形象图：保存到相册供发型师查看
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
      <View className="page">
        <AppHeader title="方案详情" back />
        <Skeleton rows={5} />
      </View>
    )
  }

  if (failed || !plan) {
    return (
      <View className="page">
        <AppHeader title="方案详情" back />
        <ErrorState onRetry={load} />
      </View>
    )
  }

  return (
    <View className="page">
      <AppHeader title="方案详情" back />
      <View className="pd">
        <View className="pd__hero fade-up">
          <View className="pd__hero-frame">
            {mode === 'plan' ? (
              <ExampleImage
                className="pd__hero-img"
                src={planImage}
                badgeText={(plan.look_provider ?? '').startsWith('demo') ? '效果示例' : 'AI 风格预览'}
              />
            ) : (
              <ExampleImage className="pd__hero-img" src={currentImage} user />
            )}
            <View className="pd__hero-toggle">
              <CompareToggle
                value={mode}
                onChange={(next) => {
                  if (next === 'current' && !currentImage) {
                    Taro.showToast({ title: '当前形象照暂不可用', icon: 'none' })
                    return
                  }
                  setMode(next)
                }}
              />
            </View>
          </View>
        </View>

        <View className="pd__head fade-up delay-1">
          <Text className="pd__eyebrow">你选择了</Text>
          <Text className="pd__name">{plan.name}</Text>
          <Text className="pd__summary">{plan.descriptor}</Text>
        </View>

        <View className="pd__tabs fade-up delay-2">
          {CATEGORIES.map((cat) => (
            <Text
              key={cat.key}
              className={`pd__tab ${active === cat.key ? 'pd__tab--active' : ''}`}
              onClick={() => setActive(cat.key)}
            >
              {cat.label}
            </Text>
          ))}
        </View>

        {step ? (
          <View className="pd__step fade-up delay-2">
            <Text className="pd__step-title">{step.title}</Text>
            <Text className="pd__step-summary">{step.summary}</Text>
          </View>
        ) : (
          <View className="pd__step pd__step--empty">
            <Text className="pd__step-summary">这一步暂无内容</Text>
          </View>
        )}

        <View className="pd__foot fade-up delay-3">
          <PrimaryButton text="生成执行清单" loading={busy} onClick={selectAndContinue} />
          {hairSpec ? (
            <Text className="pd__salon-link pressable" onClick={() => setSheetOpen(true)}>
              分享给发型师 · 查看参考卡
            </Text>
          ) : null}
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
            badgeText={(plan.look_provider ?? '').startsWith('demo') ? '效果示例' : 'AI 风格预览'}
          />
          <View className="pd__spec">
            {hairSpec?.length ? (
              <View className="pd__spec-row">
                <Text className="pd__spec-key">长度</Text>
                <Text className="pd__spec-val">{hairSpec.length}</Text>
              </View>
            ) : null}
            {hairSpec?.fringe ? (
              <View className="pd__spec-row">
                <Text className="pd__spec-key">刘海</Text>
                <Text className="pd__spec-val">{hairSpec.fringe}</Text>
              </View>
            ) : null}
            {hairSpec?.texture ? (
              <View className="pd__spec-row">
                <Text className="pd__spec-key">卷度</Text>
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
