// 场合 Brief：单页几问，答案只活在组件 state 与 POST body 里。
// 修改答案重新提交会创建一份新的方案集（服务端按幂等键与内容决定复用或受理），
// 本地不存任何 Brief——存了就成了会过期的第二份答案。
import { useCallback, useEffect, useState } from 'react'
import Taro from '@tarojs/taro'
import { Text, View } from '@tarojs/components'
import {
  ERROR_COPY,
  SCENE_BRIEF_COPY,
  SCENES,
} from '@zsm/core'
import { qualityApi } from '../../app/api/quality'
import { PublicApiError } from '../../app/api/result'
import { resourceCache } from '../../app/cache/resource-cache'
import { writePlanSetHandoff } from '../../app/plan-set-handoff'
import { handleBillingError } from '../../services/billing'
import EmptyState from '../../components/empty-state'
import ErrorState from '../../components/error-state'
import Pill from '../../components/pill'
import PrimaryButton from '../../components/primary-button'
import {
  createIdempotencyKey,
  planSetRetryMarkerKey,
  planSetSceneKey,
  sceneBriefRequest,
  sceneFields,
} from './model'
import './index.scss'

const PLANS_TAB = '/pages/plans/index'
const CAPTURE_ROUTE = '/pages/capture/index'

/** 在途 Operation 状态（与 bootstrap active_operations 语义一致；failed 不在其中——失败允许重新建档） */
const IN_FLIGHT = new Set(['accepted', 'running', 'retrying'])

interface SceneBriefScreenProps {
  scene: string
}

export default function SceneBriefScreen({ scene }: SceneBriefScreenProps) {
  const fields = sceneFields(scene)
  const [reportId, setReportId] = useState('')
  const [noReport, setNoReport] = useState(false)
  const [analyzingOperationId, setAnalyzingOperationId] = useState('')
  const [failed, setFailed] = useState(false)
  const [answers, setAnswers] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)

  // 外壳等路由参数到位才挂载本屏，页面 onLoad 早于本屏挂载——
  // 后注册的 useLoad 不会再触发，数据拉取只能走挂载 effect。
  const loadReport = useCallback(async () => {
    try {
      const current = await qualityApi.getCurrentReport()
      if (current) {
        setReportId(current.id)
        return
      }
      // 无档案要区分「分析在途」与「从未建档」：前者引导看进度，
      // 把用户送去拍摄页等于引导发起第二次建档（bootstrap 失败按无在途处理）
      const boot = await qualityApi.getHomeBootstrap().catch(() => null)
      const analyzing = (boot?.active_operations ?? []).find(
        (operation) => operation.kind === 'assessment' && IN_FLIGHT.has(operation.status),
      )
      setAnalyzingOperationId(analyzing?.id ?? '')
      setNoReport(true)
    } catch {
      setFailed(true)
    }
  }, [])

  useEffect(() => {
    void loadReport()
  }, [loadReport])

  // general 与未知场景都没有 Brief 页：回方案 tab。导航是副作用，不进渲染期。
  useEffect(() => {
    if (!fields) void Taro.switchTab({ url: PLANS_TAB })
  }, [fields])

  const sceneLabel = SCENES.find((s) => s.id === scene)?.label ?? ''
  const answered = fields ? fields.filter((field) => answers[field.key]).length : 0
  const complete = fields ? answered === fields.length : false

  const pick = (key: string, value: string) => {
    setAnswers((prev) => ({ ...prev, [key]: value }))
  }

  const submit = async () => {
    if (!reportId || busy) return
    const request = sceneBriefRequest(reportId, scene, answers)
    if (!request) return
    setBusy(true)
    try {
      // 上一次受理已到终态 failed 时，固定键 24h 内只会重放同一份失败：换新键重新受理。
      // 在途/双击仍用固定键保幂等（busy 护栏之外的第二道）。
      const retryKey = planSetRetryMarkerKey(scene)
      const fresh = Boolean(resourceCache.read<string>(retryKey))
      const start = await qualityApi.createPlanSet(
        request,
        fresh ? createIdempotencyKey(`plan-set:${reportId}:${scene}`) : `plan-set:${reportId}:${scene}`,
      )
      if (fresh) resourceCache.remove(retryKey)
      if (start.accepted) {
        // 方案 tab 常驻、受理窗内方案集还没落库：场景经侧信道留给它，
        // 规划失败时「重新生成」才知道回到哪个场合
        resourceCache.write(planSetSceneKey(start.data.id), scene)
      }
      writePlanSetHandoff({
        planSetId: start.accepted ? start.data.id : start.planSet.id,
        operationId: start.accepted ? start.operation.id : null,
      })
      await Taro.switchTab({ url: PLANS_TAB })
    } catch (error) {
      // 402/429 等计费错误先走购买引导（弹层→标记→profile 购买层），其余才落通用提示
      if (handleBillingError(error)) return
      const message = error instanceof PublicApiError && error.message ? error.message : SCENE_BRIEF_COPY.submitFailed
      Taro.showToast({ title: message, icon: 'none' })
    } finally {
      setBusy(false)
    }
  }

  if (!fields) {
    // 上面的 effect 正在回方案 tab，这一帧留空
    return <View className="scene-brief" />
  }

  if (failed) {
    return (
      <ErrorState
        title={SCENE_BRIEF_COPY.loadFailed}
        retryText={ERROR_COPY.retryAction}
        onRetry={() => {
          setFailed(false)
          void loadReport()
        }}
      />
    )
  }

  if (noReport) {
    const analyzing = Boolean(analyzingOperationId)
    return (
      <EmptyState
        title={analyzing ? SCENE_BRIEF_COPY.analyzingTitle : SCENE_BRIEF_COPY.needArchiveTitle}
        description={analyzing ? SCENE_BRIEF_COPY.analyzingBody : SCENE_BRIEF_COPY.needArchiveBody}
        actionText={analyzing ? SCENE_BRIEF_COPY.analyzingAction : SCENE_BRIEF_COPY.needArchiveAction}
        onAction={() =>
          analyzing
            ? void Taro.navigateTo({
                url:
                  `/pages/analysis/index?operation_id=${encodeURIComponent(analyzingOperationId)}` +
                  `&assessment_id=`,
              })
            : void Taro.redirectTo({ url: CAPTURE_ROUTE })
        }
      />
    )
  }

  if (!reportId) {
    return (
      <View className="scene-brief scene-brief--loading">
        <View className="spinner" />
      </View>
    )
  }

  return (
    <View className="scene-brief">
      <View className="scene-brief__intro fade-up">
        <View className="scene-brief__intro-head">
          <Text className="scene-brief__title">{sceneLabel}</Text>
          <Text className="scene-brief__badge">{SCENE_BRIEF_COPY.reuseBadge}</Text>
        </View>
        <Text className="scene-brief__lede">{SCENE_BRIEF_COPY.lede}</Text>
      </View>

      {fields.map((field, index) => (
        <View
          key={field.key}
          className={`scene-brief__field fade-up delay-${Math.min(index + 1, 3)}`}
        >
          <Text className="scene-brief__field-label">{field.label}</Text>
          <View className="scene-brief__options">
            {field.options.map((option) => (
              <Pill
                key={option.value}
                label={option.label}
                active={answers[field.key] === option.value}
                onClick={() => pick(field.key, option.value)}
              />
            ))}
          </View>
        </View>
      ))}

      <View className="scene-brief__foot fade-up delay-3">
        <PrimaryButton
          text={SCENE_BRIEF_COPY.generateAction}
          disabled={!complete}
          loading={busy}
          onClick={() => void submit()}
        />
      </View>
    </View>
  )
}
