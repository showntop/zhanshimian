// 体验实验室：发型 AR / 试衣保持候补；3D 形象 Lite 按 status 状态机接生成与轮询。
// 状态只经两条服务端通道：GET /v1/body-presentations/status（启动读模型）
// 与公开 Operation 轮询（useOperationPolling）；本地不落任何业务 id。
import { useCallback, useEffect, useRef, useState } from 'react'
import Taro from '@tarojs/taro'
import { Text, View } from '@tarojs/components'
import {
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
import { PublicApiError } from '../../../../app/api/result'
import { resourceCache, resourceKey } from '../../../../app/cache/resource-cache'
import { handleBillingError } from '../../../../services/billing'
import AppHeader from '../../../../components/app-header'
import SourceImage from '../../../../components/source-image'
import BodyViewer from '../../../../components/body-viewer'
import EmptyState from '../../../../components/empty-state'
import ErrorState from '../../../../components/error-state'
import PrimaryButton from '../../../../components/primary-button'
import TextLink from '../../../../components/text-link'
import './index.scss'

function StagePreview({ slug, dim }: { slug: string; dim?: boolean }) {
  return (
    <View className={`lab__podium ${dim ? 'lab__podium--dim' : ''}`}>
      <View className="lab__podium-glow" />
      <View className="lab__podium-spot" />
      <View className="lab__podium-ring" />
      <View className="lab__podium-floor" />
      <SourceImage className="lab__podium-figure" reference={{ slug, variant: 'full' }} mode="aspectFit" />
    </View>
  )
}

const FEATURES = [
  { key: '3d', name: LAB_COPY.title3d, status: '开发中', desc: LAB_COPY.desc3d, slug: 'natural' },
  { key: 'hair-ar', name: '发型与妆容 AR', status: '内测', desc: '实时切换发型轮廓、发色与眉眼重点。', slug: 'sharp' },
  { key: 'try-on', name: '上半身试衣', status: '排队中', desc: '先支持外套和上衣，不做完整商城。', slug: 'warm' },
] as const

type LabFeature = (typeof FEATURES)[number]
type Viewing = 'auto' | 'completed' | 'failed'

const ACTIVE_OPERATION_STATES = new Set(['accepted', 'running', 'retrying'])

function isActiveStatus(value?: string): boolean {
  return value === 'queued' || value === 'processing'
}

/** 过期 status 不盖掉更新的进行中任务（25e27d1）： prev 的 active 更新就保留。 */
function preferNewerActive(
  prev: BodyPresentationStatus | null,
  incoming: BodyPresentationStatus,
): BodyPresentationStatus {
  const prevActive = prev?.active
  if (!prevActive || !isActiveStatus(prevActive.status)) {
    return incoming
  }
  const incomingActive = incoming.active
  if (!incomingActive || !isActiveStatus(incomingActive.status)) {
    return { ...incoming, active: prevActive }
  }
  if (prevActive.id !== incomingActive.id && (prevActive.created_at || '') > (incomingActive.created_at || '')) {
    return { ...incoming, active: prevActive }
  }
  return incoming
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
  const [loadFailed, setLoadFailed] = useState(false)
  const statusEpochRef = useRef(0)
  const busyRef = useRef(false)
  const { pageClass, enter } = usePageShell(true, '', 'lab')

  const load = useCallback(async () => {
    const epoch = ++statusEpochRef.current
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
      if (epoch !== statusEpochRef.current) return
      setOperationId(nextStatus.active && running ? running.id : '')
      setPollFailed(false)
      setLoadFailed(false)
    } catch {
      if (epoch !== statusEpochRef.current) return
      // 首读失败不伪造空状态（22f7b41）：保留旧数据，卡片走可读错误态。
      setLoadFailed(true)
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
    onSettled: (settled) => {
      const outcome = settled[0]
      setOperationId('')
      // 先用轮询终态就地折叠（22f7b41：重拉失败也不能把卡片卡回进度条），
      // 再后台对齐启动读模型。
      if (outcome) {
        setStatus((prev) => {
          if (!prev?.active) return prev
          if (outcome.status === 'succeeded') {
            return {
              ...prev, active: null, failed: null,
              completed: { ...prev.active, status: 'completed', progress: 100 },
            }
          }
          if (outcome.status === 'failed') {
            return { ...prev, active: null, failed: { ...prev.active, status: 'failed' } }
          }
          return prev
        })
      }
      if (outcome?.status === 'failed') {
        setViewing((current) => (current === 'completed' ? current : 'auto'))
      }
      const epoch = statusEpochRef.current
      void peripherals.getBodyPresentationStatus().then((next) => {
        if (epoch !== statusEpochRef.current) return
        setStatus((prev) => preferNewerActive(prev, next))
      }).catch(() => {
        if (epoch !== statusEpochRef.current) return
        if (outcome?.status === 'failed') setPollFailed(true)
      })
    },
    onFetchFailure: () => {
      setPollFailed(true)
      setOperationId('')
      const epoch = statusEpochRef.current
      void peripherals.getBodyPresentationStatus().then((next) => {
        if (epoch !== statusEpochRef.current) return
        setStatus((prev) => preferNewerActive(prev, next))
      }).catch(() => undefined)
    },
  })
  const activeOperation = operations.find((operation) => operation.id === operationId)

  const generate = async () => {
    if (busyRef.current || busy || (status?.active && !pollFailed)) return
    if (!face || !body) {
      Taro.navigateTo({ url: '/pages/capture/index' })
      return
    }
    busyRef.current = true
    setBusy(true)
    setPollFailed(false)
    try {
      const accepted = await peripherals.createBodyPresentation({
        body_media_id: body.asset_id,
        face_media_id: face.asset_id,
      })
      resourceCache.write(resourceKey('operation', accepted.operation.id), accepted.operation)
      // 新任务生效：作废旧 load/轮询回调里晚到的 status（25e27d1）。
      statusEpochRef.current += 1
      setOperationId(accepted.operation.id)
      setViewing('auto')
      setStatus((prev) => ({
        available: prev?.available ?? true,
        active: accepted.data,
        completed: prev?.completed ?? null,
        failed: prev?.failed ?? null,
      }))
    } catch (error) {
      if (handleBillingError(error)) return
      // 接口错误一律是 PublicApiError（core 的 ApiError 只认 billing 自造的 payment_*）：
      // 判错类会让「能力未开放」永远落不到 waitlist 分支，只剩一句 toast。
      if (error instanceof PublicApiError && (error.code === 'capability_unavailable' || error.statusCode === 503)) {
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
      busyRef.current = false
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
  const showLoadError = loadFailed && status === null
  const showProgress = Boolean(active) && !pollFailed && (active?.status === 'queued' || active?.status === 'processing')
  const showError =
    showLoadError ||
    (Boolean(status?.available) && !busy && (Boolean(status?.failed) || pollFailed) && viewing !== 'completed' && !showProgress)
  const showViewer = Boolean(status?.available) && Boolean(status?.completed) && !showProgress && !showError
  const showEmpty = Boolean(status?.available) && !showProgress && !showError && !showViewer && (!face || !body)
  const showWaitlist = status !== null && !status.available && !showLoadError
  const useStage = !showWaitlist && !showError && !showEmpty

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

    if (showError) {
      return (
        <View className="lab__card-copy lab__card-copy--state">
          {head}
          <ErrorState
            title={LAB_COPY.failed}
            message={status?.failed?.error_message || LAB_COPY.failed}
            retryText={LAB_COPY.retry}
            onRetry={() => void (showLoadError ? load() : generate())}
          />
          {!showLoadError && status?.completed ? (
            <TextLink className="lab__card-viewlast" text={LAB_COPY.viewLast} onClick={() => setViewing('completed')} />
          ) : null}
        </View>
      )
    }

    if (showWaitlist) {
      return (
        <>
          <SourceImage className="lab__card-img" reference={{ slug: feature.slug, variant: 'full' }} anchor="top" />
          <View className="lab__card-copy">
            {head}
            <Text className="lab__card-desc">{feature.desc}</Text>
            <View className="lab__card-btn pressable" onClick={() => act(feature)}>
              <Text>{waitlisted.includes(feature.key) ? '已预约' : '预约体验'}</Text>
            </View>
          </View>
        </>
      )
    }

    if (!loaded) {
      return (
        <>
          <StagePreview slug={feature.slug} />
          <View className="lab__card-copy lab__card-copy--onstage">
            {head}
            <Text className="lab__card-desc">{feature.desc}</Text>
          </View>
        </>
      )
    }

    if (showProgress && active) {
      const progress = active.progress ?? 0
      const stage = active.stage || LAB_COPY.generating
      return (
        <>
          <StagePreview slug={feature.slug} dim />
          <View className="lab__card-copy lab__card-copy--onstage">
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

    if (showViewer && status?.completed) {
      return (
        <>
          <View className="lab__card-viewer">
            <BodyViewer
              key={status.completed.id}
              presentation={status.completed}
              bodyMedia={body}
            />
          </View>
          <View className="lab__card-copy lab__card-copy--onstage">
            {head}
            <Text className="lab__card-desc">{feature.desc}</Text>
            <PrimaryButton
              text={busy ? LAB_COPY.generating : LAB_COPY.regenerate}
              loading={busy}
              tone="onDark"
              onClick={() => void generate()}
            />
          </View>
        </>
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
        <StagePreview slug={feature.slug} />
        <View className="lab__card-copy lab__card-copy--onstage">
          {head}
          <Text className="lab__card-desc">{feature.desc}</Text>
          <PrimaryButton
            text={busy ? LAB_COPY.generating : LAB_COPY.generate}
            loading={busy}
            tone="onDark"
            onClick={() => void generate()}
          />
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
          const stageCard = feature.key === '3d' && useStage
          const wide = feature.key === '3d' && (stageCard || showViewer || showError || showEmpty || showLoadError)
          return (
            <View
              key={feature.key}
              className={`lab__card card ${stageCard ? 'card--hero lab__card--stage' : ''} ${wide ? 'lab__card--wide' : ''} ${enter((i + 1) as 1 | 2 | 3)}`}
            >
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
          )
        })}
      </View>
    </View>
  )
}
