// 场合 Brief：单页几问，答案只活在组件 state 与 POST body 里。
// 修改答案重新提交会创建一份新的方案集（服务端按幂等键与内容决定复用或受理），
// 本地不存任何 Brief——存了就成了会过期的第二份答案。
import { useEffect, useState } from 'react'
import Taro, { useLoad } from '@tarojs/taro'
import { Text, View } from '@tarojs/components'
import {
  ERROR_COPY,
  SCENE_BRIEF_COPY,
  SCENES,
} from '@zsm/core'
import { qualityApi } from '../../app/api/quality'
import { PublicApiError } from '../../app/api/result'
import { writePlanSetHandoff } from '../../app/plan-set-handoff'
import EmptyState from '../../components/empty-state'
import ErrorState from '../../components/error-state'
import Pill from '../../components/pill'
import PrimaryButton from '../../components/primary-button'
import { sceneBriefRequest, sceneFields } from './model'
import './index.scss'

const PLANS_TAB = '/pages/plans/index'
const CAPTURE_ROUTE = '/pages/capture/index'

interface SceneBriefScreenProps {
  scene: string
}

export default function SceneBriefScreen({ scene }: SceneBriefScreenProps) {
  const fields = sceneFields(scene)
  const [reportId, setReportId] = useState('')
  const [noReport, setNoReport] = useState(false)
  const [failed, setFailed] = useState(false)
  const [answers, setAnswers] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)

  useLoad(() => {
    void (async () => {
      try {
        const current = await qualityApi.getCurrentReport()
        if (current) setReportId(current.id)
        else setNoReport(true)
      } catch {
        setFailed(true)
      }
    })()
  })

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
      const start = await qualityApi.createPlanSet(request, `plan-set:${reportId}:${scene}`)
      writePlanSetHandoff({
        planSetId: start.accepted ? start.data.id : start.planSet.id,
        operationId: start.accepted ? start.operation.id : null,
      })
      await Taro.switchTab({ url: PLANS_TAB })
    } catch (error) {
      const message = error instanceof PublicApiError && error.message ? error.message : SCENE_BRIEF_COPY.submitFailed
      Taro.showToast({ title: message, icon: 'none' })
    } finally {
      setBusy(false)
    }
  }

  if (!fields) {
    // general 与未知场景都没有 Brief 页：回方案 tab
    void Taro.switchTab({ url: PLANS_TAB })
    return <View className="scene-brief" />
  }

  if (failed) {
    return (
      <ErrorState
        title={SCENE_BRIEF_COPY.loadFailed}
        retryText={ERROR_COPY.retryAction}
        onRetry={() => {
          setFailed(false)
          void (async () => {
            try {
              const current = await qualityApi.getCurrentReport()
              if (current) setReportId(current.id)
              else setNoReport(true)
            } catch {
              setFailed(true)
            }
          })()
        }}
      />
    )
  }

  if (noReport) {
    return (
      <EmptyState
        title={SCENE_BRIEF_COPY.needArchiveTitle}
        description={SCENE_BRIEF_COPY.needArchiveBody}
        actionText={SCENE_BRIEF_COPY.needArchiveAction}
        onAction={() => void Taro.redirectTo({ url: CAPTURE_ROUTE })}
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
