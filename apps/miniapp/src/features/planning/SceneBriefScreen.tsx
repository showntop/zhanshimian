// 场合 Brief：单页几问，答案只活在组件 state 与 POST body 里。
// 修改答案重新提交会创建一份新的方案集（服务端按幂等键与内容决定复用或受理），
// 本地不存任何 Brief——存了就成了会过期的第二份答案。
import { useCallback, useEffect, useRef, useState } from 'react'
import Taro from '@tarojs/taro'
import { Text, View } from '@tarojs/components'
import {
  ERROR_COPY,
  SCENE_BRIEF_COPY,
  SCENES,
  sceneIncompleteText,
} from '@zsm/core'
import type { PlanSet } from '@zsm/core'
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
  briefFingerprint,
  createIdempotencyKey,
  planSetSceneKey,
  sceneBriefPrefill,
  sceneBriefRequest,
  sceneFields,
} from './model'
import { clearPlanSetRetryMark, hasPlanSetRetryMark } from '../../app/plan-set-retry'
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
  // 点过「生成」但没答完：标出没选的题（答一题少一题，不用手动关）
  const [showMissing, setShowMissing] = useState(false)

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

  // 「重新设计」入口：用该场景最新方案集的 brief 预填答案（逐字段对表校验，缺题留空）。
  // 预填只发生一次，且不覆盖用户已经点过的选项（late response 不得回写）。
  // hadPublishedRef 同时标记「该场景已有已发布方案集」：提交带 refresh 强制重出。
  const prefilledRef = useRef(false)
  const hadPublishedRef = useRef(false)
  useEffect(() => {
    if (!reportId || prefilledRef.current) return
    prefilledRef.current = true
    let cancelled = false
    void qualityApi
      .listPlanSets(reportId, scene as PlanSet['scene'])
      .then((list) => {
        if (cancelled) return
        const latest = [...list].sort((a, b) => b.created_at.localeCompare(a.created_at))[0]
        if (!latest) return
        hadPublishedRef.current = true
        const prefill = sceneBriefPrefill(scene, latest.brief)
        if (Object.keys(prefill).length === 0) return
        setAnswers((prev) => (Object.keys(prev).length > 0 ? prev : prefill))
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [reportId, scene])

  // general 与未知场景都没有 Brief 页：回方案 tab。导航是副作用，不进渲染期。
  useEffect(() => {
    if (!fields) void Taro.switchTab({ url: PLANS_TAB })
  }, [fields])

  const sceneLabel = SCENES.find((s) => s.id === scene)?.label ?? ''
  const missing = fields ? fields.filter((field) => !answers[field.key]) : []
  const complete = fields ? missing.length === 0 : false

  const pick = (key: string, value: string) => {
    setAnswers((prev) => ({ ...prev, [key]: value }))
  }

  const submit = async () => {
    if (!reportId || busy) return
    // 没答完：主按钮是禁用态（点击被组件挡下），点它的人不知道为什么没反应——
    // 这里接住并说明还差几题，同时把没选的题标出来（红线：错误不甩锅、给下一步）
    if (!complete) {
      setShowMissing(true)
      void Taro.showToast({ title: sceneIncompleteText(missing.length), icon: 'none' })
      return
    }
    setBusy(true)
    try {
      // 重新设计（该场景已有已发布方案集）：带 refresh——同 brief 也强制重出，
      // 语义键去重只复用不新建，refresh 才绕得开（服务端折入一次性 nonce 派生新身份）。
      const refresh = hadPublishedRef.current
      const request = sceneBriefRequest(reportId, scene, answers)
      if (!request) {
        // 答案对不上当前选项表（预填了旧档案的值）：静默返回就是「点了没反应」，
        // 清掉失效答案并说明，让人重选
        for (const field of fields ?? []) {
          if (!field.options.some((option) => option.value === answers[field.key])) {
            setAnswers((prev) => {
              if (!(field.key in prev)) return prev
              const next = { ...prev }
              delete next[field.key]
              return next
            })
          }
        }
        void Taro.showToast({ title: SCENE_BRIEF_COPY.answersStale, icon: 'none' })
        return
      }
      const body = refresh ? { ...request, refresh: true } : request
      // 幂等键带答案指纹：同答案重发同键保幂等（在途/双击），改答案即新键——
      // 固定键配改过的答案会被服务端判 409（相同幂等键已被用于不同请求）。
      // 例外必须换新键：上次受理终态 failed（同键 24h 内重放同一份失败），
      // 以及 refresh（每次强制重出都是新任务，重放旧 202 会把新任务吞掉）。
      // 失败标记落 Storage：重启后重发也不能撞回旧键（见 app/plan-set-retry.ts）。
      const fresh = refresh || hasPlanSetRetryMark(scene)
      const baseKey = `plan-set:${reportId}:${scene}:${briefFingerprint(answers)}`
      const start = await qualityApi.createPlanSet(
        body,
        fresh ? createIdempotencyKey(baseKey) : baseKey,
      )
      if (fresh) clearPlanSetRetryMark(scene)
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
          className={[
            'scene-brief__field',
            `fade-up delay-${Math.min(index + 1, 3)}`,
            showMissing && !answers[field.key] ? 'scene-brief__field--missing' : ''
          ].join(' ')}
        >
          <Text className="scene-brief__field-label">
            {field.label}
            {showMissing && !answers[field.key] ? `（${SCENE_BRIEF_COPY.missingTag}）` : ''}
          </Text>
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

      {/* 未答完时按钮是禁用态，点击被组件挡下会变成「点了没反应」——
          包一层接管这次点击，把原因说出来（答完的点击照常走按钮） */}
      <View
        className="scene-brief__foot fade-up delay-3"
        onClick={() => {
          if (!busy && !complete) void submit()
        }}
      >
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
