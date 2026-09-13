// 分析进度页：屏幕上关于进度的每一个数字和每一句话都来自服务端。
//
// 与旧分析页差在四处「不再猜」：
// 1. 不再有进度补间（createDisplayProgress）——按 ~9%/s 往上爬的那个数字服务端没说过；
// 2. 不再有按时间推进的 14 档文案时间线——「正在分析侧脸线条」是客户端编的；
// 3. 不再有 6 分钟本地超时守卫——分析多久算太久由服务端判断，客户端自己宣布失败
//    会让一次本来会成功的分析在用户眼前变成错误；
// 4. 不再读 Storage 里的「当前任务」。恢复靠路由上的 operation_id + 资源缓存。
//
// 失败也不看 error_code：那是服务端内部词汇，客户端按码分支等于把内部枚举抄进界面。
// 只看 retryable 决定「重新发起」还是「重新拍摄」。
import { useEffect, useRef, useState } from 'react'
import Taro from '@tarojs/taro'
import { Text, View } from '@tarojs/components'
import { ASSESSMENT_COPY, CAPTURE_COPY, ERROR_COPY } from '@zsm/core'
import { operationView } from '../../app/operations/operation-view'
import { useOperationPolling } from '../../app/operations/use-operation-polling'
import { resourceCache, resourceKey } from '../../app/cache/resource-cache'
import EmptyState from '../../components/empty-state'
import ErrorState from '../../components/error-state'
import SourceImage from '../../components/source-image'
import TextLink from '../../components/text-link'
import { captureReady, type PhotosByRole } from '../capture/model'
import { reportRouteAfterOperation } from '../report/model'
import {
  assessmentEndedBody,
  assessmentPhotoSlots,
  assessmentPhotosFrom,
  assessmentRecovery,
  assessmentRetryKey,
  assessmentStageLine,
  assessmentStepOf,
} from './model'
import { assessmentSubmitErrorText, submitAssessment } from './start'
import './index.scss'

const HOME_ROUTE = '/pages/home/index'
const CAPTURE_ROUTE = '/pages/capture/index'

interface AssessmentScreenProps {
  assessmentId: string
  operationId: string
}

export default function AssessmentScreen({ assessmentId, operationId }: AssessmentScreenProps) {
  // 照片是提交时留在缓存里的那一份（契约里 Assessment 不带媒体，也没有按 id 读媒体的接口）。
  // 路由参数要等 useLoad 才到位，所以读缓存跟着 assessmentId 走而不是只读一次：
  // 冷启动时缓存为空 → 三个空槽，绝不去别处找一张图顶上。
  const [photos, setPhotos] = useState<PhotosByRole>({})
  useEffect(() => {
    setPhotos(
      assessmentPhotosFrom(resourceCache.read(resourceKey('assessment-photos', assessmentId))),
    )
  }, [assessmentId])
  const [offline, setOffline] = useState(false)
  // 连续五次拉取失败后轮询会自行停止（core 的失败上限），重试必须把它重新装起来。
  const [restartKey, setRestartKey] = useState(0)
  const [submitting, setSubmitting] = useState(false)
  const attemptRef = useRef(0)
  const redirected = useRef(false)

  const { operations } = useOperationPolling({
    operationIds: [operationId],
    enabled: Boolean(operationId) && !offline,
    restartKey,
    onFetchFailure: () => setOffline(true),
  })

  // 没有 operation id 就没有可轮询的东西：本页不猜「当前任务」，回首页重新走。
  useEffect(() => {
    if (!operationId) void Taro.switchTab({ url: HOME_ROUTE })
  }, [operationId])

  const operation = operations[0]
  const view = operationView(operation)
  const route = reportRouteAfterOperation(operation)

  // 终态跳转只认服务端给的 result_type / result_id（判定在 report/model 里）。
  useEffect(() => {
    if (!route || redirected.current) return
    redirected.current = true
    void Taro.redirectTo({ url: route })
  }, [route])

  const slots = assessmentPhotoSlots(photos)
  const photosReady = captureReady(photos)

  const goHome = () => {
    void Taro.switchTab({ url: HOME_ROUTE })
  }

  /**
   * 「重新发起」：同三张照片、换一把幂等键，重新受理一次。
   * 首提那把键在服务端已经和这次失败的受理绑在一起，重放只会拿回同一份失败。
   */
  const retrySubmission = async () => {
    if (submitting) return
    setSubmitting(true)
    attemptRef.current += 1
    try {
      await submitAssessment(photos, assessmentRetryKey(photos, attemptRef.current))
    } catch (error) {
      setSubmitting(false)
      Taro.showToast({ title: assessmentSubmitErrorText(error), icon: 'none' })
    }
  }

  const retryNetwork = () => {
    setOffline(false)
    setRestartKey((key) => key + 1)
  }

  // 一、拉取连续失败：这一屏只说网络，不冒充业务失败。
  if (offline || !operationId) {
    return (
      <View className="assessment-screen">
        <ErrorState
          title={ASSESSMENT_COPY.networkTitle}
          message={ASSESSMENT_COPY.networkBody}
          retryText={ERROR_COPY.retryAction}
          onRetry={retryNetwork}
        />
        <View className="assessment-screen__foot">
          <TextLink text={ASSESSMENT_COPY.homeAction} onClick={goHome} />
        </View>
      </View>
    )
  }

  // 二、业务失败：内容不渲染，只给公开文案、请求编号和一个明确动作。
  if (view.kind === 'failed') {
    // 照片不在缓存里（冷启动、或缓存被清过）时「重新发起」无从发起——没有照片就没有请求体。
    // 这时把动作降级成「重新拍摄」，那才是用户真能走通的下一步。
    const recovery = assessmentRecovery({
      message: view.message,
      retryable: view.retryable && photosReady,
    })
    return (
      <View className="assessment-screen">
        <ErrorState
          title={recovery.title}
          message={recovery.body}
          retryText={recovery.actionText}
          onRetry={() => {
            if (recovery.action === 'retry') void retrySubmission()
            else void Taro.redirectTo({ url: CAPTURE_ROUTE })
          }}
        />
        <View className="assessment-screen__foot">
          {view.requestId ? (
            <Text className="assessment-screen__request-id">
              {`${ASSESSMENT_COPY.requestIdLabel} ${view.requestId}`}
            </Text>
          ) : null}
          <TextLink text={ASSESSMENT_COPY.homeAction} onClick={goHome} />
        </View>
      </View>
    )
  }

  // 三、取消 / 被取代：不是错误，也没有「重试」可以承诺。
  if (view.kind === 'ended') {
    return (
      <View className="assessment-screen">
        <EmptyState
          title={ASSESSMENT_COPY.endedTitle}
          description={assessmentEndedBody(view.reason)}
          actionText={ASSESSMENT_COPY.homeAction}
          onAction={goHome}
        />
      </View>
    )
  }

  // 四、结束了却没有报告指针：不编一个结果，也不停在转圈上。
  // 有指针时上面那个 effect 正在跳转，这一帧只留一行说明——
  // 不能顺手渲染「没有拿到报告」，那会在成功的分析上闪一句错话。
  if (view.kind === 'succeeded') {
    if (route) {
      return (
        <View className="assessment-screen">
          <View className="assessment-screen__stage fade-up">
            <Text className="assessment-screen__stage-text">{ASSESSMENT_COPY.openingReport}</Text>
          </View>
        </View>
      )
    }
    return (
      <View className="assessment-screen">
        <EmptyState
          title={ASSESSMENT_COPY.noResultTitle}
          description={ASSESSMENT_COPY.noResultBody}
          actionText={ASSESSMENT_COPY.homeAction}
          onAction={goHome}
        />
      </View>
    )
  }

  // 五、还在跑（accepted / running / retrying）。
  const working = view.kind === 'working' ? view : null
  const step = working ? assessmentStepOf(working.stageCode) : -1

  return (
    <View className="assessment-screen">
      <View className="assessment-screen__photos fade-up">
        {slots.map((slot) => (
          <View key={slot.role} className="assessment-screen__photo">
            {slot.media ? (
              <SourceImage
                className="assessment-screen__photo-img"
                media={slot.media}
                mode="aspectFill"
                anchor="top"
                frameAspect={1}
              />
            ) : (
              <View className="assessment-screen__photo-empty">
                <Text className="assessment-screen__photo-empty-text">
                  {ASSESSMENT_COPY.photosMissing}
                </Text>
              </View>
            )}
            <Text className="assessment-screen__photo-label">
              {CAPTURE_COPY.shots[slot.role].label}
            </Text>
          </View>
        ))}
      </View>

      <View className="assessment-screen__stage fade-up delay-1">
        <Text className="assessment-screen__stage-text">
          {working ? assessmentStageLine(working) : ASSESSMENT_COPY.stageFallback}
        </Text>
        {working?.retrying ? (
          <Text className="assessment-screen__stage-note">{ASSESSMENT_COPY.retryingNote}</Text>
        ) : null}
      </View>

      <View className="assessment-screen__progress fade-up delay-2">
        <View className="assessment-screen__track">
          <View
            className="assessment-screen__fill"
            style={{ width: `${working ? working.progress : 0}%` }}
          />
        </View>
        <Text className="assessment-screen__percent">{`${working ? working.progress : 0}%`}</Text>
      </View>

      <View className="assessment-screen__steps fade-up delay-3">
        {ASSESSMENT_COPY.steps.map((label, index) => {
          const state = index < step ? 'done' : index === step ? 'active' : 'todo'
          return (
            <View key={label} className={`assessment-screen__step assessment-screen__step--${state}`}>
              <View className="assessment-screen__step-dot" />
              <Text className="assessment-screen__step-label">{label}</Text>
            </View>
          )
        })}
      </View>

      <View className="assessment-screen__foot">
        <TextLink text={ASSESSMENT_COPY.wander} onClick={goHome} />
        <Text className="assessment-screen__privacy">{ASSESSMENT_COPY.privacy}</Text>
      </View>
    </View>
  )
}
