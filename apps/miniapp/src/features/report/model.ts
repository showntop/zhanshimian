// 形象报告的纯逻辑：证据定位、终态指针、生成方案的入参。
// 不碰 Taro、不碰网络、不拼文案——页面只消费这里的判定结果。
//
// 这个文件存在的理由只有一条：报告上的每一个字和每一个锚点都必须能指回
// 一张真实的照片。所以「找不到证据」在这里是一个正式结果（null / 空态），
// 而不是一个可以拿别的图凑合的意外。
import type {
  CreatePlanSetRequest,
  DisplayMedia,
  Operation,
  Report,
  ReportFinding,
} from '@zsm/core'
// 带 .ts 后缀的值导入：这个文件被 node --test 直接加载（测试跑的是真实现，不是替身），
// 而 Node 的 ESM 解析不做后缀补全。类型导入是纯类型、编译期就没了，按包内习惯省略后缀。
import { operationView } from '../../app/operations/operation-view.ts'
import type { PlanSetHandoff } from '../../app/plan-set-handoff'
import type { PlanSetStart } from '../../app/api/quality'

/** finding 只需要它的来源声明就能定位证据；报告只需要来源照片表。 */
type EvidenceSource = Pick<Report, 'source_media'>
type FindingSource = Pick<ReportFinding, 'source_photo'>

/**
 * finding → 它该画在哪张照片上的 DisplayMedia。
 *
 * 两道闸门，缺一不可：
 * 1. 角色：`source_photo.role` 决定去哪一格取图，绝不"这张没有就换全身图"——
 *    报告的锚点是在某一张照片的坐标系里量出来的，换一张图照着画就是伪造证据；
 * 2. item：格子里的 `item_id` 必须与 finding 声明的一致。同角色换了照片（重拍、
 *    换了档案）时不匹配，说明这条 finding 不是对着这张照片算出来的。
 *
 * 任一闸门不过都返回 null，由调用方渲染空态——报告宁可有缺口，不可有假的对应。
 */
export function reportPhotoForFinding(
  report: EvidenceSource | null | undefined,
  finding: FindingSource,
): DisplayMedia | null {
  const photo = report?.source_media?.[finding.source_photo.role]
  if (!photo) return null
  if (photo.item_id !== finding.source_photo.item_id) return null
  return photo.media ?? null
}

/**
 * 分析终态 → 报告页路由；还没结束、结果不是报告、没有结果 id 一律返回 null。
 *
 * 指针只认服务端给的 `result_type` / `result_id`：客户端不猜"分析完了应该就是报告"，
 * 也不把 operation id 当报告 id 用——两者都是 uuid，猜错会跳到一个不存在的报告。
 */
export function reportRouteAfterOperation(operation: Operation | null | undefined): string | null {
  const view = operationView(operation)
  if (view.kind !== 'succeeded') return null
  if (view.resultType !== 'report' || !view.resultId) return null
  return `/pages/report/index?id=${encodeURIComponent(view.resultId)}`
}

/** 报告里的三个来源角色，与服务端 `source_media` 的键一致。 */
export type ReportRole = ReportFinding['source_photo']['role']

/** 展示顺序：全身图先立住整体印象，再是正脸与侧脸。 */
export const REPORT_ROLE_ORDER: readonly ReportRole[] = ['body', 'face', 'side']

/** 某角色格子里可渲染的照片；没有就是 null——不跨角色找，也不回退内置图。 */
export function reportSourcePhoto(
  report: EvidenceSource | null | undefined,
  role: ReportRole,
): DisplayMedia | null {
  return report?.source_media?.[role]?.media ?? null
}

/** 有照片可显示的角色，按展示顺序。没有的角色不出现，不占位。 */
export function reportAvailableRoles(
  report: EvidenceSource | null | undefined,
): ReportRole[] {
  return REPORT_ROLE_ORDER.filter((role) => reportSourcePhoto(report, role) !== null)
}

/**
 * 报告页先打开哪张照片：服务端 position 顺序里第一条「证据能落地」的 finding 所指的角色。
 *
 * 先打开被第一条建议指着的照片，用户第一眼看到的就是报告在说的那张图。
 * 证据落不了地（item 漂移、媒体缺失）就跳过它看下一条；一条都没有时退回第一张可用的照片。
 */
export function defaultReportRole(report: Report | null | undefined): ReportRole | null {
  const available = reportAvailableRoles(report)
  if (available.length === 0) return null
  for (const finding of reportFindings(report)) {
    const role = finding.source_photo.role
    if (available.includes(role) && reportPhotoForFinding(report, finding) !== null) return role
  }
  return available[0] ?? null
}

/**
 * findings 按服务端 `position` 排序。
 *
 * 不在客户端按 category 或 label 重排：顺序是报告的一部分，服务端算好了，
 * 客户端再排一次就多出一份会漂移的真相。只做一次稳定排序，让渲染可预期。
 */
export function reportFindings(report: Report | null | undefined): ReportFinding[] {
  if (!report?.findings) return []
  return [...report.findings].sort((a, b) => a.position - b.position)
}

/**
 * 画在某张照片上的 findings：角色对上，且证据能在这一格落地。
 *
 * 锚点是在某一张照片的坐标系里量出来的：找不到那张照片时这条 finding 不画在任何图上，
 * 它仍然会出现在下方文字列表里（文字不依赖照片），但不会指着一个不存在的位置。
 */
export function reportFindingsOnRole(
  report: Report | null | undefined,
  role: ReportRole,
): ReportFinding[] {
  return reportFindings(report).filter(
    (finding) =>
      finding.source_photo.role === role && reportPhotoForFinding(report, finding) !== null,
  )
}

/**
 * 「查看方案」的请求体：一份绑定当前报告、选项全默认的日常方案集。
 *
 * 默认值是刻意的——报告页不是选场景的地方（那是场合 Brief 页的职责），
 * 这里只是把"看完报告"这个动作接上规划；用户想换场合，去方案页再换。
 */
export function reportPlanSetRequest(reportId: string): CreatePlanSetRequest {
  return {
    report_id: reportId,
    scene: 'general',
    brief: { focus: 'balanced', preparation: 'closet', impression: 'natural' },
  }
}

/**
 * 幂等键由报告 id 派生：同一份报告重复点「查看方案」必须复用同一把键，
 * 否则每次点击都会在服务端多受理一次规划。
 */
export function reportPlanSetIdempotencyKey(reportId: string): string {
  return `plan-set:${reportId}`
}

/**
 * createPlanSet 的两种成功 → 交给方案页的交接条。
 *
 * 复用已发布方案集（200）时没有 operation，就不带它：方案页看到 null 就知道
 * 没有任务在跑，不会去轮询一个空 id。
 *
 * 交接条而不是路由：方案页在 tabBar 里，switchTab 不接受 query。
 * 详见 app/plan-set-handoff 的说明。
 */
export function reportPlanSetHandoff(start: PlanSetStart): PlanSetHandoff {
  if (!start.accepted) return { planSetId: start.planSet.id, operationId: null }
  return { planSetId: start.data.id, operationId: start.operation.id }
}

/**
 * finding 锚框 → 标注层的落点（归一化坐标）。
 *
 * report.v2 起服务端的 anchor.x/y 就是分析模型直接给出的语义关键点
 * （领口正中/裤脚/发顶），原样采用——客户端不拼装服务端没给的东西。
 * report.v1 的报告 anchor 是区域矩形：取几何中心会落在「T 恤正中、
 * 大腿中段」这类没语义的位置，按框在照片里的上下半身取语义边——
 * 框在上半身取顶边中点（领口/发际/眉心），下半身取底边中点（裤脚/鞋），
 * 横跨大半身的整身框留在中心。
 */
export function findingAnchorPoint(
  finding: Pick<ReportFinding, 'anchor'>,
  schemaVersion = '',
): { anchorX: number; anchorY: number } {
  const { x, y, w, h } = finding.anchor
  if (schemaVersion === 'report.v2') return { anchorX: x, anchorY: y }
  const centerX = x + w / 2
  const centerY = y + h / 2
  if (h >= 0.5) return { anchorX: centerX, anchorY: centerY }
  return centerY < 0.5
    ? { anchorX: centerX, anchorY: y }
    : { anchorX: centerX, anchorY: y + h }
}
