// 分析进度页的纯逻辑：阶段码 → 第几步、缓存里的照片 → 三个槽、失败 → 恢复动作。
// 不碰 Taro、不碰网络；所有中文都来自 @zsm/core 的 copy/zh，这里只做判断。
//
// 这一页最容易被写坏的地方是「替服务端说话」：补间出一个更好看的进度、
// 按已过时间编一句「正在分析侧脸线条」、超时六分钟就自己宣布失败。
// 那些数字和句子都很像真的，但没有一个是服务端说过的。
// 所以这里的每个函数都只做一件事：把服务端给的东西翻译成界面，翻不出来就留空。
import {
  ANALYSIS_FAIL_COPY,
  ASSESSMENT_COPY,
  ASSESSMENT_STAGE_COPY,
  assessmentStageText,
} from '@zsm/core'
import type { DisplayMedia } from '@zsm/core'
// 带 .ts 后缀：CAPTURE_ROLES 是值导入，而 node --test 直接加载这个文件，
// Node 的 ESM 解析不做后缀补全（同 features/report/model.ts 里的理由）。
import { CAPTURE_ROLES, type CaptureRole, type PhotosByRole } from '../capture/model.ts'

/**
 * 服务端阶段码 → 三步指示器的第几步。
 *
 * 键必须与 apps/server/internal/service/assessment/policy.go 的阶段常量一致；
 * 测试断言它与 ASSESSMENT_STAGE_COPY 的键集合完全相同——两张表一旦错位，
 * 表现是「文案说在核对照片、指示器却亮着整理报告」，从截图上几乎看不出来。
 */
const STEP_BY_STAGE: Record<string, number> = {
  'photo.technical_check': 0,
  'photo.content_check': 0,
  'photo.identity_check': 0,
  'report.generating': 1,
  'report.evidence_check': 1,
  'report.publishing': 2,
}

/**
 * 阶段码 → 0/1/2；不认识的码返回 -1。
 *
 * 返回 -1 而不是 0：服务端加了新阶段而客户端还没跟上时，三步全不亮，
 * 好过亮着第一步、让用户以为分析卡在「检查照片」。
 */
export function assessmentStepOf(stageCode: string | undefined): number {
  if (stageCode && stageCode in STEP_BY_STAGE) return STEP_BY_STAGE[stageCode] as number
  return -1
}

/**
 * 进度行的唯一来源：服务端公开文案优先，没有才按阶段码查表。
 *
 * 不做时间插值、不做文案轮播——旧页面那条 14 档时间线就是这么做出来的，
 * 它能让进度看起来一直在动，代价是屏幕上写着服务端从没说过的话。
 */
export function assessmentStageLine(view: { message: string; stageCode: string }): string {
  return view.message || assessmentStageText(view.stageCode)
}

/** 一个槽位：角色固定，照片可能没有。 */
export interface AssessmentPhotoSlot {
  role: CaptureRole
  media: DisplayMedia | null
}

/**
 * 缓存里的照片 → 三个固定槽位。
 *
 * 三个角色永远都在（顺序也不变），缺的那张 `media` 为 null，由页面渲染空态。
 * 这一条是报告页「不用 body 图顶替」在分析页的同一规则的投影：缺图就空着。
 */
export function assessmentPhotoSlots(photos: PhotosByRole): AssessmentPhotoSlot[] {
  return CAPTURE_ROLES.map((role) => ({ role, media: photos[role] ?? null }))
}

/**
 * 缓存值 → 可信的照片表。
 *
 * 缓存是 `Map<string, unknown>`，拿到的东西在类型上什么都不是：这里逐条验形状，
 * 认不出来的一律丢掉。宁可显示三个空槽，也不把半个对象喂给图片组件。
 */
export function assessmentPhotosFrom(value: unknown): PhotosByRole {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return {}
  const source = value as Record<string, unknown>
  const photos: PhotosByRole = {}
  for (const role of CAPTURE_ROLES) {
    const media = source[role]
    if (isDisplayMedia(media)) photos[role] = media
  }
  return photos
}

function isDisplayMedia(value: unknown): value is DisplayMedia {
  if (typeof value !== 'object' || value === null) return false
  const media = value as { asset_id?: unknown; url?: unknown }
  return typeof media.asset_id === 'string' && typeof media.url === 'string' && media.url !== ''
}

export type AssessmentRecoveryAction = 'retry' | 'reshoot'

export interface AssessmentRecovery {
  title: string
  body: string
  actionText: string
  action: AssessmentRecoveryAction
}

/**
 * 失败 → 用户能做的下一步。
 *
 * 只按 `retryable` 分流，不看 `error_code`：错误码是服务端内部词汇，
 * 客户端按码分支等于把内部枚举抄进界面，服务端一改就悄悄错位。
 * 重发有意义的（服务端说可以重试）给「重新发起」，其余一律回到拍摄——
 * 照片本身没过检查时，再发一次同一批照片不会得到不同结果。
 */
export function assessmentRecovery(view: { message: string; retryable: boolean }): AssessmentRecovery {
  if (view.retryable) {
    return {
      title: ANALYSIS_FAIL_COPY.timeoutTitle,
      body: view.message || ANALYSIS_FAIL_COPY.timeoutBody,
      actionText: ASSESSMENT_COPY.retryAction,
      action: 'retry',
    }
  }
  return {
    title: ANALYSIS_FAIL_COPY.photoTitle,
    body: view.message || ANALYSIS_FAIL_COPY.photoFallback,
    actionText: ASSESSMENT_COPY.reshootAction,
    action: 'reshoot',
  }
}

/**
 * 「重新发起」的幂等键，必须与首次提交的键**不同**。
 *
 * 首次提交的键绑在三张照片上，服务端已经把它和那次失败的受理绑在一起；
 * 幂等键重放会原样返回存下的响应，所以复用同一把键 = 把同一份失败再拿回来一次，
 * 用户按一百次也还是那一份结果。序号只活在本次会话的组件状态里，不进 Storage：
 * 它不是业务状态，只是「这是第几次点击」。
 */
export function assessmentRetryKey(photos: PhotosByRole, attempt: number): string {
  const ids = CAPTURE_ROLES.map((role) => photos[role]?.asset_id ?? '')
  return `assessment-retry:${ids.join(':')}:${attempt}`
}

/** 取消 / 被取代不是错误，各有各的说法，但都不提供「重试」。 */
export function assessmentEndedBody(reason: 'cancelled' | 'superseded'): string {
  return reason === 'cancelled' ? ASSESSMENT_COPY.endedCancelled : ASSESSMENT_COPY.endedSuperseded
}

/**
 * 受理结果 → 进度页路由。两个 id 都带上：assessment id 用来取本次提交的照片，
 * operation id 是这一页唯一要轮询的东西。
 */
export function assessmentRoute(accepted: {
  data: { id: string }
  operation: { id: string }
}): string {
  return (
    `/pages/analysis/index?assessment_id=${encodeURIComponent(accepted.data.id)}` +
    `&operation_id=${encodeURIComponent(accepted.operation.id)}`
  )
}
