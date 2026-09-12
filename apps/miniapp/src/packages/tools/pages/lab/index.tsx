// 体验实验室：发型 AR / 试衣保持候补；3D 形象 Lite 按 status 状态机接生成与轮询。
import { useCallback, useEffect, useState } from 'react'
import Taro from '@tarojs/taro'
import { Text, View } from '@tarojs/components'
import {
  ApiError,
  IMAGE_BADGE_COPY,
  LAB_COPY,
  POLL_INTERVALS,
  type Analysis,
  type BodyPresentation,
  type BodyPresentationStatus,
} from '@zsm/core'
import { usePageShell, useShowOnce } from '../../../../hooks/use-page-visibility'
import { useStablePolling } from '../../../../hooks/use-stable-polling'
import { api } from '../../../../services/api'
import { handleBillingError } from '../../../../services/billing'
import { STORAGE_KEYS, readStorage, writeStorage } from '../../../../services/storage'
import AppHeader from '../../../../components/app-header'
import BodyViewer from '../../../../components/body-viewer'
import EmptyState from '../../../../components/empty-state'
import ErrorState from '../../../../components/error-state'
import ExampleImage from '../../../../components/example-image'
import TextLink from '../../../../components/text-link'
import './index.scss'

const FEATURES = [
  { key: 'hair-ar', name: '发型与妆容 AR', status: '内测', desc: '实时切换发型轮廓、发色与眉眼重点。', slug: 'sharp' },
  { key: '3d', name: LAB_COPY.title3d, status: '开发中', desc: LAB_COPY.desc3d, slug: 'natural' },
  { key: 'try-on', name: '上半身试衣', status: '排队中', desc: '先支持外套和上衣，不做完整商城。', slug: 'warm' },
] as const

type LabFeature = (typeof FEATURES)[number]
type Viewing = 'auto' | 'completed' | 'failed'

function mediaOf(analysis: Analysis | null | undefined, kind: 'face' | 'body') {
  return analysis?.media?.find((m) => m.kind === kind)
}

function isActiveStatus(value?: string): boolean {
  return value === 'queued' || value === 'processing'
}

function emptyStatus(): BodyPresentationStatus {
  return { available: false, active: null, completed: null, failed: null }
}

function viewerBadge(presentation: BodyPresentation, bodyDemo?: boolean): string {
  const demo = (presentation.provider_version ?? '').startsWith('demo') || Boolean(bodyDemo)
  return demo ? IMAGE_BADGE_COPY.demo : LAB_COPY.badgeAI
}

export default function Lab() {
  const [waitlisted, setWaitlisted] = useState<string[]>([])
  const [status, setStatus] = useState<BodyPresentationStatus | null>(null)
  const [analysis, setAnalysis] = useState<Analysis | null>(null)
  const [viewing, setViewing] = useState<Viewing>('auto')
  const [busy, setBusy] = useState(false)
  const [pollFailed, setPollFailed] = useState(false)
  const { pageClass, enter } = usePageShell(true, '', 'lab')

  const load = useCallback(async () => {
    try {
      const [nextStatus, current] = await Promise.all([
        api.getBodyPresentationStatus(),
        api.getCurrentAnalysis(),
      ])
      let nextAnalysis = current
      // getCurrentAnalysis 只回进行中的分析；已建档用户要从当前报告补正脸/全身。
      if (!mediaOf(current, 'face') || !mediaOf(current, 'body')) {
        const reportId = readStorage(STORAGE_KEYS.reportId)
        const report = reportId
          ? await api.getReport(reportId).catch(() => null)
          : await api.getCurrentReport().catch(() => null)
        if (report?.analysis_id) {
          nextAnalysis = await api.getAnalysis(report.analysis_id).catch(() => current)
        }
      }
      let merged = nextStatus
      const storedId = readStorage(STORAGE_KEYS.activeTaskBodyOrbit)
      if (storedId) {
        try {
          const row = await api.getBodyPresentation(storedId)
          if (isActiveStatus(row.status)) merged = { ...nextStatus, active: row }
          else writeStorage(STORAGE_KEYS.activeTaskBodyOrbit, '')
        } catch {
          writeStorage(STORAGE_KEYS.activeTaskBodyOrbit, '')
        }
      }
      setStatus(merged)
      setAnalysis(nextAnalysis)
      setPollFailed(false)
    } catch {
      setStatus((prev) => prev ?? emptyStatus())
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  useShowOnce(() => {
    void load()
  })

  useStablePolling({
    fetcher: async () => {
      const id = status?.active?.id
      if (!id) throw new Error('missing id')
      const item = await api.getBodyPresentation(id)
      if (isActiveStatus(item.status)) {
        setStatus((prev) => (prev ? { ...prev, active: item } : prev))
      }
      return {
        id: item.id,
        type: 'body_orbit' as const,
        status: item.status,
        progress: item.progress,
        stage: item.stage,
      }
    },
    intervalMs: POLL_INTERVALS.bodyOrbit,
    enabled: Boolean(status?.active) && !pollFailed,
    onDone: (result) => {
      writeStorage(STORAGE_KEYS.activeTaskBodyOrbit, '')
      void api.getBodyPresentationStatus().then((next) => {
        setStatus(next)
        if (result.status === 'failed') {
          setViewing((current) => (current === 'completed' ? current : 'auto'))
        }
      })
    },
    onFailed: () => {
      setPollFailed(true)
      writeStorage(STORAGE_KEYS.activeTaskBodyOrbit, '')
      void api.getBodyPresentationStatus().then(setStatus).catch(() => undefined)
    },
  })

  const face = mediaOf(analysis, 'face')
  const body = mediaOf(analysis, 'body')

  const generate = async () => {
    if (busy || (status?.active && !pollFailed)) return
    if (!face || !body) {
      Taro.navigateTo({ url: '/pages/capture/index' })
      return
    }
    setBusy(true)
    setPollFailed(false)
    setViewing('auto')
    try {
      const { data } = await api.createBodyPresentation({
        body_media_id: body.id,
        face_media_id: face.id,
      })
      writeStorage(STORAGE_KEYS.activeTaskBodyOrbit, data.id)
      setStatus((prev) => ({
        available: prev?.available ?? true,
        active: data,
        completed: prev?.completed ?? null,
        failed: prev?.failed ?? null,
      }))
    } catch (error) {
      if (handleBillingError(error)) return
      if (error instanceof ApiError && (error.code === 'capability_unavailable' || error.statusCode === 503)) {
        setStatus((prev) => ({
          available: false,
          active: null,
          completed: prev?.completed ?? null,
          failed: prev?.failed ?? null,
        }))
        return
      }
      Taro.showToast({ title: (error as Error).message || '生成没有开始，请重试', icon: 'none' })
    } finally {
      setBusy(false)
    }
  }

  const act = (feature: LabFeature) => {
    if (feature.status === '内测') {
      Taro.showModal({
        title: feature.name,
        content: '内测名额逐步开放。生成能力需通过身份一致性评测后上线，不会用静态图冒充真实效果。',
        showCancel: false,
        confirmText: '知道了',
      })
      return
    }
    if (waitlisted.includes(feature.key)) {
      Taro.showToast({ title: '已在候补名单', icon: 'none' })
      return
    }
    setWaitlisted((prev) => [...prev, feature.key])
    Taro.showToast({ title: '已加入候补', icon: 'success' })
  }

  const loaded = status !== null
  const showProgress = Boolean(status?.active) && !pollFailed
  const showError = Boolean(status?.available) && (Boolean(status?.failed) || pollFailed) && viewing !== 'completed' && !showProgress
  const showViewer = Boolean(status?.available) && Boolean(status?.completed) && !showProgress && !showError
  const showEmpty = Boolean(status?.available) && !showProgress && !showError && !showViewer && (!face || !body)
  const showWaitlist = status !== null && !status.available

  const cardStatus = (feature: LabFeature): string => {
    if (feature.key !== '3d' || showWaitlist) {
      return waitlisted.includes(feature.key) ? '已预约' : feature.status
    }
    if (showProgress) return '生成中'
    if (showError) return '未完成'
    if (showViewer) return '已生成'
    if (showEmpty) return feature.status
    return '可体验'
  }

  const render3d = (feature: LabFeature) => {
    const head = (
      <View className="lab__card-head">
        <Text className="lab__card-name">{feature.name}</Text>
        <Text className={`lab__card-status ${!showWaitlist || feature.status === '内测' ? 'lab__card-status--live' : ''}`}>
          {cardStatus(feature)}
        </Text>
      </View>
    )

    if (!loaded || showWaitlist) {
      return (
        <>
          <ExampleImage className="lab__card-img" slug={feature.slug} variant="full" badgeText={IMAGE_BADGE_COPY.bundled} anchor="top" />
          <View className="lab__card-copy">
            {head}
            <Text className="lab__card-desc">{feature.desc}</Text>
            {showWaitlist ? (
              <View className="lab__card-btn pressable" onClick={() => act(feature)}>
                <Text>{waitlisted.includes(feature.key) ? '已预约' : '预约体验'}</Text>
              </View>
            ) : null}
          </View>
        </>
      )
    }

    if (showProgress && status?.active) {
      const progress = status.active.progress ?? 0
      const stage = status.active.stage || '正在生成 3D 形象'
      return (
        <>
          <ExampleImage className="lab__card-img" slug={feature.slug} variant="full" badgeText={IMAGE_BADGE_COPY.bundled} anchor="top" />
          <View className="lab__card-copy">
            {head}
            <Text className="lab__card-desc">{feature.desc}</Text>
            <View className="lab__card-progress">
              <View className="lab__card-progress-track">
                <View className="lab__card-progress-fill" style={{ width: `${progress}%` }} />
              </View>
              <View className="lab__card-progress-meta">
                <Text className="lab__card-progress-stage">{stage}</Text>
                <Text className="lab__card-progress-num">{Math.round(progress)}%</Text>
              </View>
            </View>
          </View>
        </>
      )
    }

    if (showError) {
      return (
        <View className="lab__card-copy lab__card-copy--state">
          {head}
          <ErrorState
            title={LAB_COPY.failed}
            message={status?.failed?.error_message || LAB_COPY.failed}
            retryText={LAB_COPY.retry}
            onRetry={() => void generate()}
          />
          {status?.completed ? (
            <TextLink className="lab__card-viewlast" text={LAB_COPY.viewLast} onClick={() => setViewing('completed')} />
          ) : null}
        </View>
      )
    }

    if (showViewer && status?.completed) {
      return (
        <View className="lab__card-copy lab__card-copy--wide">
          {head}
          <Text className="lab__card-desc">{feature.desc}</Text>
          <View className="lab__card-viewer">
            <BodyViewer
              key={status.completed.id}
              presentation={status.completed}
              bodyImageURL={body?.url ?? ''}
              badgeText={viewerBadge(status.completed, body?.demo)}
            />
          </View>
          <View
            className={`lab__card-btn pressable ${busy ? 'lab__card-btn--busy' : ''}`}
            onClick={() => void generate()}
          >
            <Text>{busy ? '正在生成…' : LAB_COPY.regenerate}</Text>
          </View>
        </View>
      )
    }

    if (showEmpty) {
      return (
        <View className="lab__card-copy lab__card-copy--state">
          {head}
          <EmptyState
            title={LAB_COPY.empty}
            actionText={LAB_COPY.emptyAction}
            onAction={() => Taro.navigateTo({ url: '/pages/capture/index' })}
          />
        </View>
      )
    }

    return (
      <>
        <ExampleImage className="lab__card-img" slug={feature.slug} variant="full" badgeText={IMAGE_BADGE_COPY.bundled} anchor="top" />
        <View className="lab__card-copy">
          {head}
          <Text className="lab__card-desc">{feature.desc}</Text>
          <View
            className={`lab__card-btn pressable ${busy ? 'lab__card-btn--busy' : ''}`}
            onClick={() => void generate()}
          >
            <Text>{busy ? '正在生成…' : LAB_COPY.generate}</Text>
          </View>
        </View>
      </>
    )
  }

  return (
    <View className={pageClass}>
      <AppHeader title="体验实验室" back />
      <View className="lab">
        <View className={`lab__intro ${enter()}`}>
          <Text className="lab__title">这里放「哇塞」，不打断核心流程</Text>
        </View>
        {FEATURES.map((feature, i) => {
          const wide = feature.key === '3d' && (showViewer || showError || showEmpty)
          return (
            <View
              key={feature.key}
              className={`lab__card card ${wide ? 'lab__card--wide' : ''} ${enter((i + 1) as 1 | 2 | 3)}`}
            >
              {feature.key === '3d' ? (
                render3d(feature)
              ) : (
                <>
                  <ExampleImage className="lab__card-img" slug={feature.slug} variant="full" badgeText={IMAGE_BADGE_COPY.bundled} anchor="top" />
                  <View className="lab__card-copy">
                    <View className="lab__card-head">
                      <Text className="lab__card-name">{feature.name}</Text>
                      <Text className={`lab__card-status ${feature.status === '内测' ? 'lab__card-status--live' : ''}`}>
                        {waitlisted.includes(feature.key) ? '已预约' : feature.status}
                      </Text>
                    </View>
                    <Text className="lab__card-desc">{feature.desc}</Text>
                    <View className="lab__card-btn pressable" onClick={() => act(feature)}>
                      <Text>{feature.status === '内测' ? '了解进展' : '预约体验'}</Text>
                    </View>
                  </View>
                </>
              )}
            </View>
          )
        })}
      </View>
    </View>
  )
}
