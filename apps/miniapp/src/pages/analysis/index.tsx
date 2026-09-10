// 分析进度页：轮询分析行(700ms) + 显示进度补间(只追不跳)。
// 失败态：照片被拒时逐图展示中文原因（error_message 按分号拆分为逐图原因）。
import { useEffect, useMemo, useRef, useState } from 'react'
import Taro, { useLoad } from '@tarojs/taro'
import { Image, Text, View } from '@tarojs/components'
import {
  ANALYSIS_FAIL_COPY,
  POLL_INTERVALS,
  analysisTimelineText,
  createDisplayProgress,
  userImage,
  type Analysis,
  type DisplayProgressHandle,
} from '@zsm/core'
import { api } from '../../services/api'
import { STORAGE_KEYS, readStorage, writeStorage } from '../../services/storage'
import { useStablePolling } from '../../hooks/use-stable-polling'
import AppHeader from '../../components/app-header'
import PrimaryButton from '../../components/primary-button'
import './index.scss'

/** 分析行零变化超过该时长视为服务端孤儿（进度动画/心跳/重试都会刷新行，正常不会触发） */
const STUCK_ROW_TIMEOUT_MS = 6 * 60 * 1000

function failureReasons(analysis: Analysis): string[] {
  const message = analysis.error_message || ''
  if (!message) return [ANALYSIS_FAIL_COPY.photoFallback]
  // 后端照片拒绝文案形如「正脸照片：…；侧脸照片：…」
  return message
    .split(/[；;]/)
    .map((part) => part.trim())
    .filter(Boolean)
}

/** 超时类失败（服务端兜底/客户端守卫都打 stage='分析未完成'）：与照片被拒分开呈现 */
function isTimeoutFailure(analysis: Analysis): boolean {
  return analysis.stage === '分析未完成'
}

export default function Analysis() {
  const [analysisId, setAnalysisId] = useState('')
  const [analysis, setAnalysis] = useState<Analysis | null>(null)
  const [failed, setFailed] = useState<Analysis | null>(null)
  // 显示进度补间：createDisplayProgress 是框架中立控制器（非 hook），
  // 整页只建一个实例，onUpdate 接 setState 驱动渲染（此前裸调导致 displayed 恒为 0）。
  const [shown, setShown] = useState(0)
  const displayRef = useRef<DisplayProgressHandle | null>(null)
  if (displayRef.current === null) {
    // maxRatePerSecond：真实进度大跳变时按 ~9%/s 平滑爬升，不瞬移。
    // pace：大模型分析调用可长达数分钟且服务端进度会长时间停滞（72% 档），
    // 时间推期让显示目标持续缓慢推进（封顶 92%，不碰 100%），终态由真实值接管收尾。
    displayRef.current = createDisplayProgress({
      onUpdate: setShown,
      maxRatePerSecond: 9,
      pace: { ceiling: 92, tauMs: 40_000 },
    })
  }
  const display = displayRef.current
  const failedRef = useRef(false)

  useLoad((options) => {
    const id = options?.id || readStorage(STORAGE_KEYS.activeTaskAnalysis)
    if (id) {
      setAnalysisId(id)
      writeStorage(STORAGE_KEYS.activeTaskAnalysis, id)
    }
  })

  // 轮询走稳定包装：单实例，页面不可见自动暂停、恢复可见补拉，卸载即停
  useStablePolling({
    fetcher: async () => {
      if (!analysisId) throw new Error('missing id')
      const item = await api.getAnalysis(analysisId)
      // 中间态也要落 state：照片层/进度都依赖完整分析行
      setAnalysis(item)
      // 适配统一任务形状：分析行的 status/progress/stage 即任务投影
      return {
        id: item.id,
        type: 'analysis' as const,
        status: item.status,
        progress: item.progress,
        stage: item.stage,
        created_at: item.created_at,
        updated_at: item.updated_at,
      }
    },
    intervalMs: POLL_INTERVALS.analysis,
    enabled: Boolean(analysisId),
    onDone: (result) => {
      display.set(result.progress)
      if (result.status === 'completed') {
        if (failedRef.current) return
        failedRef.current = true
        writeStorage(STORAGE_KEYS.activeTaskAnalysis, '')
        Taro.redirectTo({ url: '/pages/report/index' })
      } else {
        // 终态 failed：拉全量分析行取逐图原因
        api
          .getAnalysis(analysisId)
          .then(setFailed)
          .catch(() => setFailed({ ...(analysis ?? ({} as Analysis)), status: 'failed' } as Analysis))
      }
    },
  })

  useEffect(() => {
    if (analysis) display.set(analysis.progress)
  }, [analysis, display])

  // 客户端卡死守卫：分析行 6 分钟零变化（updated_at/progress/stage 全静止）
  // 且未到终态，视为服务端孤儿（worker 死亡时服务端兜底也不会执行），
  // 本地直接进超时失败态，给出下一步动作，绝不无限等待。
  // 正常流程不受影响：重试认领、进度动画、心跳都会刷新行签名。
  const rowSigRef = useRef({ sig: '', at: Date.now() })
  useEffect(() => {
    if (!analysis || failed || isTimeoutFailure(analysis)) return
    if (analysis.status === 'completed' || analysis.status === 'failed') return
    const sig = `${analysis.updated_at}|${analysis.progress}|${analysis.stage}`
    const now = Date.now()
    if (sig !== rowSigRef.current.sig) {
      rowSigRef.current = { sig, at: now }
      return
    }
    if (now - rowSigRef.current.at > STUCK_ROW_TIMEOUT_MS) {
      setFailed({ ...analysis, status: 'failed', stage: '分析未完成', error_message: ANALYSIS_FAIL_COPY.timeoutBody })
    }
  }, [analysis, failed])

  // 阶段文案：细粒度时间线随补间进度推进（覆盖服务端 15/22/32/42/48/56/64/72/82/95 上报点）
  const stageText = analysisTimelineText(shown)
  // 扫描对象：刚上传的三张照片。COS 私有桶每次轮询都会重签 URL（签名参数随时间变化），
  // 若直接使用会导致 <Image> 每 700ms 重载一次（表现为照片反复闪烁/轮播）。
  // 按 kind 锁定首次解析成功的 URL（签名 TTL 15min ≫ 分析时长 1-2min），消除签名抖动。
  const photoUrlCache = useRef<Record<string, string>>({})
  const photos = useMemo(() => {
    const order: Record<string, number> = { face: 0, side: 1, body: 2 }
    return (analysis?.media ?? [])
      .map((m) => {
        const resolved = userImage(m.url)
        if (resolved && !photoUrlCache.current[m.kind]) photoUrlCache.current[m.kind] = resolved
        return { kind: m.kind, url: photoUrlCache.current[m.kind] || resolved, demo: m.demo === true }
      })
      .filter((p) => p.url)
      .sort((a, b) => (order[a.kind] ?? 9) - (order[b.kind] ?? 9))
      .slice(0, 3)
  }, [analysis?.media])
  // 阶段细节交给时间线；提示行固定为隐私安抚，避免与阶段文案双重跳变
  const tipText = '照片全程加密，只有你能看到'
  // 三步指示器：与进度联动，点亮当前阶段
  const STEPS = [
    { label: '整理照片' },
    { label: '深度分析' },
    { label: '生成方案' },
  ]
  const currentStep = shown < 30 ? 0 : shown < 70 ? 1 : 2

  if (failed) {
    const timeout = isTimeoutFailure(failed)
    const reasons = timeout ? [ANALYSIS_FAIL_COPY.timeoutBody] : failureReasons(failed)
    return (
      <View className="page">
        <AppHeader title="正在分析" back />
        <View className="analysis-fail fade-up">
          <Text className="analysis-fail__title">
            {timeout ? ANALYSIS_FAIL_COPY.timeoutTitle : ANALYSIS_FAIL_COPY.photoTitle}
          </Text>
          {reasons.map((reason) => (
            <Text key={reason} className="analysis-fail__reason">
              {reason}
            </Text>
          ))}
          <View className="analysis-fail__action">
            <PrimaryButton
              text={timeout ? '重新发起' : '重新拍摄'}
              onClick={() => Taro.redirectTo({ url: '/pages/capture/index' })}
            />
          </View>
        </View>
      </View>
    )
  }

  return (
    <View className="page">
      <AppHeader title="正在分析" back />
      <View className="analysis">
        <View className="analysis__portrait fade-up">
          <View className="analysis__portrait-frame">
            {photos[0] ? (
              <Image
                key={photos[0].kind}
                className={`analysis__hero ${photos[0].demo ? 'example-soft' : ''}`}
                src={photos[0].url}
                mode="aspectFill"
              />
            ) : null}
            {photos.slice(1).map((photo, i) => (
              <View
                key={photo.kind}
                className={`analysis__mini analysis__mini--${i} ${photo.demo ? 'example-soft' : ''}`}
              >
                <Image className="analysis__mini-img" src={photo.url} mode="aspectFill" />
              </View>
            ))}
            <Text className="analysis__portrait-mark">AI 分析中</Text>
          </View>
        </View>

        <Text key={stageText} className="analysis__stage">
          {stageText}
        </Text>

        <View className="analysis__progress fade-up delay-2">
          <View className="analysis__progress-track">
            <View className="analysis__progress-fill" style={{ width: `${shown}%` }} />
          </View>
          <View className="analysis__progress-meta">
            <Text className="analysis__progress-num">{Math.round(shown)}%</Text>
            <Text className="analysis__progress-eta">通常需要 1-2 分钟</Text>
          </View>
        </View>

        <View className="analysis__steps fade-up delay-2">
          {STEPS.map((step, i) => (
            <View key={step.label} className="analysis__step-item">
              {i > 0 ? (
                <View className={`analysis__step-line ${currentStep >= i ? 'analysis__step-line--done' : ''}`} />
              ) : null}
              <View
                className={`analysis__step-dot ${i < currentStep ? 'analysis__step-dot--done' : ''} ${i === currentStep ? 'analysis__step-dot--current' : ''}`}
              >
                <Text className="analysis__step-dot-text">
                  {i < currentStep ? '✓' : i + 1}
                </Text>
              </View>
              <Text
                className={`analysis__step-label ${i === currentStep ? 'analysis__step-label--current' : ''}`}
              >
                {step.label}
              </Text>
            </View>
          ))}
        </View>

        <Text className="analysis__tip fade-up delay-3">{tipText}</Text>

        {/* 不锁人：后台继续分析，首页任务轨承接进度，完成即提醒 */}
        <Text
          className="analysis__wander fade-up delay-3 pressable"
          onClick={() => Taro.switchTab({ url: '/pages/home/index' })}
        >
          先去逛逛，不用守在这里 ›
        </Text>
      </View>
    </View>
  )
}
