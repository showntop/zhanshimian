// 方案集的纯逻辑：五种状态 → 显式视图、单套渲染 → 六态视图、绑定报告 → 对比左图、
// 场合 Brief 答案 → 类型化请求。不碰 Taro、不碰网络；所有中文来自 @zsm/core。
//
// 这一页的原则与报告页同源：客户端不拼装服务端没给的东西。
// 失败的方案集如果服务端仍带着已发布文字，就按"部分就绪"展示——
// 既不把服务端还在提供的内容藏起来，也从不用旧缓存替它补一份。
import {
  PLAN_DETAIL_COPY,
  SCENE_BRIEF_COPY,
  type HomeBootstrap,
  type SceneBriefScene,
} from '@zsm/core'
import type {
  CreatePlanSetRequest,
  DisplayMedia,
  PlanSet,
  PlanStep,
  PlanVariant,
  Report,
} from '@zsm/core'
// 带 .ts 后缀：这个模块被 node --test 直接加载，Node 的 ESM 解析不做后缀补全。
import { createIdempotencyKey } from '../../app/keys.ts'

// 幂等键生成器已上移到 app/keys（建档重发、渲染重试、执行事件共用同一条规则）；
// 这里保留再导出，老引用不需要知道它搬了家。
export { createIdempotencyKey }

/** 文字是否已发布：只要有一步真的内容就算。空 steps 的方案等于"还没文字"。 */
function hasText(variant: Pick<PlanVariant, 'steps'>): boolean {
  return variant.steps.length > 0
}

/**
 * 方案集 → 页面视图。与契约的五个 state 一一对应，只有一处例外：
 * `failed` 但仍带已发布文字时按 `ready_partial` 展示——
 * 服务端错误响应里还在提供内容，客户端不替它宣布"全失败"。
 */
export type PlanSetViewKind =
  | 'planning'
  | 'rendering'
  | 'ready'
  | 'ready_partial'
  | 'failed'

export function planSetView(planSet: Pick<PlanSet, 'state' | 'variants'>): { kind: PlanSetViewKind } {
  switch (planSet.state) {
    case 'planning':
      return { kind: 'planning' }
    case 'rendering':
      return { kind: 'rendering' }
    case 'ready':
      return { kind: 'ready' }
    case 'ready_partial':
      return { kind: 'ready_partial' }
    case 'failed':
      return planSet.variants.some(hasText) ? { kind: 'ready_partial' } : { kind: 'failed' }
  }
}

/** 按 slot 排序。slot 是服务端定的席位（1/2/3），客户端不按列表顺序重新编号。 */
export function sortedVariants(planSet: Pick<PlanSet, 'variants'>): PlanVariant[] {
  return [...planSet.variants].sort((a, b) => a.slot - b.slot)
}

export type VariantRenderView =
  | { kind: 'queued' | 'generating' | 'checking'; textAvailable: boolean; retryable: false; media: null }
  | { kind: 'ready'; textAvailable: true; retryable: false; media: DisplayMedia }
  | { kind: 'failed'; textAvailable: boolean; retryable: boolean; media: null }
  | { kind: 'unavailable'; textAvailable: boolean; retryable: boolean; media: null }

/**
 * 单套方案 → 渲染视图。契约的六个 render state 原样成为视图的六种 kind，
 * 只有 `ready` 缺 media 是例外：契约保证 ready 必有 media，违约时不渲染空图，
 * 按"可重试的失败"呈现——这是唯一对用户诚实的出路。
 */
export function variantRenderView(
  variant: Pick<PlanVariant, 'steps' | 'render'>,
): VariantRenderView {
  const render: PlanVariant['render'] = variant.render
  const textAvailable = hasText(variant)
  switch (render.state) {
    case 'queued':
    case 'generating':
    case 'checking':
      return { kind: render.state, textAvailable, retryable: false, media: null }
    case 'ready':
      if (render.media) {
        return { kind: 'ready', textAvailable: true, retryable: false, media: render.media }
      }
      return { kind: 'failed', textAvailable, retryable: true, media: null }
    case 'failed':
      return { kind: 'failed', textAvailable, retryable: render.retryable, media: null }
    case 'unavailable':
      return { kind: 'unavailable', textAvailable, retryable: render.retryable, media: null }
  }
}

/**
 * 对比左图 = 方案集绑定的那一份报告的全身照。
 *
 * 绑定校验是硬性的：报告 id 对不上 `report_id` 就抛错，绝不"手头有哪份用哪份"——
 * 用当前报告的图去对比一份旧报告生成的方案，对比本身就是假的。
 * 绑定正确但报告缺全身照时返回 null，由页面渲染空态。
 */
export function boundBodyMedia(
  planSet: Pick<PlanSet, 'id' | 'report_id'>,
  report: Report | null | undefined,
): DisplayMedia | null {
  if (!report || report.id !== planSet.report_id) {
    throw new Error(
      `report binding mismatch: plan set ${planSet.id} is bound to ${planSet.report_id},` +
        ` got ${report?.id ?? 'null'}`,
    )
  }
  return report.source_media?.body?.media ?? null
}

export interface StepDetailLine {
  label: string
  value: string
}

/**
 * 步骤细则 → 标签值对。按 `category` switch 收窄联合，绝不 `as { hair_spec?: … }`：
 * 契约的三类 details 各不相同，收窄之后编译器替我们守住字段名。
 * 空值行不进结果——界面上不出现"部位："这种半句。
 */
export function stepDetailLines(step: PlanStep): StepDetailLine[] {
  switch (step.category) {
    case 'hair':
    case 'makeup': {
      const lines: StepDetailLine[] = []
      if (step.details.target) lines.push({ label: PLAN_DETAIL_COPY.detailTarget, value: step.details.target })
      if (step.details.intensity) lines.push({ label: PLAN_DETAIL_COPY.detailIntensity, value: step.details.intensity })
      return lines
    }
    case 'outfit': {
      const lines: StepDetailLine[] = []
      if (step.details.silhouette) lines.push({ label: PLAN_DETAIL_COPY.detailSilhouette, value: step.details.silhouette })
      if (step.details.palette.length > 0) {
        lines.push({ label: PLAN_DETAIL_COPY.detailPalette, value: step.details.palette.join('、') })
      }
      if (step.details.layers.length > 0) {
        lines.push({ label: PLAN_DETAIL_COPY.detailLayers, value: step.details.layers.join('、') })
      }
      if (step.details.avoid.length > 0) {
        lines.push({ label: PLAN_DETAIL_COPY.detailAvoid, value: step.details.avoid.join('、') })
      }
      if (step.details.formality) lines.push({ label: PLAN_DETAIL_COPY.detailFormality, value: step.details.formality })
      return lines
    }
  }
}

/** 按动作给步骤标记：保持比调整更省事，界面要让这个差别看得见。 */
export function stepActionText(step: Pick<PlanStep, 'action'>): string {
  return step.action === 'keep' ? PLAN_DETAIL_COPY.stepActionKeep : PLAN_DETAIL_COPY.stepActionAdjust
}

/** 场景 → 该场景的问题表；general 没有 Brief 页，返回 null。 */
export function sceneFields(scene: string) {
  if (!(scene in SCENE_BRIEF_COPY.scenes)) return null
  return SCENE_BRIEF_COPY.scenes[scene as SceneBriefScene].fields
}

// 公开 OperationRef：core 没有单独命名导出，从 HomeBootstrap 派生（与 pages/profile 同一条规则）。
type OperationRef = HomeBootstrap['active_operations'][number]

/** 受理在途状态（与 bootstrap active_operations 语义一致；failed 不在其中——失败允许重新发起）。 */
const PLAN_SET_IN_FLIGHT = new Set(['accepted', 'running', 'retrying'])

/**
 * bootstrap active_operations 里在途的方案集受理 id。
 * 方案页据此进轮询与「制作中」呈现；OperationRef 不带场景，
 * 能定位到场景的只有侧信道（planSetSceneKey，受理时写入），定位不了的走全局提示。
 */
export function inFlightPlanSetOperationIds(operations: readonly OperationRef[]): string[] {
  return operations
    .filter((operation) => operation.kind === 'plan_set' && PLAN_SET_IN_FLIGHT.has(operation.status))
    .map((operation) => operation.id)
}

/** 分析在途状态（与 bootstrap active_operations 语义一致；终态不算——失败允许重拍，完成该去报告）。 */
const ASSESSMENT_IN_FLIGHT = new Set(['accepted', 'running', 'retrying'])

/**
 * bootstrap active_operations 里在途的形象分析受理 id。
 * 方案页空态据此区分「正在分析」（查看进度）与「未建档」（去拍摄）。
 */
export function analyzingAssessmentOperationId(operations: readonly OperationRef[]): string {
  return (
    operations.find((operation) => operation.kind === 'assessment' && ASSESSMENT_IN_FLIGHT.has(operation.status))
      ?.id ?? ''
  )
}

/**
 * 已发布方案集的 brief → Brief 页预填答案（「重新设计」入口）。
 * 逐字段对照文案表校验，表外的 key/值一律丢弃——契约演进丢枚举时宁可少填不填错；
 * general / 未知场景没有问题表，返回空对象。
 */
export function sceneBriefPrefill(scene: string, brief: unknown): Record<string, string> {
  const fields = sceneFields(scene)
  if (!fields || !brief || typeof brief !== 'object') return {}
  const source = brief as Record<string, unknown>
  const answers: Record<string, string> = {}
  for (const field of fields) {
    const value = source[field.key]
    if (typeof value === 'string' && field.options.some((option) => option.value === value)) {
      answers[field.key] = value
    }
  }
  return answers
}

/**
 * 受理场景侧信道的缓存 key。受理中的方案集还没落库（GET 404 规划窗），
 * 方案 tab 从交接条拿不到场景；场合 Brief 受理成功时把场景写在这个 key 下，
 * 方案 tab 消费交接条时读取——规划失败的「重新生成」才知道回到哪个场合。
 */
export function planSetSceneKey(planSetId: string): string {
  return `plan-set-scene:${planSetId}`
}

/**
 * 重试标记的缓存 key：记录「这个场景固定幂等键的受理已到终态 failed」。
 * 24h 内重放固定键只会拿回同一份失败，下一次主动生成必须换新幂等键；
 * 新受理成功（或复用到已发布方案集）后由发起方清掉。
 */
export function planSetRetryMarkerKey(scene: string): string {
  return `plan-set-retry:${scene}`
}

/**
 * Brief 答案 → 幂等键指纹。服务端幂等按「键 + 请求体指纹」判重：
 * 固定键配改过的答案会吃 409（相同幂等键已被用于不同请求），
 * 所以键里带答案指纹——同答案重发同键（双击/重放安全），改答案即新键。
 * FNV-1a 32bit：稳定、短、无依赖；只做键内区分，不做安全用途。
 */
export function briefFingerprint(answers: Record<string, string>): string {
  const canonical = Object.keys(answers)
    .sort()
    .map((key) => `${key}=${answers[key]}`)
    .join('&')
  let hash = 0x811c9dc5
  for (let index = 0; index < canonical.length; index++) {
    hash ^= canonical.charCodeAt(index)
    hash = Math.imul(hash, 0x01000193)
  }
  return (hash >>> 0).toString(16)
}

/** 每个场景臂对应的 Brief 类型。 */
type BriefOf<S extends CreatePlanSetRequest['scene']> = Extract<CreatePlanSetRequest, { scene: S }>['brief']

/**
 * Brief 答案 → 类型化的 CreatePlanSetRequest，或 null。
 *
 * 答案逐条对照文案表校验（表里的 value 就是契约枚举），任何一题缺失或答了
 * 表外的值都返回 null——宁可不发请求，不发一个服务端要 400 的请求。
 * 类型断言只发生在"已通过表校验之后"这一处，不是绕过类型检查的捷径。
 */
export function sceneBriefRequest(
  reportId: string,
  scene: string,
  answers: Record<string, string>,
): CreatePlanSetRequest | null {
  const fields = sceneFields(scene)
  if (!fields) return null
  const brief: Record<string, string> = {}
  for (const field of fields) {
    const value = answers[field.key]
    if (!value || !field.options.some((option) => option.value === value)) return null
    brief[field.key] = value
  }
  switch (scene) {
    case 'interview':
      return { report_id: reportId, scene: 'interview', brief: brief as BriefOf<'interview'> }
    case 'wedding':
      return { report_id: reportId, scene: 'wedding', brief: brief as BriefOf<'wedding'> }
    case 'date':
      return { report_id: reportId, scene: 'date', brief: brief as BriefOf<'date'> }
    case 'daily':
      return { report_id: reportId, scene: 'daily', brief: brief as BriefOf<'daily'> }
    case 'gathering':
      return { report_id: reportId, scene: 'gathering', brief: brief as BriefOf<'gathering'> }
    default:
      return null
  }
}
