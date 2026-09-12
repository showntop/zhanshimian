// 体验实验室：发型 AR / 试衣保持候补；3D 形象 Lite 按 status 状态机接生成与轮询。
// 状态只经两条服务端通道：GET /v1/body-presentations/status（启动读模型）
// 与公开 Operation 轮询（useOperationPolling）；本地不落任何业务 id。
import { useCallback, useEffect, useState } from 'react'
import Taro from '@tarojs/taro'
import { Text, View } from '@tarojs/components'
import {
  ApiError,
  IMAGE_BADGE_COPY,
  LAB_COPY,
  type BodyPresentation,
  type BodyPresentationStatus,
  type DisplayMedia,
  type Operation,
} from '@zsm/core'
import { usePageShell, useShowOnce } from '../../../../hooks/use-page-visibility'
import { useOperationPolling } from '../../../../app/operations/use-operation-polling'
import { peripherals } from '../../../../app/api/peripherals'
import { qualityApi } from '../../../../app/api/quality'
import { resourceCache, resourceKey } from '../../../../app/cache/resource-cache'
import { handleBillingError } from '../../../../services/billing'
import AppHeader from '../../../../components/app-header'
import SourceImage from '../../../../components/source-image'
import BodyViewer from '../../../../components/body-viewer'
import EmptyState from '../../../../components/empty-state'
import ErrorState from '../../../../components/error-state'
import TextLink from '../../../../components/text-link'
import './index.scss'

const FEATURES = [
  { key: 'hair-ar', name: '发型与妆容 AR', status: '内测', desc: '实时切换发型轮廓、发色与眉眼重点。', slug: 'sharp' },
  { key: '3d', name: LAB_COPY.title3d, status: '开发中', desc: LAB_COPY.desc3d, slug: 'natural' },
  { key: 'try-on', name: '上半身试衣', status: '排队中', desc: '先支持外套和上衣，不做完整商城。', slug: 'warm' },
] as const

type LabFeature = (typeof FEATURES)[number]
type Viewing = 'auto' | 'completed' | 'failed'

const ACTIVE_OPERATION_STATES = new Set(['accepted', 'running', 'retrying'])

function emptyStatus(): BodyPresentationStatus {
  return { available: false, active: null, completed: null, failed: null }
}

// 角标只认服务端投影的 source_kind（红线：不按 provider、不按 URL 推断）。
function viewerBadge(presentation: BodyPresentation): string {
  return presentation.source_kind === 'demo_example' ? IMAGE_BADGE_COPY.demo : IMAGE_BADGE_COPY.aiPreview
}

/**  Operation 进度投影成卡片的 active 展示态：阶段文案与百分比来自服务端。 */
function projectActive(base: BodyPresentation, operation: Operation | undefined): BodyPresentation {
  if (!operation) return base
  const failed = operation.status === 'failed' || operation.status === 'cancelled' || operation.status === 'superseded'
  return {
    ...base,
    status: failed ? 'failed' : operation.status === 'succeeded' ? 'completed' : base.status,
    progress: Math.round(operation.progress_bps / 100),
    stage: operation.stage_code || base.stage,
  }
}

export default function Lab() {
  const [waitlisted, setWaitlisted] = useState<string[]>([])
  const [status, setStatus] = useState<BodyPresentationStatus | null>(null)
  const [face, setFace] = useState<DisplayMedia | null>(null)
  const [body, setBody] = useState<DisplayMedia | null>(null)
  const [operationId, setOperationId] = useState('')
  const [viewing, setViewing] = useState<Viewing>('auto')
  const [busy, setBusy] = useState(false)
  const [pollFailed, setPollFailed] = useState(false)
  const { pageClass, enter } = usePageShell(true, '', 'lab')

  const load = useCallback(async () => {
    try {
      const [nextStatus, report, boot] = await Promise.all([
        peripherals.getBodyPresentationStatus(),
        // 正脸/全身来自当前报告的建档照片（服务端读模型，不落本地 id）。
        qualityApi.getCurrentReport().catch(() => null),
        qualityApi.getHomeBootstrap().catch(() => null),
      ])
      setStatus(nextStatus)
      setFace(report?.source_media?.face?.media ?? null)
      setBody(report?.source_media?.body?.media ?? null)
      // 恢复：进行中任务的操作 id 从 home bootstrap 的活跃操作里找回来。
      const running = (boot?.active_operations ?? []).find(
        (operation) => operation.kind === 'body_orbit' && ACTIVE_OPERATION_STATES.has(operation.status),
      )
      setOperationId(nextStatus.active && running ? running.id : '')
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

  // 受理后的状态只通过公开 Operation 观察；终态后重拉启动读模型。
  const { operations } = useOperationPolling({
    operationIds: operationId ? [operationId] : [],
    enabled: Boolean(operationId) && !pollFailed,
    onSettled: () => {
      setOperationId('')
      void peripherals.getBodyPresentationStatus().then((next) => {
        setStatus(next)
        setViewing((current) => (current === 'completed' ? current : 'auto'))
      }).catch(() => undefined)
    },
    onFetchFailure: () => {
      setPollFailed(true)
      setOperationId('')
      void peripherals.getBodyPresentationStatus().then(setStatus).catch(() => undefined)
    },
  })
  const activeOperation = operations.find((operation) => operation.id === operationId)

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
      const accepted = await peripherals.createBodyPresentation({
        body_media_id: body.asset_id,
        face_media_id: face.asset_id,
      })
      resourceCache.write(resourceKey('operation', accepted.operation.id), accepted.operation)
      setOperationId(accepted.operation.id)
      setStatus((prev) => ({
        available: prev?.available ?? true,
        active: accepted.data,
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
  const active = status?.active ? projectActive(status.active, activeOperation) : null
  const showProgress = Boolean(active) && !pollFailed
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
          <SourceImage className="lab__card-img" reference={{ slug: feature.slug, variant: 'full' }} anchor="top" />
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

    if (showProgress && active) {
      const progress = active.progress ?? 0
      const stage = active.stage || '正在生成 3D 形象'
      return (
        <>
          <SourceImage className="lab__card-img" reference={{ slug: feature.slug, variant: 'full' }} anchor="top" />
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
              bodyMedia={body}
              badgeText={viewerBadge(status.completed)}
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
        <SourceImage className="lab__card-img" reference={{ slug: feature.slug, variant: 'full' }} anchor="top" />
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
        {FEATURES.map((feature, i) => (
          <View key={feature.key} className={`lab__card card ${enter((i + 1) as 1 | 2 | 3)}`}>
            {feature.key === '3d' ? (
              render3d(feature)
            ) : (
              <>
                <SourceImage className="lab__card-img" reference={{ slug: feature.slug, variant: 'full' }} anchor="top" />
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
        ))}
      </View>
    </View>
  )
}
