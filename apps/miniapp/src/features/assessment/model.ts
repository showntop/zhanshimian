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
 *
 * 键取三个 asset id 的 sha256 前 32 位，而不是拼原 id：服务端上限 128 字符
 * （apps/server/internal/httpapi/idempotency.go 的 validIdempotencyKey），
 * `assessment-retry:` + 三个 UUID + 序号 ≥ 129，拼原 id 的重试必然 400。
 * 哈希后键长 = 17 + 32 + 1 + 序号位数，与 id 长度脱钩；语义不变——
 * 同三张照片同 attempt 键稳定，换 attempt 换键。
 */
export function assessmentRetryKey(photos: PhotosByRole, attempt: number): string {
  const ids = CAPTURE_ROLES.map((role) => photos[role]?.asset_id ?? '')
  return `assessment-retry:${sha256Hex(ids.join(':')).slice(0, 32)}:${attempt}`
}

// ---------- 同步 SHA-256 ----------
// 微信运行时没有 node:crypto，wx.getFileInfo 的摘要又只给文件不给字符串；
// 幂等键只需要短输入的确定性摘要，自带一份实现，正确性由测试对照 node:crypto 钉死。

const SHA256_K = [
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
  0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
  0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
  0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
  0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
  0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
]

function rotr32(x: number, n: number): number {
  return ((x >>> n) | (x << (32 - n))) >>> 0
}

function utf8Bytes(input: string): number[] {
  const bytes: number[] = []
  for (let i = 0; i < input.length; i += 1) {
    const code = input.charCodeAt(i)
    if (code < 0x80) {
      bytes.push(code)
    } else if (code < 0x800) {
      bytes.push(0xc0 | (code >> 6), 0x80 | (code & 0x3f))
    } else if (code >= 0xd800 && code <= 0xdbff && i + 1 < input.length) {
      const next = input.charCodeAt(i + 1)
      const cp = 0x10000 + ((code & 0x3ff) << 10) + (next & 0x3ff)
      i += 1
      bytes.push(0xf0 | (cp >> 18), 0x80 | ((cp >> 12) & 0x3f), 0x80 | ((cp >> 6) & 0x3f), 0x80 | (cp & 0x3f))
    } else {
      bytes.push(0xe0 | (code >> 12), 0x80 | ((code >> 6) & 0x3f), 0x80 | (code & 0x3f))
    }
  }
  return bytes
}

/** 标准 SHA-256，返回 64 位小写 hex。 */
function sha256Hex(input: string): string {
  const bytes = utf8Bytes(input)
  const bitLength = bytes.length * 8
  bytes.push(0x80)
  while (bytes.length % 64 !== 56) bytes.push(0)
  for (let shift = 56; shift >= 0; shift -= 8) {
    bytes.push(Math.floor(bitLength / 2 ** shift) & 0xff)
  }

  let h0 = 0x6a09e667
  let h1 = 0xbb67ae85
  let h2 = 0x3c6ef372
  let h3 = 0xa54ff53a
  let h4 = 0x510e527f
  let h5 = 0x9b05688c
  let h6 = 0x1f83d9ab
  let h7 = 0x5be0cd19

  const w = new Array<number>(64).fill(0)
  for (let block = 0; block < bytes.length; block += 64) {
    for (let t = 0; t < 16; t += 1) {
      const o = block + t * 4
      w[t] = (((bytes[o] ?? 0) << 24) | ((bytes[o + 1] ?? 0) << 16) | ((bytes[o + 2] ?? 0) << 8) | (bytes[o + 3] ?? 0)) >>> 0
    }
    for (let t = 16; t < 64; t += 1) {
      const w15 = w[t - 15] ?? 0
      const w2 = w[t - 2] ?? 0
      const s0 = (rotr32(w15, 7) ^ rotr32(w15, 18) ^ (w15 >>> 3)) >>> 0
      const s1 = (rotr32(w2, 17) ^ rotr32(w2, 19) ^ (w2 >>> 10)) >>> 0
      w[t] = ((w[t - 16] ?? 0) + s0 + (w[t - 7] ?? 0) + s1) >>> 0
    }
    let a = h0
    let b = h1
    let c = h2
    let d = h3
    let e = h4
    let f = h5
    let g = h6
    let h = h7
    for (let t = 0; t < 64; t += 1) {
      const s1 = (rotr32(e, 6) ^ rotr32(e, 11) ^ rotr32(e, 25)) >>> 0
      const ch = ((e & f) ^ (~e & g)) >>> 0
      const t1 = (h + s1 + ch + (SHA256_K[t] ?? 0) + (w[t] ?? 0)) >>> 0
      const s0 = (rotr32(a, 2) ^ rotr32(a, 13) ^ rotr32(a, 22)) >>> 0
      const maj = ((a & b) ^ (a & c) ^ (b & c)) >>> 0
      const t2 = (s0 + maj) >>> 0
      h = g
      g = f
      f = e
      e = (d + t1) >>> 0
      d = c
      c = b
      b = a
      a = (t1 + t2) >>> 0
    }
    h0 = (h0 + a) >>> 0
    h1 = (h1 + b) >>> 0
    h2 = (h2 + c) >>> 0
    h3 = (h3 + d) >>> 0
    h4 = (h4 + e) >>> 0
    h5 = (h5 + f) >>> 0
    h6 = (h6 + g) >>> 0
    h7 = (h7 + h) >>> 0
  }

  return [h0, h1, h2, h3, h4, h5, h6, h7].map((word) => word.toString(16).padStart(8, '0')).join('')
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
