// 执行清单：只读 execution 快照，勾选即事件。
//
// 三条纪律：
// 1. 乐观状态只在本地活着——服务端响应一到就整体替换，不做字段合并；
// 2. version 冲突（409/412）先拉最新覆盖本地，再让用户重试，本地绝不"合并出"一个
//    服务端没见过的版本；
// 3. 网络失败回滚，但保留同一个 client_event_id——重试是同一笔事件，不是第二次勾选。
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
  createClientEventId,
  executionEventBody,
  ifMatchVersion,
  isEventConflict,
  replaceExecutionFromServer,
  toggleStepLocal,
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
  // 网络失败后保留的 client_event_id：重试同一笔事件，而不是再勾一次
  const pendingEventId = useRef<Record<string, string>>({})
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

  const applyServer = (server: Execution) => {
    const replaced = replaceExecutionFromServer(executionRef.current ?? server, server)
    setExecution(replaced)
    resourceCache.write(resourceKey('execution', replaced.id), replaced)
  }

  /**
   * 一次勾选的完整事务：乐观更新 → 带当前 version 的 If-Match 发事件。
   * 成功整体替换；版本冲突先对账再请用户重试；网络失败回滚但保留事件 id。
   */
  const toggle = async (step: ExecutionStep) => {
    const current = executionRef.current
    if (!current || busy) return
    const nextCompleted = !step.completed
    // 重试沿用上次的 id；新的一次点击生成新的 id
    const clientEventId = pendingEventId.current[step.id] ?? createClientEventId()
    const optimistic = toggleStepLocal(current, step.id)
    setExecution(optimistic)
    if (nextCompleted) Taro.vibrateShort({ type: 'light' })
    setBusy(true)
    try {
      const result = await qualityApi.createExecutionEvent(
        current.id,
        executionEventBody(step.id, nextCompleted, clientEventId, new Date().toISOString()),
        `event:${clientEventId}`,
        ifMatchVersion(current.version),
      )
      delete pendingEventId.current[step.id]
      applyServer(result.execution)
    } catch (error) {
      if (isEventConflict(error)) {
        // 版本冲突：别处已经推进。先对账，重试时是"对新版本再点一次"。
        delete pendingEventId.current[step.id]
        const fresh = await qualityApi.getExecution(current.id).catch(() => null)
        if (fresh) applyServer(fresh)
        Taro.showToast({ title: CHECKLIST_COPY.syncConflict, icon: 'none' })
      } else {
        // 网络失败：回滚到服务端最后确认的样子，保留事件 id 供重试
        setExecution(current)
        pendingEventId.current[step.id] = clientEventId
        Taro.showToast({ title: CHECKLIST_COPY.syncFailed, icon: 'none' })
      }
    } finally {
      setBusy(false)
    }
  }

  /** 全部勾完后的收尾：completed 事件把执行推进终态，反馈入口随之打开。 */
  const complete = async () => {
    const current = executionRef.current
    if (!current || busy) return
    setBusy(true)
    try {
      const result = await qualityApi.createExecutionEvent(
        current.id,
        completedEventBody(createClientEventId(), new Date().toISOString()),
        `event:complete:${current.id}:${current.version}`,
        ifMatchVersion(current.version),
      )
      applyServer(result.execution)
    } catch (error) {
      Taro.showToast({
        title: isEventConflict(error) ? CHECKLIST_COPY.syncConflict : CHECKLIST_COPY.completeFailed,
        icon: 'none',
      })
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

  if (!execution) return null

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
