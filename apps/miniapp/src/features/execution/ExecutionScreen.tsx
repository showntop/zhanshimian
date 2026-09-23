// 执行清单：只读 execution 快照，勾选即事件。
//
// 三条纪律：
// 1. 乐观状态只在本地活着——服务端响应一到就整体替换，不做字段合并；
// 2. version 冲突（409/412）先拉最新覆盖本地，再让用户重试，本地绝不"合并出"一个
//    服务端没见过的版本；
// 3. 网络失败回滚，但保留同一个事件草稿（client_event_id + occurred_at 一起冻结）——
//    重试是同一笔事件的原样重放，occurred_at 变一个字节都会被幂等层拦成 409；
//    400 这类永久失败则丢掉草稿：重试同一笔必然再败，再点是一次新事件。
import { useCallback, useEffect, useRef, useState } from 'react'
import Taro from '@tarojs/taro'
import { Text, View } from '@tarojs/components'
import { CHECKLIST_COPY, ERROR_COPY, reportCategoryLabel } from '@zsm/core'
import type { Execution, ExecutionStep } from '@zsm/core'
import { qualityApi } from '../../app/api/quality'
import { resourceCache, resourceKey } from '../../app/cache/resource-cache'
import ErrorState from '../../components/error-state'
import PrimaryButton from '../../components/primary-button'
import {
  allStepsDone,
  canSubmitExecutionFeedback,
  completedEventBody,
  createEventDraft,
  executionEventBody,
  ifMatchVersion,
  isEventConflict,
  isNetworkFailure,
  replaceExecutionFromServer,
  toggleStepLocal,
  type ExecutionEventDraft,
} from './model'
import './index.scss'

const FEEDBACK_ROUTE = '/pages/feedback/index'

interface ExecutionScreenProps {
  executionId: string
}

export default function ExecutionScreen({ executionId }: ExecutionScreenProps) {
  const [execution, setExecution] = useState<Execution | null>(() =>
    resourceCache.read<Execution>(resourceKey('execution', executionId)) ?? null,
  )
  const [failed, setFailed] = useState(false)
  const [busy, setBusy] = useState(false)
  // 网络失败后保留的事件草稿：重试同一笔事件（id 与 occurred_at 都不变），而不是再勾一次
  const pendingDrafts = useRef<Record<string, ExecutionEventDraft>>({})
  // 收尾事件同理：一笔 completed 在重试间共用同一个草稿
  const pendingCompleteDraft = useRef<ExecutionEventDraft | null>(null)
  const executionRef = useRef<Execution | null>(null)
  executionRef.current = execution

  const load = useCallback(async () => {
    setFailed(false)
    try {
      const next = await resourceCache.revalidate(resourceKey('execution', executionId), () =>
        qualityApi.getExecution(executionId),
      )
      setExecution(next)
    } catch {
      setFailed(true)
    }
  }, [executionId])

  useEffect(() => {
    void load()
  }, [load])

  // 没有 execution id 就没有快照可执行：回方案 tab，不猜「上次选的那套」
  useEffect(() => {
    if (!executionId) void Taro.switchTab({ url: '/pages/plans/index' })
  }, [executionId])

  const applyServer = (server: Execution) => {
    const replaced = replaceExecutionFromServer(executionRef.current ?? server, server)
    setExecution(replaced)
    resourceCache.write(resourceKey('execution', replaced.id), replaced)
  }

  /**
   * 一次勾选的完整事务：乐观更新 → 带当前 version 的 If-Match 发事件。
   * 成功整体替换；版本冲突先对账再请用户重试；网络失败回滚但保留事件草稿，
   * 重试是原样重放；永久失败（400 校验类等）回滚并丢草稿，再点是一笔新事件。
   */
  const toggle = async (step: ExecutionStep) => {
    const current = executionRef.current
    if (!current || busy) return
    const nextCompleted = !step.completed
    // 重试沿用上次的草稿；新的一次点击生成新的草稿
    const draft = pendingDrafts.current[step.id] ?? createEventDraft()
    const optimistic = toggleStepLocal(current, step.id)
    setExecution(optimistic)
    if (nextCompleted) Taro.vibrateShort({ type: 'light' })
    setBusy(true)
    try {
      const result = await qualityApi.createExecutionEvent(
        current.id,
        executionEventBody(step.id, nextCompleted, draft.clientEventId, draft.occurredAt),
        `event:${draft.clientEventId}`,
        ifMatchVersion(current.version),
      )
      delete pendingDrafts.current[step.id]
      applyServer(result.execution)
    } catch (error) {
      if (isEventConflict(error)) {
        // 版本冲突：别处已经推进。先对账，重试时是"对新版本再点一次"。
        delete pendingDrafts.current[step.id]
        const fresh = await qualityApi.getExecution(current.id).catch(() => null)
        if (fresh) applyServer(fresh)
        Taro.showToast({ title: CHECKLIST_COPY.syncConflict, icon: 'none' })
      } else if (isNetworkFailure(error)) {
        // 网络失败：回滚到服务端最后确认的样子，保留事件草稿供原样重试
        setExecution(current)
        pendingDrafts.current[step.id] = draft
        Taro.showToast({ title: CHECKLIST_COPY.syncFailed, icon: 'none' })
      } else {
        // 永久失败（如 occurred_at 校验 400）：重试同一笔必然再败，
        // 回滚并丢掉草稿——用户再点是一笔带新 occurred_at 的新事件
        setExecution(current)
        delete pendingDrafts.current[step.id]
        Taro.showToast({ title: CHECKLIST_COPY.eventRejected, icon: 'none' })
      }
    } finally {
      setBusy(false)
    }
  }

  /**
   * 全部勾完后的收尾：completed 事件把执行推进终态，反馈入口随之打开。
   * 失败三分支与 toggle 同构：冲突先对账、网络失败保留草稿原样重试、
   * 永久失败丢草稿——只 toast 的话，超时重试会被幂等层 409 卡进死循环。
   */
  const complete = async () => {
    const current = executionRef.current
    if (!current || busy) return
    setBusy(true)
    const draft = pendingCompleteDraft.current ?? createEventDraft()
    try {
      const result = await qualityApi.createExecutionEvent(
        current.id,
        completedEventBody(draft.clientEventId, draft.occurredAt),
        `event:complete:${current.id}:${current.version}`,
        ifMatchVersion(current.version),
      )
      pendingCompleteDraft.current = null
      applyServer(result.execution)
    } catch (error) {
      if (isEventConflict(error)) {
        // 412 后本地 version 已过期：先拉最新再允许重试，否则永远 412
        pendingCompleteDraft.current = null
        const fresh = await qualityApi.getExecution(current.id).catch(() => null)
        if (fresh) applyServer(fresh)
        Taro.showToast({ title: CHECKLIST_COPY.syncConflict, icon: 'none' })
      } else if (isNetworkFailure(error)) {
        // 响应丢失但服务端可能已落库：同一草稿 + 确定性幂等键，重试即重放
        pendingCompleteDraft.current = draft
        Taro.showToast({ title: CHECKLIST_COPY.completeFailed, icon: 'none' })
      } else {
        pendingCompleteDraft.current = null
        Taro.showToast({ title: CHECKLIST_COPY.completeFailed, icon: 'none' })
      }
    } finally {
      setBusy(false)
    }
  }

  if (failed && !execution) {
    return (
      <ErrorState
        title={CHECKLIST_COPY.loadFailed}
        retryText={ERROR_COPY.retryAction}
        onRetry={() => void load()}
      />
    )
  }

  if (!execution || !executionId) return null

  const steps = [...execution.steps].sort((a, b) => a.position - b.position)
  const done = steps.filter((step) => step.completed).length
  const allDone = allStepsDone(execution)
  const feedbackOpen = canSubmitExecutionFeedback(execution)

  return (
    <View className="execution-screen">
      <View className={`execution-screen__progress fade-up ${allDone ? 'execution-screen__progress--done' : ''}`}>
        <View className="execution-screen__progress-row">
          <Text className="execution-screen__progress-num">
            {done} / {steps.length} {CHECKLIST_COPY.doneOf}
          </Text>
          <Text className="execution-screen__progress-remain">
            {allDone ? CHECKLIST_COPY.allDoneHint : `${steps.length - done} ${CHECKLIST_COPY.remainSuffix}`}
          </Text>
        </View>
        <View className="execution-screen__track">
          {/* steps.length > 0 的判断在 allStepsDone 里；这里进度条只在有步骤时渲染 */}
          {steps.length > 0 ? (
            <View
              className="execution-screen__fill"
              style={{ width: `${(done / steps.length) * 100}%` }}
            />
          ) : null}
        </View>
        <Text className="execution-screen__progress-hint">
          {allDone ? CHECKLIST_COPY.celebrate : CHECKLIST_COPY.hint}
        </Text>
      </View>

      {steps.length > 0 ? (
        <View className="execution-screen__items fade-up delay-1">
          {steps.map((step) => (
            <View
              key={step.id}
              className={`execution-screen__item pressable ${step.completed ? 'execution-screen__item--done' : ''}`}
              onClick={() => void toggle(step)}
            >
              <View className="execution-screen__check">
                {step.completed ? <Text className="execution-screen__check-mark">✓</Text> : null}
              </View>
              <View className="execution-screen__body">
                <Text className="execution-screen__cat">{reportCategoryLabel(step.category)}</Text>
                <Text className="execution-screen__title">{step.title}</Text>
                {step.summary ? <Text className="execution-screen__desc">{step.summary}</Text> : null}
              </View>
            </View>
          ))}
        </View>
      ) : (
        <Text className="execution-screen__empty fade-up delay-1">{CHECKLIST_COPY.emptyBody}</Text>
      )}

      <View className="execution-screen__foot fade-up delay-2">
        {feedbackOpen ? (
          <>
            <PrimaryButton
              text={CHECKLIST_COPY.ctaDone}
              onClick={() =>
                void Taro.navigateTo({
                  url: `${FEEDBACK_ROUTE}?execution_id=${encodeURIComponent(execution.id)}`,
                })
              }
            />
            <Text className="execution-screen__foot-note">{CHECKLIST_COPY.completedNote}</Text>
          </>
        ) : allDone ? (
          <>
            <PrimaryButton text={CHECKLIST_COPY.completeAction} loading={busy} onClick={() => void complete()} />
            <Text className="execution-screen__foot-note">{CHECKLIST_COPY.footNote}</Text>
          </>
        ) : (
          <Text className="execution-screen__foot-note">{CHECKLIST_COPY.cta}</Text>
        )}
      </View>
    </View>
  )
}
