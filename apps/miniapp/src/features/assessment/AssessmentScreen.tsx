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
//
// 视觉沿用 09-11 旧线：拱形相框（主照片 + 扫描光带 + 拍立得角卡 + 状态标记）、进度微光、
// 三步指示器；角卡归位 / 相框 settle / 扫描停止的阈值改由服务端 progress 驱动（旧线喂的是
// 补间值）。照片一律走 SourceImage 投影（角标/弱化语义），不退回旧线的裸 Image。
import { useEffect, useRef, useState } from 'react'
import Taro from '@tarojs/taro'
import { Text, View } from '@tarojs/components'
import { ASSESSMENT_COPY, ERROR_COPY } from '@zsm/core'
import { operationView } from '../../app/operations/operation-view'
import { useOperationPolling } from '../../app/operations/use-operation-polling'
import { resourceCache, resourceKey } from '../../app/cache/resource-cache'
import EmptyState from '../../components/empty-state'
import ErrorState from '../../components/error-state'
import PrimaryButton from '../../components/primary-button'
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

/** 相框宽/高比（rpx）：SourceImage 顶对齐铺满时按它决定裁底还是裁侧。 */
const ARCH_ASPECT = 600 / 620
const MINI_ASPECT = 148 / 196
const THUMB_ASPECT = 120 / 160

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

  // 二、业务失败：旧线 analysis-fail 视觉——标题、本次照片胶片条、公开文案、一个明确动作。
  // 内容不与错误同屏；请求编号与回首页是新契约要保留的功能（旧线没有，按旧视觉语言补）。
  if (view.kind === 'failed') {
    // 照片不在缓存里（冷启动、或缓存被清过）时「重新发起」无从发起——没有照片就没有请求体。
    // 这时把动作降级成「重新拍摄」，那才是用户真能走通的下一步。
    const recovery = assessmentRecovery({
      message: view.message,
      retryable: view.retryable && photosReady,
    })
    const film = slots.filter((slot) => slot.media)
    return (
      <View className="assessment-screen">
        <View className="assessment-fail fade-up">
          <Text className="assessment-fail__title">{recovery.title}</Text>
          {film.length > 0 ? (
            <View className="assessment-fail__film">
              {film.map((slot) => (
                <SourceImage
                  key={slot.role}
                  className="assessment-fail__thumb"
                  media={slot.media}
                  anchor="top"
                  frameAspect={THUMB_ASPECT}
                />
              ))}
            </View>
          ) : null}
          <Text className="assessment-fail__reason">{recovery.body}</Text>
          <View className="assessment-fail__action">
            <PrimaryButton
              text={recovery.actionText}
              loading={submitting}
              onClick={() => {
                if (recovery.action === 'retry') void retrySubmission()
                else void Taro.redirectTo({ url: CAPTURE_ROUTE })
              }}
            />
          </View>
          {view.requestId ? (
            <Text className="assessment-fail__request-id">
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
          <Text className="assessment-screen__opening fade-up">{ASSESSMENT_COPY.openingReport}</Text>
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
  // 进度数字的唯一来源是服务端 progress_bps（operation-view 已钳到 0-100），没有补间。
  const progress = working ? working.progress : 0
  const stageText = working ? assessmentStageLine(working) : ASSESSMENT_COPY.stageFallback

  // 拱形相框：正脸作主照片，侧脸/全身收成底部两张拍立得角卡；缺的那张不补位。
  const [heroSlot, ...miniSlots] = slots
  const minis = miniSlots.filter((slot) => slot.media)
  const settling = progress >= 88

  return (
    <View className="assessment-screen">
      <View className="assessment-screen__portrait fade-up">
        <View
          className={`assessment-screen__arch ${settling ? 'assessment-screen__arch--settle' : ''}`}
        >
          {heroSlot?.media ? (
            <SourceImage
              className="assessment-screen__hero"
              media={heroSlot.media}
              anchor="top"
              frameAspect={ARCH_ASPECT}
            />
          ) : (
            <View className="assessment-screen__hero-empty">
              <Text className="assessment-screen__hero-empty-text">
                {ASSESSMENT_COPY.photosMissing}
              </Text>
            </View>
          )}
          {progress < 92 ? <View className="scan-sweep" /> : null}
          {minis.map((slot, index) => (
            <SourceImage
              key={slot.role}
              className={`assessment-screen__mini assessment-screen__mini--${index} ${progress >= 70 ? 'assessment-screen__mini--rest' : ''}`}
              media={slot.media}
              anchor="top"
              frameAspect={MINI_ASPECT}
            />
          ))}
          <Text className="assessment-screen__mark">
            {settling ? ASSESSMENT_COPY.markSettling : ASSESSMENT_COPY.markAnalyzing}
          </Text>
        </View>
      </View>

      <View className="assessment-screen__stage-block">
        <Text key={stageText} className="assessment-screen__stage">
          {stageText}
        </Text>
        {working?.retrying ? (
          <Text className="assessment-screen__stage-note">{ASSESSMENT_COPY.retryingNote}</Text>
        ) : null}
      </View>

      <View className="assessment-screen__progress fade-up delay-2">
        <View className="assessment-screen__track">
          <View className="assessment-screen__fill" style={{ width: `${progress}%` }} />
        </View>
        <View className="assessment-screen__progress-meta">
          <Text className="assessment-screen__num">{`${Math.round(progress)}%`}</Text>
          <Text className="assessment-screen__eta">{ASSESSMENT_COPY.eta}</Text>
        </View>
      </View>

      <View className="assessment-screen__steps fade-up delay-2">
        {ASSESSMENT_COPY.steps.map((label, index) => (
          <View key={label} className="assessment-screen__step-item">
            {index > 0 ? (
              <View
                className={`assessment-screen__step-line ${step >= index ? 'assessment-screen__step-line--done' : ''}`}
              />
            ) : null}
            <View
              className={`assessment-screen__step-dot ${index < step ? 'assessment-screen__step-dot--done' : ''} ${index === step ? 'assessment-screen__step-dot--current' : ''}`}
            >
              <Text className="assessment-screen__step-dot-text">
                {index < step ? '✓' : index + 1}
              </Text>
            </View>
            <Text
              className={`assessment-screen__step-label ${index === step ? 'assessment-screen__step-label--current' : ''}`}
            >
              {label}
            </Text>
          </View>
        ))}
      </View>

      <Text className="assessment-screen__tip fade-up delay-3">{ASSESSMENT_COPY.privacy}</Text>

      {/* 不锁人：后台继续分析，完成 toast +「我的」任务中心承接，不回首页插进度条 */}
      <View className="assessment-screen__wander fade-up delay-3">
        <TextLink text={ASSESSMENT_COPY.wander} onClick={goHome} />
      </View>
    </View>
  )
}
